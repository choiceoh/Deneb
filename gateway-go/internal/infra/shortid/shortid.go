package shortid

import (
	"fmt"
	"sync/atomic"
)

var counter atomic.Uint64

// New returns "prefix_NNNN" where NNNN is a zero-padded 4-digit counter (0000–9999).
// Wraps around after 9999. Unique within a single process lifetime for typical usage.
func New(prefix string) string {
	n := counter.Add(1) - 1
	return fmt.Sprintf("%s_%04d", prefix, n%10000)
}
