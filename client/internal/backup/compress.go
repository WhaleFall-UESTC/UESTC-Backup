package backup

import (
	"context"
	"encoding/binary"
	"io"
	"os"

	"client/internal/errs"
	"client/internal/huffman"
	"client/internal/lz77"
)

// compressFile 把 raw 压缩进 tmp，返回写出的字节数
func compressFile(ctx context.Context, raw, tmp string, algo Algorithm) (int64, error) {
	in, err := os.Open(raw)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}

	w := &archiveWriter{f: out}
	err = writeArchiveHeader(w, algo)
	if err == nil {
		err = compress(ctx, algo, in, w)
	}
	if err == nil {
		err = binary.Write(io.NewOffsetWriter(out, archiveHeaderSize-crcSize), binary.LittleEndian, w.crc)
	}
	size := w.offset
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return size, err
}

// compress 按 algo 把 r 从头编码写到 w，Huffman 要扫两遍所以 r 要能 Seek
func compress(ctx context.Context, algo Algorithm, r io.ReadSeeker, w io.Writer) error {
	src := ctxReader{ctx: ctx, r: r}
	switch algo {
	case AlgorithmHuffman:
		return huffman.Compress(src, w)
	case AlgorithmLZ77:
		return lz77.Compress(src, w)
	}
	return errs.Newf(errs.InvalidArgument, "unknown algorithm %d", algo)
}

// decompress 按 algo 把压缩数据解开写到 w
func decompress(algo Algorithm, r io.Reader, w io.Writer) error {
	switch algo {
	case AlgorithmHuffman:
		return huffman.Decompress(r, w)
	case AlgorithmLZ77:
		return lz77.Decompress(r, w)
	}
	return errs.Newf(errs.InvalidArgument, "unknown algorithm %d", algo)
}

// ctxReader 在每次读之前检查取消，让压缩途中也能停下来
type ctxReader struct {
	ctx context.Context
	r   io.ReadSeeker
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
func (c ctxReader) Seek(offset int64, whence int) (int64, error) {
	return c.r.Seek(offset, whence)
}
