package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// ToolContacts returns the address-book lookup tool. It reads the contacts store
// mirrored from the native client's contacts sync (miniapp.capture.contacts) and
// answers "whose number is this?" (lookup) and name/company search (search).
//
// The store is read-only here: writes happen only via the contacts sync RPC, which
// fully replaces the snapshot. A nil store (contacts sync never ran / init failed)
// degrades to a clear Korean "unavailable" message rather than an error.
func ToolContacts(d *toolctx.ContactsDeps) toolctx.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Query  string `json:"query"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}

		if d.Store == nil || d.Store.Count() == 0 {
			return "주소록이 비어 있습니다. 네이티브 클라이언트에서 연락처를 동기화하면 번호 조회와 이름 검색을 쓸 수 있습니다.", nil
		}

		query := strings.TrimSpace(p.Query)
		if query == "" {
			return "query는 필수입니다.", nil
		}

		switch p.Action {
		case "lookup":
			return formatContacts(d.Store.LookupPhone(query), fmt.Sprintf("번호 '%s'", query)), nil
		case "search":
			return formatContacts(d.Store.Search(query, 20), fmt.Sprintf("'%s'", query)), nil
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: lookup (전화번호로 인물 찾기), search (이름·회사로 검색)", p.Action), nil
		}
	}
}

// formatContacts renders matched contacts as a Korean list (이름 · 번호 · 회사).
// label describes what was looked up, for the no-match line.
func formatContacts(matches []contacts.Contact, label string) string {
	if len(matches) == 0 {
		return fmt.Sprintf("%s와 일치하는 연락처 없음.", label)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## 주소록 조회 결과 (%d건)\n\n", len(matches))
	for _, c := range matches {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = "(이름 없음)"
		}
		fields := []string{name}
		if phone := strings.Join(trimNonEmpty(c.Phones), ", "); phone != "" {
			fields = append(fields, phone)
		}
		if org := strings.TrimSpace(c.Org); org != "" {
			fields = append(fields, org)
		}
		if email := strings.Join(trimNonEmpty(c.Emails), ", "); email != "" {
			fields = append(fields, email)
		}
		fmt.Fprintf(&sb, "- %s\n", strings.Join(fields, " · "))
	}
	return strings.TrimRight(sb.String(), "\n")
}

// trimNonEmpty trims each entry and drops blanks, preserving order.
func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
