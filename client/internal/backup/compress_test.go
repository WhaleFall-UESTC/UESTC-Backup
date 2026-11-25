package backup

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"client/internal/errs"
)

// 三种算法各打一个归档：记录、校验、还原都要和源目录对得上
// 文本为主的目录，两种压缩算法出来的归档都要明显小于不压缩的
func TestPackCompressedArchiveRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readme.txt"), strings.Repeat("备份要能还原回来，压缩只是中间一步。\n", 1500))
	writeFile(t, filepath.Join(root, "empty.txt"), "")
	writeFile(t, filepath.Join(root, "文档 目录", "测试 文件.txt"), "内容")
	if err := os.MkdirAll(filepath.Join(root, "empty dir"), 0o755); err != nil {
		t.Fatalf("Setup failed: create an empty directory: %v", err)
	}
	modTime := time.Date(2020, 5, 4, 3, 2, 1, 1234500, time.UTC)
	if err := os.Chtimes(filepath.Join(root, "readme.txt"), modTime, modTime); err != nil {
		t.Fatalf("Setup failed: set the modification time: %v", err)
	}
	entries := scanOK(t, root)
	rawSize := int64(len(packBytesAlgo(t, root, entries, AlgorithmNone)))

	for _, algo := range []Algorithm{AlgorithmNone, AlgorithmHuffman, AlgorithmLZ77} {
		t.Run(fmt.Sprintf("algo %d", algo), func(t *testing.T) {
			archive := packTreeAlgo(t, root, entries, algo)
			if got := storedAlgorithm(t, archive); got != algo {
				t.Errorf("Pack() stored algorithm %d, want %d", got, algo)
			}
			if diff := diffRecords(readIndexOK(t, archive), entries); diff != nil {
				t.Errorf("ReadIndex() %v", diff)
			}
			if err := Verify(archive, ""); err != nil {
				t.Errorf("Verify(%q) = %v, want <nil>", archive, err)
			}

			dst := filepath.Join(t.TempDir(), "restore")
			if err := Unpack(archive, dst, ""); err != nil {
				t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
			}
			if problem := diffTree(root, dst); problem != nil {
				t.Errorf("Unpack(%q, %q): the restored tree differs from the source: %v", archive, dst, problem)
			}

			if algo != AlgorithmNone {
				if size := fileSize(t, archive); size*10 > rawSize*7 {
					t.Errorf("Pack() with algo %d wrote %d bytes, want well under the %d bytes of an uncompressed archive",
						algo, size, rawSize)
				}
			}
		})
	}
}

// Huffman 的固定头有 2061 字节，随机数据压完反而更大，这种要原样存：
// 算法标识还是 0，归档和不压缩时逐字节相同
func TestPackKeepsUncompressedWhenCompressionIsBigger(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.bin"), string(randomBytes(100_000)))
	entries := scanOK(t, root)
	raw := packBytes(t, root, entries)

	archive := packTreeAlgo(t, root, entries, AlgorithmHuffman)
	if got := storedAlgorithm(t, archive); got != AlgorithmNone {
		t.Errorf("Pack() stored algorithm %d, want %d for data that does not compress", got, AlgorithmNone)
	}
	got, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("Pack() wrote %d bytes that differ from the %d bytes of an uncompressed archive", len(got), len(raw))
	}
}

// 压不动的数据过了 LZ77 也不会变大，压缩过的归档照样能校验、还原
func TestPackCompressedArchiveDoesNotGrow(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "photo.bin"), string(randomBytes(100_000)))
	entries := scanOK(t, root)

	archive := packTreeAlgo(t, root, entries, AlgorithmLZ77)
	if size, limit := fileSize(t, archive), int64(len(packBytes(t, root, entries))); size > limit {
		t.Errorf("Pack() with algo %d wrote %d bytes, want no more than the %d bytes of an uncompressed archive",
			AlgorithmLZ77, size, limit)
	}
	if err := Verify(archive, ""); err != nil {
		t.Errorf("Verify(%q) = %v, want <nil>", archive, err)
	}
	dst := filepath.Join(t.TempDir(), "restore")
	if err := Unpack(archive, dst, ""); err != nil {
		t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
	}
	if problem := diffTree(root, dst); problem != nil {
		t.Errorf("Unpack(%q, %q): the restored tree differs from the source: %v", archive, dst, problem)
	}
}

// 压缩归档里坏一个字节或者少一段，Verify 和 Unpack 都要报 errs.Corrupted，Unpack 不写任何文件
// 算法标识不认识时报 errs.UnsupportedVersion
func TestCompressedArchiveDamageIsDetected(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), strings.Repeat("xyz", 1000))
	archive := packTreeAlgo(t, src, scanOK(t, src), AlgorithmLZ77)
	if got := storedAlgorithm(t, archive); got != AlgorithmLZ77 {
		t.Fatalf("Setup failed: Pack() stored algorithm %d, want %d", got, AlgorithmLZ77)
	}
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}

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
	}{
		{"公共头里的校验值", flip(layoutHeaderSize - 8), errs.ErrCorrupted},
		{"公共头里的数据偏移", func(raw []byte) []byte {
			clear(raw[layoutDataOffset : layoutDataOffset+8])
			return raw
		}, errs.ErrCorrupted},
		{"压缩正文", flip(layoutHeaderSize + 10), errs.ErrCorrupted},
		{"压缩正文截断", func(raw []byte) []byte { return raw[:layoutHeaderSize+5] }, errs.ErrCorrupted},
		{"算法标识不认识", flip(6), errs.ErrUnsupportedVersion},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(archive, tc.damage(bytes.Clone(raw)), 0o644); err != nil {
				t.Fatalf("Setup failed: write the damaged archive: %v", err)
			}

			if err := Verify(archive, ""); !errors.Is(err, tc.want) {
				t.Errorf("Verify(%q) error = %v, want errors.Is(err, %v)", archive, err, tc.want)
			}

			dst := t.TempDir()
			if err := Unpack(archive, dst, ""); !errors.Is(err, tc.want) {
				t.Errorf("Unpack(%q, %q) error = %v, want errors.Is(err, %v)", archive, dst, err, tc.want)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want nothing written from a damaged archive",
					archive, dst, got)
			}
		})
	}
}

func TestPackRejectsUnknownAlgorithm(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "aaa")

	_, err := Pack(context.Background(), filepath.Join(t.TempDir(), "x.dbkp"), src, scanOK(t, src), PackOptions{Algorithm: 9})
	if problem := errOn(err, "", errs.ErrInvalidArgument); problem != nil {
		t.Errorf("Pack() %v", problem)
	}
}

// 归档路径落不了地时报错，临时文件不留下
func TestPackCleansUpWhenArchivePathIsUnusable(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), strings.Repeat("xyz", 1000))
	entries := scanOK(t, src)

	t.Run("归档路径是个目录", func(t *testing.T) {
		dir := t.TempDir()
		dst := filepath.Join(dir, "taken")
		if err := os.Mkdir(dst, 0o755); err != nil {
			t.Fatalf("Setup failed: create a directory at the archive path: %v", err)
		}
		for _, algo := range []Algorithm{AlgorithmNone, AlgorithmLZ77} {
			if _, err := Pack(context.Background(), dst, src, entries, PackOptions{Algorithm: algo}); err == nil {
				t.Errorf("Pack(algo %d) over a directory: error = nil, want one", algo)
			}
			if left := entryNames(t, dir); !slices.Equal(left, []string{"taken"}) {
				t.Errorf("Pack(algo %d) left %v beside the archive path, want only %q", algo, left, "taken")
			}
		}
	})

	t.Run("归档目录不存在", func(t *testing.T) {
		dst := filepath.Join(t.TempDir(), "missing", "x.dbkp")
		if _, err := Pack(context.Background(), dst, src, entries, PackOptions{Algorithm: AlgorithmLZ77}); err == nil {
			t.Errorf("Pack() into a missing directory: error = nil, want one")
		}
	})
}

// 压缩途中也要看取消。Pack 从头到尾没有插钩子的地方，这里直接调压缩这一步
func TestCompressHonorsContext(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "in-*")
	if err != nil {
		t.Fatalf("Setup failed: create a file: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(randomBytes(100_000)); err != nil {
		t.Fatalf("Setup failed: write the file: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, algo := range []Algorithm{AlgorithmHuffman, AlgorithmLZ77} {
		if err := compress(ctx, algo, f, io.Discard); !errors.Is(err, context.Canceled) {
			t.Errorf("compress(algo %d) error = %v, want context.Canceled", algo, err)
		}
	}
}

// ---------- 辅助函数 ----------

// storedAlgorithm 读归档公共头里的算法标识
func storedAlgorithm(t *testing.T, archive string) Algorithm {
	t.Helper()
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}
	if len(raw) < layoutHeaderSize {
		t.Fatalf("Setup failed: the archive is only %d bytes", len(raw))
	}
	return Algorithm(binary.LittleEndian.Uint16(raw[6:8]))
}

// fileSize 返回文件的字节数
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Setup failed: stat %q: %v", path, err)
	}
	return info.Size()
}

// randomBytes 生成 n 个伪随机字节，用来造压不动的数据
func randomBytes(n int) []byte {
	b := make([]byte, n)
	x := uint64(0x9E3779B97F4A7C15)
	for i := range b {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		b[i] = byte(x)
	}
	return b
}
