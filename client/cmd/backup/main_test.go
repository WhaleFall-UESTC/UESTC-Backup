package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"client/internal/backup"
	"client/internal/errs"
)

// 退出码按类别映射，取消是 130
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"部分成功", errs.New(errs.PartialBackup), 9},
		{"普通错误", errs.New(errs.IO), 5},
		{"没有类别的错误", errors.New("boom"), 1},
		{"取消", context.Canceled, 130},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCode(tc.err); got != tc.want {
				t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// 打包有跳过时报 PartialBackup，定时任务才能据此判定备份不完整。
// Windows 上没法把文件弄成读不了，这个用例只在 Linux 下跑
func TestRunPackReportsPartialBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上没有不依赖特权的办法让文件读不了")
	}
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "good.txt"), "aaa")
	hidden := filepath.Join(src, "hidden")
	if err := os.Mkdir(hidden, 0o700); err != nil {
		t.Fatalf("Setup failed: mkdir: %v", err)
	}
	if err := os.Chmod(hidden, 0); err != nil {
		t.Fatalf("Setup failed: chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(hidden, 0o700) })

	dst := filepath.Join(t.TempDir(), "backup.dbkp")
	err := runPack(context.Background(), []string{"-progress=false", src, dst})
	if !errors.Is(err, errs.ErrPartialBackup) {
		t.Errorf("runPack() error = %v, want errors.Is(err, errs.ErrPartialBackup)", err)
	}
}

// -password-file 从文件读口令，行尾的换行不算口令的一部分
func TestCommonFlagsPasswordFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pw.txt")
	writeFile(t, file, "secret\r\n")
	c := newCommonFlags("pack")
	if err := c.parse([]string{"-password-file", file}); err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if c.password != "secret" {
		t.Errorf("password = %q, want %q", c.password, "secret")
	}
}

// -password 和 -password-file 一起给分不清哪个算数
func TestCommonFlagsRejectsTwoPasswords(t *testing.T) {
	c := newCommonFlags("pack")
	err := c.parse([]string{"-password", "a", "-password-file", "b"})
	if err == nil {
		t.Errorf("parse() with both -password and -password-file = <nil>, want an error")
	}
}

// 口令文件读不了要把底层错误报出来，不能当成没给口令
func TestCommonFlagsRejectsMissingPasswordFile(t *testing.T) {
	c := newCommonFlags("pack")
	if err := c.parse([]string{"-password-file", filepath.Join(t.TempDir(), "nope.txt")}); err == nil {
		t.Errorf("parse() with a missing password file = <nil>, want an error")
	}
}

// 进度行变短时要把上一行的尾巴盖掉，长路径换成短路径后不能留下残字
func TestProgressPrinterOverwritesShorterLines(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Setup failed: os.Pipe() error = %v", err)
	}
	old := os.Stderr
	os.Stderr = w
	printer := progressPrinter(true)

	printer(backup.Progress{Phase: backup.PhasePacking, Path: strings.Repeat("very/long/path/", 8), TotalFiles: 2, DoneFiles: 1, DoneBytes: 1 << 20, TotalBytes: 2 << 20})
	printer(backup.Progress{Phase: backup.PhasePacking, Path: "a.txt", TotalFiles: 2, DoneFiles: 2, DoneBytes: 2 << 20, TotalBytes: 2 << 20})

	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatalf("Setup failed: close the pipe: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Setup failed: read the pipe: %v", err)
	}

	segments := strings.Split(string(data), "\r")
	last := segments[len(segments)-1]
	want := "打包 a.txt 2/2 个文件 2.0/2.0 MB"
	if got := strings.TrimRight(last, " \n"); got != want {
		t.Errorf("the last progress line = %q, want %q plus spaces only", got, want)
	}
}

// ---------- 辅助函数 ----------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("Setup failed: write %q: %v", path, err)
	}
}
