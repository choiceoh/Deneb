package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// anchorMinImportance gates which wiki Tier1 pages become Polaris compaction
// anchors. Stricter than the auto-injection threshold (0.85) to keep the
// anchor count small — too many anchors neutralize compaction.
const anchorMinImportance = 0.95

// anchorMaxCount caps the number of anchor keywords passed to the summarizer.
// Each adds a line to the LLM system prompt; 5 keywords ≈ 50 tokens overhead.
const anchorMaxCount = 5

// buildAnchorKeywords extracts wiki Tier1 page titles as soft anchor keywords
// for Polaris LLM compaction. Returns nil when wikiStore is unavailable.
func buildAnchorKeywords(wikiStore *wiki.Store) []string {
	if wikiStore == nil {
		return nil
	}
	pages := wikiStore.Tier1Pages(anchorMinImportance)
	if len(pages) > anchorMaxCount {
		pages = pages[:anchorMaxCount]
	}
	keywords := make([]string, 0, len(pages))
	for _, p := range pages {
		if p.Page != nil && p.Page.Meta.Title != "" {
			keywords = append(keywords, p.Page.Meta.Title)
		}
	}
	return keywords
}
