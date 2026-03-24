package xsync

import (
	"sync"
)

// Map is a concurrency-safe map backed by sync.Map with typed accessors.
type Map[K comparable, V any] struct {
	m  sync.Map
	mu sync.Mutex // serializes LoadOrCompute creation
}

// Load returns the value for key and whether it was found.
func (m *Map[K, V]) Load(key K) (V, bool) {
	v, ok := m.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return v.(V), true
}

// Store sets the value for key.
func (m *Map[K, V]) Store(key K, value V) {
	m.m.Store(key, value)
}

// Delete removes the key. Returns true if it was present.
func (m *Map[K, V]) Delete(key K) bool {
	_, loaded := m.m.LoadAndDelete(key)
	return loaded
}

// LoadOrCompute returns the existing value for key if present. Otherwise it
// calls create, stores its result, and returns it. The create function is
// called at most once per key, even under concurrent access.
func (m *Map[K, V]) LoadOrCompute(key K, create func() (V, error)) (V, error) {
	if v, ok := m.m.Load(key); ok {
		return v.(V), nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.m.Load(key); ok {
		return v.(V), nil
	}
	v, err := create()
	if err != nil {
		var zero V
		return zero, err
	}
	m.m.Store(key, v)
	return v, nil
}

// All is a range iterator over all key-value pairs.
func (m *Map[K, V]) All(yield func(K, V) bool) {
	m.m.Range(func(key, value any) bool {
		return yield(key.(K), value.(V))
	})
}
