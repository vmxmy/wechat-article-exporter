package secrets

import (
	"context"
	"errors"
	"sync"
)

var ErrNotFound = errors.New("secret not found")

type Ref struct {
	Profile string `json:"profile"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
}

type Store interface {
	Backend() string
	Get(context.Context, Ref) ([]byte, error)
	Set(context.Context, Ref, []byte) error
	Delete(context.Context, Ref) error
	DeleteProfile(string) error
}

// MemoryStore is intentionally test-only/ephemeral; production persistence is
// provided by the platform and encrypted-vault adapters introduced later.
type MemoryStore struct {
	mu     sync.RWMutex
	values map[Ref][]byte
}

func NewMemoryStore() *MemoryStore   { return &MemoryStore{values: make(map[Ref][]byte)} }
func (*MemoryStore) Backend() string { return "memory-ephemeral" }

func (s *MemoryStore) Get(_ context.Context, ref Ref) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[ref]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (s *MemoryStore) Set(_ context.Context, ref Ref, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[ref] = append([]byte(nil), value...)
	return nil
}

func (s *MemoryStore) Delete(_ context.Context, ref Ref) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, ref)
	return nil
}

func (s *MemoryStore) DeleteProfile(profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ref := range s.values {
		if ref.Profile == profile {
			delete(s.values, ref)
		}
	}
	return nil
}
