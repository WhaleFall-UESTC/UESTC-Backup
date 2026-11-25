package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"client/internal/errs"
)

// 布局和各函数的约定见 archive.go。这里的布局常数故意再写一遍，
// 不引用实现里的：实现改了宽度，测试不会跟着一起错。
const (
	layoutMagicSize        = 4
	layoutDataOffset       = 8 // DataOffset 字段的偏移
	layoutHeaderSize       = 24
	layoutRecordHeaderSize = 40
	layoutCRCOffset        = 32 // 记录头里 CRC64 的偏移
	layoutVersionOffset    = 4
)

// 读回的记录要和交给 Pack 的扫描结果逐项一致，顺序也不能变。路径里带上中文和空格。
func TestReadIndexMatchesScan(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "hello")
	writeFile(t, filepath.Join(root, "empty.txt"), "")
	writeFile(t, filepath.Join(root, "文档 目录", "测试 文件.txt"), "内容")
	writeFile(t, filepath.Join(root, "文档 目录", "data.bin"), string(dataBytes(150_000)))

	t.Run("文件和目录", func(t *testing.T) {
		entries := scanOK(t, root)
		if diff := diffRecords(readIndexOK(t, packTree(t, root, entries)), entries); diff != nil {
			t.Errorf("ReadIndex() %v", diff)
		}
	})

	// dangling 指向不存在的路径，Pack 不能去打开链接。创建不了符号链接的环境会跳过。
	t.Run("符号链接", func(t *testing.T) {
		symlink(t, filepath.Join(root, "文档 目录"), filepath.Join(root, "link"))
		symlink(t, filepath.Join(root, "missing"), filepath.Join(root, "dangling"))

		entries := scanOK(t, root)
		if diff := diffRecords(readIndexOK(t, packTree(t, root, entries)), entries); diff != nil {
			t.Errorf("ReadIndex() %v", diff)
		}
	})
}

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

	header := []byte{
		'D', 'B', 'K', 'P', // Magic
		0x01, 0x00, // Version = 1
		0x00, 0x00, // Algorithm = 0
		0x18, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // DataOffset = 24
	}
	zeroCRC := []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	// 三个 CRC64 是用另一份 CRC-64/ECMA 实现算出来的，没有借助被测代码
	emptyArchiveCRC := []byte{0x9B, 0xF7, 0x46, 0x6E, 0x38, 0x25, 0xC7, 0x03}
	fullArchiveCRC := []byte{0x44, 0x76, 0x78, 0x5E, 0xB1, 0x36, 0x73, 0xCF}
	contentCRC := []byte{0xC6, 0x2D, 0x41, 0xBA, 0xA9, 0xB5, 0x38, 0x75} // 258 个 z 的 CRC64
	modTime := time.Unix(1_000_000_000, 123_456_789)
	modTimeBytes := []byte{
		0x00, 0xCA, 0x9A, 0x3B, 0x00, 0x00, 0x00, 0x00, // ModTimeSec = 1000000000
		0x15, 0xCD, 0x5B, 0x07, // ModTimeNsec = 123456789
	}

	t.Run("空目录树", func(t *testing.T) {
		archive := packTree(t, src, nil)
		got, err := os.ReadFile(archive)
		if err != nil {
			t.Fatalf("Setup failed: read the archive: %v", err)
		}
		want := append(slices.Clone(header), emptyArchiveCRC...)
		if !bytes.Equal(got, want) {
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
			{Path: "d", Kind: KindDir, Size: 4096, Mode: fs.ModeDir | 0o755, ModTime: modTime},
			{Path: "d/a", Kind: KindFile, Size: 258, Mode: 0o644, ModTime: modTime},
			{Path: "d/s", Kind: KindSymlink, Size: 3, Mode: fs.ModeSymlink | 0o777, ModTime: modTime},
		}

		want := append(slices.Clone(header), fullArchiveCRC...)
		want = append(want,
			// 目录 d
			0x02, 0x00, 0x00, 0x00, // Kind = 目录，后面 3 字节保留
			0xED, 0x01, 0x00, 0x80, // Mode = ModeDir | 0755
			0x00, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Size = 4096
		)
		want = append(want, modTimeBytes...)
		want = append(want, 0x01, 0x00, 0x00, 0x00) // PathLen = 1
		want = append(want, zeroCRC...)
		want = append(want, 'd')

		want = append(want,
			// 文件 d/a
			0x01, 0x00, 0x00, 0x00, // Kind = 文件
			0xA4, 0x01, 0x00, 0x00, // Mode = 0644
			0x02, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Size = 258
		)
		want = append(want, modTimeBytes...)
		want = append(want, 0x03, 0x00, 0x00, 0x00) // PathLen = 3
		want = append(want, contentCRC...)
		want = append(want, 'd', '/', 'a')
		want = append(want, bytes.Repeat([]byte{'z'}, 258)...)
		want = append(want, make([]byte, 254)...) // 补零到 512

		want = append(want,
			// 符号链接 d/s，没有数据区
			0x03, 0x00, 0x00, 0x00, // Kind = 符号链接
			0xFF, 0x01, 0x00, 0x08, // Mode = ModeSymlink | 0777
			0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Size = 3，是链接目标的长度
		)
		want = append(want, modTimeBytes...)
		want = append(want, 0x03, 0x00, 0x00, 0x00) // PathLen = 3
		want = append(want, zeroCRC...)
		want = append(want, 'd', '/', 's')

		got := packBytes(t, src, entries)
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

// 拿错的文件不能被当成归档读出东西来，哪怕它开头碰巧是 DBKP。
func TestReadIndexRejectsNonArchive(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "空文件", content: ""},
		{name: "普通文本文件", content: "this is not an archive\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "x.dbkp")
			writeFile(t, archive, tc.content)

			_, err := ReadIndex(archive)
			if problem := errOn(err, archive, errs.ErrNotArchive); problem != nil {
				t.Errorf("ReadIndex(%q): %v", archive, problem)
			}
		})
	}

	// 版本号和数据偏移都是对的，只有魔数检查能把它拦下来
	t.Run("魔数不对，其余和空归档一样", func(t *testing.T) {
		archive := rewrittenArchive(t, func(raw []byte) { raw[0] = 'X' })

		_, err := ReadIndex(archive)
		if problem := errOn(err, archive, errs.ErrNotArchive); problem != nil {
			t.Errorf("ReadIndex(%q): %v", archive, problem)
		}
	})

	t.Run("文件不存在", func(t *testing.T) {
		// 错误原样返回，底层的 fs.ErrNotExist 要还在
		archive := filepath.Join(t.TempDir(), "nope.dbkp")
		if _, err := ReadIndex(archive); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReadIndex(%q) error = %v, want errors.Is(err, fs.ErrNotExist)", archive, err)
		}
	})
}

// 魔数是对的，只有版本号不对。
func TestReadIndexRejectsUnknownVersion(t *testing.T) {
	archive := rewrittenArchive(t, func(raw []byte) { raw[layoutVersionOffset] = 99 })

	_, err := ReadIndex(archive)
	if problem := errOn(err, archive, errs.ErrUnsupportedVersion); problem != nil {
		t.Errorf("ReadIndex(%q): %v", archive, problem)
	}
}

// 把真归档从每个位置截断都要报错，类别要对：连魔数都读不全是 errs.NotArchive，
// 开头是 DBKP、后面读不全是 errs.Corrupted。恰好只剩公共头时和空归档没有区别，不报错。
func TestReadIndexRejectsTruncatedArchive(t *testing.T) {
	const path = "docs/readme.txt" // 取长一点，让截断能落在路径中间
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "docs", "readme.txt"), "x")
	raw := packBytes(t, src, []Entry{{Path: path, Kind: KindFile, Size: 1}})

	recordEnd := layoutHeaderSize + layoutRecordHeaderSize + len(path)
	archive := filepath.Join(t.TempDir(), "cut.dbkp")
	for n := 0; n < recordEnd; n++ {
		if n == layoutHeaderSize {
			continue
		}
		want := errs.ErrCorrupted
		if n < layoutMagicSize {
			want = errs.ErrNotArchive
		}
		if err := os.WriteFile(archive, raw[:n], 0o644); err != nil {
			t.Fatalf("Setup failed: write an archive of %d bytes: %v", n, err)
		}

		_, err := ReadIndex(archive)
		if problem := errOn(err, archive, want); problem != nil {
			t.Errorf("ReadIndex() on the first %d bytes: %v", n, problem)
			continue
		}
		if n >= layoutMagicSize && !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("ReadIndex() on the first %d bytes: error = %v, want errors.Is(err, io.ErrUnexpectedEOF)",
				n, err)
		}
	}
}

// 归档是外来数据：记录头里的长度字段坏了，不能让读取方往回跳，也不能让它一次分配几个 GB。
func TestReadIndexRejectsInvalidRecordHeader(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a"), "x")
	raw := packBytes(t, src, []Entry{{Path: "a", Kind: KindFile, Size: 1}})
	archive := filepath.Join(t.TempDir(), "x.dbkp")

	tests := []struct {
		name   string
		damage func(raw []byte)
	}{
		{"Size 是负数", func(raw []byte) { raw[layoutHeaderSize+8+7] = 0x80 }},
		{"路径长度超过 1 MiB", func(raw []byte) {
			copy(raw[layoutHeaderSize+28:], []byte{0x01, 0x00, 0x10, 0x00}) // 1<<20 + 1
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			broken := slices.Clone(raw)
			tc.damage(broken)
			if err := os.WriteFile(archive, broken, 0o644); err != nil {
				t.Fatalf("Setup failed: write the damaged archive: %v", err)
			}

			_, err := ReadIndex(archive)
			if problem := errOn(err, archive, errs.ErrCorrupted); problem != nil {
				t.Errorf("ReadIndex(%q): %v", archive, problem)
			}
		})
	}
}

// 公共头说记录区不在归档里：偏移比公共头还小，或者超出了文件末尾。
func TestReadIndexRejectsBadDataOffset(t *testing.T) {
	tests := []struct {
		name   string
		offset []byte
	}{
		{"比公共头还小", make([]byte, 8)},
		{"超出文件末尾", []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}}, // 1<<32
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := rewrittenArchive(t, func(raw []byte) { copy(raw[layoutDataOffset:], tc.offset) })

			_, err := ReadIndex(archive)
			if problem := errOn(err, archive, errs.ErrCorrupted); problem != nil {
				t.Errorf("ReadIndex(%q): %v", archive, problem)
			}
		})
	}
}

// 扫描之后文件可能被删掉、也可能被截短，Pack 要跳过它们继续打包，见 robust_test.go。
// Entry 是调用方给的数据。Size 为负时直接转成 uint64 会回绕成一个巨大的数，
// 写出来的归档头和实际内容对不上，所以要拒掉。
func TestPackRejectsNegativeSize(t *testing.T) {
	src := t.TempDir()
	entries := []Entry{{Path: "neg.bin", Kind: KindFile, Size: -1}}

	_, err := Pack(context.Background(), filepath.Join(t.TempDir(), "x.dbkp"), src, entries, nil)
	if problem := errOn(err, "neg.bin", errs.ErrInvalidArgument); problem != nil {
		t.Errorf("Pack() %v", problem)
	}
}

func TestVerifyAcceptsIntactArchive(t *testing.T) {
	root := sampleTree(t)
	writeFile(t, filepath.Join(root, "big.bin"), string(dataBytes(150_000)))

	for _, archive := range []string{packTree(t, root, scanOK(t, root)), packTree(t, root, nil)} {
		if err := Verify(archive); err != nil {
			t.Errorf("Verify(%q) = %v, want <nil>", archive, err)
		}
	}
}

// 归档里任何一个字节坏了，Verify 都要报错。文件内容和它的 CRC64 坏了，报的是那个文件；
// 公共头和记录的其它部分坏了，只有整档的 CRC64 能发现，报的是归档本身。
func TestVerifyRejectsBrokenArchive(t *testing.T) {
	const path = "docs/a.txt"
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "docs", "a.txt"), "xyz")
	raw := packBytes(t, src, []Entry{{Path: path, Kind: KindFile, Size: 3}})
	archive := filepath.Join(t.TempDir(), "x.dbkp")

	data := layoutHeaderSize + layoutRecordHeaderSize + len(path)
	flip := func(offset int) func([]byte) []byte {
		return func(raw []byte) []byte {
			raw[offset] ^= 0xFF
			return raw
		}
	}
	tests := []struct {
		name   string
		damage func(raw []byte) []byte
		want   error
		path   string // 报错里应该带的路径
	}{
		{"公共头里的校验值", flip(layoutHeaderSize - 8), errs.ErrCorrupted, archive},
		{"记录里的路径", flip(layoutHeaderSize + layoutRecordHeaderSize), errs.ErrCorrupted, archive},
		{"补零", flip(data + 3), errs.ErrCorrupted, archive},
		{"记录里的校验值", flip(layoutHeaderSize + layoutCRCOffset), errs.ErrCorrupted, path},
		{"文件内容", flip(data), errs.ErrCorrupted, path},
		{"版本号", flip(layoutVersionOffset), errs.ErrUnsupportedVersion, archive},
		{"不是归档", func([]byte) []byte { return []byte("not an archive") }, errs.ErrNotArchive, archive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(archive, tc.damage(slices.Clone(raw)), 0o644); err != nil {
				t.Fatalf("Setup failed: write the damaged archive: %v", err)
			}

			err := Verify(archive)
			if problem := errOn(err, tc.path, tc.want); problem != nil {
				t.Errorf("Verify(%q): %v", archive, problem)
			}
		})
	}
}

// 归档少了字节的每一处，Verify 都要报 errs.Corrupted，Err 是 io.ErrUnexpectedEOF，
// 路径是写到一半的那个文件，公共头没写完时是归档本身。
func TestVerifyRejectsTruncatedArchive(t *testing.T) {
	const path = "docs/a.txt"
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "docs", "a.txt"), "xyz")
	raw := packBytes(t, src, []Entry{{Path: path, Kind: KindFile, Size: 3}})
	archive := filepath.Join(t.TempDir(), "cut.dbkp")

	data := layoutHeaderSize + layoutRecordHeaderSize + len(path)
	tests := []struct {
		name   string
		length int // 留下多少字节
		path   string
	}{
		{"公共头没写完", layoutHeaderSize - 1, archive},
		{"文件内容没写完", data + 1, path},
		{"补零没写完", data + 3, path},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(archive, raw[:tc.length], 0o644); err != nil {
				t.Fatalf("Setup failed: write the truncated archive: %v", err)
			}

			err := Verify(archive)
			if problem := errOn(err, tc.path, errs.ErrCorrupted); problem != nil {
				t.Errorf("Verify(%q): %v", archive, problem)
			}
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("Verify(%q) error = %v, want errors.Is(err, io.ErrUnexpectedEOF)", archive, err)
			}
		})
	}
}

// 还原出来的目录树要和源目录一致：节点和顺序、文件内容、文件的修改时间。
func TestUnpackRestoresTree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "hello")
	writeFile(t, filepath.Join(root, "empty.txt"), "")
	writeFile(t, filepath.Join(root, "文档 目录", "测试 文件.txt"), "内容")
	writeFile(t, filepath.Join(root, "文档 目录", "data.bin"), string(dataBytes(150_000)))
	writeFile(t, filepath.Join(root, strings.Repeat("长", 80)+".txt"), "hello") // 文件名 244 字节
	if err := os.MkdirAll(filepath.Join(root, "empty dir"), 0o755); err != nil {
		t.Fatalf("Setup failed: create an empty directory: %v", err)
	}
	// 修改时间取一个过去的值：不恢复的话，还原出来的文件带着现在的时间
	modTime := time.Date(2020, 5, 4, 3, 2, 1, 1234500, time.UTC)
	if err := os.Chtimes(filepath.Join(root, "a.txt"), modTime, modTime); err != nil {
		t.Fatalf("Setup failed: set the modification time: %v", err)
	}

	archive := packTree(t, root, scanOK(t, root))

	restoresTo := func(t *testing.T, dst string) {
		t.Helper()
		if err := Unpack(archive, dst); err != nil {
			t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
		}
		if problem := diffTree(root, dst); problem != nil {
			t.Errorf("Unpack(%q, %q): the restored tree differs from the source: %v", archive, dst, problem)
		}
	}
	t.Run("目标目录不存在", func(t *testing.T) {
		restoresTo(t, filepath.Join(t.TempDir(), "not", "created", "yet"))
	})
	t.Run("目标目录已存在且为空", func(t *testing.T) {
		restoresTo(t, t.TempDir())
	})

	t.Run("空归档", func(t *testing.T) {
		archive := packTree(t, t.TempDir(), nil)
		dst := filepath.Join(t.TempDir(), "restore")

		if err := Unpack(archive, dst); err != nil {
			t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
		}
		if _, err := os.Stat(dst); err != nil {
			t.Errorf("Unpack(%q, %q) did not create the target: %v", archive, dst, err)
		} else if got := entryNames(t, dst); len(got) != 0 {
			t.Errorf("Unpack(%q, %q) left %v in the target, want it empty", archive, dst, got)
		}
	})
}

// 目标目录里已经有东西就拒绝，里面的内容原样不动。
// 课件讲解包时要求目的目录为空，免得覆盖用户的数据。
func TestUnpackRejectsNonEmptyTarget(t *testing.T) {
	root := sampleTree(t)
	archive := packTree(t, root, scanOK(t, root))

	tests := []struct {
		name string
		fill func(t *testing.T, dst string)
	}{
		{"里面有一个文件", func(t *testing.T, dst string) {
			writeFile(t, filepath.Join(dst, "keep.txt"), "keep")
		}},
		{"里面只有一个空目录", func(t *testing.T, dst string) {
			if err := os.Mkdir(filepath.Join(dst, "sub"), 0o755); err != nil {
				t.Fatalf("Setup failed: create a subdirectory in the target: %v", err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dst := t.TempDir()
			tc.fill(t, dst)
			before := entryNames(t, dst)

			err := Unpack(archive, dst)
			if problem := errOn(err, dst, errs.ErrTargetNotEmpty); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if got := entryNames(t, dst); !slices.Equal(got, before) {
				t.Errorf("Unpack(%q, %q): the target now holds %v, want it unchanged: %v", archive, dst, got, before)
			}
		})
	}
}

// 目标路径上已经是一个文件时，不能拿它当目录，更不能动它。错误原样返回。
func TestUnpackRejectsFileTarget(t *testing.T) {
	root := sampleTree(t)
	archive := packTree(t, root, scanOK(t, root))
	dst := filepath.Join(t.TempDir(), "file.txt")
	writeFile(t, dst, "keep")

	err := Unpack(archive, dst)
	var e *errs.Error
	if err == nil || errors.As(err, &e) {
		t.Errorf("Unpack(%q, %q) error = %v, want the error from creating the target, returned as is",
			archive, dst, err)
	}
	if got, readErr := os.ReadFile(dst); readErr != nil || string(got) != "keep" {
		t.Errorf("Unpack(%q, %q): the target now holds %q (%v), want %q unchanged", archive, dst, got, readErr, "keep")
	}
}

// 归档打不开时错误原样返回。这时还没碰目标目录，不留下一个空目录。
func TestUnpackRejectsMissingArchive(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "nope.dbkp")
	dst := filepath.Join(t.TempDir(), "restore")

	err := Unpack(archive, dst)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Unpack(%q, %q) error = %v, want errors.Is(err, fs.ErrNotExist)", archive, dst, err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Unpack(%q, %q): the target exists after the failed call, want it left alone", archive, dst)
	}
}

// 归档中途截断时报 errs.Corrupted，Err 是 io.ErrUnexpectedEOF，
// 路径是写到一半的那个文件；截在公共头或记录头里时是归档本身。
func TestUnpackRejectsTruncatedArchive(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	writeFile(t, filepath.Join(src, "docs", "b.txt"), "xyz")
	raw := packBytes(t, src, []Entry{
		{Path: "a.txt", Kind: KindFile, Size: 5},
		{Path: "docs", Kind: KindDir},
		{Path: "docs/b.txt", Kind: KindFile, Size: 3},
	})
	archive := filepath.Join(t.TempDir(), "cut.dbkp")

	first := layoutHeaderSize + layoutRecordHeaderSize + len("a.txt") // 第一个文件的数据区
	docsEnd := first + 512 + layoutRecordHeaderSize + len("docs")
	second := docsEnd + layoutRecordHeaderSize + len("docs/b.txt")
	tests := []struct {
		name   string
		length int    // 留下多少字节
		path   string // 报错里应该带的路径
	}{
		{"公共头没写完", layoutHeaderSize - 1, archive},
		{"第一个记录头没写完", layoutHeaderSize + layoutRecordHeaderSize - 1, archive},
		{"第一个文件的路径没写完", layoutHeaderSize + layoutRecordHeaderSize + 1, archive},
		{"第一个文件的内容没写完", first + 2, "a.txt"},
		{"第二个记录头没写完", first + 512 + layoutRecordHeaderSize - 1, archive},
		{"第二个文件的内容没写完", second + 1, "docs/b.txt"},
		{"第二个文件的补零没写完", second + 3, "docs/b.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(archive, raw[:tc.length], 0o644); err != nil {
				t.Fatalf("Setup failed: write the truncated archive: %v", err)
			}

			dst := t.TempDir()
			err := Unpack(archive, dst)
			if problem := errOn(err, tc.path, errs.ErrCorrupted); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("Unpack(%q, %q) error = %v, want errors.Is(err, io.ErrUnexpectedEOF)", archive, dst, err)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want nothing written from a truncated archive",
					archive, dst, got)
			}
		})
	}
}

// 归档里的字节坏了就不还原，报的错和 Verify 一样。目标目录里一个文件都不该有。
func TestUnpackRejectsCorruptedArchive(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "docs", "a.txt"), "xyz")
	raw := packBytes(t, src, []Entry{
		{Path: "docs", Kind: KindDir},
		{Path: "docs/a.txt", Kind: KindFile, Size: 3},
	})
	archive := filepath.Join(t.TempDir(), "x.dbkp")

	docsEnd := layoutHeaderSize + layoutRecordHeaderSize + len("docs")
	data := docsEnd + layoutRecordHeaderSize + len("docs/a.txt")
	flip := func(offset int) func([]byte) []byte {
		return func(raw []byte) []byte {
			raw[offset] ^= 0xFF
			return raw
		}
	}
	tests := []struct {
		name   string
		damage func(raw []byte) []byte
		path   string // 报错里应该带的路径
	}{
		{"公共头里的校验值", flip(layoutHeaderSize - 8), archive},
		{"目录记录里的路径", flip(layoutHeaderSize + layoutRecordHeaderSize), archive},
		{"文件记录里的校验值", flip(docsEnd + layoutCRCOffset), "docs/a.txt"},
		{"文件内容", flip(data), "docs/a.txt"},
		{"补零", flip(data + 3), archive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(archive, tc.damage(slices.Clone(raw)), 0o644); err != nil {
				t.Fatalf("Setup failed: write the damaged archive: %v", err)
			}

			dst := t.TempDir()
			err := Unpack(archive, dst)
			if problem := errOn(err, tc.path, errs.ErrCorrupted); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want nothing written from a damaged archive",
					archive, dst, got)
			}
		})
	}
}

// 归档是外来数据，里面的路径可能是伪造的：不能让它写到目标目录外面去。
func TestUnpackRejectsEscapingPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"上一级", "../evil"},
		{"绝对路径", "/evil"},
		{"先进子目录再出来两级", "a/../../evil"},
		{"空路径", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := packTree(t, t.TempDir(), []Entry{{Path: tc.path, Kind: KindDir}})
			dst := t.TempDir()
			outside := filepath.Dir(dst)
			before := entryNames(t, outside)

			err := Unpack(archive, dst)
			if problem := errOn(err, archive, errs.ErrCorrupted); problem != nil {
				t.Errorf("Unpack(%q, %q): %v", archive, dst, problem)
			}
			if got := entryNames(t, outside); !slices.Equal(got, before) {
				t.Errorf("Unpack(%q, %q) created %v outside the target, want %v", archive, dst, got, before)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want it empty", archive, dst, got)
			}
		})
	}
}

// 记录头的 Kind 只有文件、目录、符号链接三种，其余的是坏归档。
func TestUnpackRejectsUnknownKind(t *testing.T) {
	archive := packTree(t, t.TempDir(), []Entry{{Path: "odd", Kind: Kind(9)}})

	err := Unpack(archive, t.TempDir())
	if problem := errOn(err, archive, errs.ErrCorrupted); problem != nil {
		t.Errorf("Unpack(%q): %v", archive, problem)
	}
}

// 符号链接这一步不还原：记录跳过，不报错，也不影响别的文件。
func TestUnpackSkipsSymlinks(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	archive := packTree(t, src, []Entry{
		{Path: "link", Kind: KindSymlink, Size: 5},
		{Path: "a.txt", Kind: KindFile, Size: 5},
	})
	dst := t.TempDir()

	if err := Unpack(archive, dst); err != nil {
		t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
	}
	want := []string{"a.txt"} // 符号链接不在里面
	if got := entryNames(t, dst); !slices.Equal(got, want) {
		t.Errorf("Unpack(%q, %q): the target holds %v, want %v", archive, dst, got, want)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a.txt")); err != nil || string(got) != "hello" {
		t.Errorf("Unpack(%q, %q): a.txt holds %q (%v), want %q", archive, dst, got, err, "hello")
	}
}

// 在目标目录里建不出文件或目录时报 errs.IO，带上那条记录。
// 做法是让两条记录的路径构成父子关系，父的那条是文件：子文件建不出来，子目录也建不出来。
// 记录的 Size 都是 0，Pack 打开源目录里的目录 a 时读不出东西，不会报错。
func TestUnpackReportsUnwritableEntry(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a", "b"), "")
	parent := Entry{Path: "a", Kind: KindFile}

	tests := []struct {
		name  string
		child Entry
	}{
		{"文件", Entry{Path: "a/b", Kind: KindFile}},
		{"目录", Entry{Path: "a/b", Kind: KindDir}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			archive := packTree(t, src, []Entry{parent, tc.child})

			err := Unpack(archive, t.TempDir())
			if problem := errOn(err, tc.child.Path, errs.ErrIO); problem != nil {
				t.Errorf("Unpack(%q): %v", archive, problem)
			}
		})
	}
}

// ---------- 辅助函数 ----------

// packTree 把 root 下的 entries 打包到临时文件，返回归档路径。
func packTree(t *testing.T, root string, entries []Entry) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "backup.dbkp")
	if _, err := Pack(context.Background(), archive, root, entries, nil); err != nil {
		t.Fatalf("Setup failed: Pack() error = %v", err)
	}
	return archive
}

// packBytes 打包后把归档整个读回来。
func packBytes(t *testing.T, root string, entries []Entry) []byte {
	t.Helper()
	raw, err := os.ReadFile(packTree(t, root, entries))
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}
	return raw
}

// readIndexOK 执行 ReadIndex，出错时让测试立即失败。
func readIndexOK(t *testing.T, archive string) []Record {
	t.Helper()
	records, err := ReadIndex(archive)
	if err != nil {
		t.Fatalf("Setup failed: ReadIndex(%q) error = %v", archive, err)
	}
	return records
}

// rewrittenArchive 把一份空归档按 change 改过后写到临时文件，返回路径。
func rewrittenArchive(t *testing.T, change func(raw []byte)) string {
	t.Helper()
	raw := packBytes(t, t.TempDir(), nil)
	change(raw)

	archive := filepath.Join(t.TempDir(), "patched.dbkp")
	if err := os.WriteFile(archive, raw, 0o644); err != nil {
		t.Fatalf("Setup failed: write the archive: %v", err)
	}
	return archive
}

// errOn 检查引擎对某个文件报的错：errors.Is 能认出 want，包在 *errs.Error 里，Path 就是这个文件，
// 并且不匹配 io.EOF（调用方把它当成正常结束）。有问题时返回一句说明。
func errOn(err error, path string, want error) error {
	var e *errs.Error
	switch {
	case !errors.Is(err, want):
		return fmt.Errorf("error = %v, want errors.Is(err, %v)", err, want)
	case !errors.As(err, &e) || e.Path != path:
		return fmt.Errorf("error = %v, want a *errs.Error whose Path is %q", err, path)
	case errors.Is(err, io.EOF):
		return fmt.Errorf("error = %v: it must not match io.EOF", err)
	}
	return nil
}

// diffRecords 逐项比较读回的记录和扫描结果，返回第一处不一致的说明。CRC64 由 TestPackRecordsFileCRC64 管。
func diffRecords(got []Record, want []Entry) error {
	if len(got) != len(want) {
		return fmt.Errorf("returned %d records, want %d\n got:  %+v\n want: %+v", len(got), len(want), got, want)
	}
	for i, w := range want {
		g := got[i]
		if g.Path != w.Path || g.Kind != w.Kind || g.Size != w.Size || g.Mode != w.Mode ||
			!g.ModTime.Equal(w.ModTime) {
			return fmt.Errorf("record %d = {Path: %q, Kind: %d, Size: %d, Mode: %O, ModTime: %v}, "+
				"want {Path: %q, Kind: %d, Size: %d, Mode: %O, ModTime: %v}",
				i, g.Path, g.Kind, g.Size, g.Mode, g.ModTime, w.Path, w.Kind, w.Size, w.Mode, w.ModTime)
		}
	}
	return nil
}

// diffTree 把还原出来的 dst 和源目录 src 逐项比较：节点和顺序要一致，
// 文件的内容要逐字节相同，文件的修改时间要一致。
// 目录的修改时间不比：往目录里建文件会更新它，这一步不恢复。返回第一处不一致的说明。
func diffTree(src, dst string) error {
	want, err := Scan(src)
	if err != nil {
		return fmt.Errorf("Scan(%q): %v", src, err)
	}
	got, err := Scan(dst)
	if err != nil {
		return fmt.Errorf("Scan(%q): %v", dst, err)
	}
	if len(got) != len(want) {
		return fmt.Errorf("restored %d nodes, want %d\n got:  %v\nwant: %v", len(got), len(want), paths(got), paths(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Path != w.Path || g.Kind != w.Kind {
			return fmt.Errorf("node %d = {Path: %q, Kind: %d}, want {Path: %q, Kind: %d}",
				i, g.Path, g.Kind, w.Path, w.Kind)
		}
		if w.Kind != KindFile {
			continue
		}
		if !g.ModTime.Equal(w.ModTime) {
			return fmt.Errorf("%s: ModTime = %v, want %v", w.Path, g.ModTime, w.ModTime)
		}
		original, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(w.Path)))
		if err != nil {
			return fmt.Errorf("read the source %q: %v", w.Path, err)
		}
		restored, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(w.Path)))
		if err != nil {
			return fmt.Errorf("read the restored %q: %v", w.Path, err)
		}
		if !bytes.Equal(restored, original) {
			return fmt.Errorf("%s: content differs (%d bytes restored, %d in the source)", w.Path, len(restored), len(original))
		}
	}
	return nil
}

// entryNames 返回 dir 下一层的名字，按名字排序。
func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("Setup failed: read %q: %v", dir, err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// layoutPaddedSize 是 n 个字节的数据区在归档里占的字节数：补零到 512 的整数倍。
func layoutPaddedSize(n int64) int64 {
	return (n + 511) / 512 * 512
}

// dataBytes 生成 n 个确定的字节。
func dataBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 1 + i>>3)
	}
	return b
}
