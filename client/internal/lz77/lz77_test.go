package lz77

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
	"strings"
	"testing"

	"client/internal/errs"
)

func encode(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Compress(bytes.NewReader(data), &out); err != nil {
		t.Fatalf("Compress: %v", err)
	}
	return out.Bytes()
}

func decode(t *testing.T, data []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Decompress(bytes.NewReader(data), &out); err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	return out.Bytes()
}

func roundTrip(t *testing.T, data []byte) []byte {
	t.Helper()
	encoded := encode(t, data)
	decoded := decode(t, encoded)
	if !bytes.Equal(decoded, data) {
		t.Fatalf("round trip of %d bytes: got %d bytes, first difference at offset %d",
			len(data), len(decoded), firstDiff(decoded, data))
	}
	return encoded
}

func firstDiff(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) < len(b) {
		return len(a)
	}
	return len(b)
}

type token struct {
	literals     []byte
	dist, length int
}

// parse 把压缩数据拆成记号，顺便检查每段都完整
func parse(t *testing.T, data []byte) []token {
	t.Helper()
	if len(data) < len(magic) || string(data[:len(magic)]) != magic {
		t.Fatalf("missing magic: %q", data)
	}
	var tokens []token
	for i := len(magic); i < len(data); {
		switch data[i] {
		case tagLiteral:
			if i+3 > len(data) {
				t.Fatalf("literal header cut at %d", i)
			}
			n := int(binary.LittleEndian.Uint16(data[i+1:]))
			if n == 0 || i+3+n > len(data) {
				t.Fatalf("bad literal run of %d bytes at %d", n, i)
			}
			tokens = append(tokens, token{literals: data[i+3 : i+3+n]})
			i += 3 + n
		case tagMatch:
			if i+4 > len(data) {
				t.Fatalf("match cut at %d", i)
			}
			tokens = append(tokens, token{
				dist:   int(binary.LittleEndian.Uint16(data[i+1:])),
				length: int(data[i+3]) + minMatch,
			})
			i += 4
		default:
			t.Fatalf("unknown tag %#x at %d", data[i], i)
		}
	}
	return tokens
}

func TestRoundTrip(t *testing.T) {
	cases := map[string][]byte{
		"empty":              nil,
		"overlapping run":    bytes.Repeat([]byte{'a'}, 1000),
		"repeated text":      []byte(strings.Repeat("the quick brown fox jumps over the lazy dog ", 24000)),
		"all byte values":    allBytes(),
		"no repetition":      randomBytes(64*1024, 1),
		"larger than window": randomLetters(1<<20, 2),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			roundTrip(t, data)
		})
	}
}

func TestShortInputs(t *testing.T) {
	for n := 0; n <= 8; n++ {
		data := randomBytes(n, int64(n))
		roundTrip(t, data)
		roundTrip(t, bytes.Repeat([]byte{'q'}, n))
	}
}

// 讲义的例子 abcabcabc：前三个字符没法引用，第四位起引用距离 3 的 6 个字节
func TestKnownExample(t *testing.T) {
	encoded := roundTrip(t, []byte("abcabcabc"))
	tokens := parse(t, encoded)
	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}
	if string(tokens[0].literals) != "abc" || tokens[0].dist != 0 {
		t.Fatalf("first token = %+v, want literal \"abc\"", tokens[0])
	}
	if tokens[1].literals != nil || tokens[1].dist != 3 || tokens[1].length != 6 {
		t.Fatalf("second token = %+v, want match(3, 6)", tokens[1])
	}
	if got, want := len(encoded), len(magic)+3+3+4; got != want {
		t.Fatalf("encoded size = %d, want %d", got, want)
	}
}

// 匹配刚好跨过读缓冲区的刷新位置，解压端也要跨过窗口的写出位置
func TestMatchAcrossBufferRefresh(t *testing.T) {
	pattern := randomBytes(1000, 7)
	data := bytes.Repeat(pattern, 200)
	encoded := roundTrip(t, data)
	if len(encoded) >= len(data)/4 {
		t.Fatalf("encoded size = %d, want less than %d", len(encoded), len(data)/4)
	}
}

// 距离超过窗口上限的旧数据不能再被引用
func TestWindowLimit(t *testing.T) {
	pattern := []byte("wxyz")
	pos := windowSize + 10
	e := newEncoder(bytes.NewReader(nil), io.Discard)
	e.end = pos + len(pattern)
	for i := 0; i < e.end; i++ {
		e.buf[i] = byte(i*31 + 7)
	}
	copy(e.buf[pos:], pattern)

	copy(e.buf[pos-windowSize-1:], pattern)
	e.insert(pos - windowSize - 1)
	if dist, length := e.findMatch(pos, len(pattern)); length != 0 {
		t.Fatalf("match at distance %d: got (%d, %d), want no match", windowSize+1, dist, length)
	}

	copy(e.buf[pos-windowSize:], pattern)
	e.insert(pos - windowSize)
	if dist, length := e.findMatch(pos, len(pattern)); dist != windowSize || length != len(pattern) {
		t.Fatalf("match at distance %d: got (%d, %d), want (%d, %d)",
			windowSize, dist, length, windowSize, len(pattern))
	}
}

func TestCompressesRepeatedText(t *testing.T) {
	data := []byte(strings.Repeat("abracadabra ", 80000))
	if encoded := roundTrip(t, data); len(encoded) >= len(data)/10 {
		t.Fatalf("encoded size = %d, want less than %d", len(encoded), len(data)/10)
	}
}

func TestLiteralsDoNotExpand(t *testing.T) {
	data := randomBytes(64*1024, 3)
	encoded := roundTrip(t, data)
	if len(encoded) > len(data)+16 {
		t.Fatalf("encoded size = %d, want at most %d", len(encoded), len(data)+16)
	}
}

func TestDecompressCorrupted(t *testing.T) {
	valid := encode(t, []byte("abcabcabc"))
	badMagic := bytes.Clone(valid)
	badMagic[0] = 'X'
	withTail := func(tail ...byte) []byte {
		return append(bytes.Clone(valid[:len(magic)]), tail...)
	}
	cases := map[string][]byte{
		"empty":                  nil,
		"short magic":            valid[:len(magic)-1],
		"bad magic":              badMagic,
		"unknown tag":            withTail(0x02),
		"literal run of 0":       withTail(tagLiteral, 0, 0),
		"literal header cut":     withTail(tagLiteral, 5),
		"literal data cut":       withTail(tagLiteral, 5, 0, 'a', 'b'),
		"match header cut":       withTail(tagMatch, 1),
		"match distance 0":       withTail(tagMatch, 0, 0, 0),
		"match beyond output":    withTail(tagMatch, 5, 0, 0),
		"match with no literals": withTail(tagMatch, 1, 0, 0),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Decompress(bytes.NewReader(data), &out); !errors.Is(err, errs.ErrCorrupted) {
				t.Fatalf("err = %v, want corrupted", err)
			}
		})
	}
}

func TestErrorsWrapIO(t *testing.T) {
	boom := errors.New("boom")
	if err := Compress(errReader{err: boom}, io.Discard); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Compress read: err = %v, want i/o error", err)
	}
	if err := Compress(stuckReader{}, io.Discard); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Compress stalled read: err = %v, want i/o error", err)
	}
	if err := Compress(bytes.NewReader([]byte("hello")), errWriter{err: boom}); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Compress write: err = %v, want i/o error", err)
	}
	valid := encode(t, []byte("hello"))
	if err := Decompress(errReader{err: boom}, io.Discard); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Decompress read: err = %v, want i/o error", err)
	}
	if err := Decompress(bytes.NewReader(valid), errWriter{err: boom}); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Decompress write: err = %v, want i/o error", err)
	}
}

// 压缩时写到一半失败（比如磁盘满）要报出来；超过 bufio 缓冲区的数据才会真正落到底层
func TestWriteErrorsMidStream(t *testing.T) {
	data := randomBytes(200*1024, 9)
	for _, left := range []int{0, 1024, 100 * 1024, 200 * 1024} {
		w := &limitWriter{left: left, err: errors.New("boom")}
		if err := Compress(bytes.NewReader(data), w); !errors.Is(err, errs.ErrIO) {
			t.Fatalf("fail with %d bytes left: err = %v, want i/o error", left, err)
		}
	}
	if err := Decompress(bytes.NewReader(encode(t, bytes.Repeat([]byte{'x'}, flushAt))), errWriter{err: errors.New("boom")}); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Decompress flush: err = %v, want i/o error", err)
	}
}

func BenchmarkCompressRepeated(b *testing.B) {
	data := []byte(strings.Repeat("the quick brown fox jumps over the lazy dog ", 24000))
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Compress(bytes.NewReader(data), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func allBytes() []byte {
	data := make([]byte, 0, 256*40)
	for i := 0; i < 40; i++ {
		for b := 0; b < 256; b++ {
			data = append(data, byte(b))
		}
	}
	return data
}

func randomBytes(n int, seed int64) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(data)
	return data
}

func randomLetters(n int, seed int64) []byte {
	r := rand.New(rand.NewSource(seed))
	data := make([]byte, n)
	for i := range data {
		data[i] = 'a' + byte(r.Intn(4))
	}
	return data
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

// stuckReader 一直返回 0, nil，用来测读取没有进展的情况
type stuckReader struct{}

func (stuckReader) Read([]byte) (int, error) { return 0, nil }

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// limitWriter 只接收 left 字节，超出部分返回 err
type limitWriter struct {
	left int
	err  error
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if len(p) > w.left {
		n := w.left
		w.left = 0
		return n, w.err
	}
	w.left -= len(p)
	return len(p), nil
}
