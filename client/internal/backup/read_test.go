package backup

import (
	"client/internal/errs"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
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

			_, err := ReadIndex(archive, "")
			if problem := errOn(err, archive, errs.ErrNotArchive); problem != nil {
				t.Errorf("ReadIndex(%q): %v", archive, problem)
			}
		})
	}

	// 版本号和数据偏移都是对的，只有魔数检查能把它拦下来
	t.Run("魔数不对，其余和空归档一样", func(t *testing.T) {
		archive := rewrittenArchive(t, func(raw []byte) { raw[0] = 'X' })

		_, err := ReadIndex(archive, "")
		if problem := errOn(err, archive, errs.ErrNotArchive); problem != nil {
			t.Errorf("ReadIndex(%q): %v", archive, problem)
		}
	})

	t.Run("文件不存在", func(t *testing.T) {
		// 错误原样返回，底层的 fs.ErrNotExist 要还在
		archive := filepath.Join(t.TempDir(), "nope.dbkp")
		if _, err := ReadIndex(archive, ""); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReadIndex(%q) error = %v, want errors.Is(err, fs.ErrNotExist)", archive, err)
		}
	})
}

// 魔数是对的，只有版本号不对。
func TestReadIndexRejectsUnknownVersion(t *testing.T) {
	archive := rewrittenArchive(t, func(raw []byte) { raw[layoutVersionOffset] = 99 })

	_, err := ReadIndex(archive, "")
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

		_, err := ReadIndex(archive, "")
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

			_, err := ReadIndex(archive, "")
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

			_, err := ReadIndex(archive, "")
			if problem := errOn(err, archive, errs.ErrCorrupted); problem != nil {
				t.Errorf("ReadIndex(%q): %v", archive, problem)
			}
		})
	}
}

func TestVerifyAcceptsIntactArchive(t *testing.T) {
	root := sampleTree(t)
	writeFile(t, filepath.Join(root, "big.bin"), string(dataBytes(150_000)))

	for _, archive := range []string{packTree(t, root, scanOK(t, root)), packTree(t, root, nil)} {
		if err := Verify(archive, ""); err != nil {
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

			err := Verify(archive, "")
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

			err := Verify(archive, "")
			if problem := errOn(err, tc.path, errs.ErrCorrupted); problem != nil {
				t.Errorf("Verify(%q): %v", archive, problem)
			}
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("Verify(%q) error = %v, want errors.Is(err, io.ErrUnexpectedEOF)", archive, err)
			}
		})
	}
}
