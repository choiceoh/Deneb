// rollback_dispatch.go — /rollback slash command dispatcher.
//
// Shape:
//
//	/rollback                     → last 5 snapshots (Korean summary)
//	/rollback list [N]            → last N snapshots (default 5, clamped 1..20)
//	/rollback 목록 [N]            → Korean alias for list
//	/rollback diff <id>           → unified diff (truncated to Telegram limit)
//	/rollback 비교 <id>           → Korean alias for diff
//	/rollback restore <id>        → restore snapshot
//	/rollback 복원 <id>           → Korean alias for restore
//
// All responses are delivered through the channel-bound replyFunc as plain
// text. Code blocks use triple-backtick fences because Telegram treats
// code-block content literally (no MarkdownV2 escaping required inside),
// which is what we want for diffs and IDs.
//
// Session key is the delivery.SessionKey pattern already used by the
// /insights dispatcher — a per-chat session that matches the pkg/checkpoint
// sessionID on disk.

package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// rollbackDefaultListLimit is the default number of entries shown when the
// user types bare "/rollback" or "/rollback list" without an argument.
const rollbackDefaultListLimit = 5

// rollbackMaxListLimit caps "/rollback list N" to a sane Telegram-sized
// summary. More than ~20 entries becomes hard to scan on a phone.
const rollbackMaxListLimit = 20

// rollbackDiffBodyCap is the soft cap for rendered diff output. Telegram's
// hard limit per message is 4096; we reserve headroom for the header
// ("체크포인트 #N  /path") and the "... (총 N줄)" marker.
const rollbackDiffBodyCap = 3800

// handleRollbackCommand parses the subcommand + args and dispatches to the
// list/diff/restore path. Runs in a goroutine so long I/O doesn't block the
// RPC reply that acknowledges the slash command itself.
func (h *Handler) handleRollbackCommand(sessionKey string, delivery *DeliveryContext, rawArgs string) {
	logger := h.logger
	defer func() {
		if r := recover(); r != nil && logger != nil {
			logger.Error("panic in /rollback command handler", "panic", r, "sessionKey", sessionKey)
		}
	}()

	root := strings.TrimSpace(h.checkpointRoot)
	if root == "" {
		h.deliverSlashResponse(delivery, "체크포인트 기능이 비활성화되어 있습니다.")
		return
	}
	if sessionKey == "" {
		h.deliverSlashResponse(delivery, "세션 정보를 확인할 수 없어 롤백할 수 없습니다.")
		return
	}

	sub, arg := splitRollbackArgs(rawArgs)
	mgr := checkpoint.New(root, sessionKey)

	switch sub {
	case "", "list", "목록":
		h.rollbackList(delivery, mgr, arg)
	case "diff", "비교":
		h.rollbackDiff(delivery, mgr, arg)
	case "restore", "복원":
		h.rollbackRestore(delivery, mgr, arg)
	default:
		h.deliverSlashResponse(delivery, rollbackHelp())
	}
}

// splitRollbackArgs splits the raw argument line into (subcommand, rest).
// Whitespace-trimmed, empty -> ("", ""). Subcommand is lowercased to match
// both English and Korean aliases consistently.
func splitRollbackArgs(raw string) (sub, rest string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", ""
	}
	parts := strings.SplitN(s, " ", 2)
	sub = strings.ToLower(parts[0])
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}
	return sub, rest
}

// rollbackHelp returns a Korean usage summary delivered when the user passes
// an unrecognised subcommand.
func rollbackHelp() string {
	return strings.Join([]string{
		"사용법:",
		"  /rollback — 최근 체크포인트 5개",
		"  /rollback list [N] — 최근 N개 (최대 20)",
		"  /rollback diff <id> — 스냅샷과 현재 파일 차이",
		"  /rollback restore <id> — 스냅샷으로 되돌리기",
		"한국어 별칭: /rollback 목록 | 비교 | 복원",
	}, "\n")
}

// ── list ───────────────────────────────────────────────────────────────────

func (h *Handler) rollbackList(delivery *DeliveryContext, mgr *checkpoint.Manager, arg string) {
	limit := rollbackDefaultListLimit
	if trimmed := strings.TrimSpace(arg); trimmed != "" {
		if n, err := parseRollbackCount(trimmed); err == nil {
			limit = n
		}
	}

	snaps, err := mgr.List("", limit)
	if err != nil {
		h.logger.Error("rollback list failed", "error", err)
		h.deliverSlashResponse(delivery, "체크포인트 목록을 불러오지 못했습니다.")
		return
	}
	h.deliverSlashResponse(delivery, renderRollbackList(snaps, limit))
}

// renderRollbackList renders a list result for the user. Returns Korean
// text with a code-fenced table so IDs don't get eaten by MarkdownV2.
func renderRollbackList(snaps []*checkpoint.Snapshot, limit int) string {
	if len(snaps) == 0 {
		return "최근 체크포인트가 없습니다. 파일을 수정하면 자동으로 스냅샷이 저장돼요."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "최근 체크포인트 %d개 (최대 %d)\n", len(snaps), limit)
	b.WriteString("```\n")
	for _, s := range snaps {
		ts := s.TakenAt.In(dentime.Location()).Format("01-02 15:04")
		name := baseName(s.Path)
		reason := s.Reason
		if reason == "" {
			reason = "-"
		}
		tag := ""
		if s.Tombstone {
			tag = " (삭제)"
		}
		fmt.Fprintf(&b, "#%d %s %s %s%s\n  id=%s\n",
			s.Seq, ts, truncateForList(name, 28), truncateForList(reason, 18), tag, s.ID)
	}
	b.WriteString("```\n")
	b.WriteString("복원: `/rollback restore <id>` — 차이 보기: `/rollback diff <id>`")
	return b.String()
}

// parseRollbackCount parses and clamps the "/rollback list N" argument.
func parseRollbackCount(s string) (int, error) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "개")
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("count must be positive")
	}
	if n > rollbackMaxListLimit {
		n = rollbackMaxListLimit
	}
	return n, nil
}

// ── diff ───────────────────────────────────────────────────────────────────

func (h *Handler) rollbackDiff(delivery *DeliveryContext, mgr *checkpoint.Manager, arg string) {
	id := strings.TrimSpace(arg)
	if id == "" {
		h.deliverSlashResponse(delivery, "체크포인트 ID가 필요합니다. 예: `/rollback diff <id>` — 먼저 `/rollback`으로 ID를 확인해 주세요.")
		return
	}

	// Resolve snapshot metadata first so we can label the header.
	all, err := mgr.List("", 0)
	if err != nil {
		h.logger.Error("rollback diff: list failed", "error", err)
		h.deliverSlashResponse(delivery, "체크포인트 목록을 불러오지 못했습니다.")
		return
	}
	var target *checkpoint.Snapshot
	for _, s := range all {
		if s.ID == id {
			target = s
			break
		}
	}
	if target == nil {
		h.deliverSlashResponse(delivery, fmt.Sprintf("체크포인트 `%s`를 찾을 수 없어요. `/rollback`으로 ID를 다시 확인해 주세요.", id))
		return
	}

	diff, err := mgr.Diff(id)
	if err != nil {
		h.logger.Error("rollback diff failed", "id", id, "error", err)
		h.deliverSlashResponse(delivery, "체크포인트 차이를 생성하지 못했습니다.")
		return
	}
	h.deliverSlashResponse(delivery, renderRollbackDiff(target, diff))
}

// renderRollbackDiff wraps the diff in a code fence with a Korean header.
// Truncates to stay under Telegram's 4096-char cap — keeps the header and
// the first hunk, then appends "... (총 <N>줄 중 M줄 표시)".
func renderRollbackDiff(target *checkpoint.Snapshot, diff string) string {
	header := fmt.Sprintf("체크포인트 #%d 차이 — %s", target.Seq, baseName(target.Path))
	fullLines := countLines(diff)
	body := diff
	shownLines := fullLines
	if len(body) > rollbackDiffBodyCap {
		cut := rollbackDiffBodyCap
		if idx := strings.LastIndexByte(body[:cut], '\n'); idx > 0 {
			cut = idx
		}
		body = body[:cut]
		shownLines = countLines(body)
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString("```\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```")
	if shownLines < fullLines {
		fmt.Fprintf(&b, "\n… (총 %d줄 중 %d줄만 표시)", fullLines, shownLines)
	}
	return b.String()
}

// ── restore ────────────────────────────────────────────────────────────────

func (h *Handler) rollbackRestore(delivery *DeliveryContext, mgr *checkpoint.Manager, arg string) {
	id := strings.TrimSpace(arg)
	if id == "" {
		h.deliverSlashResponse(delivery, "체크포인트 ID가 필요합니다. 예: `/rollback restore <id>` — 먼저 `/rollback`으로 ID를 확인해 주세요.")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	restored, err := mgr.Restore(ctx, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.deliverSlashResponse(delivery, fmt.Sprintf("체크포인트 `%s`를 찾을 수 없어요. `/rollback`으로 ID를 다시 확인해 주세요.", id))
			return
		}
		h.logger.Error("rollback restore failed", "id", id, "error", err)
		h.deliverSlashResponse(delivery, "체크포인트 복원에 실패했습니다.")
		return
	}

	msg := fmt.Sprintf("복원 완료 — #%d %s\n경로: `%s`", restored.Seq, baseName(restored.Path), restored.Path)
	if restored.Tombstone {
		msg = fmt.Sprintf("복원 완료 — #%d 삭제된 상태로 되돌렸습니다.\n경로: `%s`", restored.Seq, restored.Path)
	}
	h.deliverSlashResponse(delivery, msg)
}

// ── utils ──────────────────────────────────────────────────────────────────

// baseName returns the file name component of an absolute path. We avoid
// importing path/filepath here for platform-neutral display ("/a/b.txt" and
// "C:\\a\\b.txt" both resolve to "b.txt"). Falls back to the full string
// when no separator exists.
func baseName(p string) string {
	if p == "" {
		return "-"
	}
	// Prefer the last '/' or '\\', whichever appears later.
	slash := strings.LastIndexByte(p, '/')
	bslash := strings.LastIndexByte(p, '\\')
	idx := slash
	if bslash > idx {
		idx = bslash
	}
	if idx < 0 || idx == len(p)-1 {
		return p
	}
	return p[idx+1:]
}

// truncateForList hard-trims s to n runes, appending "…" when cut.
func truncateForList(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return string(runes[:n-1]) + "…"
}

// countLines counts '\n' occurrences plus one for a trailing non-empty
// fragment. Matches how a user would count rendered diff lines.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}
