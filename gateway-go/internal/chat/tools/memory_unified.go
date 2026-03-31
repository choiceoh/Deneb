package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// memoryParams is the shared parameter struct for all memory tool actions.
type memoryParams struct {
	Action     string  `json:"action"`
	Query      string  `json:"query"`
	FactID     *int64  `json:"fact_id"`
	Category   string  `json:"category"`
	Importance float64 `json:"importance"`
	Limit      int     `json:"limit"`
	Sort       string  `json:"sort"`
	Offset     int     `json:"offset"`
}

// ToolMemory creates the unified memory tool that combines structured fact store
// with file-based memory search. Degrades gracefully when memory store is nil
// (falls back to file-based search only).
func ToolMemory(d *toolctx.VegaDeps, workspaceDir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p memoryParams
		if err := jsonutil.UnmarshalInto("memory params", input, &p); err != nil {
			return "", err
		}

		switch p.Action {
		case "search", "":
			return memorySearch(ctx, d, workspaceDir, p)
		case "get":
			return memoryGet(ctx, d, p)
		case "set":
			return memorySet(ctx, d, p)
		case "forget":
			return memoryForget(ctx, d, p)
		case "browse":
			return memoryBrowse(ctx, d, p)
		case "status":
			return memoryStatus(ctx, d, workspaceDir)
		default:
			return "", fmt.Errorf("unknown action: %s (use: search, get, set, forget, status, browse)", p.Action)
		}
	}
}

func memorySearch(ctx context.Context, d *toolctx.VegaDeps, workspaceDir string, p memoryParams) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query is required for search")
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}

	var parts []string

	if d.MemoryStore != nil {
		var queryVec []float32
		if d.MemoryEmbedder != nil {
			vec, err := d.MemoryEmbedder.EmbedQuery(ctx, p.Query)
			if err == nil {
				queryVec = vec
			}
		}
		opts := memory.SearchOpts{
			Limit:    p.Limit,
			Category: p.Category,
		}
		if d.MemoryEmbedder == nil {
			// Match knowledge prefetch threshold (knowledge.go) for consistent
			// recall between tool search and automatic knowledge injection.
			opts.MinImportance = 0.6
		}
		results, err := d.MemoryStore.SearchFacts(ctx, p.Query, queryVec, opts)
		if err != nil {
			return "", fmt.Errorf("search facts: %w", err)
		}
		for _, sr := range results {
			timeLabel := formatFactTime(sr.Fact)
			if timeLabel != "" {
				parts = append(parts, fmt.Sprintf("- [fact #%d] [%.2f] {%s} (%s) %s",
					sr.Fact.ID, sr.Score, sr.Fact.Category, timeLabel, sr.Fact.Content))
			} else {
				parts = append(parts, fmt.Sprintf("- [fact #%d] [%.2f] {%s} %s",
					sr.Fact.ID, sr.Score, sr.Fact.Category, sr.Fact.Content))
			}
		}
	}

	if workspaceDir != "" {
		fileMatches := SearchMemoryFiles(workspaceDir, p.Query, p.Limit)
		if fileMatches == nil {
			// no files found: skip
		} else {
			for _, m := range fileMatches {
				parts = append(parts, fmt.Sprintf("- [file] %s (line %d): %s",
					m.File, m.Line, m.Snippet))
			}
		}
	}

	if len(parts) == 0 {
		return fmt.Sprintf("No results found for %q.", p.Query), nil
	}

	header := fmt.Sprintf("## Memory search: %q (%d results)\n", p.Query, len(parts))
	return header + strings.Join(parts, "\n"), nil
}

func memoryGet(ctx context.Context, d *toolctx.VegaDeps, p memoryParams) (string, error) {
	if p.FactID == nil {
		return "", fmt.Errorf("fact_id is required for get")
	}
	if d.MemoryStore == nil {
		return "[memory store not configured]", nil
	}

	fact, err := d.MemoryStore.GetFact(ctx, *p.FactID)
	if err != nil {
		return "", fmt.Errorf("get fact: %w", err)
	}
	if fact == nil {
		return fmt.Sprintf("Fact #%d not found.", *p.FactID), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Fact #%d\n", fact.ID)
	fmt.Fprintf(&sb, "- **Content**: %s\n", fact.Content)
	fmt.Fprintf(&sb, "- **Category**: %s\n", fact.Category)
	fmt.Fprintf(&sb, "- **Importance**: %.2f\n", fact.Importance)
	fmt.Fprintf(&sb, "- **Source**: %s\n", fact.Source)
	fmt.Fprintf(&sb, "- **Active**: %v\n", fact.Active)
	fmt.Fprintf(&sb, "- **Access Count**: %d\n", fact.AccessCount)
	if fact.MergeDepth > 0 {
		fmt.Fprintf(&sb, "- **Merge Depth**: %d\n", fact.MergeDepth)
	}
	if !fact.CreatedAt.IsZero() {
		fmt.Fprintf(&sb, "- **Created**: %s\n", fact.CreatedAt.Format(time.RFC3339))
	}
	if !fact.UpdatedAt.IsZero() {
		fmt.Fprintf(&sb, "- **Updated**: %s\n", fact.UpdatedAt.Format(time.RFC3339))
	}
	if fact.LastAccessedAt != nil {
		fmt.Fprintf(&sb, "- **Last Accessed**: %s\n", fact.LastAccessedAt.Format(time.RFC3339))
	}
	if fact.VerifiedAt != nil {
		fmt.Fprintf(&sb, "- **Verified**: %s\n", fact.VerifiedAt.Format(time.RFC3339))
	}
	if fact.ExpiresAt != nil {
		fmt.Fprintf(&sb, "- **Expires**: %s\n", fact.ExpiresAt.Format(time.RFC3339))
	}
	if fact.SupersededBy != nil {
		fmt.Fprintf(&sb, "- **Superseded By**: #%d\n", *fact.SupersededBy)
	}
	return sb.String(), nil
}

func memorySet(ctx context.Context, d *toolctx.VegaDeps, p memoryParams) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query (fact content) is required for set")
	}
	if d.MemoryStore == nil {
		return "[memory store not configured]", nil
	}

	category := p.Category
	if category == "" {
		category = memory.CategoryContext
	}
	importance := p.Importance
	if importance <= 0 {
		importance = 0.5
	}

	fact := memory.Fact{
		Content:    p.Query,
		Category:   category,
		Importance: importance,
		Source:     memory.SourceManual,
	}

	id, err := d.MemoryStore.InsertFact(ctx, fact)
	if err != nil {
		return "", fmt.Errorf("insert fact: %w", err)
	}

	if d.MemoryEmbedder != nil {
		go func() {
			bgCtx := context.Background()
			if embedErr := d.MemoryEmbedder.EmbedAndStore(bgCtx, id, p.Query); embedErr != nil {
				_ = embedErr
			}
		}()
	}

	return fmt.Sprintf("Fact #%d created: {%s} [%.2f] %s", id, category, importance, p.Query), nil
}

func memoryForget(ctx context.Context, d *toolctx.VegaDeps, p memoryParams) (string, error) {
	if p.FactID == nil {
		return "", fmt.Errorf("fact_id is required for forget")
	}
	if d.MemoryStore == nil {
		return "[memory store not configured]", nil
	}

	if err := d.MemoryStore.DeactivateFact(ctx, *p.FactID); err != nil {
		return "", fmt.Errorf("deactivate fact: %w", err)
	}

	return fmt.Sprintf("Fact #%d deactivated.", *p.FactID), nil
}

func memoryBrowse(ctx context.Context, d *toolctx.VegaDeps, p memoryParams) (string, error) {
	if d.MemoryStore == nil {
		return "[memory store not configured]", nil
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	offset := p.Offset
	if offset < 0 {
		offset = 0
	}
	sortOrder := p.Sort
	if sortOrder == "" {
		sortOrder = "importance"
	}

	facts, total, err := d.MemoryStore.BrowseFacts(ctx, p.Category, sortOrder, limit, offset)
	if err != nil {
		return "", fmt.Errorf("browse facts: %w", err)
	}

	var sb strings.Builder

	// Header with filter/sort info.
	var header string
	if p.Category != "" {
		header = fmt.Sprintf("## Memory browse: category=%s, sort=%s", p.Category, sortOrder)
	} else {
		header = fmt.Sprintf("## Memory browse: all categories, sort=%s", sortOrder)
	}
	fmt.Fprintf(&sb, "%s (%d\u2013%d / %d\uAC1C)\n", header, offset+1, offset+len(facts), total)

	if len(facts) == 0 {
		sb.WriteString("\n\uB354 \uC774\uC0C1 \uACB0\uACFC \uC5C6\uC74C.\n")
		return sb.String(), nil
	}

	for _, f := range facts {
		timeLabel := formatFactTime(f)
		if timeLabel != "" {
			fmt.Fprintf(&sb, "- [#%d] [%.2f] {%s} (%s) %s\n", f.ID, f.Importance, f.Category, timeLabel, f.Content)
		} else {
			fmt.Fprintf(&sb, "- [#%d] [%.2f] {%s} %s\n", f.ID, f.Importance, f.Category, f.Content)
		}
	}

	if offset+limit < total {
		fmt.Fprintf(&sb, "\n_\uB2E4\uC74C \uD398\uC774\uC9C0: action=browse offset=%d limit=%d_", offset+limit, limit)
	}

	return sb.String(), nil
}

func memoryStatus(ctx context.Context, d *toolctx.VegaDeps, workspaceDir string) (string, error) {
	var sb strings.Builder
	sb.WriteString("## Memory Status\n\n")

	if d.MemoryStore != nil {
		count, err := d.MemoryStore.ActiveFactCount(ctx)
		if err != nil {
			return "", fmt.Errorf("fact count: %w", err)
		}
		sb.WriteString(fmt.Sprintf("### Structured Store\n- **Active facts**: %d\n", count))

		// Category breakdown with top fact previews.
		cats, catsErr := d.MemoryStore.CategoryCounts(ctx)
		if catsErr == nil && len(cats) > 0 {
			sb.WriteString("- **Categories**:")
			for cat, n := range cats {
				sb.WriteString(fmt.Sprintf(" %s=%d", cat, n))
			}
			sb.WriteString("\n")
		}

		// Tier-1 (always-injected) fact count.
		if t1, err := d.MemoryStore.Tier1FactCount(ctx); err == nil {
			sb.WriteString(fmt.Sprintf("- **Tier-1 (always-injected)**: %d\n", t1))
		}

		// Embedding coverage.
		if embedded, total, err := d.MemoryStore.EmbeddingCoverage(ctx); err == nil && total > 0 {
			pct := float64(embedded) / float64(total) * 100
			sb.WriteString(fmt.Sprintf("- **Embedding coverage**: %d/%d (%.0f%%)\n", embedded, total, pct))
		}

		sb.WriteString(fmt.Sprintf("- **Embedding API**: %s\n", boolLabel(d.MemoryEmbedder != nil)))

		// User model.
		entries, err := d.MemoryStore.GetUserModel(ctx)
		if err == nil && len(entries) > 0 {
			sb.WriteString(fmt.Sprintf("- **User model keys**: %d\n", len(entries)))
		}

		// Last dreaming cycle.
		if lastDream, err := d.MemoryStore.LastDreamingLog(ctx); err == nil && lastDream != nil {
			ago := time.Since(lastDream.RanAt)
			sb.WriteString(fmt.Sprintf("- **Last dreaming**: %s (%s ago, verified=%d merged=%d pruned=%d)\n",
				lastDream.RanAt.Format("2006-01-02 15:04"),
				formatDuration(ago),
				lastDream.FactsVerified, lastDream.FactsMerged, lastDream.FactsPruned))
		}

		// Top fact previews per category for discoverability.
		if catsErr == nil && len(cats) > 0 {
			sb.WriteString("\n### Fact Previews (top by importance)\n")
			// Sort category names for stable output.
			catNames := make([]string, 0, len(cats))
			for cat := range cats {
				catNames = append(catNames, cat)
			}
			sort.Strings(catNames)
			for _, cat := range catNames {
				previewFacts, _, previewErr := d.MemoryStore.BrowseFacts(ctx, cat, "importance", 2, 0)
				if previewErr != nil || len(previewFacts) == 0 {
					continue
				}
				sb.WriteString(fmt.Sprintf("**%s** (%d\uAC1C):\n", cat, cats[cat]))
				for _, f := range previewFacts {
					sb.WriteString(fmt.Sprintf("  - [#%d] [%.2f] %s\n", f.ID, f.Importance, f.Content))
				}
			}
		}
	} else {
		sb.WriteString("### Structured Store\n- **Status**: not configured\n")
	}

	if workspaceDir != "" {
		files := CollectMemoryFiles(workspaceDir)
		sb.WriteString(fmt.Sprintf("\n### File-based Memory\n- **Files**: %d\n", len(files)))
		for _, f := range files {
			sb.WriteString(fmt.Sprintf("  - %s\n", f))
		}
	}

	return sb.String(), nil
}

func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func formatFactTime(f memory.Fact) string {
	if f.UpdatedAt.IsZero() && f.CreatedAt.IsZero() {
		return ""
	}
	ref := f.UpdatedAt
	if ref.IsZero() {
		ref = f.CreatedAt
	}
	d := time.Since(ref)
	switch {
	case d < time.Hour:
		return "방금"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d일 전", int(d.Hours()/24))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d주 전", int(d.Hours()/24/7))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d개월 전", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%d년 전", int(d.Hours()/24/365))
	}
}

func boolLabel(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
