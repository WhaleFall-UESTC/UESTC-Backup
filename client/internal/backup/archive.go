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

// Skip 是打包时跳过的文件。
type Skip struct {
	Path string
	Err  error
}
