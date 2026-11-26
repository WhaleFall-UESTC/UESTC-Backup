package aescbc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
)

// KeySize 是 AES-256 密钥的长度，也是认证密钥的长度
const KeySize = 32

// Iterations 是打包时口令派生的默认迭代次数。
// MaxIterations 是读归档时接受的上限：归档里写的次数是不可信的，再大只会把人拖慢，不会更安全
const (
	Iterations    = 600_000
	MaxIterations = 20_000_000
)

// ErrPadding 表示密文长度不对，或者去掉补齐后不是合法的 PKCS#7
var ErrPadding = errors.New("aescbc: invalid padding")

var errIterations = errors.New("aescbc: iterations must be positive")

// keysInfo 是把派生结果分成两把密钥时用的上下文串，换掉它派生出的密钥就全变了
const keysInfo = "UESTC-Backup AES-256-CBC + HMAC-SHA256"

// Keys 是一份加密归档用的两把密钥：一把给 AES-CBC 加密，一把给 HMAC 认证
type Keys struct {
	Encrypt [KeySize]byte
	Auth    [KeySize]byte
}

// DeriveKeys 用口令和盐派生两把密钥：先做 iterations 轮 PBKDF2-HMAC-SHA256，
// 再展开成加密密钥和认证密钥。轮数越多，拿到归档的人离线穷举口令越慢
func DeriveKeys(password string, salt []byte, iterations int) (Keys, error) {
	if iterations < 1 {
		return Keys{}, errIterations
	}
	secret, err := pbkdf2.Key(sha256.New, password, salt, iterations, KeySize)
	if err != nil {
		return Keys{}, err
	}
	both, err := hkdf.Expand(sha256.New, secret, keysInfo, 2*KeySize)
	if err != nil {
		return Keys{}, err
	}
	var k Keys
	copy(k.Encrypt[:], both[:KeySize])
	copy(k.Auth[:], both[KeySize:])
	return k, nil
}

// Verifier 是存进归档、用来核对口令的值。它是加密密钥的哈希，拿到它也推不出密钥
func (k Keys) Verifier() [sha256.Size]byte {
	return sha256.Sum256(k.Encrypt[:])
}

// MAC 返回用认证密钥初始化的 HMAC-SHA256
func (k Keys) MAC() hash.Hash {
	return hmac.New(sha256.New, k.Auth[:])
}

// DeriveKey 用口令和盐派生 32 字节密钥
func DeriveKey(password string, salt []byte) [KeySize]byte {
	h := sha256.New()
	h.Write([]byte(password))
	h.Write(salt)
	var key [KeySize]byte
	copy(key[:], h.Sum(nil))
	return key
}

// Verifier 是存进归档、用来核对口令的值。它是密钥的哈希，拿到它也推不出密钥
func Verifier(key [KeySize]byte) [sha256.Size]byte {
	return sha256.Sum256(key[:])
}

// Encrypt 用 key 和 iv 把 r 加密到 w，末尾按 PKCS#7 补齐
func Encrypt(r io.Reader, w io.Writer, key [KeySize]byte, iv []byte) error {
	mode, err := newCBCMode(key, iv, true)
	if err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	var tail []byte
	for {
		n, rerr := r.Read(buf)
		tail = append(tail, buf[:n]...)
		full := len(tail) / aes.BlockSize * aes.BlockSize
		if err := cryptAndWrite(mode, w, tail[:full]); err != nil {
			return err
		}
		tail = tail[full:]
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return rerr
		}
	}
	return encryptLastBlock(mode, w, tail)
}

// Decrypt 用 key 和 iv 把 r 解密到 w，去掉末尾的 PKCS#7 补齐。
// 密文坏掉时前面写出的明文已经落盘，调用方要自己清理
func Decrypt(r io.Reader, w io.Writer, key [KeySize]byte, iv []byte) error {
	mode, err := newCBCMode(key, iv, false)
	if err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	var tail []byte
	for {
		n, rerr := r.Read(buf)
		tail = append(tail, buf[:n]...)
		// 最后一块留到读完再处理：去补齐要看它的末字节
		if len(tail) > aes.BlockSize {
			full := (len(tail) - 1) / aes.BlockSize * aes.BlockSize
			if err := cryptAndWrite(mode, w, tail[:full]); err != nil {
				return err
			}
			tail = tail[full:]
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return rerr
		}
	}
	return decryptLastBlock(mode, w, tail)
}

// newCBCMode 按方向建出 CBC 模式，iv 的长度必须是 16
func newCBCMode(key [KeySize]byte, iv []byte, encrypt bool) (cipher.BlockMode, error) {
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("aescbc: iv is %d bytes, want %d", len(iv), aes.BlockSize)
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	if encrypt {
		return cipher.NewCBCEncrypter(block, iv), nil
	}
	return cipher.NewCBCDecrypter(block, iv), nil
}

// cryptAndWrite 原地加密或解密 p 并写出
func cryptAndWrite(mode cipher.BlockMode, w io.Writer, p []byte) error {
	if len(p) == 0 {
		return nil
	}
	mode.CryptBlocks(p, p)
	_, err := w.Write(p)
	return err
}

// encryptLastBlock 把不足一块的尾巴补成 PKCS#7 的一块再加密
func encryptLastBlock(mode cipher.BlockMode, w io.Writer, tail []byte) error {
	last := make([]byte, aes.BlockSize)
	copy(last, tail)
	for i := len(tail); i < len(last); i++ {
		last[i] = byte(len(last) - len(tail))
	}
	return cryptAndWrite(mode, w, last)
}

// decryptLastBlock 解最后一块，核对并去掉 PKCS#7 补齐
func decryptLastBlock(mode cipher.BlockMode, w io.Writer, tail []byte) error {
	if len(tail) != aes.BlockSize {
		return ErrPadding
	}
	last := make([]byte, aes.BlockSize)
	mode.CryptBlocks(last, tail)
	n := int(last[len(last)-1])
	if n == 0 || n > len(last) {
		return ErrPadding
	}
	for _, b := range last[len(last)-n:] {
		if int(b) != n {
			return ErrPadding
		}
	}
	_, err := w.Write(last[:len(last)-n])
	return err
}
