package backup

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// 打包的进度按文件推进，收尾的数等于扫描结果；压缩、加密各有一条阶段事件
func TestPackProgressPhases(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), strings.Repeat("内容", 100))
	writeFile(t, filepath.Join(src, "sub", "b.bin"), string(dataBytes(4096)))
	entries := scanOK(t, src)
	wantFiles, wantBytes := countEntries(entries)

	var events []Progress
	opts := PackOptions{Algorithm: AlgorithmLZ77, Password: "pw", Progress: func(p Progress) {
		events = append(events, p)
	}}
	if _, err := Pack(context.Background(), filepath.Join(t.TempDir(), "x.dbkp"), src, entries, opts); err != nil {
		t.Fatalf("Pack() error = %v", err)
	}

	var packing []Progress
	for _, p := range events {
		if p.Phase == PhasePacking {
			packing = append(packing, p)
		}
	}
	if len(packing) != wantFiles {
		t.Fatalf("Pack() sent %d packing events, want %d", len(packing), wantFiles)
	}
	for i := 1; i < len(packing); i++ {
		if packing[i].DoneFiles <= packing[i-1].DoneFiles || packing[i].DoneBytes < packing[i-1].DoneBytes {
			t.Fatalf("packing event %d goes backwards: %+v after %+v", i, packing[i], packing[i-1])
		}
	}
	last := packing[len(packing)-1]
	if last.DoneFiles != wantFiles || last.DoneBytes != wantBytes || last.TotalFiles != wantFiles || last.TotalBytes != wantBytes {
		t.Errorf("last packing event = %+v, want %d files and %d bytes", last, wantFiles, wantBytes)
	}
	for _, phase := range []Phase{PhaseCompressing, PhaseEncrypting} {
		if !hasPhase(events, phase) {
			t.Errorf("Pack() sent no %s event", phase)
		}
	}
}

// 跳过的文件也算处理过，进度照样走到头
func TestPackProgressCountsSkippedFiles(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	entries := append(scanOK(t, src), Entry{Path: "missing.txt", Kind: KindFile, Size: 100})

	var last Progress
	opts := PackOptions{Progress: func(p Progress) { last = p }}
	if _, err := Pack(context.Background(), filepath.Join(t.TempDir(), "x.dbkp"), src, entries, opts); err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	if last.DoneFiles != 2 || last.DoneBytes != 105 || last.TotalFiles != 2 || last.TotalBytes != 105 {
		t.Errorf("last event = %+v, want 2 files and 105 bytes", last)
	}
}

// 还原的进度按文件推进，收尾等于归档里的文件数；取消之后目标目录不留文件
func TestUnpackProgressAndCancel(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), strings.Repeat("a", 1000))
	writeFile(t, filepath.Join(src, "b.txt"), strings.Repeat("b", 1000))
	archive := packTree(t, src, scanOK(t, src))

	t.Run("正常结束", func(t *testing.T) {
		var last Progress
		opts := UnpackOptions{Progress: func(p Progress) { last = p }}
		dst := filepath.Join(t.TempDir(), "restore")
		if _, err := Unpack(context.Background(), archive, dst, "", opts); err != nil {
			t.Fatalf("Unpack() error = %v", err)
		}
		if last.Phase != PhaseRestoring || last.DoneFiles != 2 || last.DoneBytes != 2000 {
			t.Errorf("last event = %+v, want 2 files and 2000 bytes restored", last)
		}
	})

	t.Run("取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		dst := filepath.Join(t.TempDir(), "restore")
		_, err := Unpack(ctx, archive, dst, "", UnpackOptions{Progress: func(Progress) { cancel() }})
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Unpack() error = %v, want context.Canceled", err)
		}
		if got := entryNames(t, dst); len(got) != 0 {
			t.Errorf("Unpack() left %v in the target, want nothing after a cancel", got)
		}
	})
}

// 校验的进度和取消
func TestVerifyProgressAndCancel(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), strings.Repeat("a", 1000))
	writeFile(t, filepath.Join(src, "b.txt"), strings.Repeat("b", 1000))
	archive := packTree(t, src, scanOK(t, src))

	var last Progress
	opts := VerifyOptions{Progress: func(p Progress) { last = p }}
	if err := Verify(context.Background(), archive, "", opts); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if last.Phase != PhaseVerifying || last.DoneFiles != 2 || last.DoneBytes != 2000 {
		t.Errorf("last event = %+v, want 2 files and 2000 bytes verified", last)
	}

	ctx, cancel := context.WithCancel(context.Background())
	err := Verify(ctx, archive, "", VerifyOptions{Progress: func(Progress) { cancel() }})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Verify() with a canceling progress error = %v, want context.Canceled", err)
	}
}

// hasPhase 判断事件流里有没有某个阶段
func hasPhase(events []Progress, phase Phase) bool {
	for _, p := range events {
		if p.Phase == phase {
			return true
		}
	}
	return false
}
