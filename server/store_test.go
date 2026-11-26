package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// NewStore 读不出现有元数据要报错，不然接着保存就会拿空列表毁掉 meta.json
func TestNewStoreRejectsBrokenMeta(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("Setup failed: write meta.json: %v", err)
	}

	if _, err := NewStore(dir); err == nil {
		t.Errorf("NewStore() with a broken meta.json = <nil>, want an error")
	}
}

// 现有元数据要读进来
func TestNewStoreLoadsMeta(t *testing.T) {
	dir := t.TempDir()
	raw := `[{"id":"abc","name":"a.dbkp","size":3,"sha":"x","at":"2026-01-01T00:00:00Z"}]`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(raw), 0o600); err != nil {
		t.Fatalf("Setup failed: write meta.json: %v", err)
	}

	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if got := s.List(); len(got) != 1 || got[0].ID != "abc" {
		t.Errorf("List() = %v, want the one stored meta", got)
	}
}

// List 返回的切片不能是内部的：调用方改它不该影响下一次 List
func TestListReturnsACopy(t *testing.T) {
	s := &Store{metas: []Meta{{ID: "abc"}}}

	got := s.List()
	got[0].ID = "tampered"
	got = append(got, Meta{ID: "extra"})

	again := s.List()
	if len(again) != 1 || again[0].ID != "abc" {
		t.Errorf("List() = %v after the caller mutated the first copy, want the untouched meta", again)
	}
}

// meta.json 里的 id 原样拼进 blobs 的路径，长得像路径的 id 不能放行
func TestOpenRejectsPathishID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("s"), 0o600); err != nil {
		t.Fatalf("Setup failed: write secret.txt: %v", err)
	}
	ids := []string{"../secret.txt", "..", "", `a\..\..\secret.txt`}
	var metas []Meta
	for _, id := range ids {
		metas = append(metas, Meta{ID: id})
	}
	s := &Store{dir: dir, metas: metas}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			m, err := s.Open(id)
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("Open(%q) = %v, %v; want os.ErrNotExist", id, m, err)
			}
		})
	}
}

// 正常的 id 返回那份元数据
func TestOpenReturnsMeta(t *testing.T) {
	s := &Store{dir: t.TempDir(), metas: []Meta{{ID: "abc", Name: "a.dbkp"}}}

	m, err := s.Open("abc")
	if err != nil {
		t.Fatalf("Open(%q) error = %v", "abc", err)
	}
	if m.ID != "abc" || m.Name != "a.dbkp" {
		t.Errorf("Open() meta = %+v, want the stored meta", m)
	}
	if _, err := s.Open("nope"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open(%q) error = %v, want os.ErrNotExist", "nope", err)
	}
}
