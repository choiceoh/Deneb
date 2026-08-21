package mcpclient

import (
	"encoding/json"
	"fmt"
	"sort"
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
//   - isError=true becomes a Go error with the rendered (bounded) text;
//   - resultType="input_required" (2026-07-28 MRTR) becomes an explicit error:
//     this client declares no sampling/elicitation/roots capability, so it
//     cannot answer the server's input requests, and an interim result has no
//     content to render. Saying so beats returning "" — which a model reads
//     as "the tool ran and found nothing".
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
		// resultType is absent on pre-2026-07-28 servers, which the spec says
		// to read as "complete" — the zero value does exactly that.
		ResultType    string `json:"resultType"`
		InputRequests map[string]struct {
			Method string `json:"method"`
		} `json:"inputRequests"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("mcpclient: tools/call result: %w", err)
	}
	if res.ResultType == resultTypeInputRequired {
		return "", fmt.Errorf("mcp tool %s needs client input this gateway does not provide (%s)",
			name, describeInputRequests(res.InputRequests))
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

// describeInputRequests names the capabilities an MRTR server asked for, so
// the operator sees WHICH one is missing (sampling? elicitation? roots?)
// rather than a bare "unsupported". Sorted for a stable error string.
func describeInputRequests(requests map[string]struct {
	Method string `json:"method"`
},
) string {
	if len(requests) == 0 {
		return "no input requests declared"
	}
	methods := make([]string, 0, len(requests))
	for _, req := range requests {
		if req.Method != "" {
			methods = append(methods, req.Method)
		}
	}
	if len(methods) == 0 {
		return "no input requests declared"
	}
	sort.Strings(methods)
	return strings.Join(methods, ", ")
}
