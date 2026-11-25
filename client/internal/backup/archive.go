package backup

import (
	"context"
	"encoding/binary"
	"errors"
	"hash"
	"hash/crc64"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"client/internal/errs"
)

// Record 是归档里的一条记录：节点的元数据，加上文件内容的校验值。
type Record struct {
	Entry
	CRC64 uint64
}

// .dbkp 的布局，整数一律小端序：
//
//	公共头 24 字节
//	  偏移  宽度  字段         取值
//	     0     4  Magic        "DBKP"
//	     4     2  Version      1
//	     6     2  Algorithm    0 = 不压缩不加密
//	     8     8  DataOffset   记录区的起始偏移，恒为 24
//	    16     8  CRC64        整份归档的校验值
//
//	记录，一条接一条，顺序同打包时的 entries
//	  偏移  宽度  字段         取值
//	     0     1  Kind          1 文件 / 2 目录 / 3 符号链接
//	     1     3  保留          0
//	     4     4  Mode          fs.FileMode 的 uint32 原值
//	     8     8  Size          字节数，同 Entry.Size
//	    16     8  ModTimeSec    修改时间，Unix 秒
//	    24     4  ModTimeNsec   修改时间的纳秒部分
//	    28     4  PathLen       路径的字节数
//	    32     8  CRC64         文件内容的校验值
//	    40   PathLen Path         UTF-8，只用 '/' 分隔
//	  40+PathLen      数据区        仅普通文件：Size 字节内容，补零到 512 的整数倍
//
// 两个 CRC64 都是 CRC-64/ECMA。公共头的 CRC64 覆盖整个归档，
// 但各处的 CRC64 字段本身按 0 计入。
const (
	archiveMagic      = "DBKP"
	archiveVersion    = uint16(1)
	archiveAlgorithm  = uint16(0)
	archiveHeaderSize = int64(24)
	recordHeaderSize  = 40
	archiveAlignment  = int64(512)

	crcFieldOffset = 32 // 记录头里 CRC64 的偏移
	crcSize        = 8
	maxPathBytes   = 1 << 20
)

var (
	crcTable = crc64.MakeTable(crc64.ECMA)
	zeroCRC  [crcSize]byte
)

// Skip 是打包时跳过的文件。
type Skip struct {
	Path string
	Err  error
}

// Pack 把 entries 写成 .dbkp，读不了的文件跳过并记进返回值，出错时不留半截归档。
func Pack(ctx context.Context, dst, src string, entries []Entry, progress func(path string, bytes int64)) ([]Skip, error) {
	if err := rejectInsideSource(dst, src); err != nil {
		return nil, err
	}

	tmp := dst + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}

	w := &archiveWriter{f: out}
	skips, err := writeArchive(ctx, w, src, entries, progress)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return skips, nil
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
	if err := writeArchiveHeader(w); err != nil {
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

func writeArchiveHeader(w io.Writer) error {
	header := make([]byte, archiveHeaderSize)

	copy(header[0:4], archiveMagic)
	binary.LittleEndian.PutUint16(header[4:6], archiveVersion)
	binary.LittleEndian.PutUint16(header[6:8], archiveAlgorithm)
	binary.LittleEndian.PutUint64(header[8:16], uint64(archiveHeaderSize))

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
		defer f.Close()
		in = f
	}

	offset, crc := w.offset, w.crc
	pathBytes := []byte(e.Path)
	crcOffset := offset + crcFieldOffset

	header := make([]byte, recordHeaderSize+len(pathBytes))
	copy(header[recordHeaderSize:], pathBytes)
	header[0] = byte(e.Kind)
	binary.LittleEndian.PutUint32(header[4:8], uint32(e.Mode))
	binary.LittleEndian.PutUint64(header[8:16], uint64(e.Size))
	binary.LittleEndian.PutUint64(header[16:24], uint64(e.ModTime.Unix()))
	binary.LittleEndian.PutUint32(header[24:28], uint32(e.ModTime.Nanosecond()))
	binary.LittleEndian.PutUint32(header[28:32], uint32(len(pathBytes)))

	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	if e.Kind != KindFile {
		return nil, nil
	}

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
	return nil, binary.Write(io.NewOffsetWriter(w.f, crcOffset), binary.LittleEndian, content.Sum64())
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

// ReadIndex 读出归档里的全部记录，顺序同打包时。它只跳过数据区，不校验任何内容。
func ReadIndex(archive string) ([]Record, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	a := &archiveReader{path: archive, f: f}
	err = a.readHeader()
	if err != nil {
		return nil, err
	}

	var records []Record
	err = a.each(func(rec Record) error {
		records = append(records, rec)
		if rec.Kind != KindFile {
			return nil
		}
		_, err := a.f.Seek(paddedSize(rec.Size), io.SeekCurrent)
		return err
	})
	return records, err
}

// Verify 不写盘，检查整份归档：每个文件的内容和记录里的 CRC64 对不对得上，
// 整份归档和公共头里的 CRC64 对不对得上。
//
// 有问题时返回 errs.Corrupted，路径是出问题的文件，看不出是哪个文件时是 archive。
// 归档不完整时 Err 是 io.ErrUnexpectedEOF；文件打不开时错误原样返回。
func Verify(archive string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	a := &archiveReader{path: archive, f: f, sum: crc64.New(crcTable)}
	err = a.readHeader()
	if err == nil {
		err = a.each(func(rec Record) error {
			if rec.Kind != KindFile {
				return nil
			}
			return a.checkData(rec)
		})
	}
	if err == nil && a.sum.Sum64() != a.crc {
		err = errs.Newf(errs.Corrupted, "checksum mismatch").At(archive)
	}
	return err
}

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

// archiveReader 顺序读归档里的记录。sum 不为 nil 时同时算整份归档的校验值。
type archiveReader struct {
	path string
	f    *os.File
	crc  uint64 // 公共头里记的校验值
	sum  hash.Hash64
}

// incomplete 把“归档提前结束了”包成 errs.Corrupted。
// io.EOF 要换成 io.ErrUnexpectedEOF：each 靠 errors.Is(err, io.EOF) 认正常结束，不能让它成立。
func incomplete(err error, format string, args ...any) *errs.Error {
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return errs.Wrapf(err, errs.Corrupted, format, args...)
}

// readHeader 读公共头，把文件位置移到第一条记录。
func (a *archiveReader) readHeader() error {
	var magic [4]byte
	if _, err := io.ReadFull(a.f, magic[:]); err != nil || string(magic[:]) != archiveMagic {
		return errs.New(errs.NotArchive).At(a.path)
	}

	var rest [archiveHeaderSize - 4]byte
	if _, err := io.ReadFull(a.f, rest[:]); err != nil {
		return incomplete(err, "header is incomplete").At(a.path)
	}
	if version := binary.LittleEndian.Uint16(rest[0:2]); version != archiveVersion {
		return errs.Newf(errs.UnsupportedVersion, "version %d", version).At(a.path)
	}
	info, err := a.f.Stat()
	if err != nil {
		return err
	}
	dataOffset := int64(binary.LittleEndian.Uint64(rest[4:12]))
	if dataOffset < archiveHeaderSize || dataOffset > info.Size() {
		return errs.Newf(errs.Corrupted, "data offset %d", dataOffset).At(a.path)
	}

	a.crc = binary.LittleEndian.Uint64(rest[12:20])
	if a.sum != nil {
		a.sum.Write(magic[:])
		a.sum.Write(rest[:12])
		a.sum.Write(zeroCRC[:])
	}
	_, err = a.f.Seek(dataOffset, io.SeekStart)
	return err
}

// next 读下一条记录，把文件位置移到它的数据区开头。归档读完了返回 io.EOF。
func (a *archiveReader) next() (Record, error) {
	var buf [recordHeaderSize]byte
	if _, err := io.ReadFull(a.f, buf[:]); err != nil {
		if err == io.EOF {
			return Record{}, io.EOF
		}
		return Record{}, incomplete(err, "record header is incomplete").At(a.path)
	}

	// 归档是外来数据：负的 Size 会让 ReadIndex 往回跳，超大的路径长度会让下面一次分配几个 GB。
	size := int64(binary.LittleEndian.Uint64(buf[8:16]))
	pathLen := int64(binary.LittleEndian.Uint32(buf[28:32]))
	if size < 0 || pathLen > maxPathBytes {
		return Record{}, errs.Newf(errs.Corrupted, "record header is invalid").At(a.path)
	}

	path := make([]byte, pathLen)
	if _, err := io.ReadFull(a.f, path); err != nil {
		return Record{}, incomplete(err, "record path is incomplete").At(a.path)
	}
	if a.sum != nil {
		a.sum.Write(buf[:crcFieldOffset])
		a.sum.Write(zeroCRC[:])
		a.sum.Write(buf[crcFieldOffset+crcSize:])
		a.sum.Write(path)
	}

	sec := int64(binary.LittleEndian.Uint64(buf[16:24]))
	nsec := int64(binary.LittleEndian.Uint32(buf[24:28]))
	return Record{Entry: Entry{
		Path:    string(path),
		Kind:    Kind(buf[0]),
		Size:    size,
		Mode:    fs.FileMode(binary.LittleEndian.Uint32(buf[4:8])),
		ModTime: time.Unix(sec, nsec),
	}, CRC64: binary.LittleEndian.Uint64(buf[crcFieldOffset : crcFieldOffset+crcSize])}, nil
}

func (a *archiveReader) each(fn func(Record) error) error {
	for {
		rec, err := a.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
}

// checkData 读掉一条文件记录的数据区，核对文件内容的 CRC64。
func (a *archiveReader) checkData(rec Record) error {
	src := io.TeeReader(a.f, a.sum)
	content := crc64.New(crcTable)
	if _, err := io.CopyN(content, src, rec.Size); err != nil {
		return incomplete(err, "data is incomplete").At(rec.Path)
	}
	if _, err := io.CopyN(io.Discard, src, paddedSize(rec.Size)-rec.Size); err != nil {
		return incomplete(err, "padding is incomplete").At(rec.Path)
	}
	if content.Sum64() != rec.CRC64 {
		return errs.Newf(errs.Corrupted, "checksum mismatch").At(rec.Path)
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
