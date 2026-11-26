package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"client/internal/errs"
)

// Conflict 是还原时遇到已有路径的处理办法
type Conflict uint8

// Conflict 的取值
const (
	ConflictFail      Conflict = iota // 目标非空就拒绝，默认
	ConflictOverwrite                 // 覆盖同名文件
	ConflictSkip                      // 已有文件不动，跳过这一条
	ConflictRename                    // 换个名字：a.1.txt、a.2.txt
)

// maxRenameTries 是 ConflictRename 找空名字的上限
const maxRenameTries = 1000

func (c Conflict) known() bool { return c <= ConflictRename }

// UnpackOptions 是还原的可选参数
type UnpackOptions struct {
	Conflict Conflict
	Progress func(Progress)
}

// UnpackResult 是还原的结果
type UnpackResult struct {
	Restored int
	Skipped  int
	Renamed  int
}

// Unpack 把归档还原到 dst。ConflictFail 时 dst 非空就拒绝
// 中途出错只删这次新建的文件和目录：覆盖到一半的原样保留，已经覆盖成功的留着
// 已有的符号链接和目录联接不跟过去写
// password 在未加密的归档上被忽略
func Unpack(ctx context.Context, archive, dst, password string, opts UnpackOptions) (UnpackResult, error) {
	if !opts.Conflict.known() {
		return UnpackResult{}, errs.Newf(errs.InvalidArgument, "unknown conflict policy %d", opts.Conflict)
	}
	f, cleanup, err := openArchive(archive, password)
	if err != nil {
		return UnpackResult{}, err
	}
	defer cleanup()

	stats, err := verifyArchive(ctx, archive, f, nil)
	if err != nil {
		return UnpackResult{}, err
	}
	if err := prepareTarget(dst, opts.Conflict); err != nil {
		return UnpackResult{}, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return UnpackResult{}, err
	}
	a := &archiveReader{path: archive, f: f}
	if err := a.readHeader(); err != nil {
		return UnpackResult{}, err
	}

	r := &restorer{
		a: a, archive: archive, dst: dst, conflict: opts.Conflict,
		dirs: map[string]bool{filepath.Clean(dst): true},
	}
	if err := r.all(ctx, stats, opts.Progress); err != nil {
		r.undo()
		return UnpackResult{}, err
	}
	return r.result, nil
}

// restorer 是一次还原的状态：冲突策略、结果统计、确认过的目录，以及出错时要删掉的东西
type restorer struct {
	a        *archiveReader
	archive  string
	dst      string
	conflict Conflict
	result   UnpackResult
	created  []string        // 这次新建的路径，出错时删掉
	dirs     map[string]bool // 确认过是普通目录的路径，只往这些目录里写
}

// all 按顺序还原全部记录，每处理完一个文件发一次进度
func (r *restorer) all(ctx context.Context, stats Stats, progress func(Progress)) error {
	var doneFiles int
	var doneBytes int64
	return r.a.each(func(rec Record) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.record(rec); err != nil {
			return err
		}
		if rec.Kind != KindFile {
			return nil
		}
		doneFiles++
		doneBytes += rec.Size
		if progress != nil {
			progress(Progress{Phase: PhaseRestoring, Path: rec.Path, DoneFiles: doneFiles, TotalFiles: stats.Files, DoneBytes: doneBytes, TotalBytes: stats.Bytes})
		}
		return nil
	})
}

// undo 删掉这次写出来的东西，后写的先删
func (r *restorer) undo() {
	for i := len(r.created) - 1; i >= 0; i-- {
		_ = os.Remove(r.created[i])
	}
}

// record 按一条记录在 dst 里建出目录或文件，符号链接跳过。
func (r *restorer) record(rec Record) error {
	if !filepath.IsLocal(rec.Path) {
		return errs.Newf(errs.Corrupted, "path %q is not local", rec.Path).At(r.archive)
	}
	target := filepath.Join(r.dst, filepath.FromSlash(rec.Path))
	switch rec.Kind {
	case KindDir:
		return r.ensureDir(target, rec.Path)
	case KindFile:
		return r.writeFile(target, rec)
	case KindSymlink:
		// 留到“文件类型支持”，要先给 Entry 加链接目标
		return nil
	}
	return errs.Newf(errs.Corrupted, "record kind %d", rec.Kind).At(r.archive)
}

// ensureDir 确认 dir 和它的上级都是普通目录，缺的逐级建出来并记进 created
func (r *restorer) ensureDir(dir, name string) error {
	if r.dirs[dir] {
		return nil
	}
	if err := r.ensureDir(filepath.Dir(dir), name); err != nil {
		return err
	}
	err := r.makePlainDir(dir, name)
	r.dirs[dir] = err == nil
	return err
}

// makePlainDir 让 dir 处是一个普通目录。已有的符号链接、目录联接不跟过去：
// overwrite 时先删掉再建目录，其余策略报错；已有的是文件时一律报错
func (r *restorer) makePlainDir(dir, name string) error {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return errs.Wrap(err, errs.IO).At(name)
	case info.IsDir():
		return nil
	case info.Mode().IsRegular() || r.conflict != ConflictOverwrite:
		return errs.Newf(errs.IO, "path is not a directory").At(name)
	default:
		if err := os.Remove(dir); err != nil {
			return errs.Wrap(err, errs.IO).At(name)
		}
	}
	if err := os.Mkdir(dir, 0o755); err != nil { //nolint:gosec // 还原出来的目录按惯例 0755
		return errs.Wrap(err, errs.IO).At(name)
	}
	r.created = append(r.created, dir)
	return nil
}

// writeFile 按冲突策略写出一个文件，跳过的也要把数据区读掉
func (r *restorer) writeFile(target string, rec Record) error {
	if err := r.ensureDir(filepath.Dir(target), rec.Path); err != nil {
		return err
	}
	exists := false
	if _, err := os.Lstat(target); err == nil {
		exists = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}

	newFile := true
	if exists {
		switch r.conflict {
		case ConflictSkip:
			r.result.Skipped++
			return r.a.skipData(rec)
		case ConflictFail:
			return errs.New(errs.TargetNotEmpty).At(rec.Path)
		case ConflictRename:
			name, err := freeName(target)
			if err != nil {
				return err
			}
			target = name
			r.result.Renamed++
		case ConflictOverwrite:
			newFile = false
		}
	}
	if newFile {
		r.created = append(r.created, target)
	}

	var werr error
	if newFile {
		werr = r.a.restoreFile(rec, target)
	} else {
		werr = r.a.replaceFile(rec, target)
	}
	if werr != nil {
		return werr
	}
	r.result.Restored++
	return nil
}

// skipData 跳过一条文件记录的数据区
func (a *archiveReader) skipData(rec Record) error {
	if _, err := a.f.Seek(paddedSize(rec.Size), io.SeekCurrent); err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}
	return nil
}

// freeName 找一个没被占用的名字：a.txt → a.1.txt、a.2.txt
func freeName(target string) (string, error) {
	ext := filepath.Ext(target)
	base := strings.TrimSuffix(target, ext)
	for i := 1; i <= maxRenameTries; i++ {
		candidate := fmt.Sprintf("%s.%d%s", base, i, ext)
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", errs.Wrap(err, errs.IO)
		}
	}
	return "", errs.Newf(errs.IO, "no free name beside %s", target)
}

func prepareTarget(dst string, conflict Conflict) error {
	if err := os.MkdirAll(dst, 0o755); err != nil { //nolint:gosec // 还原出来的目录按惯例 0755
		return err
	}
	if conflict != ConflictFail {
		return nil
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

// restoreFile 在 target 处写出一条文件记录，写完恢复修改时间
func (a *archiveReader) restoreFile(rec Record, target string) error {
	out, err := os.Create(target)
	if err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}
	if err := a.writeData(out, rec); err != nil {
		return err
	}
	return setModTime(rec, target)
}

// replaceFile 写出一条文件记录并覆盖 target。先写到旁边的临时文件，写完才改名，
// 中途出错时原有的 target 原样保留，符号链接也不会被跟过去
func (a *archiveReader) replaceFile(rec Record, target string) (err error) {
	tmp := filepath.Join(filepath.Dir(target),
		fmt.Sprintf(".%s.%d.tmp", filepath.Base(target), time.Now().UnixNano()))
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666) //nolint:gosec // 同 os.Create，最终权限由 umask 决定
	if err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()

	if err = a.writeData(out, rec); err != nil {
		return err
	}
	if err = setModTime(rec, tmp); err != nil {
		return err
	}
	if err = os.Rename(tmp, target); err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}
	return nil
}

// writeData 把一条文件记录的数据区写进 out 并关掉它，再读掉补零
func (a *archiveReader) writeData(out *os.File, rec Record) error {
	_, err := io.CopyN(out, a.f, rec.Size)
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
	return nil
}

// setModTime 把 path 的访问时间和修改时间都设成记录里的修改时间
func setModTime(rec Record, path string) error {
	if err := os.Chtimes(path, rec.ModTime, rec.ModTime); err != nil {
		return errs.Wrap(err, errs.IO).At(rec.Path)
	}
	return nil
}
