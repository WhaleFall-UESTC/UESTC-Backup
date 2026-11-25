package backup

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/crc64"
	"io"
	"os"
	"path/filepath"

	"client/internal/errs"
)

// PackOptions 是打包的可选参数。Password 不为空时，压缩之后再加密
type PackOptions struct {
	Algorithm Algorithm
	Password  string
	Progress  func(path string, bytes int64)
}

// Pack 把 entries 写成 .dbkp，读不了的文件跳过并记进返回值，出错时不留半截归档。
// Algorithm 不为 0 时把整个归档压一遍，压完不比原样小就改存原样；
// Password 不为空时再加密整份归档
func Pack(ctx context.Context, dst, src string, entries []Entry, opts PackOptions) ([]Skip, error) {
	if err := rejectInsideSource(dst, src); err != nil {
		return nil, err
	}
	if !opts.Algorithm.compressible() {
		return nil, errs.Newf(errs.InvalidArgument, "unknown algorithm %d", opts.Algorithm)
	}

	tmp := dst + ".tmp"
	skips, size, err := packRaw(ctx, tmp, src, entries, opts)
	if err != nil {
		return nil, err
	}
	if err := finalize(ctx, tmp, size, dst, opts); err != nil {
		return nil, err
	}
	return skips, nil
}

// packRaw 把 entries 写成不压缩的归档，返回它的字节数；失败时删掉半截文件
func packRaw(ctx context.Context, dst, src string, entries []Entry, opts PackOptions) ([]Skip, int64, error) {
	out, err := os.Create(dst)
	if err != nil {
		return nil, 0, err
	}

	w := &archiveWriter{f: out}
	skips, err := writeArchive(ctx, w, src, entries, opts.Progress)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dst)
		return nil, 0, err
	}
	return skips, w.offset, nil
}

// finalize 依次接上压缩和加密，把成品落到 dst；中间文件不论成败都清掉
func finalize(ctx context.Context, raw string, rawSize int64, dst string, opts PackOptions) error {
	temps := []string{raw}
	cleanup := func() {
		for _, p := range temps {
			_ = os.Remove(p)
		}
	}

	keep := raw
	if opts.Algorithm != AlgorithmNone {
		compressed := dst + ".cmp.tmp"
		temps = append(temps, compressed)
		size, err := compressFile(ctx, raw, compressed, opts.Algorithm)
		if err != nil {
			cleanup()
			return err
		}
		if size < rawSize {
			keep = compressed
		}
	}
	if opts.Password != "" {
		encrypted := dst + ".enc.tmp"
		temps = append(temps, encrypted)
		if err := encryptFile(ctx, keep, encrypted, opts.Password); err != nil {
			cleanup()
			return err
		}
		keep = encrypted
	}

	if err := os.Rename(keep, dst); err != nil {
		cleanup()
		return err
	}
	cleanup() // keep 已经改名到 dst，剩下的是输掉的那几份
	return nil
}

// rejectInsideSource 拒绝把归档放进源目录，否则它会把自己也打进去。
func rejectInsideSource(dst, src string) error {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absSrc, absDst)
	if err != nil {
		return nil
	}
	if filepath.IsLocal(rel) {
		return errs.Newf(errs.InvalidArgument, "archive is inside the source directory").At(dst)
	}
	return nil
}
func writeArchive(ctx context.Context, w *archiveWriter, src string, entries []Entry, progress func(path string, bytes int64)) ([]Skip, error) {
	if err := writeArchiveHeader(w, AlgorithmNone); err != nil {
		return nil, err
	}

	var skips []Skip
	var written int64
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		skip, err := writeArchiveRecord(w, src, e)
		if err != nil {
			return nil, err
		}
		if skip != nil {
			skips = append(skips, *skip)
			continue
		}
		if e.Kind == KindFile && progress != nil {
			written += e.Size
			progress(e.Path, written)
		}
	}

	// 回填公共头里的校验值
	err := binary.Write(io.NewOffsetWriter(w.f, archiveHeaderSize-crcSize), binary.LittleEndian, w.crc)
	return skips, err
}
func writeArchiveHeader(w io.Writer, algo Algorithm) error {
	header := make([]byte, archiveHeaderSize)

	copy(header[0:4], archiveMagic)
	binary.LittleEndian.PutUint16(header[4:6], archiveVersion)
	binary.LittleEndian.PutUint16(header[6:8], uint16(algo))
	binary.LittleEndian.PutUint64(header[8:16], uint64(headerDataOffset(algo)))

	_, err := w.Write(header)
	return err
}

// writeArchiveRecord 写一条记录；文件读不了时返回 Skip，不算致命错误。
func writeArchiveRecord(w *archiveWriter, src string, e Entry) (*Skip, error) {
	if e.Size < 0 {
		return nil, errs.Newf(errs.InvalidArgument, "size %d", e.Size).At(e.Path)
	}

	var in *os.File
	if e.Kind == KindFile {
		f, err := os.Open(filepath.Join(src, filepath.FromSlash(e.Path)))
		if err != nil {
			return &Skip{Path: e.Path, Err: errs.Wrap(err, errs.IO).At(e.Path)}, nil
		}
		defer func() { _ = f.Close() }()
		in = f
	}

	offset, crc := w.offset, w.crc
	if _, err := w.Write(recordHeader(e)); err != nil {
		return nil, err
	}
	if in == nil {
		return nil, nil
	}
	return writeRecordData(w, in, e, offset, crc)
}

// recordHeader 拼出一条记录的头：定长字段加路径。
func recordHeader(e Entry) []byte {
	pathBytes := []byte(e.Path)
	header := make([]byte, recordHeaderSize+len(pathBytes))
	copy(header[recordHeaderSize:], pathBytes)
	header[0] = byte(e.Kind)
	binary.LittleEndian.PutUint32(header[4:8], uint32(e.Mode))
	binary.LittleEndian.PutUint64(header[8:16], uint64(e.Size))
	binary.LittleEndian.PutUint64(header[16:24], uint64(e.ModTime.Unix()))
	binary.LittleEndian.PutUint32(header[24:28], uint32(e.ModTime.Nanosecond()))
	binary.LittleEndian.PutUint32(header[28:32], uint32(len(pathBytes)))
	return header
}

// writeRecordData 写文件内容的数据区并回填 CRC64，读不了时把这条记录退掉。
func writeRecordData(w *archiveWriter, in io.Reader, e Entry, offset int64, crc uint64) (*Skip, error) {
	content := crc64.New(crcTable)
	n, err := io.CopyN(io.MultiWriter(w, content), in, e.Size)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// 源文件不够长。同 incomplete，不把 io.EOF 接进错误链
			err = errs.Wrapf(io.ErrUnexpectedEOF, errs.SourceChanged, "read %d of %d bytes", n, e.Size).At(e.Path)
		} else {
			err = errs.Wrap(err, errs.IO).At(e.Path)
		}
		if rbErr := w.rollback(offset, crc); rbErr != nil {
			return nil, rbErr
		}
		return &Skip{Path: e.Path, Err: err}, nil
	}
	if err := pad(w, e.Size); err != nil {
		return nil, err
	}
	return nil, binary.Write(io.NewOffsetWriter(w.f, offset+crcFieldOffset), binary.LittleEndian, content.Sum64())
}

// pad 把 size 字节的数据区补零到 archiveAlignment 的整数倍。
func pad(w io.Writer, size int64) error {
	if n := paddedSize(size) - size; n > 0 {
		_, err := w.Write(make([]byte, n))
		return err
	}
	return nil
}

// paddedSize 是 size 字节的数据区在归档里占的字节数。
func paddedSize(size int64) int64 {
	return (size + archiveAlignment - 1) / archiveAlignment * archiveAlignment
}

// archiveWriter 顺序写归档并累计整档的 CRC64，跳过文件时能退回之前的状态。
type archiveWriter struct {
	f      *os.File
	crc    uint64
	offset int64
}

func (w *archiveWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.crc = crc64.Update(w.crc, crcTable, p[:n])
	w.offset += int64(n)
	return n, err
}

// rollback 把归档退回 offset 处，好像那条记录没写过。
func (w *archiveWriter) rollback(offset int64, crc uint64) error {
	w.offset, w.crc = offset, crc
	if err := w.f.Truncate(offset); err != nil {
		return err
	}
	_, err := w.f.Seek(offset, io.SeekStart)
	return err
}
