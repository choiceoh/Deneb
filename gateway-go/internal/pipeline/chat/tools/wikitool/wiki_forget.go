// wiki_forget.go implements the standalone `wiki_forget` tool: a HARD delete of
// a wiki page for privacy or correctness ("이 사실 잊어줘").
//
// Deliberately a SEPARATE tool, not a `wiki` sub-action. The autonomous
// background presets (wiki-scout / noti-digest / wiki-research) allow `wiki` by
// name for bounded writes while processing untrusted web/notification content;
// a destructive delete must not be reachable from a prompt-injectable turn.
// Those presets list `wiki`, not `wiki_forget`, so this stays out of them, and
// the untrusted-origin gate treats `wiki_forget` as irreversible.
package wikitool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// SessionCacheFlushFn drops the current session's prompt snapshots (context
// files, recall, tier-1) so a hard-deleted page cannot re-surface in the same
// session's prompt from a frozen snapshot. Injected from the chat layer (the
// flush targets live in the chat package); nil is a no-op.
type SessionCacheFlushFn func(sessionKey string)

// ToolWikiForget returns the wiki_forget tool. flush is invoked after a
// successful delete to clear the current session's prompt snapshots.
func ToolWikiForget(d *tooldeps.WikiDeps, flush SessionCacheFlushFn) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if d.Store == nil {
			return "위키가 비활성 상태입니다. DENEB_WIKI_ENABLED=true 로 활성화하세요.", nil
		}

		path := strings.TrimSpace(p.Path)
		if path == "" {
			return "path에 잊을 페이지 경로를 지정하세요 (예: 인물/홍길동.md). 먼저 wiki search로 정확한 경로를 확인하세요.", nil
		}
		if strings.TrimSpace(p.Reason) == "" {
			return "reason에 잊는 사유를 한 줄로 적으세요 — 감사 로그에 남깁니다 (오정보·프라이버시 등).", nil
		}
		// Accept a namespaced "w:" ref interchangeably with a bare path.
		path = strings.TrimPrefix(path, toolport.RefWiki)
		// Escape guard: reject a path that could resolve outside the wiki root.
		if err := wiki.ValidateExternalPath(path); err != nil {
			return fmt.Sprintf("잘못된 페이지 경로입니다 (위키 루트 밖 접근 불가): %s", path), nil //nolint:nilerr // tool surface: guidance to the model, not an error
		}
		res, err := d.Store.Forget(path, p.Reason)
		if err != nil {
			return fmt.Sprintf("잊기 실패: %v", err), nil //nolint:nilerr // tool surface: guidance to the model, not an error
		}

		// Flush this session's prompt snapshots so the removed page cannot be
		// re-injected from a frozen recall/tier-1/context snapshot this session.
		if flush != nil {
			if sk := toolport.SessionKeyFromContext(ctx); sk != "" {
				flush(sk)
			}
		}

		title := res.Title
		if title == "" {
			title = res.Path
		}
		return fmt.Sprintf("위키에서 삭제(잊음): %s (%s). 감사 로그에 사유를 기록했습니다. 이 사실은 이제 검색·회상되지 않습니다.", title, res.Path), nil
	}
}
