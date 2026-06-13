package agentrunner

import (
	"sync"

	"memento/backend/internal/store"
)

type Broadcaster struct {
	mu          sync.Mutex
	subscribers map[chan store.AgentEvent]struct{}
	closed      bool
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subscribers: map[chan store.AgentEvent]struct{}{}}
}

func (b *Broadcaster) Subscribe() (<-chan store.AgentEvent, func()) {
	ch := make(chan store.AgentEvent, 32)
	b.mu.Lock()
	if b.closed {
		close(ch)
		b.mu.Unlock()
		return ch, func() {}
	}
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	unsubscribe := func() {
		b.mu.Lock()
		if _, ok := b.subscribers[ch]; ok {
			delete(b.subscribers, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, unsubscribe
}

func (b *Broadcaster) Broadcast(ev store.AgentEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for ch := range b.subscribers {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (b *Broadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subscribers {
		close(ch)
		delete(b.subscribers, ch)
	}
}
