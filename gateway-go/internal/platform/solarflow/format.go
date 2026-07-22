package solarflow

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// formatSearch renders the natural-language search response as a compact Korean
// list. The search shape is stable; each result carries a human title plus a
// dynamic data map and a link with ids the agent can chain into other actions.
func formatSearch(raw json.RawMessage, company string, limit int) string {
	var r struct {
		Query  string `json:"query"`
		Intent string `json:"intent"`
		Parsed struct {
			Manufacturer string   `json:"manufacturer"`
			SpecWp       int      `json:"spec_wp"`
			Keywords     []string `json:"keywords"`
		} `json:"parsed"`
		Results []struct {
			ResultType string         `json:"result_type"`
			Title      string         `json:"title"`
			Data       map[string]any `json:"data"`
			Link       struct {
				Module string            `json:"module"`
				Params map[string]string `json:"params"`
			} `json:"link"`
		} `json:"results"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		// Unexpected shape — fall back to the generic renderer.
		return formatGeneric("search", company, raw, limit)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[SolarFlow 검색] %q · intent=%s · 회사=%s · %d건",
		r.Query, orDash(r.Intent), company, len(r.Results))
	if r.Parsed.Manufacturer != "" || r.Parsed.SpecWp > 0 {
		b.WriteString(" · 파싱:")
		if r.Parsed.Manufacturer != "" {
			b.WriteString(" " + r.Parsed.Manufacturer)
		}
		if r.Parsed.SpecWp > 0 {
			fmt.Fprintf(&b, " %dWp", r.Parsed.SpecWp)
		}
	}
	b.WriteString("\n")

	if len(r.Results) == 0 {
		b.WriteString("(결과 없음)")
		if len(r.Warnings) > 0 {
			b.WriteString("\n경고: " + strings.Join(r.Warnings, "; "))
		}
		return b.String()
	}

	shown := r.Results
	truncated := 0
	if len(shown) > limit {
		truncated = len(shown) - limit
		shown = shown[:limit]
	}
	for i, res := range shown {
		title := strings.TrimSpace(res.Title)
		if title == "" {
			title = "(제목없음)"
		}
		fmt.Fprintf(&b, "%d. %s — %s", i+1, title, orDash(res.ResultType))
		if d := compactData(res.Data); d != "" {
			b.WriteString(" · " + d)
		}
		if id := linkID(res.Link.Params); id != "" {
			fmt.Fprintf(&b, "  [%s]", id)
		}
		b.WriteString("\n")
	}
	if truncated > 0 {
		fmt.Fprintf(&b, "…외 %d건 (limit=%d)\n", truncated, limit)
	}
	if len(r.Warnings) > 0 {
		b.WriteString("경고: " + strings.Join(r.Warnings, "; "))
	}
	return strings.TrimRight(b.String(), "\n")
}

// compactData joins up to a few scalar data fields into "k=v · k=v" order-stable.
func compactData(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := data[k]
		if v == nil {
			continue
		}
		switch vv := v.(type) {
		case string:
			if strings.TrimSpace(vv) == "" {
				continue
			}
			parts = append(parts, vv)
		case float64:
			parts = append(parts, fmt.Sprintf("%s=%s", k, trimNum(vv)))
		case bool:
			parts = append(parts, fmt.Sprintf("%s=%t", k, vv))
		}
		if len(parts) >= 5 {
			break
		}
	}
	return strings.Join(parts, " · ")
}

// linkID surfaces the first *_id param so the agent can chain (e.g. customer_id
// → outstanding). params is a string→string map.
func linkID(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.HasSuffix(k, "_id") || k == "id" {
			return k + "=" + params[k]
		}
	}
	return keys[0] + "=" + params[keys[0]]
}

// formatGeneric renders any analytics response as a Korean header line plus the
// JSON payload with long arrays capped to limit (keeps summaries/totals intact
// while bounding row lists). The agent consumes the JSON and synthesizes.
func formatGeneric(action, company string, raw json.RawMessage, limit int) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		// Not an object (rare) — emit as-is, bounded.
		s := string(raw)
		if len(s) > 12000 {
			s = s[:12000] + "\n…(잘림)"
		}
		return fmt.Sprintf("[SolarFlow %s] 회사=%s\n%s", action, company, s)
	}

	calc, _ := m["calculated_at"].(string)
	capped, truncated := capArrays(m, limit)

	pretty, err := json.MarshalIndent(capped, "", "  ")
	if err != nil {
		pretty = raw
	}
	out := string(pretty)
	if len(out) > 24000 {
		out = out[:24000] + "\n…(출력 잘림)"
	}

	var head strings.Builder
	fmt.Fprintf(&head, "[SolarFlow %s] 회사=%s", action, company)
	if calc != "" {
		head.WriteString(" · " + calc)
	}
	if truncated {
		fmt.Fprintf(&head, " · (배열은 상위 %d개로 잘림)", limit)
	}
	return head.String() + "\n" + out
}

// capArrays recursively caps every array to limit elements. Nested small arrays
// (months, data_points) stay intact; large row lists (items, matrix, movers)
// are bounded. Returns whether anything was truncated.
func capArrays(v any, limit int) (any, bool) {
	switch vv := v.(type) {
	case []any:
		truncated := false
		if len(vv) > limit {
			vv = vv[:limit]
			truncated = true
		}
		out := make([]any, len(vv))
		for i, e := range vv {
			ce, t := capArrays(e, limit)
			out[i] = ce
			truncated = truncated || t
		}
		return out, truncated
	case map[string]any:
		truncated := false
		for k, e := range vv {
			ce, t := capArrays(e, limit)
			vv[k] = ce
			truncated = truncated || t
		}
		return vv, truncated
	default:
		return v, false
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// trimNum formats a float without a trailing ".0" and with bounded precision.
func trimNum(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}
