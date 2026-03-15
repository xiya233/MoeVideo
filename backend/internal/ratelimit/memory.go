package ratelimit

import (
	"sync"
	"time"
)

type memoryStore struct {
	mu       sync.Mutex
	counters map[string]memoryCounter
	onceKeys map[string]time.Time
	ops      int
}

type memoryCounter struct {
	Count   int64
	ResetAt time.Time
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		counters: make(map[string]memoryCounter, 256),
		onceKeys: make(map[string]time.Time, 256),
	}
}

func (m *memoryStore) allow(key string, resetAt time.Time) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupLocked()
	entry := m.counters[key]
	entry.Count++
	entry.ResetAt = resetAt
	m.counters[key] = entry
	return entry.Count
}

func (m *memoryStore) claimOnce(key string, ttl time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupLocked()
	now := time.Now().UTC()
	if exp, ok := m.onceKeys[key]; ok && exp.After(now) {
		return false
	}
	m.onceKeys[key] = now.Add(ttl)
	return true
}

func (m *memoryStore) cleanupLocked() {
	m.ops++
	if m.ops%100 != 0 {
		return
	}
	now := time.Now().UTC()
	for key, entry := range m.counters {
		if !entry.ResetAt.After(now) {
			delete(m.counters, key)
		}
	}
	for key, exp := range m.onceKeys {
		if !exp.After(now) {
			delete(m.onceKeys, key)
		}
	}
}
