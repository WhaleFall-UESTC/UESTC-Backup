package backup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"client/internal/errs"
)

// 目标目录里已有文件时，四种策略各按约定处理
func TestUnpackConflictPolicies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "new")
	writeFile(t, filepath.Join(root, "b.txt"), "b")
	archive := packTree(t, root, scanOK(t, root))

	t.Run("fail", func(t *testing.T) {
		dst := t.TempDir()
		writeFile(t, filepath.Join(dst, "a.txt"), "old")
		_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
		if !errors.Is(err, errs.ErrTargetNotEmpty) {
			t.Errorf("Unpack() error = %v, want errs.TargetNotEmpty", err)
		}
		if got := readFile(t, filepath.Join(dst, "a.txt")); got != "old" {
			t.Errorf("a.txt = %q, want it untouched", got)
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		dst := t.TempDir()
		writeFile(t, filepath.Join(dst, "a.txt"), "old")
		writeFile(t, filepath.Join(dst, "keep.txt"), "keep")
		result, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictOverwrite})
		if err != nil {
			t.Fatalf("Unpack() error = %v", err)
		}
		if got := readFile(t, filepath.Join(dst, "a.txt")); got != "new" {
			t.Errorf("a.txt = %q, want the archived content", got)
		}
		if got := readFile(t, filepath.Join(dst, "keep.txt")); got != "keep" {
			t.Errorf("keep.txt = %q, want it left alone", got)
		}
		if result.Restored != 2 {
			t.Errorf("Restored = %d, want 2", result.Restored)
		}
	})

	t.Run("skip", func(t *testing.T) {
		dst := t.TempDir()
		writeFile(t, filepath.Join(dst, "a.txt"), "old")
		result, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictSkip})
		if err != nil {
			t.Fatalf("Unpack() error = %v", err)
		}
		if got := readFile(t, filepath.Join(dst, "a.txt")); got != "old" {
			t.Errorf("a.txt = %q, want it untouched", got)
		}
		if got := readFile(t, filepath.Join(dst, "b.txt")); got != "b" {
			t.Errorf("b.txt = %q, want it restored", got)
		}
		if result.Skipped != 1 || result.Restored != 1 {
			t.Errorf("result = %+v, want 1 restored and 1 skipped", result)
		}
	})

	t.Run("rename", func(t *testing.T) {
		dst := t.TempDir()
		writeFile(t, filepath.Join(dst, "a.txt"), "old")
		result, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictRename})
		if err != nil {
			t.Fatalf("Unpack() error = %v", err)
		}
		if got := readFile(t, filepath.Join(dst, "a.txt")); got != "old" {
			t.Errorf("a.txt = %q, want it untouched", got)
		}
		if got := readFile(t, filepath.Join(dst, "a.1.txt")); got != "new" {
			t.Errorf("a.1.txt = %q, want the archived content", got)
		}
		if result.Renamed != 1 || result.Restored != 2 {
			t.Errorf("result = %+v, want 2 restored and 1 renamed", result)
		}

		// 再来一次，撞上的名字往后顺延
		if _, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictRename}); err != nil {
			t.Fatalf("second Unpack() error = %v", err)
		}
		if got := readFile(t, filepath.Join(dst, "a.2.txt")); got != "new" {
			t.Errorf("a.2.txt = %q, want the archived content", got)
		}
		if got := readFile(t, filepath.Join(dst, "b.1.txt")); got != "b" {
			t.Errorf("b.1.txt = %q, want the archived content", got)
		}
	})
}

// 路径上已有的是文件、归档里却是目录，报错并指出路径
func TestUnpackTypeConflict(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sub", "a.txt"), "a")
	archive := packTree(t, root, scanOK(t, root))

	dst := t.TempDir()
	writeFile(t, filepath.Join(dst, "sub"), "not a directory")
	_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictOverwrite})
	if problem := errOn(err, "sub", errs.ErrIO); problem != nil {
		t.Errorf("Unpack() %v", problem)
	}
}

// 中途出错时只删这次写出来的文件，目标里原有的东西不动
func TestUnpackUndoKeepsExistingFiles(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "good.txt"), "good")
	entries := []Entry{
		{Path: "good.txt", Kind: KindFile, Size: 4},
		{Path: "../evil", Kind: KindDir},
	}
	archive := filepath.Join(t.TempDir(), "x.dbkp")
	if _, err := Pack(context.Background(), archive, src, entries, PackOptions{}); err != nil {
		t.Fatalf("Setup failed: Pack() error = %v", err)
	}

	dst := t.TempDir()
	writeFile(t, filepath.Join(dst, "keep.txt"), "keep")
	_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictOverwrite})
	if !errors.Is(err, errs.ErrCorrupted) {
		t.Fatalf("Unpack() error = %v, want errs.Corrupted", err)
	}
	if got := entryNames(t, dst); len(got) != 1 || got[0] != "keep.txt" {
		t.Errorf("target holds %v, want only keep.txt", got)
	}
}

func TestUnpackRejectsUnknownConflict(t *testing.T) {
	archive := packTree(t, t.TempDir(), nil)
	_, err := Unpack(context.Background(), archive, t.TempDir(), "", UnpackOptions{Conflict: 99})
	if problem := errOn(err, "", errs.ErrInvalidArgument); problem != nil {
		t.Errorf("Unpack() %v", problem)
	}
}

// 覆盖写到一半时出错，原有的文件内容和修改时间都原样保留，临时文件也不留
func TestUnpackOverwriteFailureKeepsExistingFile(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "first.txt"), "first")
	writeFile(t, filepath.Join(src, "second.txt"), "new")
	archive := packTree(t, src, scanOK(t, src))
	// 截在 second.txt 数据区的第二个字节上
	cut := layoutHeaderSize + layoutRecordHeaderSize + len("first.txt") + 512 +
		layoutRecordHeaderSize + len("second.txt") + 1

	dst := t.TempDir()
	target := filepath.Join(dst, "second.txt")
	modTime := time.Date(2020, 5, 4, 3, 2, 1, 0, time.UTC)
	writeFile(t, target, "OLD CONTENT")
	if err := os.Chtimes(target, modTime, modTime); err != nil {
		t.Fatalf("Setup failed: set the modification time: %v", err)
	}

	// first.txt 还原完之后把归档截短，轮到 second.txt 时补零就读不全了
	_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictOverwrite,
		Progress: func(Progress) {
			if err := os.Truncate(archive, int64(cut)); err != nil {
				t.Errorf("Setup failed: truncate the archive: %v", err)
			}
		}})
	if !errors.Is(err, errs.ErrCorrupted) {
		t.Fatalf("Unpack() error = %v, want errs.Corrupted", err)
	}
	if got := readFile(t, target); got != "OLD CONTENT" {
		t.Errorf("second.txt = %q, want the old content", got)
	}
	if info, err := os.Stat(target); err != nil {
		t.Fatal(err)
	} else if !info.ModTime().Equal(modTime) {
		t.Errorf("second.txt: ModTime = %v, want it untouched %v", info.ModTime(), modTime)
	}
	if got := entryNames(t, dst); !slices.Equal(got, []string{"second.txt"}) {
		t.Errorf("the target holds %v, want only second.txt", got)
	}
}

// 覆盖成功之后别的记录出错或者用户取消，已经覆盖好的文件留着，不会被删
func TestUnpackOverwriteSurvivesLaterFailure(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "new")
	entries := append([]Entry{{Path: "a.txt", Kind: KindFile, Size: 3}}, failingRecords(t, src)...)
	archive := packTree(t, src, entries)

	tests := []struct {
		name   string
		cancel bool
		want   error
	}{
		{name: "后面的记录出错", want: errs.ErrIO},
		{name: "覆盖之后取消", cancel: true, want: context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := t.TempDir()
			writeFile(t, filepath.Join(dst, "a.txt"), "OLD CONTENT")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			opts := UnpackOptions{Conflict: ConflictOverwrite}
			if tc.cancel {
				opts.Progress = func(Progress) { cancel() }
			}

			_, err := Unpack(ctx, archive, dst, "", opts)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Unpack() error = %v, want errors.Is(err, %v)", err, tc.want)
			}
			if got := entryNames(t, dst); !slices.Equal(got, []string{"a.txt"}) {
				t.Errorf("the target holds %v, want only a.txt", got)
			}
			if got := readFile(t, filepath.Join(dst, "a.txt")); got != "new" {
				t.Errorf("a.txt = %q, want the archived content", got)
			}
		})
	}
}

// 中途出错时，这次建出来的目录也一并清掉，包括归档里没有单独列出的上级目录
func TestUnpackUndoRemovesCreatedDirs(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "x", "y", "f.txt"), "f")
	entries := append([]Entry{
		{Path: "x/y", Kind: KindDir},
		{Path: "x/y/f.txt", Kind: KindFile, Size: 1},
	}, failingRecords(t, src)...)
	archive := packTree(t, src, entries)

	dst := t.TempDir()
	_, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{})
	if !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Unpack() error = %v, want errs.IO", err)
	}
	if got := entryNames(t, dst); len(got) != 0 {
		t.Errorf("the target holds %v after the failed call, want nothing", got)
	}
}

// 目标目录里已有的符号链接和目录联接，还原时不跟过去写：
// overwrite 把链接换成真目录，其余策略报错
func TestUnpackDoesNotWriteThroughLinks(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "proj", "evil.txt"), "evil")
	withDir := scanOK(t, src)
	fileOnly := []Entry{{Path: "proj/evil.txt", Kind: KindFile, Size: 4}}

	links := []struct {
		name string
		make func(t *testing.T, target, link string)
	}{
		{"符号链接", symlink},
		{"目录联接", junction},
	}
	tests := []struct {
		name    string
		policy  Conflict
		entries []Entry
		blocked string // 报错的那条记录；空串表示还原成功
	}{
		{"overwrite 换成真目录", ConflictOverwrite, withDir, ""},
		{"overwrite 归档里没有目录记录", ConflictOverwrite, fileOnly, ""},
		{"skip 报错", ConflictSkip, withDir, "proj"},
		{"rename 报错", ConflictRename, withDir, "proj"},
	}
	for _, l := range links {
		for _, tc := range tests {
			t.Run(l.name+"/"+tc.name, func(t *testing.T) {
				base := t.TempDir()
				outside := filepath.Join(base, "outside")
				dst := filepath.Join(base, "dst")
				writeFile(t, filepath.Join(outside, "keep.txt"), "keep")
				if err := os.MkdirAll(dst, 0o755); err != nil {
					t.Fatalf("Setup failed: create the target: %v", err)
				}
				l.make(t, outside, filepath.Join(dst, "proj"))

				_, err := Unpack(context.Background(), packTree(t, src, tc.entries), dst, "", UnpackOptions{Conflict: tc.policy})
				if tc.blocked != "" {
					if problem := errOn(err, tc.blocked, errs.ErrIO); problem != nil {
						t.Fatalf("Unpack() %v", problem)
					}
				} else if err != nil {
					t.Fatalf("Unpack() error = %v, want <nil>", err)
				}
				if got := entryNames(t, outside); !slices.Equal(got, []string{"keep.txt"}) {
					t.Errorf("the link target now holds %v, want only keep.txt", got)
				}
				if tc.blocked == "" {
					if got := readFile(t, filepath.Join(dst, "proj", "evil.txt")); got != "evil" {
						t.Errorf("proj/evil.txt = %q, want %q", got, "evil")
					}
					if info, err := os.Lstat(filepath.Join(dst, "proj")); err != nil || !info.IsDir() {
						t.Errorf("proj should now be a plain directory, got %v (%v)", info, err)
					}
				}
			})
		}
	}
}

// 目标是已有的符号链接文件时，overwrite 换掉的是链接本身，不动它指向的文件
func TestUnpackOverwriteReplacesSymlinkItself(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "new")
	archive := packTree(t, src, scanOK(t, src))
	base := t.TempDir()
	outside := filepath.Join(base, "outside.txt")
	dst := filepath.Join(base, "dst")
	writeFile(t, outside, "keep")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("Setup failed: create the target: %v", err)
	}
	symlink(t, outside, filepath.Join(dst, "a.txt"))

	if _, err := Unpack(context.Background(), archive, dst, "", UnpackOptions{Conflict: ConflictOverwrite}); err != nil {
		t.Fatalf("Unpack() error = %v, want <nil>", err)
	}
	if got := readFile(t, outside); got != "keep" {
		t.Errorf("the symlink target now holds %q, want %q", got, "keep")
	}
	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "new" {
		t.Errorf("a.txt = %q, want %q", got, "new")
	}
	if info, err := os.Lstat(filepath.Join(dst, "a.txt")); err != nil || info.Mode()&fs.ModeSymlink != 0 {
		t.Errorf("a.txt should now be a plain file, got %v (%v)", info, err)
	}
}

// readFile 读一个还原出来的文件
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}
