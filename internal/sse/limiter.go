package sse

import "sync"

// StreamLimiter caps concurrent SSE connections. SSE holds sockets open, so
// without caps a single client could exhaust the server's connection budget.
type StreamLimiter struct {
	mu       sync.Mutex
	total    int
	maxTotal int
	maxPerIP int
	perIP    map[string]int
}

func NewStreamLimiter(maxTotal, maxPerIP int) *StreamLimiter {
	return &StreamLimiter{maxTotal: maxTotal, maxPerIP: maxPerIP, perIP: make(map[string]int)}
}

// Acquire reserves a slot. Callers MUST call Release with the same ip
// exactly once after a successful Acquire.
func (l *StreamLimiter) Acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= l.maxTotal || l.perIP[ip] >= l.maxPerIP {
		return false
	}
	l.total++
	l.perIP[ip]++
	return true
}

func (l *StreamLimiter) Release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total > 0 {
		l.total--
	}
	if l.perIP[ip] > 1 {
		l.perIP[ip]--
	} else {
		delete(l.perIP, ip)
	}
}
