package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/rlm/repl"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// buildWikiFuncs creates REPL wiki callbacks from a wiki.Store.
// These are injected into the Starlark environment so the Root LM
// can programmatically navigate wiki MD files.
func buildWikiFuncs(store *wiki.Store) *repl.WikiFuncs {
	return &repl.WikiFuncs{
		Read: func(relPath string) (string, error) {
			page, err := store.ReadPage(relPath)
			if err != nil {
				return "", fmt.Errorf("page not found: %s", relPath)
			}
			return string(page.Render()), nil
		},

		ReadBatch: func(relPaths []string) ([]string, error) {
			results := make([]string, len(relPaths))
			for i, p := range relPaths {
				page, err := store.ReadPage(p)
				if err != nil {
					results[i] = fmt.Sprintf("ERROR: %s not found", p)
					continue
				}
				results[i] = string(page.Render())
			}
			return results, nil
		},

		List: func(category string) ([]string, error) {
			return store.ListPages(category)
		},

		Index: func(category string) (string, error) {
			idx := store.GetIndex()
			if category == "" {
				return idx.Render(), nil
			}
			// Filter rendered index to requested category section.
			rendered := idx.Render()
			return filterIndexCategory(rendered, category), nil
		},

		Search: func(ctx context.Context, query string, limit int) (string, error) {
			results, err := store.Search(ctx, query, limit)
			if err != nil {
				return "", err
			}
			var sb strings.Builder
			for _, r := range results {
				sb.WriteString(fmt.Sprintf("%s\t%.2f\t%s\n", r.Path, r.Score, truncate(r.Content, 200)))
			}
			if sb.Len() == 0 {
				return "(no results)", nil
			}
			return sb.String(), nil
		},

		Write: func(relPath, content string) error {
			// Try reading existing page for update.
			page, err := store.ReadPage(relPath)
			if err != nil {
				// New page: parse content as full page (may include frontmatter).
				page, err = wiki.ParsePage([]byte(content))
				if err != nil {
					return err
				}
			} else {
				// Update: replace body.
				page.Body = content
			}
			return store.WritePage(relPath, page)
		},
	}
}

// filterIndexCategory extracts a single category section from the rendered index.
func filterIndexCategory(rendered, category string) string {
	lines := strings.Split(rendered, "\n")
	var result []string
	capturing := false

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			heading := strings.TrimPrefix(line, "## ")
			if heading == category {
				capturing = true
				result = append(result, line)
				continue
			}
			if capturing {
				break // reached next category
			}
			continue
		}
		if capturing {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
