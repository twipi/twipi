package xcontainer

// Queue is a FIFO queue.
type Queue[T any] struct {
	vs []T
}

// NewQueue creates a new queue.
func NewQueue[T any](cap int) *Queue[T] {
	return &Queue[T]{vs: make([]T, 0, cap)}
}

// Pop pops the first element from the queue.
func (q *Queue[T]) Pop() (T, bool) {
	if len(q.vs) == 0 {
		var z T
		return z, false
	}

	v := q.vs[0]
	q.vs = q.vs[1:]
	return v, true
}

// Popz is like Pop, except the zero-value is returned if the queue is empty.
// Use this if the caller is OK with this behavior.
func (q *Queue[T]) Popz() T {
	v, _ := q.Pop()
	return v
}

// IsEmpty returns true if the queue is empty.
func (q *Queue[T]) IsEmpty() bool {
	return len(q.vs) == 0
}

// Queue queues the given item.
func (q *Queue[T]) Queue(v T) {
	q.vs = append(q.vs, v)
}
