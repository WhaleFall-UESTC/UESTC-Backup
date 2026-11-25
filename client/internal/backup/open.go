package backup

import (
	"encoding/binary"
	"errors"
	"hash/crc64"
	"io"
	"os"

	"client/internal/errs"
)

// header 是公共头里的字段
type header struct {
	algorithm  Algorithm
	dataOffset int64
	crc        uint64
	raw        []byte // 读到的 24 个字节
}

// readHeader 读公共头并校验魔数、版本和算法
func readHeader(r io.Reader, path string) (header, error) {
	var h header
	h.raw = make([]byte, archiveHeaderSize)
	n, err := io.ReadFull(r, h.raw)
	if n < len(archiveMagic) || string(h.raw[:len(archiveMagic)]) != archiveMagic {
		return header{}, errs.New(errs.NotArchive).At(path)
	}
	if err != nil {
		return header{}, incomplete(err, "header is incomplete").At(path)
	}
	if version := binary.LittleEndian.Uint16(h.raw[4:6]); version != archiveVersion {
		return header{}, errs.Newf(errs.UnsupportedVersion, "version %d", version).At(path)
	}
	h.algorithm = Algorithm(binary.LittleEndian.Uint16(h.raw[6:8]))
	if !h.algorithm.known() {
		return header{}, errs.Newf(errs.UnsupportedVersion, "algorithm %d", h.algorithm).At(path)
	}
	h.dataOffset = int64(binary.LittleEndian.Uint64(h.raw[8:16]))
	h.crc = binary.LittleEndian.Uint64(h.raw[16:24])
	return h, nil
}

// openArchive 打开归档，返回可读的归档文件，位置在开头。压缩归档先解压到临时文件，
// 顺带核对存盘字节的 CRC64；调用方必须调用返回的 cleanup
func openArchive(path string) (*os.File, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	h, err := readHeader(f, path)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if h.algorithm == AlgorithmNone {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, nil, err
		}
		return f, func() { f.Close() }, nil
	}
	return uncompressArchive(f, path, h)
}
func uncompressArchive(f *os.File, path string, h header) (*os.File, func(), error) {
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if h.dataOffset < archiveHeaderSize || h.dataOffset > info.Size() {
		return nil, nil, errs.Newf(errs.Corrupted, "data offset %d", h.dataOffset).At(path)
	}
	if _, err := f.Seek(h.dataOffset, io.SeekStart); err != nil {
		return nil, nil, err
	}

	tmp, err := os.CreateTemp("", "dbkp-*")
	if err != nil {
		return nil, nil, err
	}
	crc := crc64.New(crcTable)
	crc.Write(h.raw[:archiveHeaderSize-crcSize])
	crc.Write(zeroCRC[:])
	src := io.TeeReader(f, crc)
	err = decompress(h.algorithm, src, tmp)
	if err == nil {
		// 解压没读完的尾部也要算进校验值
		_, err = io.Copy(io.Discard, src)
	}
	if err == nil && crc.Sum64() != h.crc {
		err = errs.Newf(errs.Corrupted, "checksum mismatch").At(path)
	}
	if err == nil {
		_, err = tmp.Seek(0, io.SeekStart)
	}
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		var e *errs.Error
		if errors.As(err, &e) {
			return nil, nil, e.At(path)
		}
		return nil, nil, err
	}
	return tmp, func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}, nil
}
