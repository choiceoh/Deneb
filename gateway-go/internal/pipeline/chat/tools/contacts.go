package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
			return formatContacts(d.Store.LookupPhone(query), query, ""), nil
		case "search":
			// Fetch one past the display cap so truncation is visible instead
			// of silent ("외 N건" was previously impossible to know).
			matches := d.Store.Search(query, contactsSearchCap+1)
			note := ""
			if len(matches) > contactsSearchCap {
				matches = matches[:contactsSearchCap]
				note = fmt.Sprintf("결과가 %d건을 넘어 일부만 표시했습니다 — query를 좁히거나, 회사 전체는 action=by_company를 쓰세요.", contactsSearchCap)
			}
			return formatContacts(matches, query, note), nil
		case "by_company", "company":
			// Company roster: enumerate everyone whose Org matches — "우리가
			// 현대차에 아는 사람 전부" was not answerable via ranked search.
			matches := contactsByOrg(d.Store.All(), query)
			note := ""
			if len(matches) > contactsCompanyCap {
				note = fmt.Sprintf("총 %d명 중 %d명 표시 — query를 좁히세요.", len(matches), contactsCompanyCap)
				matches = matches[:contactsCompanyCap]
			}
			return formatContacts(matches, query, note), nil
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: lookup (전화번호로 인물 찾기), search (이름·회사·이메일 검색), by_company (회사명으로 소속 인물 전체 열거)", p.Action), nil
		}
	}
}

const (
	contactsSearchCap  = 20
	contactsCompanyCap = 50
)

// contactsByOrg returns every contact whose Org contains the query
// (case-insensitive), sorted by name for a stable roster.
func contactsByOrg(all []contacts.Contact, query string) []contacts.Contact {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []contacts.Contact
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Org), q) {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// formatContacts renders matched contacts through the shared recall format so a
// person (인물) result reads like wiki/knowledge/polaris hits: a "c:<이름>"
// namespaced ref, phone/org as meta, emails as the snippet. The c: ref is a
// locator into the address book; the curated wiki page (if any) lives at
// w:인물/<이름>, which the trailing hint points at. query is what was looked up.
func formatContacts(matches []contacts.Contact, query, note string) string {
	if len(matches) == 0 {
		return fmt.Sprintf("'%s'와 일치하는 연락처 없음.", query)
	}
	var sb strings.Builder
	sb.WriteString(recallHeader(query, len(matches), "주소록"))
	for i, c := range matches {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = "(이름 없음)"
		}
		var meta []string
		if phone := strings.Join(trimNonEmpty(c.Phones), ", "); phone != "" {
			meta = append(meta, phone)
		}
		if org := strings.TrimSpace(c.Org); org != "" {
			meta = append(meta, org)
		}
		emails := strings.Join(trimNonEmpty(c.Emails), ", ")
		sb.WriteString(recallRow(i+1, RefContact+name, strings.Join(meta, " · "), emails))
	}
	if note != "" {
		sb.WriteString(note + "\n")
	}
	sb.WriteString("큐레이션된 인물 정보는 `knowledge(op=\"recall\", query=\"이름\")` → `w:인물/...`.")
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
