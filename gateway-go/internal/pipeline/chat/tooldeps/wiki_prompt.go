package tooldeps

import (
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	chatknowledge "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/knowledge"
)

// FormatWikiTier1 builds the ambient "## 핵심 지식" section.
func FormatWikiTier1(store *WikiStore) string {
	cfg := wiki.ConfigFromEnv()
	return chatknowledge.FormatTier1(store, cfg.Tier1MinImportance)
}

// BuildAnchorKeywords extracts wiki Tier1 titles as Polaris compaction anchors.
func BuildAnchorKeywords(store *WikiStore) []string {
	const anchorMinImportance = 0.95
	const anchorMaxCount = 5
	if store == nil {
		return nil
	}
	pages := store.Tier1Pages(anchorMinImportance)
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
