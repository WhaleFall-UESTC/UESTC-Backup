package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"client/internal/errs"
)

// 扫描之后源文件还在变：删掉的、截短的。这两种都算“读不了”，要跳过它们继续打包，
// 原因进错误清单；其余文件照常进归档，归档本身照样校验通过。
func TestPackSkipsUnreadableFiles(t *testing.T) {
	tests := []struct {
		name   string
		damage func(t *testing.T, src string)
		want   error // errs 里对应的类别
		wantIs error // 底层错误，调用方靠它决定怎么补偿
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
				writeFile(t, filepath.Join(src, "a.txt"), "a") // 记录里的 Size 还是 3
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
			skips, err := Pack(context.Background(), archive, src, entries, nil)
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

// 取消之后 Pack 返回 context.Canceled，目标目录里连临时文件都不留——
// 半截归档留在目标位置上，调用方很难分辨它是不是完整的。
func TestPackContextCancel(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")
	writeFile(t, filepath.Join(src, "b.txt"), "bbb")
	entries := scanOK(t, src)

	t.Run("还没开始就取消", func(t *testing.T) {
		dir := t.TempDir()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := Pack(ctx, filepath.Join(dir, "backup.dbkp"), src, entries, nil)
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
			func(path string, _ int64) { cancel() })
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Pack() error = %v, want context.Canceled", err)
		}
		if problem := checkNothingLeft(dir); problem != nil {
			t.Errorf("Pack() %v", problem)
		}
	})
}

// progress 每写完一个文件报告一次：已处理字节数只增不减，收尾时等于全部文件大小之和。
// 空文件也要报告，只是字节数不前进——否则调用方收不到它的进度。
func TestPackProgressReportsBytes(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")
	writeFile(t, filepath.Join(src, "empty.txt"), "")
	writeFile(t, filepath.Join(src, "sub", "b.bin"), string(dataBytes(150_000))) // 比读缓冲区大
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
		func(path string, bytes int64) { got = append(got, report{path, bytes}) })
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

// 归档不能放进源目录：临时文件会在扫描之后出现，被当成源文件打进它自己。
// 刚好是源目录邻居的路径不算在里面，必须照常打包。
func TestPackRejectsArchiveInsideSource(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")

	t.Run("源目录里面", func(t *testing.T) {
		dst := filepath.Join(src, "sub", "backup.dbkp")
		_, err := Pack(context.Background(), dst, src, scanOK(t, src), nil)
		if problem := errOn(err, dst, errs.ErrInvalidArgument); problem != nil {
			t.Errorf("Pack() %v", problem)
		}
		// 拒绝发生在建临时文件之前，源目录里不该多出 sub 来
		if _, statErr := os.Stat(filepath.Join(src, "sub")); !errors.Is(statErr, fs.ErrNotExist) {
			t.Errorf("stat %q = %v, want ErrNotExist", filepath.Join(src, "sub"), statErr)
		}
	})

	t.Run("路径就是源目录本身", func(t *testing.T) {
		_, err := Pack(context.Background(), src, src, scanOK(t, src), nil)
		if problem := errOn(err, src, errs.ErrInvalidArgument); problem != nil {
			t.Errorf("Pack() %v", problem)
		}
	})

	t.Run("源目录的同名邻居", func(t *testing.T) {
		dst := src + "x" // 前缀相同但不在源目录里面的路径
		if _, err := Pack(context.Background(), dst, src, scanOK(t, src), nil); err != nil {
			t.Errorf("Pack(%q) error = %v, want <nil>", dst, err)
		}
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("stat %q = %v, want the archive", dst, err)
		}
	})
}

// 成功打包后 dst 旁边不留临时文件；再往同一个 dst 打包时直接覆盖上次的归档。
func TestPackTemporaryFileIsRenamed(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "backup.dbkp")
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")

	if _, err := Pack(context.Background(), dst, src, scanOK(t, src), nil); err != nil {
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
	if _, err := Pack(context.Background(), dst, src, scanOK(t, src), nil); err != nil {
		t.Errorf("Pack() over an existing archive: error = %v, want <nil>", err)
	}
	if err := Verify(dst); err != nil {
		t.Errorf("Verify(%q) = %v, want <nil>", dst, err)
	}
}

// 还原到一半出错时，这一次写出来的文件要清掉，只剩下一个空的 dst。
// 否则用户面对的是一个既不像成功也不像失败的半截目录。
func TestUnpackClearsPartialOutput(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a", "b"), "")
	// 两条记录是父子关系，父的那条是文件：写 a 成功，接着建 a/b 就建不出来
	archive := packTree(t, src, []Entry{
		{Path: "a", Kind: KindFile},
		{Path: "a/b", Kind: KindFile},
	})

	dst := t.TempDir()
	err := Unpack(archive, dst)
	if problem := errOn(err, "a/b", errs.ErrIO); problem != nil {
		t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
	}
	if got := entryNames(t, dst); len(got) != 0 {
		t.Errorf("Unpack(%q, %q) left %v in the target, want it empty", archive, dst, got)
	}
}

// ---------- 辅助函数 ----------

// checkArchiveOnlyHas 检查归档里只剩 want 这一个文件，并且整档校验通过：
// 跳过发生在记录头已经写盘之后，退回去的字节数对不对，只有校验值能查出来。
func checkArchiveOnlyHas(archive, want string) error {
	records, err := ReadIndex(archive)
	if err != nil {
		return fmt.Errorf("ReadIndex(%q) error = %v", archive, err)
	}
	if len(records) != 1 || records[0].Path != want {
		return fmt.Errorf("ReadIndex() = %v, want only %q", records, want)
	}
	if err := Verify(archive); err != nil {
		return fmt.Errorf("Verify(%q) = %v, want <nil>", archive, err)
	}
	return nil
}

// checkNothingLeft 检查 dir 里没有任何文件留下来。
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
