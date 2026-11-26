//go:build !windows

package backup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// lockStaging 打开暂存目录里的锁文件并加上排他锁，整个操作期间一直开着
// 清扫的时候试着再锁一次，锁不上就知道这个目录还在用
func lockStaging(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// removeStagingDir 删掉一个不再使用的暂存目录。锁文件还锁着说明有操作在用，不算错
func removeStagingDir(dir string) error {
	f, err := lockStaging(dir)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return nil
	}
	if err != nil {
		return err
	}
	_ = f.Close()
	return os.RemoveAll(dir)
}
