package aescbc

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// testIterations 是测试用的迭代次数，取小值省时间
const testIterations = 1000

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

// 派生结果钉死，保证以后改了算法，旧归档还能解开。期望值是用 Python 的 hashlib.pbkdf2_hmac
// 和 hmac 按 RFC 8018、RFC 5869 另算的，没有借助被测代码。
// 换盐、换口令、换迭代次数都不能得到同一把密钥，加密和认证用的两把也不能一样
func TestDeriveKeys(t *testing.T) {
	keys, err := DeriveKeys("pw", testSalt, testIterations)
	if err != nil {
		t.Fatalf("DeriveKeys() error = %v", err)
	}
	if got := hex.EncodeToString(keys.Encrypt[:]); got != "e4f3ea89fe52a0e0f5188b0757091b4a6817810528c8dec5b3bf97886f87e691" {
		t.Errorf("DeriveKeys(%q).Encrypt = %s, want the frozen value", "pw", got)
	}
	if got := hex.EncodeToString(keys.Auth[:]); got != "bd22a794c83944211f3f9061b01002cdbd9e58a02104fcb1ac66efbb21202fc0" {
		t.Errorf("DeriveKeys(%q).Auth = %s, want the frozen value", "pw", got)
	}

	for name, other := range map[string]Keys{
		"换了盐":    mustDerive(t, "pw", dataBytes(16), testIterations),
		"换了口令":   mustDerive(t, "pw ", testSalt, testIterations),
		"换了迭代次数": mustDerive(t, "pw", testSalt, testIterations+1),
	} {
		if other == keys {
			t.Errorf("DeriveKeys() gave the same keys after %s", name)
		}
	}
	if keys.Encrypt == keys.Auth {
		t.Error("DeriveKeys() gave the same key for encryption and authentication")
	}
	if _, err := DeriveKeys("pw", testSalt, 0); err == nil {
		t.Error("DeriveKeys() with 0 iterations: error = nil, want one")
	}
}

// 校验值钉死，并且不能和密钥本身一样
func TestKeysVerifier(t *testing.T) {
	keys := mustDerive(t, "pw", testSalt, testIterations)
	got := keys.Verifier()
	if hex.EncodeToString(got[:]) != "e411b8875d072ba9686fe47f3bdab0354e84b85ef66e384f6775ef6422da7d4d" {
		t.Errorf("Verifier() = %x, want the frozen value", got)
	}
	if bytes.Equal(got[:], keys.Encrypt[:]) || bytes.Equal(got[:], keys.Auth[:]) {
		t.Error("Verifier() returned one of the keys")
	}
}

// 认证标签就是认证密钥下的 HMAC-SHA256，期望值同样是用 Python 的 hmac 另算的
func TestKeysMAC(t *testing.T) {
	mac := mustDerive(t, "pw", testSalt, testIterations).MAC()
	if _, err := mac.Write([]byte("备份要能还原回来")); err != nil {
		t.Fatalf("MAC() write error = %v", err)
	}
	const want = "e62eea5c9467b329e0e2c5dd7dd56fc50aa9efc0f09cc927a201047af3a7605a"
	if got := hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("MAC() over the test message = %s, want %s", got, want)
	}
}

// 默认的迭代次数能派生出密钥，并且不超过读归档时接受的上限
func TestDefaultIterations(t *testing.T) {
	if Iterations < 1 || Iterations > MaxIterations {
		t.Fatalf("Iterations = %d, want it between 1 and MaxIterations (%d)", Iterations, MaxIterations)
	}
	if _, err := DeriveKeys("pw", testSalt, Iterations); err != nil {
		t.Errorf("DeriveKeys() with the default iterations: error = %v", err)
	}
}

// ---------- 辅助函数 ----------

// mustDerive 执行 DeriveKeys，出错时让测试立即失败
func mustDerive(t *testing.T, password string, salt []byte, iterations int) Keys {
	t.Helper()
	keys, err := DeriveKeys(password, salt, iterations)
	if err != nil {
		t.Fatalf("DeriveKeys(%q) error = %v", password, err)
	}
	return keys
}

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
