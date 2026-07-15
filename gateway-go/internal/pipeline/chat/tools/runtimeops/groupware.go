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

// ToolGroupware reads Amaranth10 전자결재 / 게시판 via srv4 headless login.
// Approval folders: pending(미결) · done(기결) · cc(수신참조) · total(전체결재문서) · all(순회).
// Read-only: never approve, post, or delete. Unconfigured → calm off message.
func ToolGroupware() toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Area   string `json:"area"`
			Folder string `json:"folder"`
			Query  string `json:"query"`
			Limit  int    `json:"limit"`
		}
		if err := jsonutil.UnmarshalInto("groupware params", input, &p); err != nil {
			return "", err
		}

		cfg, ok := groupware.FromEnv()
		action := strings.ToLower(strings.TrimSpace(p.Action))
		switch action {
		case "", "status":
			return groupware.StatusLine(cfg, ok), nil
		case "list", "read":
			// continue
		default:
			return "", fmt.Errorf("unknown groupware action %q (status|list|read)", p.Action)
		}

		if !ok {
			return groupware.StatusLine(cfg, false), nil
		}

		area := strings.ToLower(strings.TrimSpace(p.Area))
		switch area {
		case "approval", "board":
		case "전자결재", "결재":
			area = "approval"
		case "게시판", "공지", "공지사항":
			area = "board"
		case "":
			return "area가 필요합니다 — approval(전자결재) 또는 board(게시판).", nil
		default:
			return "", fmt.Errorf("unknown groupware area %q (approval|board)", p.Area)
		}

		folder, ferr := normalizeFolder(p.Folder, action, area)
		if ferr != nil {
			return ferr.Error(), nil
		}

		if action == "read" && strings.TrimSpace(p.Query) == "" {
			return "read에는 query(제목·키워드)가 필요합니다. 목록은 action=list 로 먼저 확인하라.", nil
		}

		out, err := groupware.Run(ctx, cfg, groupware.Request{
			Area:   groupware.Area(area),
			Action: groupware.Action(action),
			Folder: folder,
			Query:  strings.TrimSpace(p.Query),
			Limit:  p.Limit,
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
