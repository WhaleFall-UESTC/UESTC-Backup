package backup

import (
	"io"
	"os"
	"path/filepath"

	"client/internal/errs"
)

// Unpack 把归档还原到 dst；dst 非空就拒绝，中途出错清掉这次写的文件。
func Unpack(archive, dst string) error {
	if err := Verify(archive); err != nil {
		return err
	}
	if err := prepareTarget(dst); err != nil {
		return err
	}

	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	a := &archiveReader{path: archive, f: f}
	if err := a.readHeader(); err != nil {
		return err
	}
	err = a.each(func(rec Record) error {
		if !filepath.IsLocal(rec.Path) {
			return errs.Newf(errs.Corrupted, "path %q is not local", rec.Path).At(archive)
		}
		target := filepath.Join(dst, filepath.FromSlash(rec.Path))
		switch rec.Kind {
		case KindDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return errs.Wrap(err, errs.IO).At(rec.Path)
			}
		case KindFile:
			return a.restoreFile(rec, target)
		case KindSymlink:
			// 留到“文件类型支持”，要先给 Entry 加链接目标
		default:
			return errs.Newf(errs.Corrupted, "record kind %d", rec.Kind).At(archive)
		}
		return nil
	})
	if err != nil {
		// dst 开始时是空的
		_ = clearDir(dst)
	}
	return err
}

// clearDir 删掉 dir 里的内容，目录本身留着。
func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
func prepareTarget(dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dst)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return errs.New(errs.TargetNotEmpty).At(dst)
	}
	return nil
}

// restoreFile 在 target 处写出一条文件记录，写完恢复修改时间。
func (a *archiveReader) restoreFile(rec Record, target string) error {
	out, err := os.Create(target)
	if err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}

	_, err = io.CopyN(out, a.f, rec.Size)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == io.EOF {
		// 归档在 Verify 之后又被截短了
		return incomplete(err, "data is incomplete").At(rec.Path)
	}
	if err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}
	if _, err := io.CopyN(io.Discard, a.f, paddedSize(rec.Size)-rec.Size); err != nil {
		return incomplete(err, "padding is incomplete").At(rec.Path)
	}
	if err := os.Chtimes(target, rec.ModTime, rec.ModTime); err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}
	return nil
}
