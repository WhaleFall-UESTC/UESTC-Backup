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

// encrypted 字段落盘后再读出来还在；旧的任务文件没有这个字段，读出来是 false
func TestEncryptedSurvivesSaveLoad(t *testing.T) {
	src := t.TempDir()
	path := filepath.Join(t.TempDir(), "tasks.json")
	want := []Task{
		{Name: "a", Source: src, Destination: filepath.Join(t.TempDir(), "a.dbkp"), Algorithm: "lz77", Encrypted: true},
		{Name: "b", Source: src, Destination: filepath.Join(t.TempDir(), "b.dbkp"), Algorithm: "none"},
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got[0].Encrypted || got[1].Encrypted {
		t.Errorf("Load() = %+v, want a encrypted and b plain", got)
	}

	// 旧版任务文件没有 encrypted 字段
	old := `[{"name":"old","source":"C:/s","destination":"C:/o.dbkp","algorithm":"lz77"}]`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("Setup failed: write the task file: %v", err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got) != 1 || got[0].Encrypted {
		t.Errorf("Load() on an old task file = %+v, want one task with Encrypted false", got)
	}
}

// Update 换掉一条任务，位置不动；目标不存在或新名字和别的任务重复都报错
func TestUpdate(t *testing.T) {
	src := t.TempDir()
	dest := filepath.Join(t.TempDir(), "a.dbkp")
	first := Task{Name: "a", Source: src, Destination: dest, Algorithm: "lz77"}
	list, err := Add(nil, first)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	list, err = Add(list, Task{Name: "b", Source: src, Destination: filepath.Join(t.TempDir(), "b.dbkp")})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	list, err = Update(list, "a", Task{Name: "c", Source: src, Destination: dest, Algorithm: "lz77"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if list[0] != (Task{Name: "c", Source: src, Destination: dest, Algorithm: "lz77"}) {
		t.Errorf("Update() left the first task as %+v, want it replaced by c", list[0])
	}

	if _, err := Update(list, "a", first); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Update() of a missing task: error = %v, want errs.ErrInvalidArgument", err)
	}
	if _, err := Update(list, "c", Task{Name: "b", Source: src, Destination: dest, Algorithm: "lz77"}); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("Update() to a duplicate name: error = %v, want errs.ErrInvalidArgument", err)
	}
}

// 改了源目录或归档路径之后要重新校验，坏的改动进不来
func TestUpdateRevalidates(t *testing.T) {
	src := t.TempDir()
	dest := filepath.Join(t.TempDir(), "a.dbkp")
	list, err := Add(nil, Task{Name: "a", Source: src, Destination: dest, Algorithm: "lz77"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	tests := []struct {
		name string
		task Task
	}{
		{"源目录不存在", Task{Name: "a", Source: filepath.Join(src, "nope"), Destination: dest}},
		{"归档挪进源目录", Task{Name: "a", Source: src, Destination: filepath.Join(src, "a.dbkp")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Update(list, "a", tc.task); !errors.Is(err, errs.ErrInvalidArgument) {
				t.Errorf("Update() error = %v, want errs.ErrInvalidArgument", err)
			}
		})
	}
}

// 任务落盘用绝对路径，换一个目录执行还指向同一处
func TestAddMakesPathsAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("src", 0o700); err != nil {
		t.Fatalf("Setup failed: mkdir: %v", err)
	}

	list, err := Add(nil, Task{Name: "a", Source: "src", Destination: "a.dbkp"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	wantSrc, _ := filepath.Abs("src")
	wantDst, _ := filepath.Abs("a.dbkp")
	if list[0].Source != wantSrc || list[0].Destination != wantDst {
		t.Errorf("Add() stored Source %q Destination %q, want %q and %q",
			list[0].Source, list[0].Destination, wantSrc, wantDst)
	}
}
