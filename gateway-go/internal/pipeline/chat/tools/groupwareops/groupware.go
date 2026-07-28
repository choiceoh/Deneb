package groupwareops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	contactdomain "github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const peopleJSONMarker = "DENEB_PEOPLE_JSON:"

// ToolGroupware reads Amaranth10 전자결재 / 게시판 / 매출·재고·발주·입고·출고·단가·사원 via srv4 headless login.
// Approval folders: pending(미결) · done(기결) · cc(수신참조) · total(전체결재문서) · all(순회).
// Read-only: never approve, post, or delete. Unconfigured → calm off message.
// store may be nil — people wiki enrichment is then skipped (Amaranth text only).
func ToolGroupware(store *wiki.Store) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action     string `json:"action"`
			Area       string `json:"area"`
			Folder     string `json:"folder"`
			Query      string `json:"query"`
			DocID      string `json:"doc_id"`
			Attachment string `json:"attachment"`
			Limit      int    `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("groupware params", input, &p); err != nil {
			return "", err
		}

		cfg, ok := groupware.FromEnv()
		action := strings.ToLower(strings.TrimSpace(p.Action))
		switch action {
		case "", "status":
			return groupware.StatusLine(cfg, ok), nil
		case "list", "read", "attachment", "summary":
			// continue
		default:
			return "", fmt.Errorf("unknown groupware action %q (status|list|read|attachment|summary)", p.Action)
		}

		if !ok {
			return groupware.StatusLine(cfg, false), nil
		}

		area := strings.ToLower(strings.TrimSpace(p.Area))
		if action == "attachment" && area == "" {
			area = "approval"
		}
		switch area {
		case "approval", "board", "sales", "stock", "po", "receive", "ship", "price", "people":
		case "전자결재", "결재":
			area = "approval"
		case "게시판", "공지", "공지사항":
			area = "board"
		case "매출", "매출마감":
			area = "sales"
		case "재고", "현재고", "자재재고":
			area = "stock"
		case "발주", "발주현황", "구매발주":
			area = "po"
		case "입고", "입고현황":
			area = "receive"
		case "출고", "출고현황", "물류출고":
			area = "ship"
		case "단가", "품목단가", "단가등록":
			area = "price"
		case "사원", "직원", "인사", "조직", "연락처", "사람들":
			area = "people"
		case "영업":
			// ambiguous legacy alias → sales (매출마감)
			area = "sales"
		case "":
			return "area가 필요합니다 — approval|board|sales|stock|po|receive|ship|price|people.", nil
		default:
			return "", fmt.Errorf("unknown groupware area %q (approval|board|sales|stock|po|receive|ship|price|people)", p.Area)
		}

		switch area {
		case "sales":
			switch action {
			case "summary", "list":
			default:
				return "sales는 action=summary(또는 list)만 지원합니다 — folder=ytd|month|today|year|last_year.", nil
			}
		case "stock", "po", "receive", "ship", "price":
			if action != "list" && action != "summary" {
				return fmt.Sprintf("%s는 action=list 만 지원합니다 (기간 folder=ytd|month|today, query=키워드).", area), nil
			}
			if action == "summary" {
				action = "list"
			}
		case "people":
			if action != "list" && action != "summary" && action != "read" {
				return "people는 action=list 만 지원합니다 — query에 이름(일부) 필수.", nil
			}
			action = "list"
			if strings.TrimSpace(p.Query) == "" {
				return `people는 query(이름 키워드)가 필요합니다. 예: query="김".`, nil
			}
		default:
			if action == "summary" {
				return "summary는 area=sales(매출마감)에서만 지원합니다.", nil
			}
		}

		folder, ferr := normalizeFolder(p.Folder, action, area)
		if ferr != nil {
			//nolint:nilerr // tool contract: user-facing guidance rides the result text, not the error
			return ferr.Error(), nil
		}

		if action == "read" && strings.TrimSpace(p.Query) == "" {
			return "read에는 query(제목·키워드)가 필요합니다. 목록은 action=list 로 먼저 확인하라.", nil
		}
		if action == "attachment" {
			if area != "approval" {
				return "attachment는 전자결재(area=approval)에서만 지원합니다.", nil
			}
			if strings.TrimSpace(p.DocID) == "" || strings.TrimSpace(p.Attachment) == "" {
				return "attachment에는 read 결과의 doc_id와 읽을 첨부 번호·파일명이 필요합니다.", nil
			}
		}

		out, err := groupware.Run(ctx, cfg, groupware.Request{
			Area:       groupware.Area(area),
			Action:     groupware.Action(action),
			Folder:     folder,
			Query:      strings.TrimSpace(p.Query),
			DocID:      strings.TrimSpace(p.DocID),
			Attachment: strings.TrimSpace(p.Attachment),
			Limit:      p.Limit,
		})
		if err != nil && strings.TrimSpace(out) != "" {
			return out, nil
		}
		if err != nil {
			return fmt.Sprintf("그룹웨어 읽기 실패: %v", err), nil
		}
		if area == "people" {
			out = linkPeopleWikiAndOrg(store, out)
		}
		return out, nil
	}
}

// linkPeopleWikiAndOrg strips DENEB_PEOPLE_JSON, enriches/creates 인물 pages
// when store is non-nil, and appends a 연계 block (wiki path + org match).
func linkPeopleWikiAndOrg(store *wiki.Store, out string) string {
	body, cards := stripAndParsePeopleJSON(out)
	if len(cards) == 0 {
		return body
	}
	var wikiLinks []wiki.GroupwarePersonLink
	if store != nil {
		res, err := store.EnrichGroupwarePeople(cards)
		if err == nil {
			wikiLinks = res.Links
		}
	}
	byName := map[string]wiki.GroupwarePersonLink{}
	for _, l := range wikiLinks {
		key := contactdomain.NormalizePersonName(l.Name)
		if key != "" {
			byName[key] = l
		}
	}
	tree, _ := org.Load()
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n\n연계:\n")
	for _, card := range cards {
		name := strings.TrimSpace(card.Name)
		if name == "" {
			name = "(이름없음)"
		}
		key := contactdomain.NormalizePersonName(card.Name)
		wikiLine := "위키: (스킵)"
		if store == nil {
			wikiLine = "위키: (미연결 — wiki store 없음)"
		} else if l, ok := byName[key]; ok && l.Path != "" {
			wikiLine = "위키: " + l.Path + " (" + wikiActionKo(l.Action) + ")"
		} else if l, ok := byName[key]; ok {
			wikiLine = "위키: (" + wikiActionKo(l.Action) + ")"
		}
		orgLine := "조직도: " + matchOrgChain(tree, card.Name)
		b.WriteString("- ")
		b.WriteString(name)
		b.WriteString("\n  - ")
		b.WriteString(wikiLine)
		b.WriteString("\n  - ")
		b.WriteString(orgLine)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func wikiActionKo(action string) string {
	switch action {
	case "created":
		return "생성"
	case "updated":
		return "갱신"
	case "unchanged":
		return "기존"
	default:
		return action
	}
}

func stripAndParsePeopleJSON(out string) (body string, cards []wiki.GroupwarePersonCard) {
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, peopleJSONMarker) {
			payload := strings.TrimPrefix(line, peopleJSONMarker)
			_ = json.Unmarshal([]byte(payload), &cards)
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), cards
}

// matchOrgChain returns "탑솔라 > … > 팀 · 전무" or "조직도 미매칭".
// org.json is never modified — read-only name match.
func matchOrgChain(tree org.OrgTree, name string) string {
	key := contactdomain.NormalizePersonName(name)
	if len([]rune(key)) < 2 || len(tree.Nodes) == 0 {
		return "조직도 미매칭"
	}
	byID := make(map[string]org.OrgNode, len(tree.Nodes))
	for _, n := range tree.Nodes {
		byID[n.ID] = n
	}
	var hits []string
	seen := map[string]bool{}
	for _, n := range tree.Nodes {
		for _, m := range n.Members {
			if contactdomain.NormalizePersonName(m.Name) != key {
				continue
			}
			path := orgDepartmentPath(byID, n)
			role := orgMemberRole(m)
			line := path
			if role != "" {
				line = path + " · " + role
			}
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			hits = append(hits, line)
		}
	}
	if len(hits) == 0 {
		return "조직도 미매칭"
	}
	return strings.Join(hits, "; ")
}

func orgDepartmentPath(byID map[string]org.OrgNode, node org.OrgNode) string {
	parts := []string{node.Name}
	seen := map[string]bool{node.ID: true}
	current := node
	for i := 0; i < len(byID)+1; i++ {
		parent, ok := byID[current.ParentID]
		if !ok || seen[parent.ID] {
			break
		}
		parts = append([]string{parent.Name}, parts...)
		seen[parent.ID] = true
		current = parent
	}
	return strings.Join(parts, " > ")
}

func orgMemberRole(m org.Member) string {
	var parts []string
	if r := strings.TrimSpace(m.Rank); r != "" {
		parts = append(parts, r)
	}
	if p := strings.TrimSpace(m.Position); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, " · ")
}

func normalizeFolder(raw, action, area string) (string, error) {
	switch area {
	case "sales", "stock", "po", "receive", "ship":
		f := strings.ToLower(strings.TrimSpace(raw))
		switch f {
		case "", "ytd", "올해", "연초", "올해누적":
			if area == "receive" || area == "ship" {
				return "month", nil
			}
			return "ytd", nil
		case "month", "이번달", "당월", "월":
			return "month", nil
		case "today", "오늘", "당일":
			return "today", nil
		case "year", "연간", "당해":
			return "year", nil
		case "last_year", "lastyear", "작년", "전년":
			return "last_year", nil
		default:
			return "", fmt.Errorf("unknown period folder %q (ytd|month|today|year|last_year)", raw)
		}
	case "price", "people":
		return "", nil
	}
	if area != "approval" {
		return "", nil
	}
	f := strings.ToLower(strings.TrimSpace(raw))
	switch f {
	case "":
		if action == "list" {
			return "all", nil
		}
		return "pending", nil
	case "pending", "미결", "미결문서", "미결함":
		return "pending", nil
	case "done", "기결", "기결문서", "완료", "종결":
		return "done", nil
	case "cc", "수신참조", "수신참조문서", "참조", "수신":
		return "cc", nil
	case "total", "전체결재", "전체결재문서", "전체문서", "결재문서전체", "전체":
		return "total", nil
	case "all", "전체함", "순회":
		return "all", nil
	default:
		return "", fmt.Errorf("unknown folder %q (pending|done|cc|total|all — 미결|기결|수신참조|전체결재문서|순회)", raw)
	}
}
