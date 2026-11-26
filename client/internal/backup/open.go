package backup

import (
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"hash/crc64"
	"io"
	"os"

	"client/internal/aescbc"
	"client/internal/errs"
)

// header 是公共头里的字段，Algorithm 为 AES 时还带着扩展头
type header struct {
	algorithm  Algorithm
	dataOffset int64
	crc        uint64
	raw        []byte // 读到的 24 个字节
	enc        encHeader
	encRaw     []byte // 读到的整个加密扩展头
}

// readHeader 读公共头并校验魔数、版本和算法；加密归档连扩展头一起读
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
	if h.algorithm == AlgorithmAES {
		if err := h.readEnc(r, path); err != nil {
			return header{}, err
		}
	}
	return h, nil
}

// readEnc 读加密扩展头，要求数据偏移正好指向密文
func (h *header) readEnc(r io.Reader, path string) error {
	h.encRaw = make([]byte, archiveEncHeaderSize)
	if _, err := io.ReadFull(r, h.encRaw); err != nil {
		return incomplete(err, "encryption header is incomplete").At(path)
	}
	if h.dataOffset != archiveEncDataOffset {
		return errs.Newf(errs.Corrupted, "data offset %d", h.dataOffset).At(path)
	}
	copy(h.enc.salt[:], h.encRaw[encSaltOffset:])
	copy(h.enc.iv[:], h.encRaw[encIVOffset:])
	h.enc.iterations = binary.LittleEndian.Uint32(h.encRaw[encIterOffset:])
	copy(h.enc.verifier[:], h.encRaw[encVerifierOffset:])
	return nil
}

// openArchive 打开归档，按需解密、解压到临时文件，返回位置在开头的内层 .dbkp。
// password 在未加密的归档上被忽略；调用方必须调用返回的 cleanup
func openArchive(path, password string) (*os.File, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	h, err := readHeader(f, path)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}

	cleanup := func() { _ = f.Close() }
	if h.algorithm == AlgorithmAES {
		if f, cleanup, err = decryptArchive(f, path, password, h); err != nil {
			return nil, nil, err
		}
		// 解出来的是内层归档，它自己的公共头说明压缩算法
		if h, err = readHeader(f, path); err != nil {
			cleanup()
			return nil, nil, err
		}
		if h.algorithm == AlgorithmAES {
			cleanup()
			return nil, nil, errs.Newf(errs.UnsupportedVersion, "nested encryption").At(path)
		}
	}
	if h.algorithm == AlgorithmNone {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			cleanup()
			return nil, nil, err
		}
		return f, cleanup, nil
	}

	inner, innerCleanup, err := uncompressArchive(f, path, h)
	cleanup() // uncompressArchive 已经关掉了 f，这里再清掉可能残留的临时文件
	if err != nil {
		return nil, nil, err
	}
	return inner, innerCleanup, nil
}

// decryptArchive 先核对口令，再把密文解到临时文件；存盘字节的 CRC64 对不上就删掉重来。
// 口令不对时一个字节都不解
func decryptArchive(f *os.File, path, password string, h header) (*os.File, func(), error) {
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if (info.Size()-h.dataOffset)%aesBlockSize != 0 {
		return nil, nil, errs.Newf(errs.Corrupted, "ciphertext is %d bytes", info.Size()-h.dataOffset).At(path)
	}
	// 迭代次数是归档说了算，先卡住上限再去派生，不然一份归档就能让人算上几分钟
	if h.enc.iterations == 0 || h.enc.iterations > aescbc.MaxIterations {
		return nil, nil, errs.Newf(errs.Corrupted, "iterations %d", h.enc.iterations).At(path)
	}
	keys, err := aescbc.DeriveKeys(password, h.enc.salt[:], int(h.enc.iterations))
	if err != nil {
		return nil, nil, errs.Wrap(err, errs.Corrupted).At(path)
	}
	if verifier := keys.Verifier(); subtle.ConstantTimeCompare(verifier[:], h.enc.verifier[:]) != 1 {
		return nil, nil, errs.New(errs.WrongPassword).At(path)
	}
	transform := func(r io.Reader, w io.Writer) error {
		err := aescbc.Decrypt(r, w, keys.Encrypt, h.enc.iv[:])
		if errors.Is(err, aescbc.ErrPadding) {
			return errs.Wrap(err, errs.Corrupted)
		}
		return err
	}
	return toTemp(h, h.encRaw, f, path, transform)
}

func uncompressArchive(f *os.File, path string, h header) (*os.File, func(), error) {
	defer func() { _ = f.Close() }()

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
	transform := func(r io.Reader, w io.Writer) error {
		return decompress(h.algorithm, r, w)
	}
	return toTemp(h, nil, f, path, transform)
}

// toTemp 把 r 经 transform 写进临时文件，同时核对存盘字节的 CRC64；
// extra 是记录区之前、不属于公共头的字节（加密扩展头），校验时一并算进去
func toTemp(h header, extra []byte, r io.Reader, path string, transform func(io.Reader, io.Writer) error) (*os.File, func(), error) {
	tmp, err := os.CreateTemp("", "dbkp-*")
	if err != nil {
		return nil, nil, err
	}
	crc := crc64.New(crcTable)
	_, _ = crc.Write(h.raw[:archiveHeaderSize-crcSize])
	_, _ = crc.Write(zeroCRC[:])
	_, _ = crc.Write(extra)
	src := io.TeeReader(r, crc)
	err = transform(src, tmp)
	if err == nil {
		// 变换没读完的尾部也要算进校验值
		_, err = io.Copy(io.Discard, src)
	}
	if err == nil && crc.Sum64() != h.crc {
		err = errs.Newf(errs.Corrupted, "checksum mismatch").At(path)
	}
	if err == nil {
		_, err = tmp.Seek(0, io.SeekStart)
	}
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		var e *errs.Error
		if errors.As(err, &e) {
			return nil, nil, e.At(path)
		}
		return nil, nil, err
	}
	return tmp, func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}, nil
}
