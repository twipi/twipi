package xcontainer

import (
	"sync"
	"time"
)

// Expirable is a type that holds a value that can expire.
// Expirable is thread-safe.
type Expirable[T any] struct {
	m sync.Mutex
	v T
	e time.Time
}

// Value returns the value and a boolean indicating if the value is still valid.
func (e *Expirable[T]) Value() (T, bool) {
	e.m.Lock()
	v, ok := e.value()
	e.m.Unlock()

	return v, ok
}

func (e *Expirable[T]) value() (T, bool) {
	return e.v, !e.e.Before(time.Now())
}

// Renew renews the expirable with a new value for the given duration.
func (e *Expirable[T]) Renew(d time.Duration, v T) {
	e.m.Lock()
	e.renew(d, v)
	e.m.Unlock()
}

func (e *Expirable[T]) renew(d time.Duration, v T) {
	e.v = v
	e.e = time.Now().Add(d)
}

// RenewableValue returns the value and renews the value if it is expired.
func (e *Expirable[T]) RenewableValue(d time.Duration, f func() (T, error)) (T, error) {
	v, ok := e.Value()
	if ok {
		return v, nil
	}

	v, err := f()
	if err != nil {
		return v, err
	}

	e.m.Lock()
	if v, ok := e.value(); ok {
		// lost to another goroutine
		e.m.Unlock()
		return v, nil
	}

	e.renew(d, v)
	e.m.Unlock()

	return v, nil
}
