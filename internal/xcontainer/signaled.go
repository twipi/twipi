package xcontainer

import "context"

// Signaled is a value that can be waited on.
type Signaled[T any] struct {
	v T
	r chan struct{}
}

// NewSignaled creates a new Signaled with the given value.
func NewSignaled[T any]() *Signaled[T] {
	return &Signaled[T]{r: make(chan struct{})}
}

// Wait waits for the value to be signaled.
func (s *Signaled[T]) Wait(ctx context.Context) (T, error) {
	select {
	case <-s.r:
		return s.v, nil
	case <-ctx.Done():
		var z T
		return z, ctx.Err()
	}
}

// Signal signals the value. It panics if called more than once.
func (s *Signaled[T]) Signal(v T) {
	s.v = v
	close(s.r)
}

// Value returns the value of the signaled value, if it has been signaled.
func (s *Signaled[T]) Value() (T, bool) {
	select {
	case <-s.r:
		return s.v, true
	default:
		var z T
		return z, false
	}
}
