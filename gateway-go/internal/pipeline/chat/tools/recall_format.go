package tools

import (
	"fmt"
	"strings"
)

// Shared recall presentation for the memory-search tools (wiki, knowledge,
// polaris). The tools stay separate — different backends, different read paths
// — but a hit looks the same whichever tool produced it, and every source is
// cited by a namespaced ref. This is the "결과 포맷·ref 통일" integration: unify
// the rendered shape and the ref scheme, not the storage.
//
// Ref namespaces:
//
//	w:<path>      wiki page                (read: wiki read / knowledge read)
//	h:<id>        hindsight memory         (read: knowledge read)
//	p:msg<index>  polaris session message  (locate: polaris describe → expand)
const (
	RefWiki      = "w:"
	RefHindsight = "h:"
	RefSession   = "p:"
)

// recallHeader renders the shared "🔍 query (N건)" header. extra is an optional
// note placed inside the parentheses (e.g. "wiki" or "layers=[wiki hindsight]").
func recallHeader(query string, count int, extra string) string {
	if extra != "" {
		return fmt.Sprintf("## 🔍 %q (%d건, %s)\n\n", query, count, extra)
	}
	return fmt.Sprintf("## 🔍 %q (%d건)\n\n", query, count)
}

// recallRow renders one shared result row: index, backtick-quoted namespaced
// ref, an optional meta suffix (time/role/score), then the snippet on its own
// line. Snippets are trimmed and truncated to a uniform width.
func recallRow(idx int, ref, meta, snippet string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d. `%s`", idx, ref)
	if meta != "" {
		fmt.Fprintf(&sb, " · %s", meta)
	}
	sb.WriteString("\n")
	if s := strings.TrimSpace(snippet); s != "" {
		sb.WriteString("   ")
		sb.WriteString(truncate(s, 240))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	return sb.String()
}
