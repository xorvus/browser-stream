package session

import (
	"sync"
	"sync/atomic"
)

type Manager struct {
	mu     sync.Mutex
	active bool
}

func (m *Manager) Acquire() (func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active {
		return nil, false
	}
	m.active = true

	var released atomic.Bool
	return func() {
		if !released.CompareAndSwap(false, true) {
			return
		}
		m.mu.Lock()
		m.active = false
		m.mu.Unlock()
	}, true
}
