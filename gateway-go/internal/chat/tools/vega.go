package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolVega creates the vega ToolFunc.
// d is passed by pointer so VegaDeps.Backend is read at call time (late-binding).
func ToolVega(d *toolctx.VegaDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Query  string `json:"query"`
			Limit  int    `json:"limit"`
			Mode   string `json:"mode"`
		}
		if err := jsonutil.UnmarshalInto("vega params", input, &p); err != nil {
			return "", err
		}
		if p.Query == "" {
			return "", fmt.Errorf("query is required")
		}

		backend := d.Backend
		if backend == nil {
			return "[vega unavailable: backend not configured]", nil
		}

		action := p.Action
		if action == "" {
			action = "search"
		}

		switch action {
		case "search":
			return vegaSearch(ctx, backend, p.Query, p.Limit, p.Mode)
		case "ask":
			return vegaAsk(ctx, backend, p.Query)
		default:
			return "", fmt.Errorf("unknown vega action: %s (use search or ask)", action)
		}
	}
}

func vegaSearch(ctx context.Context, backend vega.Backend, query string, limit int, mode string) (string, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	opts := vega.SearchOpts{
		Limit: limit,
		Mode:  mode,
	}

	results, err := backend.Search(ctx, query, opts)
	if err != nil {
		return fmt.Sprintf("[vega search error: %s]", err), nil
	}

	if len(results) == 0 {
		return fmt.Sprintf("No results found for: %s", query), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Vega search: %d results for %q\n\n", len(results), query)
	for i, r := range results {
		fmt.Fprintf(&sb, "### %d. %s", i+1, r.ProjectName)
		if r.Section != "" {
			fmt.Fprintf(&sb, " — %s", r.Section)
		}
		fmt.Fprintf(&sb, " (score: %.2f)\n", r.Score)
		sb.WriteString(r.Content)
		sb.WriteByte('\n')
		if i < len(results)-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String(), nil
}

func vegaAsk(ctx context.Context, backend vega.Backend, query string) (string, error) {
	result, err := backend.Execute(ctx, "ask", map[string]any{"query": query})
	if err != nil {
		return fmt.Sprintf("[vega ask error: %s]", err), nil
	}
	return string(result), nil
}
