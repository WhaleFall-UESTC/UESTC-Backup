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
	copy(h.enc.mac[:], h.encRaw[encMACOffset:])
	return nil
}

// openArchive 打开归档，按需解密、解压到暂存目录，返回位置在开头的内层 .dbkp。
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
	if h.algorithm == AlgorithmNone {
		// 不压缩不加密的归档直接读原文件，没有明文要暂存
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			_ = f.Close()
			return nil, nil, err
		}
		return f, func() { _ = f.Close() }, nil
	}

	// 解密、解压出来的都是明文，放到本机的暂存目录，用完连目录一起删
	stage, err := newStaging()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	cleanup := func() { stage.remove() }
	plain := f
	if h.algorithm == AlgorithmAES {
		// 解出来的是内层归档，它自己的公共头说明压缩算法
		if h, plain, err = decryptArchive(f, path, password, h, stage); err != nil {
			cleanup()
			return nil, nil, err
		}
		if h.algorithm == AlgorithmAES {
			cleanup()
			return nil, nil, errs.Newf(errs.UnsupportedVersion, "nested encryption").At(path)
		}
	}
	if h.algorithm == AlgorithmNone {
		return plain, cleanup, nil
	}
	inner, err := uncompressArchive(plain, path, h, stage)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return inner, cleanup, nil
}

// decryptArchive 核对口令、认证整份归档，再把密文解进暂存目录，返回解密后的文件和它开头的公共头。
// 口令不对或者认证不过时一个字节都不解
func decryptArchive(f *os.File, path, password string, h header, stage *staging) (header, *os.File, error) {
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return header{}, nil, err
	}
	if (info.Size()-h.dataOffset)%aesBlockSize != 0 {
		return header{}, nil, errs.Newf(errs.Corrupted, "ciphertext is %d bytes", info.Size()-h.dataOffset).At(path)
	}
	keys, err := unseal(f, path, password, h)
	if err != nil {
		return header{}, nil, err
	}
	// CRC64 和认证标签刚才都核过了，这里只管解密
	plain, err := stage.file("decrypted")
	if err != nil {
		return header{}, nil, err
	}
	if _, err := f.Seek(h.dataOffset, io.SeekStart); err != nil {
		return header{}, nil, err
	}
	err = aescbc.Decrypt(f, plain, keys.Encrypt, h.enc.iv[:])
	if errors.Is(err, aescbc.ErrPadding) {
		err = errs.Wrap(err, errs.Corrupted).At(path)
	}
	if err != nil {
		return header{}, nil, err
	}
	if _, err := plain.Seek(0, io.SeekStart); err != nil {
		return header{}, nil, err
	}
	inner, err := readHeader(plain, path)
	if err != nil {
		return header{}, nil, err
	}
	// 读头把位置推进了，调用方还要从头读一遍内层归档
	if _, err := plain.Seek(0, io.SeekStart); err != nil {
		return header{}, nil, err
	}
	return inner, plain, nil
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

// uncompressArchive 把压缩的归档解进暂存目录，顺带核对存盘字节的 CRC64；读完关掉 f
func uncompressArchive(f *os.File, path string, h header, stage *staging) (*os.File, error) {
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if h.dataOffset < archiveHeaderSize || h.dataOffset > info.Size() {
		return nil, errs.Newf(errs.Corrupted, "data offset %d", h.dataOffset).At(path)
	}
	if _, err := f.Seek(h.dataOffset, io.SeekStart); err != nil {
		return nil, err
	}
	plain, err := stage.file("uncompressed")
	if err != nil {
		return nil, err
	}
	crc := crc64.New(crcTable)
	_, _ = crc.Write(h.raw[:archiveHeaderSize-crcSize])
	_, _ = crc.Write(zeroCRC[:])
	src := io.TeeReader(f, crc)
	err = decompress(h.algorithm, src, plain)
	if err == nil {
		// 解压器没读完的尾部也要算进校验值
		_, err = io.Copy(io.Discard, src)
	}
	if err == nil && crc.Sum64() != h.crc {
		err = errs.Newf(errs.Corrupted, "checksum mismatch")
	}
	if err == nil {
		_, err = plain.Seek(0, io.SeekStart)
	}
	if err != nil {
		var e *errs.Error
		if errors.As(err, &e) {
			return nil, e.At(path)
		}
		return nil, err
	}
	return plain, nil
}
