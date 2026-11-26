package backup

import (
	"errors"
	"hash/crc64"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

var ecmaTable = crc64.MakeTable(crc64.ECMA)

// 结果只含 root 的子孙节点，深度优先、同级按名称字典序，空目录也必须出现
// （否则还原时会丢掉它）。路径分隔符的写法由 TestScanPathsArePortable 单独检查，
// 这里比较前先统一成 '/'。
func TestScanSampleTree(t *testing.T) {
	got := scanOK(t, sampleTree(t))

	want := []Entry{
		{Path: "a.txt", Kind: KindFile, Size: 5},
		{Path: "c.bin", Kind: KindFile, Size: 3},
		{Path: "docs", Kind: KindDir},
		{Path: "docs/b.md", Kind: KindFile},
		{Path: "docs/sub", Kind: KindDir},
	}
	if len(got) != len(want) {
		t.Fatalf("Scan() returned %d entries, want %d: %v", len(got), len(want), paths(got))
	}
	for i, w := range want {
		g := got[i]
		if filepath.ToSlash(g.Path) != w.Path || g.Kind != w.Kind {
			t.Errorf("entry %d = {Path: %q, Kind: %d}, want {Path: %q, Kind: %d}",
				i, g.Path, g.Kind, w.Path, w.Kind)
		}
		// Size 只对普通文件有意义，目录的大小因文件系统而异
		if w.Kind == KindFile && g.Size != w.Size {
			t.Errorf("entry %d (%s): Size = %d, want %d", i, w.Path, g.Size, w.Size)
		}
	}
}

// Path 会被写进归档文件，所以必须是规范的相对路径，并且只用 '/' 分隔，
// 这样在 Windows 上生成的归档拿到 Linux 上也能还原。
func TestScanPathsArePortable(t *testing.T) {
	for _, e := range scanOK(t, sampleTree(t)) {
		if e.Path != path.Clean(e.Path) || !filepath.IsLocal(e.Path) || strings.ContainsRune(e.Path, '\\') {
			t.Errorf("Path = %q, want a clean relative path separated by '/' only", e.Path)
		}
	}
}

// 同一棵树扫多次，结果的路径、类型、大小必须完全一致。
// 修改时间不参与比较：新建的目录被枚举过之后，NTFS 可能更新它的 mtime。
func TestScanIsDeterministic(t *testing.T) {
	root := sampleTree(t)
	first := scanOK(t, root)
	second := scanOK(t, root)

	same := slices.EqualFunc(first, second, func(a, b Entry) bool {
		return a.Path == b.Path && a.Kind == b.Kind && a.Size == b.Size
	})
	if !same {
		t.Errorf("two scans of the same tree differ\nfirst:  %v\nsecond: %v", paths(first), paths(second))
	}
}

func TestScanEmptyRoot(t *testing.T) {
	if got := scanOK(t, t.TempDir()); len(got) != 0 {
		t.Errorf("Scan() on an empty directory returned %v, want no entries", paths(got))
	}
}

// root 不可用时必须报错，不能返回空结果——否则界面会把"一个文件都没备到"显示成成功。
func TestScanRejectsBadRoot(t *testing.T) {
	fileRoot := filepath.Join(t.TempDir(), "file.txt")
	writeFile(t, fileRoot, "not a directory")

	tests := []struct {
		name   string
		root   string
		wantIs error // nil 表示只要求返回错误
	}{
		{name: "root does not exist", root: filepath.Join(t.TempDir(), "nope"), wantIs: fs.ErrNotExist},
		{name: "root is a file", root: fileRoot},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Scan(tc.root)
			if err == nil {
				t.Fatalf("Scan(%q) error = nil, want an error", tc.root)
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("Scan(%q) error = %v, want errors.Is(err, %v)", tc.root, err, tc.wantIs)
			}
		})
	}
}

// 符号链接要作为节点记下来，但不能跟着它走进去；
// 指向不存在目标的悬空链接同样不能让扫描出错。
func TestScanDoesNotFollowSymlinks(t *testing.T) {
	root := sampleTree(t)
	writeFile(t, filepath.Join(root, "real", "x.txt"), "x")
	symlink(t, filepath.Join(root, "real"), filepath.Join(root, "link"))
	symlink(t, filepath.Join(root, "missing"), filepath.Join(root, "dangling"))

	got := byPath(scanOK(t, root))
	for _, name := range []string{"link", "dangling"} {
		if e, ok := got[name]; !ok || e.Kind != KindSymlink {
			t.Errorf("%s = {found: %v, Kind: %d}, want it present with KindSymlink", name, ok, e.Kind)
		}
	}
	if _, ok := got["link/x.txt"]; ok {
		t.Error(`Scan() followed the symlink "link"`)
	}
}

// 源目录里出现无法备份的节点时，扫描不能整体失败，其余文件必须照常出现在结果里。
// Windows 上这种节点是目录联接（junction，AppData 下很常见），
// 类 Unix 上是 socket（Linux 的用户目录里 socket 和管道很常见）。
func TestScanKeepsGoingPastUnsupportedNode(t *testing.T) {
	root := sampleTree(t)
	makeUnsupportedNode(t, filepath.Join(root, "odd"))

	entries := scanOK(t, root)
	got := byPath(entries)
	if _, ok := got["a.txt"]; !ok {
		t.Errorf("Scan() result is missing a.txt: %v", paths(entries))
	}
	// 把这类节点当成普通文件去读，备份时会卡在 open 上或者直接报错
	if e, ok := got["odd"]; ok && e.Kind == KindFile {
		t.Error("Scan() reported the unsupported node as a regular file")
	}
}

// ModTime 取自文件元数据，还原时要靠它恢复时间戳。
func TestScanRecordsModTime(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	writeFile(t, file, "hello")
	// 纳秒部分取 100 的整数倍，这是 NTFS 的时间精度
	want := time.Date(2020, 5, 4, 3, 2, 1, 1234500, time.UTC)
	if err := os.Chtimes(file, want, want); err != nil {
		t.Fatal(err)
	}

	got := scanOK(t, root)
	if len(got) != 1 {
		t.Fatalf("Scan() returned %d entries, want 1", len(got))
	}
	if !got[0].ModTime.Equal(want) {
		t.Errorf("ModTime = %v, want %v", got[0].ModTime, want)
	}
}

// 文件内容远大于一个读缓冲区，用来确认函数是分块读取、分块累加的。
func TestFileCRC64(t *testing.T) {
	data := make([]byte, 300<<10)
	for i := range data {
		data[i] = byte(i*31 + i>>3)
	}
	path := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, path, string(data))

	crc, size, err := FileCRC64(path)
	if err != nil {
		t.Fatalf("FileCRC64() error = %v", err)
	}
	if want := crc64.Checksum(data, ecmaTable); crc != want {
		t.Errorf("crc = %#x, want %#x", crc, want)
	}
	if want := int64(len(data)); size != want {
		t.Errorf("size = %d, want %d", size, want)
	}
}

func TestFileCRC64EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty")
	writeFile(t, path, "")

	crc, size, err := FileCRC64(path)
	if crc != 0 || size != 0 || err != nil {
		t.Errorf("FileCRC64() = (%d, %d, %v), want (0, 0, <nil>)", crc, size, err)
	}
}

func TestFileCRC64MissingFile(t *testing.T) {
	crc, size, err := FileCRC64(filepath.Join(t.TempDir(), "nope"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("FileCRC64() error = %v, want errors.Is(err, fs.ErrNotExist)", err)
	}
	if crc != 0 || size != 0 {
		t.Errorf("FileCRC64() = (%d, %d, err), want (0, 0, err)", crc, size)
	}
}

// 读不了的目录和节点跳过并报告，其余的照常出现；只有 root 自身读不了才报错。
// 做法是给真实的文件系统套一层，让指定的路径报权限错误
func TestScanSkipsUnreadableNodes(t *testing.T) {
	root := sampleTree(t)
	writeFile(t, filepath.Join(root, "secret", "s.txt"), "s")

	tests := []struct {
		name   string
		broken unreadableFS
		skip   string // 应该出现在跳过清单里的路径
		fails  bool   // Scan 自身该不该报错
	}{
		{name: "目录读不了", broken: unreadableFS{dir: "secret"}, skip: "secret"},
		{name: "节点的信息读不了", broken: unreadableFS{entry: "a.txt"}, skip: "a.txt"},
		{name: "根目录读不了", broken: unreadableFS{dir: "."}, fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.broken.ReadDirFS = os.DirFS(root).(fs.ReadDirFS)
			entries, skips, err := scanFS(tc.broken, root)
			if tc.fails {
				if !errors.Is(err, fs.ErrPermission) {
					t.Fatalf("scanFS() error = %v, want errors.Is(err, fs.ErrPermission)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("scanFS() error = %v, want <nil>", err)
			}

			var want []string
			for _, e := range scanOK(t, root) {
				if e.Path != tc.skip && !strings.HasPrefix(e.Path, tc.skip+"/") {
					want = append(want, e.Path)
				}
			}
			if got := paths(entries); !slices.Equal(got, want) {
				t.Errorf("scanFS() entries = %v, want %v", got, want)
			}
			if len(skips) != 1 || skips[0].Path != tc.skip || !errors.Is(skips[0].Err, fs.ErrPermission) {
				t.Errorf("scanFS() skips = %+v, want only %s with a permission error", skips, tc.skip)
			}
		})
	}
}

// unreadableFS 让 dir 这个目录读不出来，让 entry 这个节点的信息读不出来
type unreadableFS struct {
	fs.ReadDirFS
	dir   string
	entry string
}

func (u unreadableFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == u.dir {
		return nil, &fs.PathError{Op: "readdirent", Path: name, Err: fs.ErrPermission}
	}
	entries, err := u.ReadDirFS.ReadDir(name)
	for i, e := range entries {
		if path.Join(name, e.Name()) == u.entry {
			entries[i] = unreadableEntry{e}
		}
	}
	return entries, err
}

// unreadableEntry 的 Info 报权限错误，类型信息照旧
type unreadableEntry struct{ fs.DirEntry }

func (u unreadableEntry) Info() (fs.FileInfo, error) {
	return nil, &fs.PathError{Op: "lstat", Path: u.Name(), Err: fs.ErrPermission}
}

// ---------- 辅助函数 ----------

// scanOK 执行 Scan，出错或者有跳过的节点时让测试立即失败：用例里搭的树都该是能读的
func scanOK(t *testing.T, root string) []Entry {
	t.Helper()
	entries, skips, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan(%q) error = %v", root, err)
	}
	if len(skips) > 0 {
		t.Fatalf("Scan(%q) skipped %v, want nothing skipped", root, skips)
	}
	return entries
}

// writeFile 在 path 处写一个内容为 content 的文件，父目录不存在时自动创建。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// sampleTree 建一棵固定的示例树并返回根目录：
//
//	a.txt       内容 "hello"
//	c.bin       3 字节
//	docs/
//	  b.md      空文件
//	  sub/      空目录
func sampleTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "hello")
	writeFile(t, filepath.Join(root, "c.bin"), "\x01\x02\x03")
	writeFile(t, filepath.Join(root, "docs", "b.md"), "")
	if err := os.MkdirAll(filepath.Join(root, "docs", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// symlink 创建指向 target 的符号链接；当前环境没有创建权限时跳过测试。
func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
}

// makeUnsupportedNode 在 path 处创建一个既不是普通文件、目录，也不是符号链接的节点。
// Windows 上用目录联接（mklink 是 cmd 的内建命令，/J 不需要管理员权限），
// 其余平台用 unix socket。当前环境创建不了时跳过测试。
func makeUnsupportedNode(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		target := t.TempDir()
		if _, err := exec.Command("cmd", "/c", "mklink", "/J", path, target).CombinedOutput(); err != nil {
			t.Skipf("cannot create junction: %v", err)
		}
		// 清理按注册的逆序执行：这条先于两个 t.TempDir 的清理，只删联接本身
		t.Cleanup(func() { _ = os.Remove(path) })
		return
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("cannot create unix socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
}

// byPath 把扫描结果整理成 "路径 → 条目" 的映射；键统一成 '/' 分隔，便于按名字查找。
func byPath(entries []Entry) map[string]Entry {
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		m[filepath.ToSlash(e.Path)] = e
	}
	return m
}

// paths 返回各条目的路径，用在失败信息里。
func paths(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Path
	}
	return out
}
