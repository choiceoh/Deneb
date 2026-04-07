package chat

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

func TestFormatToolActivitySummary(t *testing.T) {
	tests := []struct {
		name       string
		activities []agent.ToolActivity
		want       string
	}{
		{name: "nil", activities: nil, want: ""},
		{name: "empty", activities: []agent.ToolActivity{}, want: ""},
		{
			name:       "single tool",
			activities: []agent.ToolActivity{{Name: "read_file"}},
			want:       "Tools used: read_file",
		},
		{
			name: "multiple distinct",
			activities: []agent.ToolActivity{
				{Name: "read_file"},
				{Name: "edit"},
				{Name: "exec"},
			},
			want: "Tools used: read_file, edit, exec",
		},
		{
			name: "repeated tools with counts",
			activities: []agent.ToolActivity{
				{Name: "read_file"},
				{Name: "edit"},
				{Name: "read_file"},
				{Name: "exec"},
				{Name: "read_file"},
			},
			want: "Tools used: read_file ×3, edit, exec",
		},
		{
			name: "preserves first-seen order",
			activities: []agent.ToolActivity{
				{Name: "exec"},
				{Name: "read_file"},
				{Name: "exec"},
			},
			want: "Tools used: exec ×2, read_file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatToolActivitySummary(tc.activities)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

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
