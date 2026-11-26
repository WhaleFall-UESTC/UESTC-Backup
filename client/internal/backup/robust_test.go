package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"client/internal/errs"
)

// 扫描后源文件被删或被截短，Pack 跳过它，其余照常打包。
func TestPackSkipsUnreadableFiles(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, src string)
		want   error
		wantIs error
	}{
		{
			name: "文件删掉了",
			damage: func(t *testing.T, src string) {
				if err := os.Remove(filepath.Join(src, "a.txt")); err != nil {
					t.Fatalf("Setup failed: remove a.txt: %v", err)
				}
			},
			want:   errs.ErrIO,
			wantIs: fs.ErrNotExist,
		},
		{
			name: "文件被截短了",
			damage: func(t *testing.T, src string) {
				writeFile(t, filepath.Join(src, "a.txt"), "a")
			},
			want:   errs.ErrSourceChanged,
			wantIs: io.ErrUnexpectedEOF,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			writeFile(t, filepath.Join(src, "a.txt"), "aaa")
			writeFile(t, filepath.Join(src, "b.txt"), "bbb")
			entries := scanOK(t, src)
			tc.damage(t, src)

			archive := filepath.Join(t.TempDir(), "backup.dbkp")
			skips, err := Pack(context.Background(), archive, src, entries, PackOptions{})
			if err != nil {
				t.Fatalf("Pack() error = %v, want <nil>", err)
			}
			if len(skips) != 1 {
				t.Fatalf("Pack() skips = %v, want only a.txt", skips)
			}
			if skips[0].Path != "a.txt" {
				t.Errorf("skips[0].Path = %q, want %q", skips[0].Path, "a.txt")
			}
			if problem := errOn(skips[0].Err, "a.txt", tc.want); problem != nil {
				t.Errorf("Pack() skips[0].Err %v", problem)
			}
			if !errors.Is(skips[0].Err, tc.wantIs) {
				t.Errorf("skips[0].Err = %v, want errors.Is(err, %v)", skips[0].Err, tc.wantIs)
			}
			if problem := checkArchiveOnlyHas(archive, "b.txt"); problem != nil {
				t.Errorf("Pack() %v", problem)
			}
		})
	}
}

// 取消后不留任何文件，连临时文件也不留。
func TestPackContextCancel(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")
	writeFile(t, filepath.Join(src, "b.txt"), "bbb")
	entries := scanOK(t, src)

	t.Run("还没开始就取消", func(t *testing.T) {
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := Pack(ctx, filepath.Join(dir, "backup.dbkp"), src, entries, PackOptions{})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Pack() error = %v, want context.Canceled", err)
		}
		if problem := checkNothingLeft(dir); problem != nil {
			t.Errorf("Pack() %v", problem)
		}
	})

	t.Run("写完第一个文件后取消", func(t *testing.T) {
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())

		_, err := Pack(ctx, filepath.Join(dir, "backup.dbkp"), src, entries,
			PackOptions{Progress: func(Progress) { cancel() }})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Pack() error = %v, want context.Canceled", err)
		}
		if problem := checkNothingLeft(dir); problem != nil {
			t.Errorf("Pack() %v", problem)
		}
	})
}

// 已处理字节数只增不减，收尾等于所有文件大小之和。
func TestPackProgressReportsBytes(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")
	writeFile(t, filepath.Join(src, "empty.txt"), "")
	writeFile(t, filepath.Join(src, "sub", "b.bin"), string(dataBytes(150_000)))
	entries := scanOK(t, src)

	var want []string
	var total int64
	for _, e := range entries {
		if e.Kind == KindFile {
			want = append(want, e.Path)
			total += e.Size
		}
	}

	type report struct {
		path  string
		bytes int64
	}
	var got []report
	_, err := Pack(context.Background(), filepath.Join(t.TempDir(), "x.dbkp"), src, entries,
		PackOptions{Progress: func(p Progress) { got = append(got, report{p.Path, p.DoneBytes}) }})
	if err != nil {
		t.Fatalf("Pack() error = %v, want <nil>", err)
	}

	if len(got) != len(want) {
		t.Fatalf("Pack() reported %d files, want %d", len(got), len(want))
	}
	var last int64
	for i, s := range got {
		if s.path != want[i] {
			t.Errorf("report %d = %q, want %q", i, s.path, want[i])
		}
		if s.bytes < last {
			t.Errorf("report %d: bytes = %d, went backwards from %d", i, s.bytes, last)
		}
		last = s.bytes
	}
	if last != total {
		t.Errorf("final bytes = %d, want %d", last, total)
	}
}

// 归档不能放进源目录，前缀相同的邻居不算。
func TestPackRejectsArchiveInsideSource(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")

	t.Run("源目录里面", func(t *testing.T) {
		dst := filepath.Join(src, "sub", "backup.dbkp")
		_, err := Pack(context.Background(), dst, src, scanOK(t, src), PackOptions{})
		if problem := errOn(err, dst, errs.ErrInvalidArgument); problem != nil {
			t.Errorf("Pack() %v", problem)
		}
		// 拒绝发生在建临时文件之前
		if _, statErr := os.Stat(filepath.Join(src, "sub")); !errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("stat %q = %v, want ErrNotExist", filepath.Join(src, "sub"), statErr)
		}
	})

	t.Run("路径就是源目录本身", func(t *testing.T) {
		_, err := Pack(context.Background(), src, src, scanOK(t, src), PackOptions{})
		if problem := errOn(err, src, errs.ErrInvalidArgument); problem != nil {
			t.Errorf("Pack() %v", problem)
		}
	})

	t.Run("源目录的同名邻居", func(t *testing.T) {
		dst := src + "x"
		if _, err := Pack(context.Background(), dst, src, scanOK(t, src), PackOptions{}); err != nil {
			t.Errorf("Pack(%q) error = %v, want <nil>", dst, err)
		}
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("stat %q = %v, want the archive", dst, err)
		}
	})
}

// 调用方给的 Entry 坏了要报 InvalidArgument，且不留任何归档文件。
// 归档路径只用 '/' 分隔，反斜杠在 Windows 上是分隔符，混进来还原时会跑错地方。
func TestPackRejectsInvalidEntries(t *testing.T) {
	tests := []struct {
		name string
		bad  Entry
	}{
		{"路径跳出源目录", Entry{Path: "../x", Kind: KindFile, Size: 1}},
		{"路径是绝对的", Entry{Path: "/x", Kind: KindFile, Size: 1}},
		{"路径带反斜杠", Entry{Path: `a\b`, Kind: KindFile, Size: 1}},
		{"路径超过 1 MiB", Entry{Path: strings.Repeat("a", maxPathBytes+1), Kind: KindFile, Size: 1}},
		{"Kind 是零值", Entry{Path: "x", Kind: 0}},
		{"Kind 不认识", Entry{Path: "x", Kind: Kind(9)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := t.TempDir()
			writeFile(t, filepath.Join(src, "ok.txt"), "aaa")
			entries := append(scanOK(t, src), tc.bad)

			dir := t.TempDir()
			dst := filepath.Join(dir, "backup.dbkp")
			_, err := Pack(context.Background(), dst, src, entries, PackOptions{})
			if problem := errOn(err, tc.bad.Path, errs.ErrInvalidArgument); problem != nil {
				t.Errorf("Pack() %v", problem)
			}
			if problem := checkNothingLeft(dir); problem != nil {
				t.Errorf("Pack() %v", problem)
			}
		})
	}
}

// 成功后不留临时文件；再打一次覆盖已有归档。
func TestPackTemporaryFileIsRenamed(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "backup.dbkp")
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")

	if _, err := Pack(context.Background(), dst, src, scanOK(t, src), PackOptions{}); err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Setup failed: read %q: %v", dir, err)
	}
	if len(left) != 1 || left[0].Name() != "backup.dbkp" {
		t.Errorf("the target directory should hold only backup.dbkp, got %d entries", len(left))
	}

	writeFile(t, filepath.Join(src, "b.txt"), "bbb")
	if _, err := Pack(context.Background(), dst, src, scanOK(t, src), PackOptions{}); err != nil {
		t.Errorf("Pack() over an existing archive: error = %v, want <nil>", err)
	}
	if err := Verify(context.Background(), dst, "", VerifyOptions{}); err != nil {
		t.Errorf("Verify(%q) = %v, want <nil>", dst, err)
	}
}

// ---------- 辅助函数 ----------

// checkArchiveOnlyHas 检查归档只剩 want，且整档校验通过。
func checkArchiveOnlyHas(archive, want string) error {
	records, err := ReadIndex(archive, "")
	if err != nil {
		return fmt.Errorf("ReadIndex(%q) error = %v", archive, err)
	}
	if len(records) != 1 || records[0].Path != want {
		return fmt.Errorf("ReadIndex() = %v, want only %q", records, want)
	}
	if err := Verify(context.Background(), archive, "", VerifyOptions{}); err != nil {
		return fmt.Errorf("Verify(%q) = %v, want <nil>", archive, err)
	}
	return nil
}

// checkNothingLeft 检查 dir 是空的。
func checkNothingLeft(dir string) error {
	left, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %q: %v", dir, err)
	}
	if len(left) != 0 {
		return fmt.Errorf("ReadDir(%q) left %d entries, want none", dir, len(left))
	}
	return nil
}
