package huffman

import "bufio"

type bitWriter struct {
	w   *bufio.Writer
	cur byte
	n   uint8 // cur 里已经填好的位数
}

// write 按高位在前写一串 '0'/'1'
func (b *bitWriter) write(code string) error {
	for i := 0; i < len(code); i++ {
		b.cur = b.cur<<1 | (code[i] - '0')
		b.n++
		if b.n == 8 {
			if err := b.w.WriteByte(b.cur); err != nil {
				return err
			}
			b.cur, b.n = 0, 0
		}
	}
	return nil
}

// flush 写出末尾不足一字节的部分，低位补零
func (b *bitWriter) flush() error {
	if b.n == 0 {
		return nil
	}
	cur := b.cur << (8 - b.n)
	b.cur, b.n = 0, 0
	return b.w.WriteByte(cur)
}

type bitReader struct {
	r   *bufio.Reader
	cur byte
	n   uint8 // cur 里还没用掉的位数
}

// next 返回下一位
func (b *bitReader) next() (byte, error) {
	if b.n == 0 {
		c, err := b.r.ReadByte()
		if err != nil {
			return 0, err
		}
		b.cur, b.n = c, 8
	}
	b.n--
	return (b.cur >> b.n) & 1, nil
}

// rest 返回当前字节还没用掉的位数和这些位的值
func (b *bitReader) rest() (uint8, byte) {
	return b.n, b.cur & ((1 << b.n) - 1)
}
