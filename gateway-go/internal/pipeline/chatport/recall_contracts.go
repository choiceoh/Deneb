package chatport

import "context"

// FileRecallHit is one transport-neutral file-search result surfaced to chat
// recall preflight.
type FileRecallHit struct {
	Path       string
	Snippet    string
	Score      float64
	ModifiedAt int64
}

// FileRecallFunc runs degradation-safe file recall for a chat turn.
type FileRecallFunc func(ctx context.Context, query string, limit int) []FileRecallHit
