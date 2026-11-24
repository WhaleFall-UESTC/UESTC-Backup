package backup

import (
	"hash/crc64"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type Kind uint8

const (
	KindFile Kind = iota + 1
	KindDir
	KindSymlink
)

// Entry 目录树中的节点
type Entry struct {
	Path    string      `json:"path"`
	Kind    Kind        `json:"kind"`
	Size    int64       `json:"size"`
	Mode    fs.FileMode `json:"mode"`
	ModTime time.Time   `json:"mod_time"`
}

// Scan 遍历 root，返回全部子孙节点。不包含 root 自身，不跟随符号链接
func Scan(root string) ([]Entry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}

	if !rootInfo.IsDir() {
		return nil, &os.PathError{Op: "scan", Path: root, Err: fs.ErrInvalid}
	}

	var entries []Entry

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// 跳过 root
		if path == root {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		entry := Entry{
			Path:    filepath.ToSlash(rel),
			Size:    info.Size(),
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		}

		switch {
		case d.Type()&fs.ModeSymlink != 0:
			entry.Kind = KindSymlink
			// 符号链接的 Size 取决于目标文件
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entry.Size = int64(len(target))

		case d.IsDir():
			entry.Kind = KindDir
		case d.Type().IsRegular():
			entry.Kind = KindFile
		default:
			// 管道、socket、设备文件、Windows 目录联接等无法备份的节点，跳过
			return nil
		}

		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// FileCRC64 流式计算文件的 CRC-64/ECMA 校验值，并返回文件字节数
func FileCRC64(path string) (crc uint64, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	table := crc64.MakeTable(crc64.ECMA)
	h := crc64.New(table)

	size, err = io.Copy(h, f)
	if err != nil {
		return 0, size, err
	}

	return h.Sum64(), size, nil
}
