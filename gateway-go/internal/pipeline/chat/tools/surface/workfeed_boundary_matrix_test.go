package surface

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

// These matrices moved here with the workFeedPriority/workFeedTitle helpers when
// the workfeed tool was relocated to the surface package (#3679 moved the impl
// but left these boundary tests behind in tools/, breaking that package's test
// build). The helpers are unexported, so the tests must live alongside them.

func TestBoundaryWorkFeedPriorityMatrix(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{raw: "", want: 0},
		{raw: "unknown", want: 0},
		{raw: "urgent", want: tooldeps.WorkFeedPriorityUrgent},
		{raw: " URGENT ", want: tooldeps.WorkFeedPriorityUrgent},
		{raw: "긴급", want: tooldeps.WorkFeedPriorityUrgent},
		{raw: "high", want: tooldeps.WorkFeedPriorityHigh},
		{raw: "높음", want: tooldeps.WorkFeedPriorityHigh},
		{raw: "normal", want: tooldeps.WorkFeedPriorityNormal},
		{raw: "보통", want: tooldeps.WorkFeedPriorityNormal},
		{raw: "low", want: tooldeps.WorkFeedPriorityLow},
		{raw: "낮음", want: tooldeps.WorkFeedPriorityLow},
		{raw: "highest", want: 0},
	}
	for _, tt := range tests {
		if got := workFeedPriority(tt.raw); got != tt.want {
			t.Fatalf("workFeedPriority(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestBoundaryWorkFeedTitleFallbackMatrix(t *testing.T) {
	tests := []struct {
		name string
		item tooldeps.WorkFeedItem
		want string
	}{
		{name: "title wins", item: tooldeps.WorkFeedItem{Title: " Title ", Summary: "summary", Body: "body"}, want: "Title"},
		{name: "summary fallback", item: tooldeps.WorkFeedItem{Summary: " summary line \nsecond", Body: "body"}, want: "summary line "},
		{name: "body fallback", item: tooldeps.WorkFeedItem{Body: " body line \nsecond"}, want: "body line "},
		{name: "empty", item: tooldeps.WorkFeedItem{}, want: ""},
		{name: "blank title skipped", item: tooldeps.WorkFeedItem{Title: " ", Summary: "summary"}, want: "summary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workFeedTitle(tt.item); got != tt.want {
				t.Fatalf("workFeedTitle = %q, want %q", got, tt.want)
			}
		})
	}
}
