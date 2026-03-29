package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestShouldStopVerifyBatches(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: true},
		{name: "wrapped deadline exceeded", err: fmt.Errorf("verify: %w", context.DeadlineExceeded), want: true},
		{name: "context canceled", err: context.Canceled, want: true},
		{name: "other error", err: errors.New("parse failed"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldStopVerifyBatches(tc.err); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
