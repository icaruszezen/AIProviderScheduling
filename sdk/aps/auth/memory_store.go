package auth

import (
	"context"
	"sync"
)

// MemoryStore keeps auth records in process memory.
// Provider API keys are synthesized from config on reload, so disk persistence is unnecessary.
type MemoryStore struct {
	mu    sync.Mutex
	auths map[string]*Auth
}

// NewMemoryStore creates an empty in-memory auth store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{auths: make(map[string]*Auth)}
}

// List returns clones of all stored auth records.
func (s *MemoryStore) List(context.Context) ([]*Auth, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Auth, 0, len(s.auths))
	for _, auth := range s.auths {
		out = append(out, auth.Clone())
	}
	return out, nil
}

// Save replaces any existing record with the same ID.
func (s *MemoryStore) Save(_ context.Context, auth *Auth) (string, error) {
	if s == nil || auth == nil {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auths == nil {
		s.auths = make(map[string]*Auth)
	}
	s.auths[auth.ID] = auth.Clone()
	return auth.ID, nil
}

// Delete removes the record identified by id.
func (s *MemoryStore) Delete(_ context.Context, id string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.auths, id)
	return nil
}
