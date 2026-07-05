// Package sse is the in-process pub/sub used by the request-inspector
// Server-Sent Events stream. The app is a single process (ARCHITECTURE.md),
// so no external broker is needed.
package sse

import "sync"

// Broker fans "something changed for mock X" signals out to stream handlers.
// Keys are mock IDs. Events carry no payload: a signal only means "refetch".
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[chan struct{}]struct{})}
}

// Subscribe registers for signals about one mock. The channel has capacity 1:
// one pending signal is enough, extra publishes are coalesced. The returned
// cancel is idempotent and must be called to avoid leaks.
func (b *Broker) Subscribe(mockID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	set := b.subs[mockID]
	if set == nil {
		set = make(map[chan struct{}]struct{})
		b.subs[mockID] = set
	}
	set[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs[mockID], ch)
			if len(b.subs[mockID]) == 0 {
				delete(b.subs, mockID)
			}
			b.mu.Unlock()
		})
	}
	return ch, cancel
}

// Publish signals every subscriber of mockID. Never blocks: a full buffer
// already holds a pending "refetch" signal, so dropping is lossless.
func (b *Broker) Publish(mockID string) {
	b.mu.Lock()
	for ch := range b.subs[mockID] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	b.mu.Unlock()
}
