package server

import (
	"encoding/json"
	"io"
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
	if raw, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil {
		json.Unmarshal(raw, &s.metas)
	}
	return s, nil
}

func (s *Store) Put(r io.Reader, name string) (Meta, error) {
	return Meta{}, nil
}

func (s *Store) List() []Meta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.metas
}

func (s *Store) Open(id string) (*Meta, *os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.metas {
		if s.metas[i].ID == id {
			f, err := os.Open(filepath.Join(s.dir, "blobs", id))
			m := s.metas[i]
			return &m, f, err
		}
	}
	return nil, nil, os.ErrNotExist
}
