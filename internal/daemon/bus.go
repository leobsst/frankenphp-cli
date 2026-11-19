package daemon

import "sync"

// Message is what the event stream (SSE) sends: an operation that changed
// ("operation") or an event of an operation ("event").
type Message struct {
	Type string
	Data any
}

// subscriberBuffer is how many messages a slow client may lag behind
// before it misses some.
const subscriberBuffer = 256

// Bus broadcasts the messages to the event stream clients.
type Bus struct {
	mu   sync.Mutex
	subs map[chan Message]struct{}
}

// NewBus returns a bus without subscriber.
func NewBus() *Bus { return &Bus{subs: map[chan Message]struct{}{}} }

// Subscribe returns a channel of the messages and the function that ends
// the subscription.
func (b *Bus) Subscribe() (<-chan Message, func()) {
	ch := make(chan Message, subscriberBuffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}

// Publish sends a message to every subscriber, without waiting: a client
// whose buffer is full misses it (it can read the operation again).
func (b *Bus) Publish(typ string, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- Message{Type: typ, Data: data}:
		default:
		}
	}
}
