package backup

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc64"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"client/internal/errs"
)

// plaintextMarker 是测试里当明文用的内容，归档和密文里都不会碰巧出现
const plaintextMarker = "PLAINTEXT-MARKER-0123456789"

// 带口令打包时目标目录里从始至终不出现明文：原样归档和压缩后的归档都在本机的暂存目录里，
// 目标目录只落最后的密文，打完只剩归档本身，暂存目录也清空
func TestPackKeepsPlaintextOffDestination(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "secret.txt"), strings.Repeat(plaintextMarker, 40))
	entries := scanOK(t, root)
	dstDir := t.TempDir()

	var leaked []string
	opts := PackOptions{Algorithm: AlgorithmLZ77, Password: "pw", Progress: func(p Progress) {
		if p.Phase == PhaseCompressing || p.Phase == PhaseEncrypting {
			leaked = append(leaked, dirHolding(t, dstDir, plaintextMarker)...)
		}
	}}
	if _, err := Pack(context.Background(), filepath.Join(dstDir, "backup.dbkp"), root, entries, opts); err != nil {
		t.Fatalf("Pack() error = %v", err)
	}
	if len(leaked) > 0 {
		t.Errorf("the destination directory held plaintext while packing: %v", leaked)
	}
	if got := entryNames(t, dstDir); !slices.Equal(got, []string{"backup.dbkp"}) {
		t.Errorf("the destination directory holds %v, want only backup.dbkp", got)
	}
	if got := stagingFiles(t); len(got) != 0 {
		t.Errorf("the staging directory still holds %v after Pack", got)
	}
}

// 打包中途取消，暂存目录里的明文也一起清掉
func TestPackCancelClearsStaging(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "secret.txt"), strings.Repeat(plaintextMarker, 40))
	entries := scanOK(t, root)
	dstDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts := PackOptions{Algorithm: AlgorithmLZ77, Password: "pw", Progress: func(p Progress) {
		if p.Phase == PhaseCompressing {
			cancel()
		}
	}}
	_, err := Pack(ctx, filepath.Join(dstDir, "backup.dbkp"), root, entries, opts)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Pack() error = %v, want context.Canceled", err)
	}
	if got := stagingFiles(t); len(got) != 0 {
		t.Errorf("the staging directory still holds %v after the cancelled Pack", got)
	}
	if got := entryNames(t, dstDir); len(got) != 0 {
		t.Errorf("the destination directory holds %v, want nothing", got)
	}
}

// 解密出来的明文放在暂存目录里，用完连目录一起删；系统临时目录里不出现解密出来的内容
func TestUnpackKeepsPlaintextInStaging(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "secret.txt"), strings.Repeat(plaintextMarker, 40))
	archive := packTreeEnc(t, root, scanOK(t, root), AlgorithmNone, "pw")

	var inTemp, inStaging []string
	_, err := Unpack(context.Background(), archive, t.TempDir(), "pw", UnpackOptions{Progress: func(p Progress) {
		if p.Phase != PhaseRestoring {
			return
		}
		inTemp = tempHolding(t, plaintextMarker)
		inStaging = stagingHolding(t, plaintextMarker)
	}})
	if err != nil {
		t.Fatalf("Unpack() error = %v", err)
	}
	if len(inTemp) > 0 {
		t.Errorf("decrypted plaintext landed in the system temp directory: %v", inTemp)
	}
	if len(inStaging) == 0 {
		t.Error("the staging directory should hold the decrypted plaintext while restoring")
	}
	if got := stagingFiles(t); len(got) != 0 {
		t.Errorf("the staging directory still holds %v after Unpack", got)
	}
}

// 认证不过的归档不往任何地方写明文
func TestTamperedArchiveWritesNoPlaintext(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "secret.txt"), strings.Repeat(plaintextMarker, 40))
	archive := packTreeEnc(t, root, scanOK(t, root), AlgorithmLZ77, "pw")
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatalf("Setup failed: read the archive: %v", err)
	}
	raw[int(archiveEncDataOffset)+16] ^= 0xFF
	crc := crc64.New(crc64.MakeTable(crc64.ECMA))
	_, _ = crc.Write(raw[:16])
	_, _ = crc.Write(make([]byte, 8))
	_, _ = crc.Write(raw[24:92])
	_, _ = crc.Write(make([]byte, 32))
	_, _ = crc.Write(raw[124:])
	binary.LittleEndian.PutUint64(raw[16:24], crc.Sum64())
	tampered := filepath.Join(t.TempDir(), "tampered.dbkp")
	if err := os.WriteFile(tampered, raw, 0o644); err != nil {
		t.Fatalf("Setup failed: write the tampered archive: %v", err)
	}

	if err := Verify(context.Background(), tampered, "pw", VerifyOptions{}); !errors.Is(err, errs.ErrCorrupted) {
		t.Fatalf("Verify() error = %v, want errs.Corrupted", err)
	}
	if got := stagingHolding(t, plaintextMarker); len(got) != 0 {
		t.Errorf("a tampered archive left plaintext behind: %v", got)
	}
	if got := tempHolding(t, plaintextMarker); len(got) != 0 {
		t.Errorf("a tampered archive left plaintext in the system temp directory: %v", got)
	}
}

// 清扫删掉没人用的暂存目录，正在用的不动；根目录还没有的时候什么都不做
func TestSweepStaging(t *testing.T) {
	if err := SweepStaging(); err != nil {
		t.Fatalf("SweepStaging() before any staging: error = %v, want <nil>", err)
	}
	root, err := stagingRoot()
	if err != nil {
		t.Fatalf("stagingRoot() error = %v", err)
	}
	leftover := filepath.Join(root, "op-leftover")
	writeFile(t, filepath.Join(leftover, "raw"), "stale plaintext")

	inUse, err := newStaging()
	if err != nil {
		t.Fatalf("newStaging() error = %v", err)
	}
	defer inUse.remove()
	if _, err := inUse.file("raw"); err != nil {
		t.Fatalf("staging.file() error = %v", err)
	}

	if err := SweepStaging(); err != nil {
		t.Fatalf("SweepStaging() error = %v", err)
	}
	if pathExists(leftover) {
		t.Error("SweepStaging() left the stale directory behind")
	}
	if !pathExists(filepath.Join(inUse.dir, "raw")) {
		t.Error("SweepStaging() removed a directory that is still in use")
	}

	inUse.remove()
	if err := SweepStaging(); err != nil {
		t.Fatalf("SweepStaging() error = %v", err)
	}
	if got := entryNames(t, root); len(got) != 0 {
		t.Errorf("the staging directory holds %v after everything is done, want nothing", got)
	}
}

// ---------- 辅助函数 ----------

// pathExists 报告 path 在不在
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// stagingFiles 列出暂存目录根下所有的文件，路径相对根
func stagingFiles(t *testing.T) []string {
	t.Helper()
	root, err := stagingRoot()
	if err != nil {
		t.Fatalf("stagingRoot() error = %v", err)
	}
	var out []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("walk the staging directory: %v", err)
	}
	return out
}

// stagingHolding 返回暂存目录里内容含 marker 的文件，路径相对根
func stagingHolding(t *testing.T, marker string) []string {
	t.Helper()
	root, err := stagingRoot()
	if err != nil {
		t.Fatalf("stagingRoot() error = %v", err)
	}
	var found []string
	for _, rel := range stagingFiles(t) {
		if fileHolds(t, filepath.Join(root, filepath.FromSlash(rel)), marker) {
			found = append(found, rel)
		}
	}
	return found
}

// dirHolding 返回 dir 下一层里内容含 marker 的文件
func dirHolding(t *testing.T, dir, marker string) []string {
	t.Helper()
	var found []string
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("read %q: %v", dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() && fileHolds(t, filepath.Join(dir, e.Name()), marker) {
			found = append(found, e.Name())
		}
	}
	return found
}

// fileHolds 报告文件内容里有没有 marker
func fileHolds(t *testing.T, path, marker string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return bytes.Contains(data, []byte(marker))
}

// tempHolding 返回系统临时目录里名字以 dbkp- 开头、内容含 marker 的文件。
// 以前解密和解压的结果就落在这里
func tempHolding(t *testing.T, marker string) []string {
	t.Helper()
	dir := os.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read the temp directory: %v", err)
	}
	var found []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "dbkp-") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
			continue // 别的进程正用着的文件读不了，也不是这里写的
		}
		if err != nil {
			t.Fatalf("read %q: %v", e.Name(), err)
		}
		if bytes.Contains(data, []byte(marker)) {
			found = append(found, e.Name())
		}
	}
	return found
}
