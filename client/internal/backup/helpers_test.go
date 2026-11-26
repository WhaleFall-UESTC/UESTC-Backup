package backup

import (
	"bytes"
	"client/internal/errs"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// 布局和各函数的约定见 archive.go。这里的布局常数故意再写一遍，
// 不引用实现里的：实现改了宽度，测试不会跟着一起错。
const (
	layoutMagicSize        = 4
	layoutDataOffset       = 8 // DataOffset 字段的偏移
	layoutHeaderSize       = 24
	layoutRecordHeaderSize = 40
	layoutCRCOffset        = 32 // 记录头里 CRC64 的偏移
	layoutVersionOffset    = 4
)

// packTree 把 root 下的 entries 打包到临时文件，返回归档路径。
func packTree(t *testing.T, root string, entries []Entry) string {
	t.Helper()
	return packTreeAlgo(t, root, entries, AlgorithmNone)
}

// packTreeAlgo 同上，用 algo 指定的算法。
func packTreeAlgo(t *testing.T, root string, entries []Entry, algo Algorithm) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "backup.dbkp")
	if _, err := Pack(context.Background(), archive, root, entries, PackOptions{Algorithm: algo}); err != nil {
		t.Fatalf("Setup failed: Pack() error = %v", err)
	}
	return archive
}

// packBytes 打包后把归档整个读回来。
func packBytes(t *testing.T, root string, entries []Entry) []byte {
	t.Helper()
	return packBytesAlgo(t, root, entries, AlgorithmNone)
}

// packBytesAlgo 同上，用 algo 指定的算法。
func packBytesAlgo(t *testing.T, root string, entries []Entry, algo Algorithm) []byte {
	t.Helper()
	raw, err := os.ReadFile(packTreeAlgo(t, root, entries, algo))
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}
	return raw
}

// readIndexOK 执行 ReadIndex，出错时让测试立即失败。
func readIndexOK(t *testing.T, archive string) []Record {
	t.Helper()
	records, err := ReadIndex(archive, "")
	if err != nil {
		t.Fatalf("Setup failed: ReadIndex(%q) error = %v", archive, err)
	}
	return records
}

// rewrittenArchive 把一份空归档按 change 改过后写到临时文件，返回路径。
func rewrittenArchive(t *testing.T, change func(raw []byte)) string {
	t.Helper()
	raw := packBytes(t, t.TempDir(), nil)
	change(raw)

	archive := filepath.Join(t.TempDir(), "patched.dbkp")
	if err := os.WriteFile(archive, raw, 0o644); err != nil {
		t.Fatalf("Setup failed: write the archive: %v", err)
	}
	return archive
}

// errOn 检查引擎对某个文件报的错：errors.Is 能认出 want，包在 *errs.Error 里，Path 就是这个文件，
// 并且不匹配 io.EOF（调用方把它当成正常结束）。有问题时返回一句说明。
func errOn(err error, path string, want error) error {
	var e *errs.Error
	switch {
	case !errors.Is(err, want):
		return fmt.Errorf("error = %v, want errors.Is(err, %v)", err, want)
	case !errors.As(err, &e) || e.Path != path:
		return fmt.Errorf("error = %v, want a *errs.Error whose Path is %q", err, path)
	case errors.Is(err, io.EOF):
		return fmt.Errorf("error = %v: it must not match io.EOF", err)
	}
	return nil
}

// diffRecords 逐项比较读回的记录和扫描结果，返回第一处不一致的说明。CRC64 由 TestPackRecordsFileCRC64 管。
func diffRecords(got []Record, want []Entry) error {
	if len(got) != len(want) {
		return fmt.Errorf("returned %d records, want %d\n got:  %+v\n want: %+v", len(got), len(want), got, want)
	}
	for i, w := range want {
		g := got[i]
		if g.Path != w.Path || g.Kind != w.Kind || g.Size != w.Size || g.Mode != w.Mode ||
			!g.ModTime.Equal(w.ModTime) {
			return fmt.Errorf("record %d = {Path: %q, Kind: %d, Size: %d, Mode: %O, ModTime: %v}, "+
				"want {Path: %q, Kind: %d, Size: %d, Mode: %O, ModTime: %v}",
				i, g.Path, g.Kind, g.Size, g.Mode, g.ModTime, w.Path, w.Kind, w.Size, w.Mode, w.ModTime)
		}
	}
	return nil
}

// diffTree 把还原出来的 dst 和源目录 src 逐项比较：节点和顺序要一致，
// 文件的内容要逐字节相同，文件的修改时间要一致。
// 目录的修改时间不比：往目录里建文件会更新它，这一步不恢复。返回第一处不一致的说明。
func diffTree(src, dst string) error {
	want, err := Scan(src)
	if err != nil {
		return fmt.Errorf("Scan(%q): %v", src, err)
	}
	got, err := Scan(dst)
	if err != nil {
		return fmt.Errorf("Scan(%q): %v", dst, err)
	}
	if len(got) != len(want) {
		return fmt.Errorf("restored %d nodes, want %d\n got:  %v\nwant: %v", len(got), len(want), paths(got), paths(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Path != w.Path || g.Kind != w.Kind {
			return fmt.Errorf("node %d = {Path: %q, Kind: %d}, want {Path: %q, Kind: %d}",
				i, g.Path, g.Kind, w.Path, w.Kind)
		}
		if w.Kind != KindFile {
			continue
		}
		if !g.ModTime.Equal(w.ModTime) {
			return fmt.Errorf("%s: ModTime = %v, want %v", w.Path, g.ModTime, w.ModTime)
		}
		original, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(w.Path)))
		if err != nil {
			return fmt.Errorf("read the source %q: %v", w.Path, err)
		}
		restored, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(w.Path)))
		if err != nil {
			return fmt.Errorf("read the restored %q: %v", w.Path, err)
		}
		if !bytes.Equal(restored, original) {
			return fmt.Errorf("%s: content differs (%d bytes restored, %d in the source)", w.Path, len(restored), len(original))
		}
	}
	return nil
}

// entryNames 返回 dir 下一层的名字，按名字排序。
func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Setup failed: read %q: %v", dir, err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// failingRecords 造出两条还原时必然失败的记录：fail 是文件，fail/child 在它下面建不出来
func failingRecords(t *testing.T, src string) []Entry {
	t.Helper()
	writeFile(t, filepath.Join(src, "fail", "child"), "")
	return []Entry{
		{Path: "fail", Kind: KindFile, Size: 0},
		{Path: "fail/child", Kind: KindFile, Size: 0},
	}
}

// junction 创建指向 target 的目录联接；Windows 之外或者创建不了时跳过测试
func junction(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("junction only exists on Windows")
	}
	if _, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot create junction: %v", err)
	}
	// 清理按注册的逆序执行：这条先于目录的清理，只删联接本身
	t.Cleanup(func() { _ = os.Remove(link) })
}

// layoutPaddedSize 是 n 个字节的数据区在归档里占的字节数：补零到 512 的整数倍。
func layoutPaddedSize(n int64) int64 {
	return (n + 511) / 512 * 512
}

// dataBytes 生成 n 个确定的字节。
func dataBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 1 + i>>3)
	}
	return b
}
