package huffman

import (
	"bufio"
	"container/heap"
	"encoding/binary"
	"io"

	"client/internal/errs"
)

// 压缩数据的布局，整数小端序：
//
//	偏移  宽度   字段
//	   0    4   Magic        "HUF1"
//	   4    1   Padding      末尾补零的位数，0..7
//	   5    8   OrigSize     原始字节数
//	  13 2048   Frequencies  256 个字节值的出现次数，各 8 字节
//	2061   ...  编码数据     按位打包，高位在前，末字节低位补零
//
// 词频表放定宽整数，解压时用同一张表重建哈夫曼树
const (
	magic        = "HUF1"
	headerSize   = 4 + 1 + 8 + symbolCount*8
	paddingIndex = 4
	sizeIndex    = 5
	freqIndex    = 13
	symbolCount  = 256
	readBufSize  = 32 * 1024
)

// Compress 把 r 从头开始按 Huffman 编码写到 w，内部扫两遍所以 r 要能 Seek
func Compress(r io.ReadSeeker, w io.Writer) error {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return errs.Wrap(err, errs.IO)
	}

	var freq [symbolCount]uint64
	var size uint64
	err := eachChunk(r, func(p []byte) error {
		for _, b := range p {
			freq[b]++
		}
		size += uint64(len(p))
		return nil
	})
	if err != nil {
		return errs.Wrap(err, errs.IO)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return errs.Wrap(err, errs.IO)
	}

	codes := buildCodes(buildTree(&freq))
	var bits uint64
	for b, f := range freq {
		bits += f * uint64(len(codes[b]))
	}

	header := make([]byte, headerSize)
	copy(header, magic)
	header[paddingIndex] = uint8((8 - bits%8) % 8)
	binary.LittleEndian.PutUint64(header[sizeIndex:], size)
	for b, f := range freq {
		binary.LittleEndian.PutUint64(header[freqIndex+b*8:], f)
	}

	bw := bufio.NewWriterSize(w, readBufSize)
	if _, err := bw.Write(header); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	bitsw := &bitWriter{w: bw}
	err = eachChunk(r, func(p []byte) error {
		for _, b := range p {
			if err := bitsw.write(codes[b]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return errs.Wrap(err, errs.IO)
	}
	if err := bitsw.flush(); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	if err := bw.Flush(); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	return nil
}

// Decompress 解开 Huffman 编码的数据写到 w
func Decompress(r io.Reader, w io.Writer) error {
	br := bufio.NewReaderSize(r, readBufSize)
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(br, header); err != nil {
		return incomplete(err)
	}
	if string(header[:4]) != magic {
		return errs.Newf(errs.Corrupted, "not a huffman stream")
	}
	padding := header[paddingIndex]
	if padding > 7 {
		return errs.Newf(errs.Corrupted, "padding is %d", padding)
	}

	var freq [symbolCount]uint64
	var total uint64
	for i := range freq {
		freq[i] = binary.LittleEndian.Uint64(header[freqIndex+i*8:])
		total += freq[i]
	}
	size := binary.LittleEndian.Uint64(header[sizeIndex:])
	if total != size {
		return errs.Newf(errs.Corrupted, "%d bytes but frequencies add up to %d", size, total)
	}

	root := buildTree(&freq)
	if root == nil {
		return nil
	}

	out := bufio.NewWriterSize(w, readBufSize)
	bits := &bitReader{r: br}
	if root.leaf() {
		// 只有一个符号时码表约定为 "0"，每个符号吃掉一个补出来的位
		for i := uint64(0); i < size; i++ {
			if _, err := bits.next(); err != nil {
				return incomplete(err)
			}
			if err := out.WriteByte(root.sym); err != nil {
				return errs.Wrap(err, errs.IO)
			}
		}
	} else {
		n := root
		for i := uint64(0); i < size; i++ {
			for !n.leaf() {
				bit, err := bits.next()
				if err != nil {
					return incomplete(err)
				}
				if bit == 0 {
					n = n.left
				} else {
					n = n.right
				}
			}
			if err := out.WriteByte(n.sym); err != nil {
				return errs.Wrap(err, errs.IO)
			}
			n = root
		}
	}
	if restBits, restValue := bits.rest(); restBits != padding || restValue != 0 {
		return errs.Newf(errs.Corrupted, "%d trailing bits, padding is %d", restBits, padding)
	}
	if err := out.Flush(); err != nil {
		return errs.Wrap(err, errs.IO)
	}
	return nil
}

// eachChunk 把 r 读到底，每块交给 fn
func eachChunk(r io.Reader, fn func([]byte) error) error {
	buf := make([]byte, readBufSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if ferr := fn(buf[:n]); ferr != nil {
				return ferr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// incomplete 把读到一半的错误归为坏数据
func incomplete(err error) error {
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return errs.Wrapf(io.ErrUnexpectedEOF, errs.Corrupted, "huffman data is incomplete")
	}
	return errs.Wrap(err, errs.IO)
}

type node struct {
	freq        uint64
	order       int
	sym         byte
	left, right *node
}

func (n *node) leaf() bool { return n.left == nil && n.right == nil }

// buildTree 用词频表建哈夫曼树，没有符号时返回 nil
func buildTree(freq *[symbolCount]uint64) *node {
	var h nodeHeap
	for i, f := range freq {
		if f > 0 {
			h = append(h, &node{freq: f, order: i, sym: byte(i)})
		}
	}
	heap.Init(&h)
	order := symbolCount
	for h.Len() > 1 {
		left := heap.Pop(&h).(*node)
		right := heap.Pop(&h).(*node)
		order++
		heap.Push(&h, &node{freq: left.freq + right.freq, order: order, left: left, right: right})
	}
	if len(h) == 0 {
		return nil
	}
	return h[0]
}

// buildCodes 从根往下生成码表，左 0 右 1；只有一个符号时约定为 "0"
func buildCodes(root *node) *[symbolCount]string {
	var codes [symbolCount]string
	if root == nil {
		return &codes
	}
	if root.leaf() {
		codes[root.sym] = "0"
		return &codes
	}
	var walk func(*node, string)
	walk = func(n *node, prefix string) {
		if n.leaf() {
			codes[n.sym] = prefix
			return
		}
		walk(n.left, prefix+"0")
		walk(n.right, prefix+"1")
	}
	walk(root, "")
	return &codes
}

// nodeHeap 按频率排小顶堆，频率相同按建节点顺序，保证每次建出的树一样
type nodeHeap []*node

func (h nodeHeap) Len() int { return len(h) }
func (h nodeHeap) Less(i, j int) bool {
	if h[i].freq != h[j].freq {
		return h[i].freq < h[j].freq
	}
	return h[i].order < h[j].order
}
func (h nodeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *nodeHeap) Push(x any)   { *h = append(*h, x.(*node)) }
func (h *nodeHeap) Pop() any {
	old := *h
	last := len(old) - 1
	item := old[last]
	*h = old[:last]
	return item
}
