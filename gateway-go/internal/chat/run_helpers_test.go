package chat

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestShouldLogStructuredMemoryExtractionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: false},
		{name: "wrapped deadline exceeded", err: fmt.Errorf("importance extraction: %w", context.DeadlineExceeded), want: false},
		{name: "context canceled", err: context.Canceled, want: false},
		{name: "generic error", err: errors.New("boom"), want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldLogStructuredMemoryExtractionError(tc.err)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
