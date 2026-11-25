package huffman

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

// firstDiff 返回两个切片第一个不同的下标，前面都一样时返回较短的长度
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

func TestRoundTrip(t *testing.T) {
	cases := map[string][]byte{
		"repeated text":      []byte(strings.Repeat("the quick brown fox jumps over the lazy dog ", 2000)),
		"all byte values":    allBytes(),
		"random":             randomBytes(64*1024, 1),
		"larger than buffer": randomLetters(4<<20, 2),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			roundTrip(t, data)
		})
	}
}

// 讲义的例子 ABACCDA，树是 A 0、C 10、B 110、D 111，共 13 位
func TestKnownExample(t *testing.T) {
	encoded := roundTrip(t, []byte("ABACCDA"))

	if got, want := len(encoded), headerSize+2; got != want {
		t.Fatalf("encoded size = %d, want %d", got, want)
	}
	if padding := encoded[paddingIndex]; padding != 3 {
		t.Fatalf("padding = %d, want 3", padding)
	}
	if bits := (len(encoded)-headerSize)*8 - int(encoded[paddingIndex]); bits != 13 {
		t.Fatalf("encoded bits = %d, want 13", bits)
	}
	if got, want := bitString(encoded[headerSize:], 13), "0110010101110"; got != want {
		t.Fatalf("bit stream = %s, want %s", got, want)
	}
	for c, want := range map[byte]uint64{'A': 3, 'B': 1, 'C': 2, 'D': 1} {
		if got := binary.LittleEndian.Uint64(encoded[freqIndex+int(c)*8:]); got != want {
			t.Fatalf("frequency of %q = %d, want %d", c, got, want)
		}
	}
}

func TestEmptyInput(t *testing.T) {
	encoded := roundTrip(t, nil)
	if len(encoded) != headerSize {
		t.Fatalf("encoded size = %d, want %d", len(encoded), headerSize)
	}
}

func TestSingleByteValue(t *testing.T) {
	for _, n := range []int{1, 7, 8, 1000} {
		encoded := roundTrip(t, bytes.Repeat([]byte{'x'}, n))
		if got, want := encoded[paddingIndex], uint8((8-n%8)%8); got != want {
			t.Fatalf("%d bytes: padding = %d, want %d", n, got, want)
		}
	}
}

func TestCompressesRepeatedText(t *testing.T) {
	data := []byte(strings.Repeat("abracadabra ", 4000))
	if encoded := roundTrip(t, data); len(encoded) >= len(data) {
		t.Fatalf("encoded size = %d, want less than %d", len(encoded), len(data))
	}
}

func TestDecompressCorrupted(t *testing.T) {
	valid := encode(t, []byte("ABACCDA"))
	cases := map[string][]byte{
		"empty":           nil,
		"short header":    valid[:headerSize-1],
		"bad magic":       mutate(valid, func(b []byte) { b[0] = 'X' }),
		"padding too big": mutate(valid, func(b []byte) { b[paddingIndex] = 8 }),
		"size mismatch":   mutate(valid, func(b []byte) { b[sizeIndex] = 8 }),
		"no data":         valid[:headerSize],
		"truncated data":  valid[:len(valid)-1],
		"dirty padding":   mutate(valid, func(b []byte) { b[len(b)-1] |= 1 }),
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
		t.Fatalf("Compress: err = %v, want i/o error", err)
	}
	if err := Compress(bytes.NewReader([]byte("hello")), errWriter{err: boom}); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Compress write: err = %v, want i/o error", err)
	}
	if err := Decompress(errReader{err: boom}, io.Discard); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Decompress read: err = %v, want i/o error", err)
	}
	valid := encode(t, []byte("hello"))
	if err := Decompress(bytes.NewReader(valid), errWriter{err: boom}); !errors.Is(err, errs.ErrIO) {
		t.Fatalf("Decompress write: err = %v, want i/o error", err)
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

func mutate(data []byte, f func([]byte)) []byte {
	out := bytes.Clone(data)
	f(out)
	return out
}

// bitString 把前 n 位写成 '0'/'1' 的串
func bitString(data []byte, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte('0' + (data[i/8] >> (7 - i%8) & 1))
	}
	return b.String()
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error)       { return 0, r.err }
func (r errReader) Seek(int64, int) (int64, error) { return 0, nil }

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }
