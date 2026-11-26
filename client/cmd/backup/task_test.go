package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"client/internal/backup"
	"client/internal/errs"
	"client/internal/tasks"
)

// -encrypt 写进任务文件，不加时是 false
func TestRunTaskAddEncryptFlag(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "a.dbkp")

	encrypted := filepath.Join(t.TempDir(), "encrypted.json")
	err := runTaskAdd([]string{"-config", encrypted, "-name", "a", "-source", src, "-dest", dst, "-algo", "none", "-encrypt"})
	if err != nil {
		t.Fatalf("runTaskAdd() error = %v", err)
	}
	list, err := tasks.Load(encrypted)
	if err != nil {
		t.Fatalf("tasks.Load() error = %v", err)
	}
	if len(list) != 1 || !list[0].Encrypted {
		t.Errorf("task add -encrypt stored %+v, want Encrypted", list)
	}

	plain := filepath.Join(t.TempDir(), "plain.json")
	err = runTaskAdd([]string{"-config", plain, "-name", "b", "-source", src, "-dest", dst, "-algo", "none"})
	if err != nil {
		t.Fatalf("runTaskAdd() error = %v", err)
	}
	list, err = tasks.Load(plain)
	if err != nil {
		t.Fatalf("tasks.Load() error = %v", err)
	}
	if len(list) != 1 || list[0].Encrypted {
		t.Errorf("task add without -encrypt stored %+v, want a plain task", list)
	}
}

// 加密任务没给口令运行要报 InvalidArgument，不能拿明文归档盖掉密文；给了口令就正常加密
func TestRunTaskRunRequiresPassword(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "f.txt"), "data")
	dst := filepath.Join(t.TempDir(), "a.dbkp")
	config := filepath.Join(t.TempDir(), "tasks.json")
	err := runTaskAdd([]string{"-config", config, "-name", "a", "-source", src, "-dest", dst, "-algo", "none", "-encrypt"})
	if err != nil {
		t.Fatalf("runTaskAdd() error = %v", err)
	}

	err = runTaskRun(context.Background(), []string{"-config", config, "-progress=false", "a"})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Errorf("runTaskRun() without a password: error = %v, want errs.ErrInvalidArgument", err)
	}

	err = runTaskRun(context.Background(), []string{"-config", config, "-progress=false", "-password", "pw", "a"})
	if err != nil {
		t.Fatalf("runTaskRun() with a password: error = %v", err)
	}
	if err := backup.Verify(context.Background(), dst, "", backup.VerifyOptions{}); !errors.Is(err, errs.ErrWrongPassword) {
		t.Errorf("Verify() without a password = %v, want errs.ErrWrongPassword", err)
	}
	if err := backup.Verify(context.Background(), dst, "pw", backup.VerifyOptions{}); err != nil {
		t.Errorf("Verify() with the password = %v, want <nil>", err)
	}
}
