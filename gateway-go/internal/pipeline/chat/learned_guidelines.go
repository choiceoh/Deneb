package chat

import (
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
)

// guidelineStorePath is the learned-guidelines file under the resolved state
// dir (DENEB_STATE_DIR-aware), the same path the compaction tuner writes.
func guidelineStorePath() string {
	dir := config.ResolveStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, compaction.GuidelineFileName)
}

// buildLearnedGuidelines loads the ACON-style learned compaction guidelines for
// injection into the summarizer prompt. Mirrors buildAnchorKeywords: read fresh
// per run (the file is tiny and changes only when the refinement task fires),
// nil when unavailable so the feature is inert until guidelines exist.
func buildLearnedGuidelines() []string {
	path := guidelineStorePath()
	if path == "" {
		return nil
	}
	return compaction.NewGuidelineStore(path).Load()
}
