package chat

import (
	"testing"
	"time"
)

func TestComputeProactiveGraceWait(t *testing.T) {
	tests := []struct {
		name    string
		elapsed time.Duration
		want    time.Duration
	}{
		{
			name:    "short prep uses capped max wait",
			elapsed: 100 * time.Millisecond,
			want:    proactiveMaxGraceWait,
		},
		{
			name:    "medium prep uses remaining budget",
			elapsed: 900 * time.Millisecond,
			want:    proactiveMaxGraceWait,
		},
		{
			name:    "long prep keeps minimum wait",
			elapsed: 2 * time.Second,
			want:    proactiveMinGraceWait,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeProactiveGraceWait(tt.elapsed); got != tt.want {
				t.Fatalf("computeProactiveGraceWait(%v) = %v, want %v", tt.elapsed, got, tt.want)
			}
		})
	}
}
