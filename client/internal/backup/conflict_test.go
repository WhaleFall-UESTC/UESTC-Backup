package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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

// readFile 读一个还原出来的文件
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}
