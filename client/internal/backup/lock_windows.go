//go:build windows

package backup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// lockStaging 打开暂存目录里的锁文件，整个操作期间一直开着
// 开着的文件在 Windows 上删不掉，清扫的时候据此分辨哪些目录还在用
func lockStaging(dir string) (*os.File, error) {
	return os.OpenFile(filepath.Join(dir, lockName), os.O_RDWR|os.O_CREATE, 0o600)
}

// removeStagingDir 删掉一个不再使用的暂存目录。先单独试删锁文件：删不掉说明有操作正开着它，
// 这时候整个目录都不动，不能先把别的文件删了一半
func removeStagingDir(dir string) error {
	if err := os.Remove(filepath.Join(dir, lockName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return os.RemoveAll(dir)
}
