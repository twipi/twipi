package xiter

// https://github.com/golang/go/issues/61405
type Seq[T any] func(yield func(T) bool) bool

// FromSlice returns a Seq that yields the elements of the given slice.
func FromSlice[T any](slice []T) Seq[T] {
	return func(yield func(T) bool) bool {
		for _, v := range slice {
			if !yield(v) {
				return false
			}
		}
		return true
	}
}

// Seq2 is a two-element sequence.
type Seq2[T1, T2 any] func(yield func(T1, T2) bool) bool
