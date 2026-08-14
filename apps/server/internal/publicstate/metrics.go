package publicstate

import "sync"

type fenceMetrics struct {
	mu         sync.Mutex
	mismatches int64
	leases     map[Representation]int64
}

type metricsSnapshot struct {
	mismatches int64
	leases     map[Representation]int64
}

func (m *fenceMetrics) snapshot() metricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	leases := make(map[Representation]int64, len(m.leases))
	for representation, count := range m.leases {
		leases[representation] = count
	}
	return metricsSnapshot{mismatches: m.mismatches, leases: leases}
}

func (m *fenceMetrics) recordMismatch() {
	m.mu.Lock()
	m.mismatches++
	m.mu.Unlock()
}

func (m *fenceMetrics) recordLease(rep Representation, delta int64) {
	m.mu.Lock()
	if m.leases == nil {
		m.leases = make(map[Representation]int64)
	}
	m.leases[rep] += delta
	m.mu.Unlock()
}

func errorsNewNilCancelHook() error { return ErrAdmissionClosed }
