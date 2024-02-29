package pubsub

import (
	"sync"

	"github.com/twipi/twipi/internal/containerx"
)

// Subscriber is a subscriber that subscribes to a Pipe. A zero-value Subscriber
// is a valid Subscriber.
type Subscriber[T any] struct {
	mu   sync.RWMutex
	subs map[chan<- T]pipeSub[T]
}

type pipeSub[T any] struct {
	filter func(T) bool
	queue  chan<- T
	stop   chan struct{}
}

// NewSubscriber creates a new Subscriber.
func NewSubscriber[T any]() *Subscriber[T] {
	return &Subscriber[T]{}
}

// Listen spawns a goroutine that broadcasts messages received from the given
// src channel.
func (s *Subscriber[T]) Listen(src <-chan T) {
	if s.subs == nil {
		s.subs = make(map[chan<- T]pipeSub[T])
	}

	go func() {
		for msg := range src {
			s.publish(msg)
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		if s.subs != nil {
			// Unsubscribe all channels. This will purge them from the map.
			for ch := range s.subs {
				s.unsub(ch)
			}

			// Mark as done.
			s.subs = nil
		}
	}()
}

func (s *Subscriber[T]) publish(value T) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for ch, sub := range s.subs {
		if sub.filter(value) {
			ch <- value
		}
	}
}

// Subscribe subscribes ch to incoming messages from the given recipient.
// Calls to Subscribe should always be paired with Unsubscribe. It is
// recommended to use defer.
//
// Subscribe panics if it's called on a Subscriber w/ a src that's already
// closed.
func (s *Subscriber[T]) Subscribe(ch chan<- T, filter func(T) bool) {
	if filter == nil {
		filter = func(T) bool { return true }
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.subs == nil {
		panic("pubsub.Subscriber: src is already closed")
	}

	_, ok := s.subs[ch]
	if ok {
		panic("twipi: channel already subscribed")
	}

	queue := make(chan T, 1)
	stop := make(chan struct{})

	go func() {
		var dst chan<- T
		var pending containerx.Queue[T]

		for {
			select {
			case msg := <-queue:
				pending.Queue(msg)
				dst = ch
			case dst <- pending.Popz():
				// If the pending queue is drained, then stop sending.
				if pending.IsEmpty() {
					dst = nil
				}
			case <-stop:
				close(ch)
				return
			}
		}
	}()

	s.subs[ch] = pipeSub[T]{
		filter: filter,
		queue:  queue,
		stop:   stop,
	}
}

// Unsubscribe unsubscribes ch from incoming messages from all its recipients.
// Once unsubscribed, ch will be closed.
func (s *Subscriber[T]) Unsubscribe(ch chan<- T) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.unsub(ch)
}

func (s *Subscriber[T]) unsub(ch chan<- T) {
	sub, ok := s.subs[ch]
	if ok {
		close(sub.stop)
		delete(s.subs, ch)
	}
}
