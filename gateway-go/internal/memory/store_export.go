// store_export.go — Markdown export and legacy import for the memory store.
package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// ExportToMarkdown generates MEMORY.md content from active facts above ExportMinImportance.
func (s *Store) ExportToMarkdown(ctx context.Context) (string, error) {
	facts, err := s.GetActiveFactsAboveImportance(ctx, ExportMinImportance)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("# Memory\n\nAuto-recorded learnings and decisions.\n\n")

	// Group by category for readability.
	categories := []string{CategoryDecision, CategoryPreference, CategorySolution, CategoryContext, CategoryUserModel, CategoryMutual}
	categoryNames := map[string]string{
		CategoryDecision:   "결정사항",
		CategoryPreference: "선호도",
		CategorySolution:   "해결방법",
		CategoryContext:    "맥락",
		CategoryUserModel:  "사용자 모델",
		CategoryMutual:     "상호 인식",
	}

	for _, cat := range categories {
		var catFacts []Fact
		for _, f := range facts {
			if f.Category == cat {
				catFacts = append(catFacts, f)
			}
		}
		if len(catFacts) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("## %s\n\n", categoryNames[cat]))
		for _, f := range catFacts {
			date := f.CreatedAt.Format("2006-01-02")
			sb.WriteString(fmt.Sprintf("- [%.1f] %s (%s)\n", f.Importance, f.Content, date))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// ExportToFile writes the markdown export to MEMORY.md in the given directory.
func (s *Store) ExportToFile(ctx context.Context, dir string) error {
	content, err := s.ExportToMarkdown(ctx)
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), nil)
}

// ImportFromMarkdown parses a legacy MEMORY.md file and imports its entries as facts.
// Handles the format produced by sglang_hooks.go: "## YYYY-MM-DD HH:MM\n\n- bullet\n- bullet\n"
// Returns the number of imported facts.
func (s *Store) ImportFromMarkdown(ctx context.Context, path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("import memory: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	imported := 0
	var currentDate string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse date header: "## 2026-01-15 14:30"
		if strings.HasPrefix(line, "## ") {
			currentDate = strings.TrimPrefix(line, "## ")
			continue
		}

		// Parse bullet entries: "- fact content"
		if (strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")) && len(line) > 3 {
			content := strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* ")
			if content == "" {
				continue
			}

			fact := Fact{
				Content:    content,
				Category:   CategoryContext,
				Importance: 0.5,
				Source:     "migration",
			}

			// Try to parse the date for created_at.
			if currentDate != "" {
				if t, err := time.Parse("2006-01-02 15:04", currentDate); err == nil {
					fact.CreatedAt = t
				}
			}

			if _, err := s.InsertFact(ctx, fact); err == nil {
				imported++
			}
		}
	}

	return imported, nil
}
