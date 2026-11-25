package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"client/internal/errs"
)

// 写进去再读出来，内容一样；同一份列表写两次，文件字节也一样
func TestSaveLoadRoundTrip(t *testing.T) {
	src := t.TempDir()
	path := filepath.Join(t.TempDir(), "tasks.json")
	want := []Task{
		{Name: "文档", Source: src, Destination: filepath.Join(t.TempDir(), "a.dbkp"), Algorithm: "lz77"},
		{Name: "照片", Source: src, Destination: filepath.Join(t.TempDir(), "b.dbkp"), Algorithm: "none"},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Setup failed: read the task file: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("Load() returned %d tasks, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("task %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Setup failed: read the task file: %v", err)
	}
	if string(first) != string(second) {
		t.Error("saving the same list twice wrote different bytes")
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Load() = %+v, want no tasks", got)
	}
}

// 坏的 JSON 和不认识的字段都按坏配置拒掉
func TestLoadRejectsBadFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"不是 JSON", "not json"},
		{"字段名不认识", `[{"name":"a","source":"s","destination":"d","algorithm":"lz77","extra":1}]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tasks.json")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatalf("Setup failed: write the task file: %v", err)
			}
			if _, err := Load(path); !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("Load() error = %v, want errors.Is(err, errs.ErrInvalidArgument)", err)
			}
		})
	}
}

func TestAddAndRemove(t *testing.T) {
	src := t.TempDir()
	task := Task{Name: "a", Source: src, Destination: filepath.Join(t.TempDir(), "a.dbkp"), Algorithm: "lz77"}

	list, err := Add(nil, task)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, err := Add(list, task); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Add() with a duplicate name: error = %v, want errs.InvalidArgument", err)
	}
	list, err = Remove(list, "a")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(list) != 0 {
		t.Errorf("Remove() left %+v", list)
	}
	if _, err := Remove(list, "a"); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Remove() of a missing task: error = %v, want errs.InvalidArgument", err)
	}
}

// 必填项和路径关系
func TestValidate(t *testing.T) {
	src := t.TempDir()
	file := filepath.Join(src, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("Setup failed: write a file: %v", err)
	}
	dest := filepath.Join(t.TempDir(), "a.dbkp")

	tests := []struct {
		name string
		task Task
	}{
		{"名字为空", Task{Source: src, Destination: dest}},
		{"源目录为空", Task{Name: "a", Destination: dest}},
		{"源目录不存在", Task{Name: "a", Source: filepath.Join(src, "nope"), Destination: dest}},
		{"源路径是文件", Task{Name: "a", Source: file, Destination: dest}},
		{"归档路径为空", Task{Name: "a", Source: src}},
		{"归档落在源目录里", Task{Name: "a", Source: src, Destination: filepath.Join(src, "a.dbkp")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.task); !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("Validate() error = %v, want errors.Is(err, errs.ErrInvalidArgument)", err)
			}
		})
	}

	ok := Task{Name: "a", Source: src, Destination: dest, Algorithm: "lz77"}
	if err := Validate(ok); err != nil {
		t.Errorf("Validate() on a good task = %v, want <nil>", err)
	}
}
