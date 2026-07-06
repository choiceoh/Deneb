package mcpclient

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// maxErrorTextBytes bounds the server-supplied text embedded in a Go error
// for an isError tool result. Error strings bypass the executor's normal
// output truncation/spillover, so an unbounded (or adversarial) remote error
// payload must be capped here.
const maxErrorTextBytes = 2000

// renderToolResult flattens a tools/call result into agent-consumable text.
// Fidelity rules, in spirit of "never silently drop what the model may need":
//
//   - text blocks pass through;
//   - embedded resource blocks contribute their text (labeled with the URI)
//     instead of being elided — MCP servers use these for file-like payloads;
//   - other non-text blocks (image/audio) are named so the model knows
//     something was omitted;
//   - structuredContent (2025-06-18 spec) is used as the result when the
//     content array rendered to nothing — servers SHOULD mirror it in text,
//     so it is only a fallback, never a duplicate;
//   - isError=true becomes a Go error with the rendered (bounded) text.
func renderToolResult(name string, raw json.RawMessage) (string, error) {
	var res struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Resource *struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"resource"`
		} `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("mcpclient: tools/call result: %w", err)
	}
	var sb strings.Builder
	textSubstance := false
	for _, block := range res.Content {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		switch {
		case block.Type == "text":
			sb.WriteString(block.Text)
			if strings.TrimSpace(block.Text) != "" {
				textSubstance = true
			}
		case block.Type == "resource" && block.Resource != nil && block.Resource.Text != "":
			fmt.Fprintf(&sb, "[resource %s]\n%s", block.Resource.URI, block.Resource.Text)
			textSubstance = true
		default:
			fmt.Fprintf(&sb, "[%s content omitted]", block.Type)
		}
	}
	out := sb.String()
	// Substance-based fallback: "[image content omitted]" placeholders are
	// not substance — a result whose only text is placeholders must still
	// surface its structuredContent, or an image+structured result loses
	// its machine-readable payload entirely.
	if !textSubstance && len(res.StructuredContent) > 0 {
		if strings.TrimSpace(out) != "" {
			out += "\n"
		} else {
			out = ""
		}
		out += string(res.StructuredContent)
	}
	if res.IsError {
		return "", fmt.Errorf("mcp tool %s failed: %s", name, truncateRuneSafe(out, maxErrorTextBytes))
	}
	return out, nil
}

// truncateRuneSafe bounds s to max bytes without splitting a multi-byte rune
// (Korean error text), appending a truncation marker when cut. The boundary
// work is textutil.TruncateBytes — the repo's canonical helper.
func truncateRuneSafe(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return textutil.TruncateBytes(s, max) + "… [truncated]"
}
