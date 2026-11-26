package backup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// stagingRoot 返回暂存目录的根，测试里换成临时目录
var stagingRoot = func() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cache, "UESTC-Backup", "staging"), nil
}

// staging 是一次操作专用的暂存目录。解密、解压出来的明文和带口令打包的中间文件都放这里：
// 它在本机用户自己的目录下，只有本人能进，目标目录和系统临时目录都不留明文
type staging struct {
	dir  string
	lock *os.File   // 整个操作期间一直开着，清扫的时候据此知道这个目录还在用
	open []*os.File // 这次开的文件，删目录之前要先关：Windows 上开着的文件删不掉
}

// newStaging 建一个暂存目录
func newStaging() (*staging, error) {
	root, err := stagingRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, "op-")
	if err != nil {
		return nil, err
	}
	s := &staging{dir: dir}
	if s.lock, err = lockStaging(dir); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return s, nil
}

// file 在暂存目录里建一个新文件，只有本人能读写
func (s *staging) file(name string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(s.dir, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	s.open = append(s.open, f)
	return f, nil
}

// path 是暂存目录里一个文件的全路径，文件由调用方自己建
func (s *staging) path(name string) string {
	return filepath.Join(s.dir, name)
}

// remove 关掉这次开的文件，删掉整个暂存目录，之后不能再用它
func (s *staging) remove() {
	for _, f := range s.open {
		_ = f.Close()
	}
	_ = s.lock.Close()
	_ = os.RemoveAll(s.dir)
}

// lockName 是暂存目录里锁文件的名字
const lockName = "lock"

// SweepStaging 清掉以前没来得及删的暂存目录，正在用的不动。每次启动时调一次
func SweepStaging() error {
	root, err := stagingRoot()
	if err != nil {
		return err
	}
	dirs, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var failed []error
	for _, d := range dirs {
		if d.IsDir() {
			if err := removeStagingDir(filepath.Join(root, d.Name())); err != nil {
				failed = append(failed, err)
			}
		}
	}
	return errors.Join(failed...)
}
