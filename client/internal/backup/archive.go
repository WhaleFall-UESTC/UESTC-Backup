package backup

import (
	"hash/crc64"
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
//	     6     2  Algorithm    0 不压缩 / 1 Huffman / 2 LZ77 / 3 AES-256-CBC
//	     8     8  DataOffset   第一条记录的偏移：3 时是 88，其余是 24
//	    16     8  CRC64        整份归档的校验值
//
//	Algorithm 为 0 时，公共头后面直接是记录。为 1、2 时，后面是压过的整个归档：
//	解开得到一份完整的、Algorithm 为 0 的 .dbkp。压缩不比原样小就改存原样，
//	Algorithm 仍是 0。
//
//	Algorithm 为 3 时，公共头后面是 64 字节的加密扩展头，再后面是密文。密文解出来
//	是一份完整的 .dbkp，它自己的公共头说明压缩算法；加密作用在压缩之后。
//
//	  偏移  宽度  字段         取值
//	    24    16  Salt         盐，每次加密随机
//	    40    16  IV           CBC 的初始向量，每次加密随机
//	    56    32  Verifier     密钥的 SHA-256，用来核对口令
//
//	密文按 PKCS#7 补齐，长度是 16 的整数倍。
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
// 两个 CRC64 都是 CRC-64/ECMA。公共头的 CRC64 覆盖存盘的那份字节，
// 但各处的 CRC64 字段本身按 0 计入。
const (
	archiveMagic         = "DBKP"
	archiveVersion       = uint16(1)
	archiveHeaderSize    = int64(24)
	archiveEncHeaderSize = 64
	archiveEncDataOffset = archiveHeaderSize + archiveEncHeaderSize
	recordHeaderSize     = 40
	archiveAlignment     = int64(512)

	aesBlockSize    = 16
	aesSaltSize     = 16
	aesIVSize       = aesBlockSize
	aesVerifierSize = 32

	crcFieldOffset = 32 // 记录头里 CRC64 的偏移
	crcSize        = 8
	maxPathBytes   = 1 << 20
)

// Algorithm 是作用在存盘数据上的变换，取值写进公共头的 Algorithm 字段
type Algorithm uint16

// Algorithm 的取值，含义见文件开头的布局说明
const (
	AlgorithmNone    Algorithm = 0
	AlgorithmHuffman Algorithm = 1
	AlgorithmLZ77    Algorithm = 2
	AlgorithmAES     Algorithm = 3
)

// known 是 ReadIndex、Verify、Unpack 认得的取值
func (a Algorithm) known() bool { return a.compressible() || a == AlgorithmAES }

// compressible 是 PackOptions.Algorithm 能取的值
func (a Algorithm) compressible() bool {
	return a == AlgorithmNone || a == AlgorithmHuffman || a == AlgorithmLZ77
}

// headerDataOffset 是公共头之后第一条记录的偏移
func headerDataOffset(algo Algorithm) int64 {
	if algo == AlgorithmAES {
		return archiveEncDataOffset
	}
	return archiveHeaderSize
}

// encHeader 是加密扩展头：盐、IV 和口令校验值
type encHeader struct {
	salt     [aesSaltSize]byte
	iv       [aesIVSize]byte
	verifier [aesVerifierSize]byte
}

// bytes 按字段表拼出扩展头的 64 个字节
func (h encHeader) bytes() []byte {
	b := make([]byte, archiveEncHeaderSize)
	copy(b[0:16], h.salt[:])
	copy(b[16:32], h.iv[:])
	copy(b[32:64], h.verifier[:])
	return b
}

var (
	crcTable = crc64.MakeTable(crc64.ECMA)
	zeroCRC  [crcSize]byte
)

// Skip 是打包时跳过的文件。
type Skip struct {
	Path string
	Err  error
}
