// Package lcm implements Lossless Context Management for Deneb.
//
// LCM preserves all conversation messages in an immutable SQLite store
// and builds a DAG of hierarchical summaries for efficient context assembly.
// Raw messages are never deleted; summaries compress older history while
// keeping the original accessible via retrieval tools.
package lcm

import "time"

// CompactUrgency indicates how urgently compaction is needed.
type CompactUrgency int

const (
	CompactNone CompactUrgency = iota
	CompactSoft                // async between turns (> SoftThresholdPct)
	CompactHard                // immediate (> HardThresholdPct)
)

// Config controls LCM compaction and assembly behavior.
type Config struct {
	// SoftThresholdPct triggers async compaction between turns (default 0.75).
	SoftThresholdPct float64
	// HardThresholdPct triggers immediate compaction (default 0.90).
	HardThresholdPct float64
	// LeafChunkTokens is the target token size per leaf summary batch (default 20000).
	LeafChunkTokens int
	// CondenseFanIn is how many leaf summaries are merged into one condensed node (default 4).
	CondenseFanIn int
}

// DefaultConfig returns sensible defaults for single-user deployment.
func DefaultConfig() Config {
	return Config{
		SoftThresholdPct: 0.75,
		HardThresholdPct: 0.90,
		LeafChunkTokens:  20_000,
		CondenseFanIn:    4,
	}
}

// SummaryNode is a node in the summary DAG.
// Level 1 = leaf summary (from raw messages), level 2+ = condensed.
type SummaryNode struct {
	ID        int64  // auto-increment primary key
	SessionKey string
	Level     int    // 1 = leaf, 2+ = condensed
	Content   string // summary text (Korean)
	TokenEst  int    // estimated token count
	CreatedAt int64  // unix milliseconds
	MsgStart  int    // first source message index (inclusive)
	MsgEnd    int    // last source message index (inclusive)
	ParentID  *int64 // parent summary node ID (nil for root-level leaves)
}

// CreatedTime returns CreatedAt as time.Time.
func (n *SummaryNode) CreatedTime() time.Time {
	return time.UnixMilli(n.CreatedAt)
}
