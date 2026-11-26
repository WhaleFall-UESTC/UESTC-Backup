package backup

import (
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"hash"
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
	copy(h.enc.mac[:], h.encRaw[encMACOffset:])
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

// decryptArchive 核对口令、认证整份归档，再把密文解到临时文件。
// 口令不对或者认证不过时一个字节都不解
func decryptArchive(f *os.File, path, password string, h header) (*os.File, func(), error) {
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if (info.Size()-h.dataOffset)%aesBlockSize != 0 {
		return nil, nil, errs.Newf(errs.Corrupted, "ciphertext is %d bytes", info.Size()-h.dataOffset).At(path)
	}
	keys, err := unseal(f, path, password, h)
	if err != nil {
		return nil, nil, err
	}
	// CRC64 和认证标签刚才都核过了，这里只管解密
	if _, err := f.Seek(h.dataOffset, io.SeekStart); err != nil {
		return nil, nil, err
	}
	transform := func(r io.Reader, w io.Writer) error {
		err := aescbc.Decrypt(r, w, keys.Encrypt, h.enc.iv[:])
		if errors.Is(err, aescbc.ErrPadding) {
			return errs.Wrap(err, errs.Corrupted)
		}
		return err
	}
	plain, cleanup, err := transformToTemp(f, transform)
	if err != nil {
		var e *errs.Error
		if errors.As(err, &e) {
			return nil, nil, e.At(path)
		}
		return nil, nil, err
	}
	return plain, cleanup, nil
}

// unseal 核对口令，再核对存盘字节的 CRC64 和认证标签，都过了才交出这份归档的密钥
func unseal(f *os.File, path, password string, h header) (aescbc.Keys, error) {
	// 迭代次数是归档说了算的，先卡住上限再去派生，不然一份归档就能让人算上几分钟
	if h.enc.iterations == 0 || h.enc.iterations > aescbc.MaxIterations {
		return aescbc.Keys{}, errs.Newf(errs.Corrupted, "iterations %d", h.enc.iterations).At(path)
	}
	keys, err := aescbc.DeriveKeys(password, h.enc.salt[:], int(h.enc.iterations))
	if err != nil {
		return aescbc.Keys{}, errs.Wrap(err, errs.Corrupted).At(path)
	}
	if verifier := keys.Verifier(); subtle.ConstantTimeCompare(verifier[:], h.enc.verifier[:]) != 1 {
		return aescbc.Keys{}, errs.New(errs.WrongPassword).At(path)
	}
	if err := checkSeal(f, h, keys); err != nil {
		return aescbc.Keys{}, err.At(path)
	}
	return keys, nil
}

// checkSeal 把存盘的字节从头到尾读一遍，核对公共头里的 CRC64 和扩展头里的认证标签
func checkSeal(f *os.File, h header, keys aescbc.Keys) *errs.Error {
	crc := crc64.New(crcTable)
	mac := keys.MAC()
	if err := readSeal(f, h, io.MultiWriter(crc, mac)); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	if crc.Sum64() != h.crc {
		return errs.Newf(errs.Corrupted, "checksum mismatch")
	}
	if subtle.ConstantTimeCompare(mac.Sum(nil), h.enc.mac[:]) != 1 {
		return errs.Newf(errs.Corrupted, "authentication failed")
	}
	return nil
}

// readSeal 把归档存盘的字节从头到尾读进 w。
// CRC64 和认证标签是回填的，算的时候按 0 计入，这里也换成 0
func readSeal(f *os.File, h header, w io.Writer) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, _ = w.Write(h.raw[:archiveHeaderSize-crcSize])
	_, _ = w.Write(zeroCRC[:])
	_, _ = w.Write(h.encRaw[:encMACOffset])
	_, _ = w.Write(zeroMAC[:])
	if _, err := f.Seek(h.dataOffset, io.SeekStart); err != nil {
		return err
	}
	_, err := io.Copy(w, f)
	return err
}

// uncompressArchive 把压缩的归档解到临时文件，顺带核对存盘字节的 CRC64
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
	crc := crc64.New(crcTable)
	_, _ = crc.Write(h.raw[:archiveHeaderSize-crcSize])
	_, _ = crc.Write(zeroCRC[:])
	transform := func(r io.Reader, w io.Writer) error {
		return decompress(h.algorithm, r, w)
	}
	return toTemp(h, crc, f, path, transform)
}

// toTemp 把 r 经 transform 写进临时文件，同时核对存盘字节的 CRC64。
// crc 是读到 r 之前已经算了一段的校验值，r 读到的字节都算进去，变换没读完的尾部也要算
func toTemp(h header, crc hash.Hash64, r io.Reader, path string, transform func(io.Reader, io.Writer) error) (*os.File, func(), error) {
	plain, cleanup, err := transformToTemp(io.TeeReader(r, crc), drainAfter(transform))
	if err == nil && crc.Sum64() != h.crc {
		cleanup()
		err = errs.Newf(errs.Corrupted, "checksum mismatch")
	}
	if err != nil {
		var e *errs.Error
		if errors.As(err, &e) {
			return nil, nil, e.At(path)
		}
		return nil, nil, err
	}
	return plain, cleanup, nil
}

// drainAfter 让 transform 跑完之后把 r 剩下的字节读干净
func drainAfter(transform func(io.Reader, io.Writer) error) func(io.Reader, io.Writer) error {
	return func(r io.Reader, w io.Writer) error {
		if err := transform(r, w); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, r)
		return err
	}
}

// transformToTemp 把 r 经 transform 写进临时文件，返回位置在开头的文件；出错时不留文件
func transformToTemp(r io.Reader, transform func(io.Reader, io.Writer) error) (*os.File, func(), error) {
	tmp, err := os.CreateTemp("", "dbkp-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}
	err = transform(r, tmp)
	if err == nil {
		_, err = tmp.Seek(0, io.SeekStart)
	}
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return tmp, cleanup, nil
}
