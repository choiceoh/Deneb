package chat

import (
	"strings"

	runstate "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/runstate"
)

// SteerQueue stores per-session notes waiting for the next tool result.
type SteerQueue = runstate.SteerQueue

// NewSteerQueue creates an empty steer queue.
func NewSteerQueue() *SteerQueue {
	return runstate.NewSteerQueue()
}

// steerMarkerPrefix is the Korean-first label prepended to steer text when
// it is attached to a tool_result. Marker is inside a tool_result content
// string (not a new message), so the model sees it as additional context for
// that tool's output, not as a new user turn. "사용자 조정" ("user steer")
// matches the codebase's Korean-first convention while still being clear
// enough that the model will not mistake it for a real tool result line.
const steerMarkerPrefix = "\n\n[사용자 조정: "

// steerMarkerSuffix closes the marker block.
const steerMarkerSuffix = "]"

// formatSteerMarker returns the injection text for a set of notes. Notes
// are joined with " / " so multi-steer bursts read as a single annotation.
func formatSteerMarker(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	// Filter out empty elements defensively (Enqueue already trims).
	clean := make([]string, 0, len(notes))
	for _, n := range notes {
		n = strings.TrimSpace(n)
		if n != "" {
			clean = append(clean, n)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return steerMarkerPrefix + strings.Join(clean, " / ") + steerMarkerSuffix
}
