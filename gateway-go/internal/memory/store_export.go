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

	// Batch-load relations and entity names for all facts to avoid N+1 queries.
	factIDs := make([]int64, len(facts))
	for i, f := range facts {
		factIDs[i] = f.ID
	}
	relationsMap := s.batchGetRelatedFacts(ctx, factIDs)
	entityNamesMap := s.batchGetFactEntityNames(ctx, factIDs)

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

			// Append relation info if available.
			for _, rf := range relationsMap[f.ID] {
				arrow := "→"
				if rf.Direction == "incoming" {
					arrow = "←"
				}
				sb.WriteString(fmt.Sprintf("  %s [%s] id:%d \"%s\"\n",
					arrow, rf.RelationType, rf.Fact.ID, truncateExport(rf.Fact.Content, 60)))
			}

			// Append entity names if linked.
			if names := entityNamesMap[f.ID]; len(names) > 0 {
				sb.WriteString(fmt.Sprintf("  **엔티티:** %s\n", strings.Join(names, ", ")))
			}
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

// FlushToDateFile appends important facts to a date-stamped markdown file
// (memory/YYYY-MM-DD.md). This is a deterministic fallback for LLM-based
// memory flush — works without any LLM calls.
//
// Only facts with importance >= flushMinImportance that haven't been flushed
// before (tracked via metadata) are written. Returns the number of facts flushed.
func (s *Store) FlushToDateFile(ctx context.Context, dir string, timezone string) (int, error) {
	const flushMinImportance = 0.6

	// Resolve date-stamped path.
	now := time.Now()
	if timezone != "" {
		if loc, err := time.LoadLocation(timezone); err == nil {
			now = now.In(loc)
		}
	}
	dateStr := now.Format("2006-01-02")
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		return 0, fmt.Errorf("flush: create memory dir: %w", err)
	}
	datePath := filepath.Join(memDir, dateStr+".md")

	// Load existing file content to avoid duplicates.
	existingPrefixes := make(map[string]struct{})
	if data, err := os.ReadFile(datePath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") && len(line) > 20 {
				// Use first 60 runes as prefix for dedup.
				content := strings.TrimPrefix(line, "- ")
				prefix := normalizeFlushPrefix(content)
				if prefix != "" {
					existingPrefixes[prefix] = struct{}{}
				}
			}
		}
	}

	// Get last flush fact ID to only export new facts.
	lastFlushIDStr, _ := s.GetMeta(ctx, "flush_last_fact_id")
	var lastFlushID int64
	if lastFlushIDStr != "" {
		fmt.Sscanf(lastFlushIDStr, "%d", &lastFlushID)
	}

	facts, err := s.GetActiveFactsAboveImportance(ctx, flushMinImportance)
	if err != nil {
		return 0, err
	}

	var newEntries []string
	var maxID int64
	for _, f := range facts {
		if f.ID <= lastFlushID {
			continue
		}
		// Skip if already in file.
		prefix := normalizeFlushPrefix(f.Content)
		if _, dup := existingPrefixes[prefix]; dup && prefix != "" {
			continue
		}
		newEntries = append(newEntries, fmt.Sprintf("- [%s] %s", f.Category, f.Content))
		if f.ID > maxID {
			maxID = f.ID
		}
	}

	if len(newEntries) == 0 {
		return 0, nil
	}

	// Append to file.
	f, err := os.OpenFile(datePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("flush: open file: %w", err)
	}
	defer f.Close()

	// Write header if file is new.
	if info, err := f.Stat(); err == nil && info.Size() == 0 {
		fmt.Fprintf(f, "## %s\n\n", dateStr)
	}

	for _, entry := range newEntries {
		fmt.Fprintln(f, entry)
	}

	// Update flush cursor.
	if maxID > 0 {
		_ = s.SetMeta(ctx, "flush_last_fact_id", fmt.Sprintf("%d", maxID))
	}

	return len(newEntries), nil
}

// normalizeFlushPrefix returns the first 60 runes of content, lowercased and
// whitespace-collapsed, for duplicate detection during flush.
func normalizeFlushPrefix(s string) string {
	runes := []rune(strings.ToLower(strings.TrimSpace(s)))
	if len(runes) > 60 {
		runes = runes[:60]
	}
	return strings.Join(strings.Fields(string(runes)), " ")
}

// getFactEntityNames returns entity names linked to a fact.
func (s *Store) getFactEntityNames(ctx context.Context, factID int64) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT e.name FROM entities e
		 JOIN fact_entities fe ON fe.entity_id = e.id
		 WHERE fe.fact_id = ?
		 ORDER BY e.name`,
		factID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			names = append(names, name)
		}
	}
	return names
}

// batchGetFactEntityNames returns entity names for multiple facts in a single query.
func (s *Store) batchGetFactEntityNames(ctx context.Context, factIDs []int64) map[int64][]string {
	if len(factIDs) == 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	placeholders := make([]string, len(factIDs))
	args := make([]any, len(factIDs))
	for i, id := range factIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT fe.fact_id, e.name FROM entities e
		 JOIN fact_entities fe ON fe.entity_id = e.id
		 WHERE fe.fact_id IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY e.name`,
		args...,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[int64][]string)
	for rows.Next() {
		var factID int64
		var name string
		if err := rows.Scan(&factID, &name); err == nil {
			result[factID] = append(result[factID], name)
		}
	}
	return result
}

// batchGetRelatedFacts returns related facts for multiple fact IDs in batch.
func (s *Store) batchGetRelatedFacts(ctx context.Context, factIDs []int64) map[int64][]RelatedFact {
	if len(factIDs) == 0 {
		return nil
	}

	result := make(map[int64][]RelatedFact)
	// GetRelatedFacts is already efficient per-fact (single UNION ALL query).
	// Batch here to avoid lock churn — acquire once for all lookups.
	for _, id := range factIDs {
		related, err := s.GetRelatedFacts(ctx, id)
		if err == nil && len(related) > 0 {
			result[id] = related
		}
	}
	return result
}

// truncateExport truncates s to at most maxRunes runes for export display.
func truncateExport(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// ImportFromMarkdown parses a legacy MEMORY.md file and imports its entries as facts.
// Handles the format produced by localai_hooks.go: "## YYYY-MM-DD HH:MM\n\n- bullet\n- bullet\n"
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
