package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

// ---------- 辅助函数 ----------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("Setup failed: write %q: %v", path, err)
	}
}
