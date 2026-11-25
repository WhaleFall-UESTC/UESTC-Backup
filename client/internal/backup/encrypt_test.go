package backup

import (
	"bytes"
	"client/internal/aescbc"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc64"
	"os"
	"path/filepath"
	"testing"
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
	if offset := binary.LittleEndian.Uint64(got[8:16]); offset != 88 {
		t.Errorf("Pack() stored data offset %d, want 88", offset)
	}
	if want := int(archiveEncDataOffset) + (len(raw)/16+1)*16; len(got) != want {
		t.Errorf("Pack() wrote %d bytes, want %d", len(got), want)
	}
	if bytes.Equal(got[24:56], make([]byte, 32)) {
		t.Error("Pack() left the salt and IV all zero")
	}

	key := aescbc.DeriveKey(password, got[24:40])
	if verifier := aescbc.Verifier(key); !bytes.Equal(got[56:88], verifier[:]) {
		t.Errorf("Pack() stored verifier % x, want % x", got[56:88], verifier)
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

// 扩展头的字段顺序和宽度钉死：盐、IV、校验值各就各位，校验值是可复现的
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
		0x58, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // DataOffset = 88
	}
	if !bytes.Equal(got[:16], want) {
		t.Errorf("public header = % x, want % x", got[:16], want)
	}
	want = append(append(append([]byte{}, frozenSalt()...), frozenIV()...), frozenVerifier()...)
	if !bytes.Equal(got[24:88], want) {
		t.Errorf("encryption header = % x, want % x", got[24:88], want)
	}
	if err := checkStoredCRC(got); err != nil {
		t.Errorf("sealArchive() %v", err)
	}
}

// ---------- 辅助函数 ----------

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

// decryptBytes 按字段表手工解开一份加密归档，返回内层 .dbkp 的字节
func decryptBytes(t *testing.T, raw []byte, password string) []byte {
	t.Helper()
	if len(raw) < int(archiveEncDataOffset) {
		t.Fatalf("Setup failed: the encrypted archive is only %d bytes", len(raw))
	}
	key := aescbc.DeriveKey(password, raw[24:40])
	var plain bytes.Buffer
	if err := aescbc.Decrypt(bytes.NewReader(raw[archiveEncDataOffset:]), &plain, key, raw[40:56]); err != nil {
		t.Fatalf("Setup failed: decrypt the archive: %v", err)
	}
	return plain.Bytes()
}

// checkStoredCRC 核对公共头里的 CRC64，按“校验值字段按 0 计入”的约定
func checkStoredCRC(raw []byte) error {
	crc := crc64.New(crc64.MakeTable(crc64.ECMA))
	crc.Write(raw[:16])
	crc.Write(make([]byte, 8))
	crc.Write(raw[24:])
	if stored := binary.LittleEndian.Uint64(raw[16:24]); crc.Sum64() != stored {
		return fmt.Errorf("stored CRC64 = %#x, want %#x", stored, crc.Sum64())
	}
	return nil
}

// frozenSalt 是扩展头测试用的盐，取值 0x00..0x0f
func frozenSalt() []byte {
	return []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}
}

// frozenIV 是扩展头测试用的 IV，取值 0x10..0x1f
func frozenIV() []byte {
	return []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}
}

// frozenVerifier 是口令 "pw" 配上面的盐得到的校验值
func frozenVerifier() []byte {
	b, err := hex.DecodeString("18fed495fc51dbb9824c230727f5282d49c7efd6b4c1278eb305c809e97bb872")
	if err != nil {
		panic(err)
	}
	return b
}
