// people.go — one entry point for "who is this person".
//
// Three stores answer person questions and none of them dominates: the synced
// address book keeps 번호↔이름 (and only it can go from a number back to a
// name), org.json keeps the operator-curated 조직 위치·직급, and Amaranth's HR
// area keeps the live 부서·휴대폰·생년월일. Until the 2026-08-29 audit each was
// its own tool, which handed the model a routing question it had no way to
// answer from the outside — and the person slot is the weakest recall slot we
// measure. This tool answers by fanning out instead of by choosing: one call
// asks every store that can speak to the query and returns their answers under
// labeled sections.
//
// Fan-out is sequential on purpose (simple beats concurrent here): the cost
// that mattered was LLM round-trips, not backend latency, and three sequential
// reads still collapse three turns into one. Sources are injected as plain
// ToolFuncs so this package depends on none of the three implementations.
package peopleops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Sources holds the three person stores, each already wired as a tool func.
// A nil member is simply skipped — an unconfigured groupware or a missing
// address book degrades the answer instead of failing it.
type Sources struct {
	Contacts  toolport.ToolFunc // address book: 번호↔이름, 회사 로스터
	Org       toolport.ToolFunc // org.json: 조직 위치·직급 (read-only)
	Groupware toolport.ToolFunc // Amaranth area=people: 라이브 부서·휴대폰
}

// unavailable marks a source that returned nothing worth showing. Each source
// has its own "not set up" sentence, so the facade matches on the shapes it
// knows rather than trying to classify arbitrary text.
func unavailable(out string) bool {
	s := strings.TrimSpace(out)
	if s == "" {
		return true
	}
	for _, marker := range []string{
		"주소록이 비어 있습니다",
		"조직도가 아직 설정되지 않았습니다",
		"검색 결과 없음",
		"일치하는 연락처가 없습니다",
		"그룹웨어가 설정되지",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

// section renders one labeled block, or nothing when the source had no answer.
func section(b *strings.Builder, title, body string) {
	if unavailable(body) {
		return
	}
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	fmt.Fprintf(b, "## %s\n%s", title, strings.TrimRight(body, "\n"))
}

// call runs one source with the given input, swallowing its error into a
// skipped section: a broken address book must not take the org answer with it.
func call(ctx context.Context, fn toolport.ToolFunc, input any) string {
	if fn == nil {
		return ""
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	out, err := fn(ctx, raw)
	if err != nil {
		return ""
	}
	return out
}

// ToolPeople returns the unified person-lookup tool.
func ToolPeople(s Sources) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Query  string `json:"query"`
		}
		if err := jsonutil.UnmarshalInto("people params", input, &p); err != nil {
			return "", err
		}
		action := strings.ToLower(strings.TrimSpace(p.Action))
		if action == "" {
			action = "find"
		}
		query := strings.TrimSpace(p.Query)

		switch action {
		case "phone":
			// Only the address book maps a number back to a name.
			if query == "" {
				return "phone에는 query에 전화번호가 필요합니다.", nil
			}
			out := call(ctx, s.Contacts, map[string]string{"action": "lookup", "query": query})
			if unavailable(out) {
				return fmt.Sprintf("주소록에서 %s를 찾지 못했습니다.", query), nil
			}
			return out, nil

		case "company":
			if query == "" {
				return "company에는 query에 회사명이 필요합니다.", nil
			}
			out := call(ctx, s.Contacts, map[string]string{"action": "by_company", "query": query})
			if unavailable(out) {
				return fmt.Sprintf("주소록에 %s 소속으로 등록된 사람이 없습니다.", query), nil
			}
			return out, nil

		case "tree":
			// Whole chart (or a subtree when query names a team/company).
			out := call(ctx, s.Org, map[string]string{"query": query})
			if unavailable(out) {
				return "조직도가 아직 설정되지 않았습니다 (네이티브 앱 설정 > 조직도에서 편집).", nil
			}
			return out, nil

		case "find":
			if query == "" {
				return "query는 필수입니다 — 사람 이름(또는 회사·팀)을 넣으세요.", nil
			}
			var b strings.Builder
			section(&b, "주소록", call(ctx, s.Contacts, map[string]string{"action": "search", "query": query}))
			section(&b, "조직도", call(ctx, s.Org, map[string]string{"query": query}))
			section(&b, "그룹웨어(라이브)", call(ctx, s.Groupware,
				map[string]any{"action": "list", "area": "people", "query": query}))
			if b.Len() == 0 {
				return fmt.Sprintf("%s: 주소록·조직도·그룹웨어 어디에도 일치하는 사람이 없습니다.", query), nil
			}
			return b.String(), nil

		default:
			return "action은 find, phone, company, tree 중 하나입니다 (기본 find).", nil
		}
	}
}
