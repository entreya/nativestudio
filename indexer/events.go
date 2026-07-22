package indexer

import "sync"

type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{}
}

func NewBroker() *Broker { return &Broker{subscribers: map[string]map[chan Event]struct{}{}} }
func (b *Broker) Subscribe(workspaceID string) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	b.mu.Lock()
	if b.subscribers[workspaceID] == nil {
		b.subscribers[workspaceID] = map[chan Event]struct{}{}
	}
	b.subscribers[workspaceID][ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subscribers[workspaceID][ch]; ok {
			delete(b.subscribers[workspaceID], ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}
func (b *Broker) Emit(workspaceID, eventType string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers[workspaceID] {
		select {
		case ch <- Event{Type: eventType, Data: data}:
		default:
		}
	}
}
