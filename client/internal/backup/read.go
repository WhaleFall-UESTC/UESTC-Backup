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
	"time"

	"client/internal/errs"
)

// ReadIndex 读出归档里的全部记录，顺序同打包时。它只跳过数据区，不校验记录内容
func ReadIndex(archive, password string) ([]Record, error) {
	f, cleanup, err := openArchive(archive, password)
	if err != nil {
		return nil, err
	}
	defer cleanup()

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

// Stats 是一次操作覆盖的范围
type Stats struct {
	Files int
	Bytes int64
}

// VerifyOptions 是校验的可选参数
type VerifyOptions struct {
	Progress func(Progress)
}

// Verify 不写盘，检查整份归档：每个文件的内容和记录里的 CRC64 对不对得上，
// 整份归档和公共头里的 CRC64 对不对得上。
//
// 有问题时返回 errs.Corrupted，路径是出问题的文件，看不出是哪个文件时是 archive。
// 归档不完整时 Err 是 io.ErrUnexpectedEOF；文件打不开时错误原样返回。
func Verify(ctx context.Context, archive, password string, opts VerifyOptions) error {
	f, cleanup, err := openArchive(archive, password)
	if err != nil {
		return err
	}
	defer cleanup()
	_, err = verifyArchive(ctx, archive, f, opts.Progress)
	return err
}

// verifyArchive 核对整份归档，顺带数出文件数和总字节数
func verifyArchive(ctx context.Context, path string, f *os.File, progress func(Progress)) (Stats, error) {
	stats, err := countIndex(path, f)
	if err != nil {
		return Stats{}, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Stats{}, err
	}

	a := &archiveReader{path: path, f: f, sum: crc64.New(crcTable)}
	err = a.readHeader()
	var doneFiles int
	var doneBytes int64
	if err == nil {
		err = a.each(func(rec Record) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if rec.Kind != KindFile {
				return nil
			}
			if err := a.checkData(rec); err != nil {
				return err
			}
			doneFiles++
			doneBytes += rec.Size
			if progress != nil {
				progress(Progress{Phase: PhaseVerifying, Path: rec.Path, DoneFiles: doneFiles, TotalFiles: stats.Files, DoneBytes: doneBytes, TotalBytes: stats.Bytes})
			}
			return nil
		})
	}
	if err == nil && a.sum.Sum64() != a.crc {
		err = errs.Newf(errs.Corrupted, "checksum mismatch").At(path)
	}
	return stats, err
}

// countIndex 数出文件数和总字节数，只读记录头，不碰数据区。读完文件位置不定
func countIndex(path string, f *os.File) (Stats, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Stats{}, err
	}
	a := &archiveReader{path: path, f: f}
	if err := a.readHeader(); err != nil {
		return Stats{}, err
	}
	var stats Stats
	err := a.each(func(rec Record) error {
		if rec.Kind != KindFile {
			return nil
		}
		stats.Files++
		stats.Bytes += rec.Size
		_, err := f.Seek(paddedSize(rec.Size), io.SeekCurrent)
		return err
	})
	return stats, err
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
func incomplete(err error, detail string) *errs.Error {
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return &errs.Error{Code: errs.Corrupted, Detail: detail, Err: err}
}

// readHeader 读公共头，把文件位置移到第一条记录。
func (a *archiveReader) readHeader() error {
	h, err := readHeader(a.f, a.path)
	if err != nil {
		return err
	}
	if a.sum != nil {
		_, _ = a.sum.Write(h.raw[:archiveHeaderSize-crcSize])
		_, _ = a.sum.Write(zeroCRC[:])
	}
	info, err := a.f.Stat()
	if err != nil {
		return err
	}
	if h.dataOffset < archiveHeaderSize || h.dataOffset > info.Size() {
		return errs.Newf(errs.Corrupted, "data offset %d", h.dataOffset).At(a.path)
	}

	a.crc = h.crc
	_, err = a.f.Seek(h.dataOffset, io.SeekStart)
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
		_, _ = a.sum.Write(buf[:crcFieldOffset])
		_, _ = a.sum.Write(zeroCRC[:])
		_, _ = a.sum.Write(buf[crcFieldOffset+crcSize:])
		_, _ = a.sum.Write(path)
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
