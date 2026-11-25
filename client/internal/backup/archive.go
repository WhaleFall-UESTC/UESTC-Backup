package backup

import (
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

// Pack 把 entries 列出的节点从 src 读出来，连同文件内容写成一份 .dbkp 归档放到 dst。
// dst 已存在就覆盖。
//
// 某条记录出错时返回带路径的 *errs.Error：文件读不出来是 errs.IO，Err 是底层错误；
// 文件比 Entry.Size 短是 errs.SourceChanged，Err 是 io.ErrUnexpectedEOF；
// Entry.Size 是负数是 errs.InvalidArgument。其余错误原样返回。
func Pack(dst, src string, entries []Entry) error {
	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	w := &archiveWriter{f: out, sum: crc64.New(crcTable)}
	err = writeArchive(w, src, entries)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

func writeArchive(w *archiveWriter, src string, entries []Entry) error {
	if err := writeArchiveHeader(w); err != nil {
		return err
	}
	for _, e := range entries {
		if err := writeArchiveRecord(w, src, e); err != nil {
			return err
		}
	}
	// 回填公共头里的校验值
	return binary.Write(io.NewOffsetWriter(w.f, archiveHeaderSize-crcSize), binary.LittleEndian, w.sum.Sum64())
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

func writeArchiveRecord(w *archiveWriter, src string, e Entry) error {
	if e.Size < 0 {
		return errs.Newf(errs.InvalidArgument, "size %d", e.Size).At(e.Path)
	}

	pathBytes := []byte(e.Path)
	crcOffset := w.offset + crcFieldOffset

	// CRC64 先留 0，文件内容写完后回填
	header := make([]byte, recordHeaderSize+len(pathBytes))
	copy(header[recordHeaderSize:], pathBytes)
	header[0] = byte(e.Kind)
	binary.LittleEndian.PutUint32(header[4:8], uint32(e.Mode))
	binary.LittleEndian.PutUint64(header[8:16], uint64(e.Size))
	binary.LittleEndian.PutUint64(header[16:24], uint64(e.ModTime.Unix()))
	binary.LittleEndian.PutUint32(header[24:28], uint32(e.ModTime.Nanosecond()))
	binary.LittleEndian.PutUint32(header[28:32], uint32(len(pathBytes)))

	if _, err := w.Write(header); err != nil {
		return err
	}
	if e.Kind != KindFile {
		return nil
	}

	in, err := os.Open(filepath.Join(src, filepath.FromSlash(e.Path)))
	if err != nil {
		return errs.Wrap(err, errs.IO).At(e.Path)
	}
	defer in.Close()

	content := crc64.New(crcTable)
	n, err := io.CopyN(io.MultiWriter(w, content), in, e.Size)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// 源文件不够长。同 incomplete，不把 io.EOF 接进错误链
			return errs.Wrapf(io.ErrUnexpectedEOF, errs.SourceChanged, "read %d of %d bytes", n, e.Size).At(e.Path)
		}
		return errs.Wrap(err, errs.IO).At(e.Path)
	}
	if err := pad(w, e.Size); err != nil {
		return err
	}
	return binary.Write(io.NewOffsetWriter(w.f, crcOffset), binary.LittleEndian, content.Sum64())
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

// archiveWriter 顺序写归档，同时算整份归档的校验值。
// 回填 CRC64 字段时不经过它，所以校验值里这些字段按 0 算。
type archiveWriter struct {
	f      *os.File
	sum    hash.Hash64
	offset int64 // 已写的字节数
}

func (w *archiveWriter) Write(p []byte) (int, error) {
	n, err := w.f.Write(p)
	w.sum.Write(p[:n])
	w.offset += int64(n)
	return n, err
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
