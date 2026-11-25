package lz77

import (
	"bufio"
	"encoding/binary"
	"io"

	"client/internal/errs"
)

// 压缩数据的布局，整数小端序：
//
//	偏移  宽度  字段
//	   0    4  Magic  "LZ77"
//	   4  ...  记号序列，读到 EOF 结束
//
// 每个记号以 1 字节标签开头：
//
//	0x00  字面量段：u16 长度 N（1..65535），后跟 N 个原始字节
//	0x01  匹配：u16 距离 D（1..65535）、u8 长度减 minMatch
//
// 讲义里每个记号固定 4 字节，没有匹配的字符也占 4 字节，不重复的数据会涨到
// 四倍。这里把连续的字面量合并成段，每 64 KB 只多出三个字节
const (
	magic      = "LZ77"
	tagLiteral = 0
	tagMatch   = 1

	minMatch   = 4
	maxMatch   = 258
	maxLiteral = 65535
	windowSize = 65535
	ringSize   = windowSize + 1

	hashBits = 16
	hashSize = 1 << hashBits
	maxChain = 64

	readBufSize = 32 * 1024
	flushAt     = windowSize + readBufSize
	outCap      = flushAt + maxLiteral + maxMatch
	bufCap      = windowSize + maxMatch + readBufSize
)

// Compress 把 r 读到底，按 LZ77 编码写到 w，一遍扫描，不需要 Seek
func Compress(r io.Reader, w io.Writer) error {
	e := newEncoder(r, w)
	if _, err := e.w.WriteString(magic); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	if err := e.run(); err != nil {
		return err
	}
	if err := e.w.Flush(); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	return nil
}

// Decompress 解开 LZ77 编码的数据写到 w
func Decompress(r io.Reader, w io.Writer) error {
	d := &decoder{
		r:   bufio.NewReaderSize(r, readBufSize),
		w:   bufio.NewWriterSize(w, readBufSize),
		out: make([]byte, 0, outCap),
	}
	head := make([]byte, len(magic))
	if _, err := io.ReadFull(d.r, head); err != nil {
		return incomplete(err)
	}
	if string(head) != magic {
		return errs.Newf(errs.Corrupted, "not a lz77 stream")
	}
	for {
		tag, err := d.r.ReadByte()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errs.Wrap(err, errs.IO)
		}
		switch tag {
		case tagLiteral:
			err = d.literal()
		case tagMatch:
			err = d.match()
		default:
			return errs.Newf(errs.Corrupted, "unknown token %#x", tag)
		}
		if err != nil {
			return err
		}
		if err := d.flush(); err != nil {
			return err
		}
	}
	if _, err := d.w.Write(d.out); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	if err := d.w.Flush(); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	return nil
}

// incomplete 把读到一半的错误归为坏数据
func incomplete(err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return errs.Wrapf(io.ErrUnexpectedEOF, errs.Corrupted, "lz77 data is incomplete")
	}
	return errs.Wrap(err, errs.IO)
}

type encoder struct {
	r    io.Reader
	w    *bufio.Writer
	buf  []byte // 滑动窗口，buf[base:end] 是有效数据
	base int    // buf[0] 对应的输入偏移
	end  int    // 已读入数据的结束偏移
	eof  bool
	lit  []byte // 攒着还没写的字面量
	head []int  // 哈希桶，存最近的绝对位置
	prev []int  // 同一哈希的上一处位置，按窗口大小取环形下标
}

func newEncoder(r io.Reader, w io.Writer) *encoder {
	e := &encoder{
		r:    r,
		w:    bufio.NewWriterSize(w, readBufSize),
		buf:  make([]byte, bufCap),
		lit:  make([]byte, 0, maxLiteral),
		head: make([]int, hashSize),
		prev: make([]int, ringSize),
	}
	for i := range e.head {
		e.head[i] = -1
	}
	for i := range e.prev {
		e.prev[i] = -1
	}
	return e
}

func (e *encoder) run() error {
	pos := 0
	for {
		if err := e.fill(pos); err != nil {
			return errs.Wrap(err, errs.IO)
		}
		if pos >= e.end {
			break
		}
		maxLen := e.end - pos
		if maxLen > maxMatch {
			maxLen = maxMatch
		}
		dist, length := 0, 0
		if maxLen >= minMatch {
			dist, length = e.findMatch(pos, maxLen)
		}
		if length >= minMatch {
			if err := e.emitLiterals(); err != nil {
				return err
			}
			if err := e.emitMatch(dist, length); err != nil {
				return err
			}
			for i := 0; i < length; i++ {
				e.insert(pos + i)
			}
			pos += length
			continue
		}
		e.lit = append(e.lit, e.buf[pos-e.base])
		e.insert(pos)
		pos++
		if len(e.lit) == maxLiteral {
			if err := e.emitLiterals(); err != nil {
				return err
			}
		}
	}
	return e.emitLiterals()
}

// fill 保证 pos 之后至少还有 maxMatch 字节，读到文件尾就不管了
func (e *encoder) fill(pos int) error {
	for e.end-pos < maxMatch && !e.eof {
		if keep := pos - windowSize; keep > e.base {
			copy(e.buf, e.buf[keep-e.base:e.end-e.base])
			e.base = keep
		}
		n, err := e.r.Read(e.buf[e.end-e.base:])
		e.end += n
		switch {
		case err == io.EOF:
			e.eof = true
			return nil
		case err != nil:
			return err
		case n == 0:
			return io.ErrNoProgress
		}
	}
	return nil
}

// findMatch 在窗口里找 pos 处的最长匹配，找不到返回 0
func (e *encoder) findMatch(pos, maxLen int) (dist, length int) {
	i := pos - e.base
	cand := e.head[hash(e.buf[i], e.buf[i+1], e.buf[i+2])]
	for steps := 0; cand >= 0 && pos-cand <= windowSize && steps < maxChain; steps++ {
		c := cand - e.base
		l := 0
		for l < maxLen && e.buf[c+l] == e.buf[i+l] {
			l++
		}
		if l > length {
			length, dist = l, pos-cand
		}
		if length == maxLen {
			break
		}
		cand = e.prev[cand%ringSize]
	}
	if length < minMatch {
		return 0, 0
	}
	return dist, length
}

// insert 把 pos 处的三字节前缀记进哈希链
func (e *encoder) insert(pos int) {
	if pos+minMatch > e.end {
		return
	}
	i := pos - e.base
	h := hash(e.buf[i], e.buf[i+1], e.buf[i+2])
	e.prev[pos%ringSize] = e.head[h]
	e.head[h] = pos
}

func hash(a, b, c byte) int {
	return int((uint32(a)<<16 | uint32(b)<<8 | uint32(c)) * 2654435761 >> (32 - hashBits))
}

func (e *encoder) emitLiterals() error {
	if len(e.lit) == 0 {
		return nil
	}
	var hdr [3]byte
	hdr[0] = tagLiteral
	binary.LittleEndian.PutUint16(hdr[1:], uint16(len(e.lit)))
	if _, err := e.w.Write(hdr[:]); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	if _, err := e.w.Write(e.lit); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	e.lit = e.lit[:0]
	return nil
}

func (e *encoder) emitMatch(dist, length int) error {
	var token [4]byte
	token[0] = tagMatch
	binary.LittleEndian.PutUint16(token[1:], uint16(dist))
	token[3] = byte(length - minMatch)
	if _, err := e.w.Write(token[:]); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	return nil
}

type decoder struct {
	r   *bufio.Reader
	w   *bufio.Writer
	out []byte // 已解出但还没写出的数据，保留最后 windowSize 字节供匹配引用
}

// literal 追加一段字面量
func (d *decoder) literal() error {
	var hdr [2]byte
	if _, err := io.ReadFull(d.r, hdr[:]); err != nil {
		return incomplete(err)
	}
	n := int(binary.LittleEndian.Uint16(hdr[:]))
	if n == 0 {
		return errs.Newf(errs.Corrupted, "literal run of 0 bytes")
	}
	start := len(d.out)
	d.out = d.out[:start+n]
	if _, err := io.ReadFull(d.r, d.out[start:]); err != nil {
		return incomplete(err)
	}
	return nil
}

// match 从已解出的数据里往前复制，长度可以超过距离，按字节逐个复制就行
func (d *decoder) match() error {
	var hdr [3]byte
	if _, err := io.ReadFull(d.r, hdr[:]); err != nil {
		return incomplete(err)
	}
	dist := int(binary.LittleEndian.Uint16(hdr[:2]))
	length := int(hdr[2]) + minMatch
	if dist == 0 {
		return errs.Newf(errs.Corrupted, "match distance is 0")
	}
	if dist > len(d.out) {
		return errs.Newf(errs.Corrupted, "match distance %d but only %d bytes decoded", dist, len(d.out))
	}
	start := len(d.out) - dist
	for i := 0; i < length; i++ {
		d.out = append(d.out, d.out[start+i])
	}
	return nil
}

// flush 把窗口外的数据写出去
func (d *decoder) flush() error {
	if len(d.out) < flushAt {
		return nil
	}
	n := len(d.out) - windowSize
	if _, err := d.w.Write(d.out[:n]); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	copy(d.out, d.out[n:])
	d.out = d.out[:windowSize]
	return nil
}
