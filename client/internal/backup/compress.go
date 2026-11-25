package backup

import (
	"io"

	"client/internal/errs"
	"client/internal/huffman"
	"client/internal/lz77"
)

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
