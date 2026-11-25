package backup

import (
	"client/internal/errs"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// 还原出来的目录树要和源目录一致：节点和顺序、文件内容、文件的修改时间。
func TestUnpackRestoresTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "hello")
	writeFile(t, filepath.Join(root, "empty.txt"), "")
	writeFile(t, filepath.Join(root, "文档 目录", "测试 文件.txt"), "内容")
	writeFile(t, filepath.Join(root, "文档 目录", "data.bin"), string(dataBytes(150_000)))
	writeFile(t, filepath.Join(root, strings.Repeat("长", 80)+".txt"), "hello") // 文件名 244 字节
	if err := os.MkdirAll(filepath.Join(root, "empty dir"), 0o755); err != nil {
		t.Fatalf("Setup failed: create an empty directory: %v", err)
	}
	// 修改时间取一个过去的值：不恢复的话，还原出来的文件带着现在的时间
	modTime := time.Date(2020, 5, 4, 3, 2, 1, 1234500, time.UTC)
	if err := os.Chtimes(filepath.Join(root, "a.txt"), modTime, modTime); err != nil {
		t.Fatalf("Setup failed: set the modification time: %v", err)
	}

	archive := packTree(t, root, scanOK(t, root))

	restoresTo := func(t *testing.T, dst string) {
		t.Helper()
		if _, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{}); err != nil {
			t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
		}
		if problem := diffTree(root, dst); problem != nil {
			t.Errorf("Unpack(%q, %q): the restored tree differs from the source: %v", archive, dst, problem)
		}
	}
	t.Run("目标目录不存在", func(t *testing.T) {
		restoresTo(t, filepath.Join(t.TempDir(), "not", "created", "yet"))
	})
	t.Run("目标目录已存在且为空", func(t *testing.T) {
		restoresTo(t, t.TempDir())
	})

	t.Run("空归档", func(t *testing.T) {
		archive := packTree(t, t.TempDir(), nil)
		dst := filepath.Join(t.TempDir(), "restore")

		if _, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{}); err != nil {
			t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
		}
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("Unpack(%q, %q) did not create the target: %v", archive, dst, err)
		} else if got := entryNames(t, dst); len(got) != 0 {
			t.Errorf("Unpack(%q, %q) left %v in the target, want it empty", archive, dst, got)
		}
	})
}

// 目标目录里已经有东西就拒绝，里面的内容原样不动。
// 课件讲解包时要求目的目录为空，免得覆盖用户的数据。
func TestUnpackRejectsNonEmptyTarget(t *testing.T) {
	root := sampleTree(t)
	archive := packTree(t, root, scanOK(t, root))

	tests := []struct {
		name string
		fill func(t *testing.T, dst string)
	}{
		{"里面有一个文件", func(t *testing.T, dst string) {
			writeFile(t, filepath.Join(dst, "keep.txt"), "keep")
		}},
		{"里面只有一个空目录", func(t *testing.T, dst string) {
			if err := os.Mkdir(filepath.Join(dst, "sub"), 0o755); err != nil {
				t.Fatalf("Setup failed: create a subdirectory in the target: %v", err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := t.TempDir()
			tc.fill(t, dst)
			before := entryNames(t, dst)

			_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
			if problem := errOn(err, dst, errs.ErrTargetNotEmpty); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if got := entryNames(t, dst); !slices.Equal(got, before) {
				t.Errorf("Unpack(%q, %q): the target now holds %v, want it unchanged: %v", archive, dst, got, before)
			}
		})
	}
}

// 目标路径上已经是一个文件时，不能拿它当目录，更不能动它。错误原样返回。
func TestUnpackRejectsFileTarget(t *testing.T) {
	root := sampleTree(t)
	archive := packTree(t, root, scanOK(t, root))
	dst := filepath.Join(t.TempDir(), "file.txt")
	writeFile(t, dst, "keep")

	_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
	var e *errs.Error
	if err == nil || errors.As(err, &e) {
		t.Errorf("Unpack(%q, %q) error = %v, want the error from creating the target, returned as is",
			archive, dst, err)
	}
	if got, readErr := os.ReadFile(dst); readErr != nil || string(got) != "keep" {
		t.Errorf("Unpack(%q, %q): the target now holds %q (%v), want %q unchanged", archive, dst, got, readErr, "keep")
	}
}

// 归档打不开时错误原样返回。这时还没碰目标目录，不留下一个空目录。
func TestUnpackRejectsMissingArchive(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "nope.dbkp")
	dst := filepath.Join(t.TempDir(), "restore")

	_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Unpack(%q, %q) error = %v, want errors.Is(err, fs.ErrNotExist)", archive, dst, err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Unpack(%q, %q): the target exists after the failed call, want it left alone", archive, dst)
	}
}

// 归档中途截断时报 errs.Corrupted，Err 是 io.ErrUnexpectedEOF，
// 路径是写到一半的那个文件；截在公共头或记录头里时是归档本身。
func TestUnpackRejectsTruncatedArchive(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	writeFile(t, filepath.Join(src, "docs", "b.txt"), "xyz")
	raw := packBytes(t, src, []Entry{
		{Path: "a.txt", Kind: KindFile, Size: 5},
		{Path: "docs", Kind: KindDir},
		{Path: "docs/b.txt", Kind: KindFile, Size: 3},
	})
	archive := filepath.Join(t.TempDir(), "cut.dbkp")

	first := layoutHeaderSize + layoutRecordHeaderSize + len("a.txt") // 第一个文件的数据区
	docsEnd := first + 512 + layoutRecordHeaderSize + len("docs")
	second := docsEnd + layoutRecordHeaderSize + len("docs/b.txt")
	tests := []struct {
		name   string
		length int    // 留下多少字节
		path   string // 报错里应该带的路径
	}{
		{"公共头没写完", layoutHeaderSize - 1, archive},
		{"第一个记录头没写完", layoutHeaderSize + layoutRecordHeaderSize - 1, archive},
		{"第一个文件的路径没写完", layoutHeaderSize + layoutRecordHeaderSize + 1, archive},
		{"第一个文件的内容没写完", first + 2, "a.txt"},
		{"第二个记录头没写完", first + 512 + layoutRecordHeaderSize - 1, archive},
		{"第二个文件的内容没写完", second + 1, "docs/b.txt"},
		{"第二个文件的补零没写完", second + 3, "docs/b.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(archive, raw[:tc.length], 0o644); err != nil {
				t.Fatalf("Setup failed: write the truncated archive: %v", err)
			}

			dst := t.TempDir()
			_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
			if problem := errOn(err, tc.path, errs.ErrCorrupted); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("Unpack(%q, %q) error = %v, want errors.Is(err, io.ErrUnexpectedEOF)", archive, dst, err)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want nothing written from a truncated archive",
					archive, dst, got)
			}
		})
	}
}

// 归档里的字节坏了就不还原，报的错和 Verify 一样。目标目录里一个文件都不该有。
func TestUnpackRejectsCorruptedArchive(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "docs", "a.txt"), "xyz")
	raw := packBytes(t, src, []Entry{
		{Path: "docs", Kind: KindDir},
		{Path: "docs/a.txt", Kind: KindFile, Size: 3},
	})
	archive := filepath.Join(t.TempDir(), "x.dbkp")

	docsEnd := layoutHeaderSize + layoutRecordHeaderSize + len("docs")
	data := docsEnd + layoutRecordHeaderSize + len("docs/a.txt")
	flip := func(offset int) func([]byte) []byte {
		return func(raw []byte) []byte {
			raw[offset] ^= 0xFF
			return raw
		}
	}
	tests := []struct {
		name   string
		damage func(raw []byte) []byte
		path   string // 报错里应该带的路径
	}{
		{"公共头里的校验值", flip(layoutHeaderSize - 8), archive},
		{"目录记录里的路径", flip(layoutHeaderSize + layoutRecordHeaderSize), archive},
		{"文件记录里的校验值", flip(docsEnd + layoutCRCOffset), "docs/a.txt"},
		{"文件内容", flip(data), "docs/a.txt"},
		{"补零", flip(data + 3), archive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(archive, tc.damage(slices.Clone(raw)), 0o644); err != nil {
				t.Fatalf("Setup failed: write the damaged archive: %v", err)
			}

			dst := t.TempDir()
			_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
			if problem := errOn(err, tc.path, errs.ErrCorrupted); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want nothing written from a damaged archive",
					archive, dst, got)
			}
		})
	}
}

// 归档是外来数据，里面的路径可能是伪造的：不能让它写到目标目录外面去。
func TestUnpackRejectsEscapingPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"上一级", "../evil"},
		{"绝对路径", "/evil"},
		{"先进子目录再出来两级", "a/../../evil"},
		{"空路径", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := packTree(t, t.TempDir(), []Entry{{Path: tc.path, Kind: KindDir}})
			dst := t.TempDir()
			outside := filepath.Dir(dst)
			before := entryNames(t, outside)

			_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
			if problem := errOn(err, archive, errs.ErrCorrupted); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if got := entryNames(t, outside); !slices.Equal(got, before) {
				t.Errorf("Unpack(%q, %q) created %v outside the target, want %v", archive, dst, got, before)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want it empty", archive, dst, got)
			}
		})
	}
}

// 记录头的 Kind 只有文件、目录、符号链接三种，其余的是坏归档。
func TestUnpackRejectsUnknownKind(t *testing.T) {
	archive := packTree(t, t.TempDir(), []Entry{{Path: "odd", Kind: Kind(9)}})

	_, err := Unpack(context.Background(), archive, t.TempDir(), "", UnpackOptions{})
	if problem := errOn(err, archive, errs.ErrCorrupted); problem != nil {
		t.Errorf("Unpack(%q): %v", archive, problem)
	}
}

// 符号链接这一步不还原：记录跳过，不报错，也不影响别的文件。
func TestUnpackSkipsSymlinks(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	archive := packTree(t, src, []Entry{
		{Path: "link", Kind: KindSymlink, Size: 5},
		{Path: "a.txt", Kind: KindFile, Size: 5},
	})
	dst := t.TempDir()

	if _, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{}); err != nil {
		t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
	}
	want := []string{"a.txt"} // 符号链接不在里面
	if got := entryNames(t, dst); !slices.Equal(got, want) {
		t.Errorf("Unpack(%q, %q): the target holds %v, want %v", archive, dst, got, want)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "hello" {
		t.Errorf("Unpack(%q, %q): a.txt holds %q (%v), want %q", archive, dst, got, err, "hello")
	}
}

// 在目标目录里建不出文件或目录时报 errs.IO，带上那条记录。
// 做法是让两条记录的路径构成父子关系，父的那条是文件：子文件建不出来，子目录也建不出来。
// 记录的 Size 都是 0，Pack 打开源目录里的目录 a 时读不出东西，不会报错。
func TestUnpackReportsUnwritableEntry(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a", "b"), "")
	parent := Entry{Path: "a", Kind: KindFile}

	tests := []struct {
		name  string
		child Entry
	}{
		{"文件", Entry{Path: "a/b", Kind: KindFile}},
		{"目录", Entry{Path: "a/b", Kind: KindDir}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := packTree(t, src, []Entry{parent, tc.child})

			_, err := Unpack(context.Background(), archive, t.TempDir(), "", UnpackOptions{})
			if problem := errOn(err, tc.child.Path, errs.ErrIO); problem != nil {
				t.Errorf("Unpack(%q): %v", archive, problem)
			}
		})
	}
}

// ---------- 辅助函数 ----------

// 还原中途出错，dst 里不留这次写的文件。
func TestUnpackClearsPartialOutput(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a", "b"), "")
	// 两条记录是父子关系，父的是文件：写 a 成功，建 a/b 失败
	archive := packTree(t, src, []Entry{
		{Path: "a", Kind: KindFile},
		{Path: "a/b", Kind: KindFile},
	})

	dst := t.TempDir()
	_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
	if problem := errOn(err, "a/b", errs.ErrIO); problem != nil {
		t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
	}
	if got := entryNames(t, dst); len(got) != 0 {
		t.Errorf("Unpack(%q, %q) left %v in the target, want it empty", archive, dst, got)
	}
}
