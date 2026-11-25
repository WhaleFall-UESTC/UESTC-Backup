package backup

import (
	"bytes"
	"client/internal/errs"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 归档大小必须和按布局算出来的字节数完全相等。
func TestPackSizeFollowsLayout(t *testing.T) {
	sizes := []int{0, 1, 511, 512, 513, 1024, 300 << 10}

	root := t.TempDir()
	for _, n := range sizes {
		writeFile(t, filepath.Join(root, fmt.Sprintf("f%06d.bin", n)), string(dataBytes(n)))
	}

	entries := scanOK(t, root)
	archive := packTree(t, root, entries)

	var want int64 = layoutHeaderSize
	for _, e := range entries {
		want += layoutRecordHeaderSize + int64(len(e.Path))
		if e.Kind == KindFile {
			want += layoutPaddedSize(e.Size)
		}
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatalf("Setup failed: stat the archive: %v", err)
	}
	if got := info.Size(); got != want {
		t.Errorf("Pack() wrote %d bytes, want %d", got, want)
	}
}

// 课件把路径放在固定 200 字节里，这里用"长度 + 变长"，超过 200 字节的路径也要能原样往返。
func TestPackLongPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, strings.Repeat("长", 80)+".txt"), "hello") // 文件名 244 字节

	entries := scanOK(t, root)
	if len(entries) != 1 || len(entries[0].Path) <= 200 {
		t.Errorf("Scan() returned %v, want a single path longer than 200 bytes", paths(entries))
		return
	}
	if diff := diffRecords(readIndexOK(t, packTree(t, root, entries)), entries); diff != nil {
		t.Errorf("ReadIndex() %v", diff)
	}
}

// 记录里的 CRC64 要和第 1 步的 FileCRC64 算出来的一致；目录和符号链接没有内容，是 0。
func TestPackRecordsFileCRC64(t *testing.T) {
	root := sampleTree(t)
	writeFile(t, filepath.Join(root, "big.bin"), string(dataBytes(150_000)))
	entries := scanOK(t, root)

	records := readIndexOK(t, packTree(t, root, entries))
	if diff := diffRecords(records, entries); diff != nil {
		t.Fatalf("Setup failed: ReadIndex() %v", diff)
	}
	for i, rec := range records {
		var want uint64
		if entries[i].Kind == KindFile {
			crc, _, err := FileCRC64(filepath.Join(root, filepath.FromSlash(entries[i].Path)))
			if err != nil {
				t.Fatalf("Setup failed: FileCRC64() error = %v", err)
			}
			want = crc
		}
		if got := rec.CRC64; got != want {
			t.Errorf("ReadIndex() record %s: CRC64 = %#x, want %#x", entries[i].Path, got, want)
		}
	}
}

// 布局要一直兼容下去，所以用手工数出来的字节钉死它：字段顺序或宽度一变，这里先失败。
// entries 手工构造、时间取固定值，归档的每个字节都能预先写出来。
// 各字段的取值特意互不相同、不是回文，字节序写反才对不上。
func TestPackLayoutIsFrozen(t *testing.T) {
	src := t.TempDir()
	// d/a 在磁盘上有 300 个 z，记录的 Size 却是 258：多出来的 42 个不能进归档。
	writeFile(t, filepath.Join(src, "d", "a"), string(bytes.Repeat([]byte{'z'}, 300)))

	t.Run("空目录树", func(t *testing.T) {
		archive := packTree(t, src, nil)
		got, err := os.ReadFile(archive)
		if err != nil {
			t.Fatalf("Setup failed: read the archive: %v", err)
		}
		if want := frozenEmptyArchive(); !bytes.Equal(got, want) {
			t.Errorf("Pack() on an empty tree wrote % x, want only the header % x", got, want)
		}
		// 空归档读回来应该是 0 条记录
		if records := readIndexOK(t, archive); len(records) != 0 {
			t.Errorf("ReadIndex() on an empty archive returned %v, want no records", records)
		}
	})

	t.Run("目录、文件、符号链接各一条", func(t *testing.T) {
		// d 的 Size 写 4096：磁盘上它没有这么大，Pack 不能重新 stat 把它改掉。
		entries := []Entry{
			{Path: "d", Kind: KindDir, Size: 4096, Mode: fs.ModeDir | 0o755, ModTime: layoutModTime()},
			{Path: "d/a", Kind: KindFile, Size: 258, Mode: 0o644, ModTime: layoutModTime()},
			{Path: "d/s", Kind: KindSymlink, Size: 3, Mode: fs.ModeSymlink | 0o777, ModTime: layoutModTime()},
		}

		got, want := packBytes(t, src, entries), frozenSampleArchive()
		if !bytes.Equal(got, want) {
			n := min(len(got), len(want))
			first := n
			for i := 0; i < n; i++ {
				if got[i] != want[i] {
					first = i
					break
				}
			}
			t.Errorf("Pack() 从第 %d 个字节起和期望不一致（实际 %d 字节，期望 %d 字节）"+
				"\n实际: % x\n期望: % x", first, len(got), len(want), got, want)
		}
	})
}

// layoutModTime 是布局测试用的固定修改时间
func layoutModTime() time.Time { return time.Unix(1_000_000_000, 123_456_789) }

// frozenHeader 是公共头，字段取值见 archive.go 的字段表
func frozenHeader() []byte {
	return []byte{
		'D', 'B', 'K', 'P', // Magic
		0x01, 0x00, // Version = 1
		0x00, 0x00, // Algorithm = 0
		0x18, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // DataOffset = 24
	}
}

// frozenEmptyArchive 是空目录树的归档应该长什么样。CRC64 是用另一份
// CRC-64/ECMA 实现算出来的，没有借助被测代码
func frozenEmptyArchive() []byte {
	return append(frozenHeader(), 0x9B, 0xF7, 0x46, 0x6E, 0x38, 0x25, 0xC7, 0x03)
}

// frozenSampleArchive 是"目录、文件、符号链接各一条"那份归档应该长什么样
func frozenSampleArchive() []byte {
	zeroCRC := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	want := append(frozenHeader(), 0x44, 0x76, 0x78, 0x5E, 0xB1, 0x36, 0x73, 0xCF)
	want = append(want,
		// 目录 d
		0x02, 0x00, 0x00, 0x00, // Kind = 目录，后面 3 字节保留
		0xED, 0x01, 0x00, 0x80, // Mode = ModeDir | 0755
		0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Size = 4096
	)
	want = append(want, frozenModTime()...)
	want = append(want, 0x01, 0x00, 0x00, 0x00) // PathLen = 1
	want = append(want, zeroCRC...)
	want = append(want, 'd')

	want = append(want,
		// 文件 d/a
		0x01, 0x00, 0x00, 0x00, // Kind = 文件
		0xA4, 0x01, 0x00, 0x00, // Mode = 0644
		0x02, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Size = 258
	)
	want = append(want, frozenModTime()...)
	want = append(want, 0x03, 0x00, 0x00, 0x00)                         // PathLen = 3
	want = append(want, 0xC6, 0x2D, 0x41, 0xBA, 0xA9, 0xB5, 0x38, 0x75) // 258 个 z 的 CRC64
	want = append(want, 'd', '/', 'a')
	want = append(want, bytes.Repeat([]byte{'z'}, 258)...)
	want = append(want, make([]byte, 254)...) // 补零到 512

	want = append(want,
		// 符号链接 d/s，没有数据区
		0x03, 0x00, 0x00, 0x00, // Kind = 符号链接
		0xFF, 0x01, 0x00, 0x08, // Mode = ModeSymlink | 0777
		0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Size = 3，是链接目标的长度
	)
	want = append(want, frozenModTime()...)
	want = append(want, 0x03, 0x00, 0x00, 0x00) // PathLen = 3
	want = append(want, zeroCRC...)
	return append(want, 'd', '/', 's')
}

// frozenModTime 是记录头里固定的修改时间字节：ModTimeSec 和 ModTimeNsec
func frozenModTime() []byte {
	return []byte{
		0x00, 0xCA, 0x9A, 0x3B, 0x00, 0x00, 0x00, 0x00, // ModTimeSec = 1000000000
		0x15, 0xCD, 0x5B, 0x07, // ModTimeNsec = 123456789
	}
}

// 扫描之后文件可能被删掉、也可能被截短，Pack 要跳过它们继续打包，见 robust_test.go。
// Entry 是调用方给的数据。Size 为负时直接转成 uint64 会回绕成一个巨大的数，
// 写出来的归档头和实际内容对不上，所以要拒掉。
func TestPackRejectsNegativeSize(t *testing.T) {
	src := t.TempDir()
	entries := []Entry{{Path: "neg.bin", Kind: KindFile, Size: -1}}

	_, err := Pack(context.Background(), filepath.Join(t.TempDir(), "x.dbkp"), src, entries, PackOptions{})
	if problem := errOn(err, "neg.bin", errs.ErrInvalidArgument); problem != nil {
		t.Errorf("Pack() %v", problem)
	}
}
