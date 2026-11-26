package backup

import (
	"bytes"
	"client/internal/aescbc"
	"client/internal/errs"
	"client/internal/lz77"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 加密归档的公共头、扩展头和大小都按字段表来，密文手工解出来就是一份不加密的 .dbkp
func TestPackEncryptedArchiveLayout(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "docs", "a.txt"), "内容 abc")
	entries := scanOK(t, root)
	raw := packBytes(t, root, entries)

	const password = "口令 pw"
	archive := packTreeEnc(t, root, entries, AlgorithmNone, password)
	got, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}

	if algo := Algorithm(binary.LittleEndian.Uint16(got[6:8])); algo != AlgorithmAES {
		t.Errorf("Pack() stored algorithm %d, want %d", algo, AlgorithmAES)
	}
	if offset := binary.LittleEndian.Uint64(got[8:16]); offset != uint64(archiveEncDataOffset) {
		t.Errorf("Pack() stored data offset %d, want %d", offset, archiveEncDataOffset)
	}
	if want := int(archiveEncDataOffset) + (len(raw)/16+1)*16; len(got) != want {
		t.Errorf("Pack() wrote %d bytes, want %d", len(got), want)
	}
	if bytes.Equal(got[24:56], make([]byte, 32)) {
		t.Error("Pack() left the salt and IV all zero")
	}
	if got := binary.LittleEndian.Uint32(got[56:60]); got != testIterations {
		t.Errorf("Pack() stored %d iterations, want %d", got, testIterations)
	}

	keys := mustDerive(t, password, got)
	if verifier := keys.Verifier(); !bytes.Equal(got[60:92], verifier[:]) {
		t.Errorf("Pack() stored verifier % x, want % x", got[60:92], verifier)
	}
	if mac := storedMAC(keys, got); !bytes.Equal(got[92:124], mac) {
		t.Errorf("Pack() stored MAC % x, want % x", got[92:124], mac)
	}
	if err := checkStoredCRC(got); err != nil {
		t.Errorf("Pack() %v", err)
	}
	if plain := decryptBytes(t, got, password); !bytes.Equal(plain, raw) {
		t.Errorf("the ciphertext decrypts to %d bytes, want the %d bytes of the same archive packed without a password",
			len(plain), len(raw))
	}
}

// 同一份数据加密两次，盐、IV 和密文都不该一样，但解出来是一样的
func TestPackEncryptedArchiveIsRandomized(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "备份要能还原回来")
	entries := scanOK(t, root)

	const password = "pw"
	first := packTreeEnc(t, root, entries, AlgorithmNone, password)
	second := packTreeEnc(t, root, entries, AlgorithmNone, password)
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}

	if bytes.Equal(a, b) {
		t.Error("Pack() wrote the same bytes twice, want a random salt and IV every time")
	}
	if !bytes.Equal(decryptBytes(t, a, password), decryptBytes(t, b, password)) {
		t.Error("the two archives decrypt to different bytes")
	}
}

// 加密排在压缩后面：密文解出来是一份压缩归档，不是源文件的拼接
func TestPackEncryptsAfterCompression(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readme.txt"), string(bytes.Repeat([]byte("备份要能还原回来\n"), 500)))
	entries := scanOK(t, root)
	rawSize := len(packBytes(t, root, entries))

	archive := packTreeEnc(t, root, entries, AlgorithmLZ77, "pw")
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}
	plain := decryptBytes(t, raw, "pw")
	if algo := Algorithm(binary.LittleEndian.Uint16(plain[6:8])); algo != AlgorithmLZ77 {
		t.Errorf("the decrypted archive has algorithm %d, want %d", algo, AlgorithmLZ77)
	}
	if len(plain) >= rawSize {
		t.Errorf("the decrypted archive is %d bytes, want fewer than the %d bytes packed without compression",
			len(plain), rawSize)
	}
}

// 扩展头的字段顺序和宽度钉死：盐、IV、迭代次数、校验值各就各位，校验值是可复现的
func TestEncryptedHeaderIsFrozen(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "")
	entries := scanOK(t, root)
	rawPath := filepath.Join(t.TempDir(), "raw.dbkp")
	if err := os.WriteFile(rawPath, packBytes(t, root, entries), 0o644); err != nil {
		t.Fatalf("Setup failed: write the archive: %v", err)
	}

	var h encHeader
	copy(h.salt[:], frozenSalt())
	copy(h.iv[:], frozenIV())
	h.iterations = testIterations
	dst := filepath.Join(t.TempDir(), "enc.dbkp")
	if err := sealArchive(context.Background(), rawPath, dst, "pw", h); err != nil {
		t.Fatalf("Setup failed: sealArchive() error = %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}

	want := []byte{
		'D', 'B', 'K', 'P', // Magic
		0x01, 0x00, // Version = 1
		0x03, 0x00, // Algorithm = 3
		0x7C, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // DataOffset = 124
	}
	if !bytes.Equal(got[:16], want) {
		t.Errorf("public header = % x, want % x", got[:16], want)
	}
	want = append(frozenSalt(), frozenIV()...)
	want = append(want, 0xE8, 0x03, 0x00, 0x00) // Iterations = 1000
	if !bytes.Equal(got[24:60], want) {
		t.Errorf("salt, IV and iterations = % x, want % x", got[24:60], want)
	}
	if verifier := hex.EncodeToString(got[60:92]); verifier != frozenVerifier {
		t.Errorf("stored verifier = %s, want %s", verifier, frozenVerifier)
	}
	if mac := storedMAC(mustDerive(t, "pw", got), got); !bytes.Equal(got[92:124], mac) {
		t.Errorf("stored MAC = % x, want % x", got[92:124], mac)
	}
	if err := checkStoredCRC(got); err != nil {
		t.Errorf("sealArchive() %v", err)
	}
}

// 加密归档走公开接口读索引、校验、还原，结果和源目录一致；不给口令的未加密归档照旧能用
func TestEncryptedArchiveRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readme.txt"), strings.Repeat("备份要能还原回来，加密只是最后一步。\n", 300))
	writeFile(t, filepath.Join(root, "empty.txt"), "")
	writeFile(t, filepath.Join(root, "文档 目录", "测试 文件.txt"), "内容")
	if err := os.MkdirAll(filepath.Join(root, "empty dir"), 0o755); err != nil {
		t.Fatalf("Setup failed: create an empty directory: %v", err)
	}
	entries := scanOK(t, root)
	const password = "口令 pw"

	for _, algo := range []Algorithm{AlgorithmNone, AlgorithmHuffman, AlgorithmLZ77} {
		t.Run(fmt.Sprintf("algo %d", algo), func(t *testing.T) {
			archive := packTreeEnc(t, root, entries, algo, password)
			if diff := diffRecords(readIndexOKPassword(t, archive, password), entries); diff != nil {
				t.Errorf("ReadIndex() %v", diff)
			}
			if err := Verify(context.Background(), archive, password, VerifyOptions{}); err != nil {
				t.Errorf("Verify(%q) = %v, want <nil>", archive, err)
			}

			dst := filepath.Join(t.TempDir(), "restore")
			if _, err := Unpack(context.Background(), archive, dst, password, UnpackOptions{}); err != nil {
				t.Fatalf("Unpack(%q, %q) error = %v", archive, dst, err)
			}
			if problem := diffTree(root, dst); problem != nil {
				t.Errorf("Unpack(%q, %q): the restored tree differs from the source: %v", archive, dst, problem)
			}
		})
	}

	t.Run("未加密的归档不用给口令", func(t *testing.T) {
		archive := packTree(t, root, entries)
		if err := Verify(context.Background(), archive, password, VerifyOptions{}); err != nil {
			t.Errorf("Verify(%q) with a password on an unencrypted archive = %v, want <nil>", archive, err)
		}
	})
}

// 口令不对时读索引、校验、还原都报 errs.WrongPassword，还原的目标目录里一个文件都不写
func TestEncryptedArchiveRejectsWrongPassword(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), strings.Repeat("内容", 100))
	archive := packTreeEnc(t, root, scanOK(t, root), AlgorithmLZ77, "right")

	for _, password := range []string{"wrong", ""} {
		t.Run(fmt.Sprintf("口令 %q", password), func(t *testing.T) {
			if _, err := ReadIndex(archive, password); !errors.Is(err, errs.ErrWrongPassword) {
				t.Errorf("ReadIndex() error = %v, want errors.Is(err, errs.ErrWrongPassword)", err)
			}
			if err := Verify(context.Background(), archive, password, VerifyOptions{}); !errors.Is(err, errs.ErrWrongPassword) {
				t.Errorf("Verify() error = %v, want errors.Is(err, errs.ErrWrongPassword)", err)
			}

			dst := t.TempDir()
			_, err := Unpack(context.Background(), archive, dst, password, UnpackOptions{})
			if !errors.Is(err, errs.ErrWrongPassword) {
				t.Errorf("Unpack() error = %v, want errors.Is(err, errs.ErrWrongPassword)", err)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack() left %v in the target, want nothing written without the right password", got)
			}
		})
	}
}

// 加密归档里坏一个字节或者少一段，Verify 和 Unpack 都要报错，Unpack 不写任何文件。
// 改盐、改校验值算口令不对；改公共头、改密文算归档坏了。
// 校验值谁都能重算，所以改完再把 CRC64 算成对的也要拦下，这是认证标签的活
func TestEncryptedArchiveDamageIsDetected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), strings.Repeat("内容 abc\n", 200))
	const password = "pw"
	archive := packTreeEnc(t, root, scanOK(t, root), AlgorithmLZ77, password)
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
	// withCRC 表示改完之后按格式约定把公共头里的 CRC64 重算成对的
	withCRC := func(damage func([]byte) []byte) func([]byte) []byte {
		return func(raw []byte) []byte {
			raw = damage(raw)
			crc := crc64.New(crc64.MakeTable(crc64.ECMA))
			_, _ = crc.Write(raw[:16])
			_, _ = crc.Write(make([]byte, 8))
			_, _ = crc.Write(raw[24:92])
			_, _ = crc.Write(make([]byte, 32))
			_, _ = crc.Write(raw[124:])
			binary.LittleEndian.PutUint64(raw[16:24], crc.Sum64())
			return raw
		}
	}
	cipherStart := int(archiveEncDataOffset)

	tests := []struct {
		name   string
		damage func([]byte) []byte
		want   error
		detail string // 错误里应该出现的说明，空串表示不查
	}{
		{"公共头里的校验值", flip(16), errs.ErrCorrupted, ""},
		{"公共头里的数据偏移", flip(8), errs.ErrCorrupted, ""},
		{"扩展头里的盐", flip(24), errs.ErrWrongPassword, ""},
		{"扩展头里的校验值", flip(60), errs.ErrWrongPassword, ""},
		{"扩展头里的 IV", flip(40), errs.ErrCorrupted, ""},
		{"密文中间", flip(cipherStart + 16), errs.ErrCorrupted, ""},
		{"密文最后一块", flip(len(raw) - 1), errs.ErrCorrupted, ""},
		{"密文截断一个字节", func(raw []byte) []byte { return raw[:len(raw)-1] }, errs.ErrCorrupted, ""},
		{"密文只剩扩展头", func(raw []byte) []byte { return raw[:cipherStart] }, errs.ErrCorrupted, ""},
		{"扩展头没读完", func(raw []byte) []byte { return raw[:50] }, errs.ErrCorrupted, ""},

		{"IV，校验值重算过", withCRC(flip(40)), errs.ErrCorrupted, "authentication failed"},
		{"密文中间，校验值重算过", withCRC(flip(cipherStart + 16)), errs.ErrCorrupted, "authentication failed"},
		{"认证标签，校验值重算过", withCRC(flip(92)), errs.ErrCorrupted, "authentication failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			broken := filepath.Join(t.TempDir(), "broken.dbkp")
			if err := os.WriteFile(broken, tc.damage(bytes.Clone(raw)), 0o644); err != nil {
				t.Fatalf("Setup failed: write the damaged archive: %v", err)
			}

			check := func(err error) error {
				var e *errs.Error
				switch {
				case !errors.Is(err, tc.want):
					return fmt.Errorf("error = %v, want errors.Is(err, %v)", err, tc.want)
				case tc.detail != "" && (!errors.As(err, &e) || !strings.Contains(e.Detail, tc.detail)):
					return fmt.Errorf("error = %v, want its detail to mention %q", err, tc.detail)
				}
				return nil
			}
			if problem := check(Verify(context.Background(), broken, password, VerifyOptions{})); problem != nil {
				t.Errorf("Verify(%q) %v", broken, problem)
			}
			dst := t.TempDir()
			_, err := Unpack(context.Background(), broken, dst, password, UnpackOptions{})
			if problem := check(err); problem != nil {
				t.Errorf("Unpack(%q, %q) %v", broken, dst, problem)
			}
			if got := entryNames(t, dst); len(got) != 0 {
				t.Errorf("Unpack(%q, %q) left %v in the target, want nothing written from a damaged archive",
					broken, dst, got)
			}
		})
	}
}

// 迭代次数是从归档里读来的，不能让它想写多大就多大：
// 是 0 或者超过上限的归档按坏归档拒绝，不去派生密钥
func TestEncryptedArchiveRejectsAbsurdIterations(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "内容")
	archive := packTreeEnc(t, root, scanOK(t, root), AlgorithmNone, "pw")
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}

	for _, tc := range []struct {
		name       string
		iterations uint32
	}{
		{"0 轮", 0},
		{"刚好超过上限", aescbc.MaxIterations + 1},
		{"接近 uint32 的上限", 0xFFFFFFF0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			broken := bytes.Clone(raw)
			binary.LittleEndian.PutUint32(broken[56:60], tc.iterations)
			path := filepath.Join(t.TempDir(), "broken.dbkp")
			if err := os.WriteFile(path, broken, 0o644); err != nil {
				t.Fatalf("Setup failed: write the damaged archive: %v", err)
			}
			start := time.Now()
			err := Verify(context.Background(), path, "pw", VerifyOptions{})
			if !errors.Is(err, errs.ErrCorrupted) {
				t.Errorf("Verify() error = %v, want errs.Corrupted", err)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("Verify() took %v with %d iterations in the header, want it to refuse before deriving", d, tc.iterations)
			}
		})
	}
}

// 加密归档里面再套一层加密，读的时候按不支持拒绝，不能落到“算法不认识”上
func TestNestedEncryptionIsRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "内容")
	inner := packTreeEnc(t, root, scanOK(t, root), AlgorithmNone, "pw")

	var h encHeader
	copy(h.salt[:], frozenSalt())
	copy(h.iv[:], frozenIV())
	h.iterations = testIterations
	outer := filepath.Join(t.TempDir(), "nested.dbkp")
	if err := sealArchive(context.Background(), inner, outer, "pw", h); err != nil {
		t.Fatalf("Setup failed: sealArchive() error = %v", err)
	}

	if err := Verify(context.Background(), outer, "pw", VerifyOptions{}); !errors.Is(err, errs.ErrUnsupportedVersion) {
		t.Errorf("Verify() error = %v, want errors.Is(err, errs.ErrUnsupportedVersion)", err)
	}
	if _, err := Unpack(context.Background(), outer, t.TempDir(), "pw", UnpackOptions{}); !errors.Is(err, errs.ErrUnsupportedVersion) {
		t.Errorf("Unpack() error = %v, want errors.Is(err, errs.ErrUnsupportedVersion)", err)
	}
}

// 先加密再压缩没有意义：密文没有冗余，压缩算法压不动，比先压缩再加密大得多
func TestEncryptThenCompressDoesNotShrink(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "readme.txt"), strings.Repeat("备份要能还原回来，压缩利用冗余，加密消除冗余。\n", 2000))
	entries := scanOK(t, root)

	compressThenEncrypt := fileSize(t, packTreeEnc(t, root, entries, AlgorithmLZ77, "pw"))
	encrypted := packTreeEnc(t, root, entries, AlgorithmNone, "pw")

	in, err := os.Open(encrypted)
	if err != nil {
		t.Fatalf("Setup failed: open the archive: %v", err)
	}
	defer func() { _ = in.Close() }()
	var recompressed bytes.Buffer
	if err := lz77.Compress(in, &recompressed); err != nil {
		t.Fatalf("Setup failed: compress the ciphertext: %v", err)
	}
	encryptThenCompress := int64(recompressed.Len())

	t.Logf("先压缩再加密 %d 字节，先加密再压缩 %d 字节", compressThenEncrypt, encryptThenCompress)
	if encryptThenCompress <= compressThenEncrypt {
		t.Errorf("先加密再压缩得到 %d 字节，先压缩再加密得到 %d 字节，want 后者更小",
			encryptThenCompress, compressThenEncrypt)
	}
}

// ---------- 辅助函数 ----------

// readIndexOKPassword 执行 ReadIndex，出错时让测试立即失败
func readIndexOKPassword(t *testing.T, archive, password string) []Record {
	t.Helper()
	records, err := ReadIndex(archive, password)
	if err != nil {
		t.Fatalf("Setup failed: ReadIndex(%q) error = %v", archive, err)
	}
	return records
}

// packTreeEnc 用 algo 压缩、password 加密，打包 root 下的 entries
func packTreeEnc(t *testing.T, root string, entries []Entry, algo Algorithm, password string) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "backup.dbkp")
	opts := PackOptions{Algorithm: algo, Password: password}
	if _, err := Pack(context.Background(), archive, root, entries, opts); err != nil {
		t.Fatalf("Setup failed: Pack() error = %v", err)
	}
	return archive
}

// mustDerive 按一份加密归档里的盐和迭代次数派生密钥
func mustDerive(t *testing.T, password string, raw []byte) aescbc.Keys {
	t.Helper()
	iterations := binary.LittleEndian.Uint32(raw[56:60])
	keys, err := aescbc.DeriveKeys(password, raw[24:40], int(iterations))
	if err != nil {
		t.Fatalf("Setup failed: DeriveKeys() error = %v", err)
	}
	return keys
}

// decryptBytes 按字段表手工解开一份加密归档，返回内层 .dbkp 的字节
func decryptBytes(t *testing.T, raw []byte, password string) []byte {
	t.Helper()
	if len(raw) < int(archiveEncDataOffset) {
		t.Fatalf("Setup failed: the encrypted archive is only %d bytes", len(raw))
	}
	keys := mustDerive(t, password, raw)
	var plain bytes.Buffer
	if err := aescbc.Decrypt(bytes.NewReader(raw[archiveEncDataOffset:]), &plain, keys.Encrypt, raw[40:56]); err != nil {
		t.Fatalf("Setup failed: decrypt the archive: %v", err)
	}
	return plain.Bytes()
}

// checkStoredCRC 核对公共头里的 CRC64：按约定，回填的校验值和认证标签两栏都按 0 计入
func checkStoredCRC(raw []byte) error {
	crc := crc64.New(crc64.MakeTable(crc64.ECMA))
	_, _ = crc.Write(raw[:16])
	_, _ = crc.Write(make([]byte, 8))
	_, _ = crc.Write(raw[24:92])
	_, _ = crc.Write(make([]byte, 32))
	_, _ = crc.Write(raw[124:])
	if stored := binary.LittleEndian.Uint64(raw[16:24]); crc.Sum64() != stored {
		return fmt.Errorf("stored CRC64 = %#x, want %#x", stored, crc.Sum64())
	}
	return nil
}

// storedMAC 按字段表重算一份加密归档的认证标签：
// 公共头里的 CRC64、扩展头里的认证标签自己按 0 计入
func storedMAC(keys aescbc.Keys, raw []byte) []byte {
	mac := hmac.New(sha256.New, keys.Auth[:])
	_, _ = mac.Write(raw[:16])
	_, _ = mac.Write(make([]byte, 8))
	_, _ = mac.Write(raw[24:92])
	_, _ = mac.Write(make([]byte, 32))
	_, _ = mac.Write(raw[124:])
	return mac.Sum(nil)
}

// frozenSalt 是扩展头测试用的盐，取值 0x00..0x0f
func frozenSalt() []byte {
	return []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
}

// frozenIV 是扩展头测试用的 IV，取值 0x10..0x1f
func frozenIV() []byte {
	return []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}
}

// frozenVerifier 是口令 "pw" 配上面的盐、1000 轮派生出的校验值，和 aescbc 包里钉的是同一个值
const frozenVerifier = "e411b8875d072ba9686fe47f3bdab0354e84b85ef66e384f6775ef6422da7d4d"
