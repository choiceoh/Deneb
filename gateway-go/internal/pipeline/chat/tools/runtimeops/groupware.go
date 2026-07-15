package runtimeops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolGroupware reads Amaranth10 전자결재 / 게시판 / 매출·재고·발주·입고·출고·단가·사원 via srv4 headless login.
// Approval folders: pending(미결) · done(기결) · cc(수신참조) · total(전체결재문서) · all(순회).
// Read-only: never approve, post, or delete. Unconfigured → calm off message.
func ToolGroupware() toolport.ToolFunc {
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
		return out, nil
	}
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
