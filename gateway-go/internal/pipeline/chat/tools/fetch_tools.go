// fetch_tools.go — Meta-tool that activates deferred tools mid-run.
//
// Deferred tools have their name+description visible in the system prompt but
// full JSON schemas are not sent in the initial Tools array. When the LLM
// needs a deferred tool, it calls fetch_tools to:
//  1. Get the full schema description (returned as text).
//  2. Signal DeferredActivation so the executor injects schemas on the next turn.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// FetchToolsRegistry is the subset of ToolRegistry needed by fetch_tools.
type FetchToolsRegistry interface {
	DeferredToolDef(name string) (toolctx.ToolDef, bool)
	DeferredSummaries() []toolctx.DeferredToolSummary
}

// ToolFetchTools returns a tool that activates deferred tools and returns their schemas.
func ToolFetchTools(registry FetchToolsRegistry) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Names []string `json:"names"`
			Query string   `json:"query"`
		}
		if err := jsonutil.UnmarshalInto("fetch_tools params", input, &p); err != nil {
			return "", err
		}

		if len(p.Names) == 0 && p.Query == "" {
			return "", fmt.Errorf("names or query is required")
		}

		// If query is provided, search deferred tools by keyword.
		if p.Query != "" && len(p.Names) == 0 {
			q := strings.ToLower(p.Query)
			for _, s := range registry.DeferredSummaries() {
				if strings.Contains(strings.ToLower(s.Name), q) ||
					strings.Contains(strings.ToLower(s.Description), q) {
					p.Names = append(p.Names, s.Name)
				}
			}
			if len(p.Names) == 0 {
				return fmt.Sprintf("No deferred tools match query %q.", p.Query), nil
			}
		}

		// Activate and collect schema descriptions.
		da := toolctx.DeferredActivationFromContext(ctx)

		var sb strings.Builder
		var activated []string
		for _, name := range p.Names {
			def, ok := registry.DeferredToolDef(name)
			if !ok {
				fmt.Fprintf(&sb, "- %s: not found or not a deferred tool\n", name)
				continue
			}
			activated = append(activated, name)

			// Format schema for LLM readability.
			fmt.Fprintf(&sb, "## %s\n%s\n", def.Name, def.Description)
			if def.InputSchema != nil {
				schemaJSON, _ := json.MarshalIndent(def.InputSchema, "", "  ")
				fmt.Fprintf(&sb, "```json\n%s\n```\n", schemaJSON)
			}
			sb.WriteString("\n")
		}

		if da != nil && len(activated) > 0 {
			da.Activate(activated)
		}

		if len(activated) > 0 {
			fmt.Fprintf(&sb, "Activated %d tool(s): %s. You can now call them directly.",
				len(activated), strings.Join(activated, ", "))
		}

		return sb.String(), nil
	}
}
