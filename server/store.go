package main

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Meta struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Size int64     `json:"size"`
	SHA  string    `json:"sha"`
	At   time.Time `json:"at"`
}

type Store struct {
	mu    sync.Mutex
	dir   string
	metas []Meta
}

func NewStore(dir string) (*Store, error) {
	s := &Store{dir: dir}
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o755); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	// 坏元数据接着用，下次保存会拿空列表把原记录毁掉
	if err := json.Unmarshal(raw, &s.metas); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Put(r io.Reader, name string) (Meta, error) {
	return Meta{}, nil
}

// List 返回全部元数据的副本，调用方改动不影响仓库内部
func (s *Store) List() []Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Meta, len(s.metas))
	copy(out, s.metas)
	return out
}

// Open 按 id 找元数据，返回一份拷贝。id 以后会拼进 blobs 的路径，长得像路径的按不存在处理
func (s *Store) Open(id string) (*Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !filepath.IsLocal(id) {
		return nil, os.ErrNotExist
	}
	for i := range s.metas {
		if s.metas[i].ID == id {
			m := s.metas[i]
			return &m, nil
		}
	}
	return nil, os.ErrNotExist
}
