package xsync

import (
	"sync"
	"sync/atomic"
)

// Set is a concurrency-safe set backed by sync.Map with an O(1) Len.
type Set[T comparable] struct {
	m   sync.Map
	len atomic.Int64
}

// Add inserts v into the set. Returns true if the value was added, false if it
// was already present.
func (s *Set[T]) Add(v T) bool {
	if _, loaded := s.m.LoadOrStore(v, struct{}{}); loaded {
		return false
	}
	s.len.Add(1)
	return true
}

// Has reports whether v is in the set.
func (s *Set[T]) Has(v T) bool {
	_, ok := s.m.Load(v)
	return ok
}

// Delete removes v from the set. Returns true if the value was present.
func (s *Set[T]) Delete(v T) bool {
	if _, loaded := s.m.LoadAndDelete(v); !loaded {
		return false
	}
	s.len.Add(-1)
	return true
}

// Len returns the number of elements in the set.
func (s *Set[T]) Len() int {
	return int(s.len.Load())
}

// All is a range iterator over all elements in the set.
func (s *Set[T]) All(yield func(T) bool) {
	s.m.Range(func(key, _ any) bool {
		return yield(key.(T))
	})
}
