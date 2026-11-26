package backup

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"os"

	"client/internal/aescbc"
	"client/internal/errs"
)

// kdfIterations 是打包时口令派生的迭代次数，测试里换成小值
var kdfIterations = aescbc.Iterations

// encryptFile 用本次随机生成的盐和 IV 把 raw 加密进 dst
func encryptFile(ctx context.Context, raw, dst, password string) error {
	seeds := make([]byte, aesSaltSize+aesIVSize)
	if _, err := rand.Read(seeds); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	var h encHeader
	copy(h.salt[:], seeds[:aesSaltSize])
	copy(h.iv[:], seeds[aesSaltSize:])
	h.iterations = uint32(kdfIterations)
	return sealArchive(ctx, raw, dst, password, h)
}

// sealArchive 按字段表写公共头和扩展头，再把 raw 加密成密文；失败时删掉半截文件
func sealArchive(ctx context.Context, raw, dst, password string, h encHeader) error {
	keys, err := aescbc.DeriveKeys(password, h.salt[:], int(h.iterations))
	if err != nil {
		return errs.Wrap(err, errs.InvalidArgument)
	}
	h.verifier = keys.Verifier()

	in, err := os.Open(raw)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	w := &archiveWriter{f: out}

	err = writeArchiveHeader(w, AlgorithmAES)
	if err == nil {
		_, err = w.Write(h.bytes())
	}
	if err == nil {
		err = aescbc.Encrypt(ctxReader{ctx: ctx, r: in}, w, keys.Encrypt, h.iv[:])
	}
	if err == nil {
		// 回填公共头里的校验值
		err = binary.Write(io.NewOffsetWriter(out, archiveHeaderSize-crcSize), binary.LittleEndian, w.crc)
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
