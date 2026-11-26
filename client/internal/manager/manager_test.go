package manager

import (
	"errors"
	"path/filepath"
	"testing"

	"client/internal/errs"
	"client/internal/tasks"
)

// 测试里把配置目录换到临时位置，不碰真实的 %AppData%
func useTempConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := configDir
	configDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { configDir = old })
}

// sampleTask 造一条能通过校验的任务
func sampleTask(t *testing.T, name string) tasks.Task {
	t.Helper()
	return tasks.Task{
		Name:        name,
		Source:      t.TempDir(),
		Destination: filepath.Join(t.TempDir(), name+".dbkp"),
		Algorithm:   "lz77",
	}
}

// 新增后落盘再读出来一致，encrypted 也在；还没有任务文件时是空列表
func TestTaskAddAndList(t *testing.T) {
	useTempConfig(t)

	list, err := ListTasks()
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 0 {
		t.Errorf("ListTasks() on a fresh config dir = %+v, want empty", list)
	}

	a := sampleTask(t, "a")
	a.Encrypted = true
	if err := AddTask(a); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	b := sampleTask(t, "b")
	if err := AddTask(b); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	list, err = ListTasks()
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 2 || list[0] != a || list[1] != b {
		t.Errorf("ListTasks() = %+v, want [a encrypted, b]", list)
	}
}

// 改名和删除后落盘再读出来一致，留下的任务位置不动
func TestTaskSaveAndDelete(t *testing.T) {
	useTempConfig(t)
	a := sampleTask(t, "a")
	if err := AddTask(a); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}
	b := sampleTask(t, "b")
	if err := AddTask(b); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	c := sampleTask(t, "c")
	if err := SaveTask("b", c); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	list, err := ListTasks()
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 2 || list[1] != c {
		t.Errorf("SaveTask() renamed b to c, but ListTasks() = %+v", list)
	}

	if err := DeleteTask("a"); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	list, err = ListTasks()
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 1 || list[0] != c {
		t.Errorf("ListTasks() after DeleteTask() = %+v, want only c", list)
	}
}

// 重名和任务不存在都报 InvalidArgument，任务文件不因此写坏
func TestTaskServiceRejectsBadChanges(t *testing.T) {
	useTempConfig(t)
	a := sampleTask(t, "a")
	if err := AddTask(a); err != nil {
		t.Fatalf("AddTask() error = %v", err)
	}

	if err := AddTask(sampleTask(t, "a")); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("AddTask() with a duplicate name: error = %v, want errs.ErrInvalidArgument", err)
	}
	if err := SaveTask("missing", sampleTask(t, "x")); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("SaveTask() of a missing task: error = %v, want errs.ErrInvalidArgument", err)
	}
	if err := DeleteTask("missing"); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("DeleteTask() of a missing task: error = %v, want errs.ErrInvalidArgument", err)
	}

	list, err := ListTasks()
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(list) != 1 || list[0] != a {
		t.Errorf("ListTasks() after the rejected changes = %+v, want only a", list)
	}
}
