package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"

// buildAnchorKeywords extracts wiki Tier1 page titles as soft Polaris anchors.
// Delegates to tooldeps so the chat parent does not import wikiport/knowledge.
func buildAnchorKeywords(wikiStore *tooldeps.WikiStore) []string {
	return tooldeps.BuildAnchorKeywords(wikiStore)
}
