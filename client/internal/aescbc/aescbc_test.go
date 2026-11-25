package aescbc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// testSalt 是测试用的盐，取值 0x00..0x0f
var testSalt = []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}

// testIV 是测试用的 IV，取值 0x10..0x1f
var testIV = []byte{0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}

// 各种长度的数据都要能原样往返，密文长度是补齐后的块数
func TestRoundTrip(t *testing.T) {
	key := DeriveKey("口令 pw", testSalt)
	for _, n := range []int{0, 1, 15, 16, 17, 255, 16 << 10, 100_000} {
		t.Run(fmt.Sprintf("%d 字节", n), func(t *testing.T) {
			plain := dataBytes(n)
			var cipherText bytes.Buffer
			if err := Encrypt(bytes.NewReader(plain), &cipherText, key, testIV); err != nil {
				t.Fatalf("Encrypt() error = %v", err)
			}
			if want := (n/16 + 1) * 16; cipherText.Len() != want {
				t.Errorf("Encrypt() wrote %d bytes, want %d", cipherText.Len(), want)
			}

			var got bytes.Buffer
			if err := Decrypt(&cipherText, &got, key, testIV); err != nil {
				t.Fatalf("Decrypt() error = %v", err)
			}
			if !bytes.Equal(got.Bytes(), plain) {
				t.Errorf("Decrypt() returned %d bytes, want the %d bytes put in", got.Len(), n)
			}
		})
	}
}

// 派生结果钉死，保证以后改了算法，旧归档还能解开
func TestDeriveKey(t *testing.T) {
	key := DeriveKey("pw", testSalt)
	const want = "aeccda1f1cd6e017ba424c99721582657657694a830db7f6b74142479ab05fd2"
	if got := hex.EncodeToString(key[:]); got != want {
		t.Errorf("DeriveKey(%q, salt 0x00..0x0f) = %s, want %s", "pw", got, want)
	}
	if DeriveKey("pw", dataBytes(16)) == key {
		t.Error("DeriveKey() gave the same key for a different salt")
	}
	if DeriveKey("pw ", testSalt) == key {
		t.Error("DeriveKey() gave the same key for a different password")
	}
}

// 校验值钉死，并且不能和密钥本身一样
func TestVerifier(t *testing.T) {
	key := DeriveKey("pw", testSalt)
	const want = "18fed495fc51dbb9824c230727f5282d49c7efd6b4c1278eb305c809e97bb872"
	got := Verifier(key)
	if hex.EncodeToString(got[:]) != want {
		t.Errorf("Verifier() = %x, want %s", got, want)
	}
	if bytes.Equal(got[:], key[:]) {
		t.Error("Verifier() returned the key itself")
	}
}

// 口令错了，或者密文坏了，都要报错而不是交出明文
func TestDecryptRejectsBadInput(t *testing.T) {
	key := DeriveKey("pw", testSalt)
	var cipherText bytes.Buffer
	if err := Encrypt(strings.NewReader("备份要能还原回来"), &cipherText, key, testIV); err != nil {
		t.Fatalf("Setup failed: Encrypt() error = %v", err)
	}
	raw := cipherText.Bytes()

	tests := []struct {
		name    string
		key     [KeySize]byte
		cipher  []byte
		wantPad bool
	}{
		{"口令不对", DeriveKey("pw!", testSalt), raw, true},
		{"密文截断一个字节", key, raw[:len(raw)-1], true},
		{"密文是空的", key, nil, true},
		{"最后一块乱掉", key, flipLast(raw), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got bytes.Buffer
			err := Decrypt(bytes.NewReader(tc.cipher), &got, tc.key, testIV)
			if err == nil {
				t.Fatalf("Decrypt() error = nil, want one; it returned %d bytes", got.Len())
			}
			if tc.wantPad && !errors.Is(err, ErrPadding) {
				t.Errorf("Decrypt() error = %v, want errors.Is(err, ErrPadding)", err)
			}
		})
	}
}

func TestRejectsBadIV(t *testing.T) {
	key := DeriveKey("pw", testSalt)
	if err := Encrypt(strings.NewReader("x"), &bytes.Buffer{}, key, testIV[:15]); err == nil {
		t.Error("Encrypt() with a 15-byte IV: error = nil, want one")
	}
	if err := Decrypt(bytes.NewReader(make([]byte, 16)), &bytes.Buffer{}, key, nil); err == nil {
		t.Error("Decrypt() with a nil IV: error = nil, want one")
	}
}

// ---------- 辅助函数 ----------

// dataBytes 生成 n 个确定的字节
func dataBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + 1 + i>>3)
	}
	return b
}

// flipLast 翻转密文最后一个字节的副本
func flipLast(cipherText []byte) []byte {
	b := bytes.Clone(cipherText)
	b[len(b)-1] ^= 0xFF
	return b
}
