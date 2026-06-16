package history

import (
	"context"
	"sort"
	"strings"
	"sync"
)

type MemoryStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string][]byte{}}
}

func (store *MemoryStore) Put(_ context.Context, key string, value []byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.data[key] = append([]byte(nil), value...)
	return nil
}

func (store *MemoryStore) Get(_ context.Context, key string) (Entry, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	value, ok := store.data[key]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return Entry{Key: key, Value: append([]byte(nil), value...)}, nil
}

func (store *MemoryStore) GetPrefix(_ context.Context, prefix string) ([]Entry, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	entries := make([]Entry, 0)
	for key, value := range store.data {
		if strings.HasPrefix(key, prefix) {
			entries = append(entries, Entry{Key: key, Value: append([]byte(nil), value...)})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
	return entries, nil
}
