package aescbc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// KeySize 是 AES-256 的密钥长度
const KeySize = 32

// ErrPadding 表示密文长度不对，或者去掉补齐后不是合法的 PKCS#7
var ErrPadding = errors.New("aescbc: invalid padding")

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
