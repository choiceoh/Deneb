package chat

import (
	"fmt"
	"strings"
)

// appendUnknownArgNotice tells the CALLER that some of its arguments were not
// part of the tool's schema and were therefore dropped.
//
// Phase 1 only counted these (run.end unknownArgToolCalls). The counter's own
// motivating case then happened live on 2026-08-26: a wiki write carrying an
// invented "path" key was silently re-slugged to a different page and reported
// plain success, and the caller concluded the server had redirected the write.
// A dropped argument that changes what the tool did must not read as success.
//
// A notice, not an error: the call already ran and its result stands. Only key
// NAMES appear — argument values may hold user content.
func appendUnknownArgNotice(output, tool string, unknown []string) string {
	if len(unknown) == 0 {
		return output
	}
	notice := fmt.Sprintf(
		"[무시된 인자: %s — %s 스키마에 없는 키다. 결과가 의도와 다르면 스키마의 파라미터로 다시 호출하라.]",
		strings.Join(unknown, ", "), tool,
	)
	if strings.TrimSpace(output) == "" {
		return notice
	}
	return output + "\n\n" + notice
}
