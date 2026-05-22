// update_dispatch.go — /update slash command dispatcher.
//
// Lets the operator update Deneb from inside Telegram: pull the latest
// `main`, rebuild the production binary, and restart the gateway — no SSH,
// no terminal.
//
// Shape:
//
//	/update            → preview: which new commits are waiting on origin/main
//	/update 확인        → execute: git pull --ff-only → make gateway-prod → restart
//	/update confirm    → English alias for execute
//
// Korean alias: /업데이트 routes here too (see ParseSlashCommand).
//
// Restart works the same way scripts/deploy/deploy.sh does: a SIGUSR1 to our
// own PID makes the process exit with bootstrap.ExitCodeRestart (75), which
// the supervising wrapper (systemd unit or start-go-gateway.sh loop) turns
// into a relaunch of the freshly built dist/deneb-gateway binary.

package chat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// updateBuildTimeout bounds the whole pull + build step. A clean production
// rebuild on the DGX Spark finishes well under two minutes; five gives slack
// for a cold module cache without letting a wedged build hang forever.
const updateBuildTimeout = 5 * time.Minute

// updateFetchTimeout bounds the read-only preview path (branch/status/fetch).
const updateFetchTimeout = 30 * time.Second

// updatePreviewCommitCap caps how many pending commits the preview lists.
// Twenty short one-line entries stay comfortably inside Telegram's message
// limit while still giving a clear sense of "how big is this update".
const updatePreviewCommitCap = 20

// updateInFlight guards against a second "/update 확인" kicking off a parallel
// pull + build while the first is still running. Single operator, single
// machine — a process-wide flag is sufficient.
var updateInFlight atomic.Bool

// confirmIntent classifies the argument of a confirm-gated lifecycle command
// (/update, /restart): a bare command shows guidance, a confirm word runs it.
type confirmIntent int

const (
	confirmIntentUnknown confirmIntent = iota
	confirmIntentBare                  // no argument — show guidance/preview
	confirmIntentYes                   // confirm word — proceed
)

// normalizeConfirmArg maps a raw command argument to a confirmIntent. Accepts
// both Korean and English confirm words. Shared by /update and /restart.
func normalizeConfirmArg(raw string) confirmIntent {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return confirmIntentBare
	}
	switch s {
	case "확인", "실행", "진행", "응", "네", "ㅇㅇ", "confirm", "yes", "y", "ok", "go":
		return confirmIntentYes
	default:
		return confirmIntentUnknown
	}
}

// handleUpdateCommand parses the /update argument and runs either the
// read-only preview or the pull + build + restart. Runs in a goroutine
// spawned by the dispatcher so the slow git/make I/O never blocks the RPC
// ack for the slash command itself.
func (h *Handler) handleUpdateCommand(delivery *DeliveryContext, rawArgs string) {
	defer func() {
		if r := recover(); r != nil && h.logger != nil {
			h.logger.Error("panic in /update command handler", "panic", r)
		}
	}()

	switch normalizeConfirmArg(rawArgs) {
	case confirmIntentBare:
		h.updatePreview(delivery)
	case confirmIntentYes:
		h.updateExecute(delivery)
	default:
		h.deliverSlashResponse(delivery, strings.Join([]string{
			"사용법:",
			"  /update — 받을 수 있는 업데이트가 있는지 확인",
			"  /update 확인 — 업데이트 실행 (게이트웨이 재시작 포함)",
		}, "\n"))
	}
}

// ── preview ─────────────────────────────────────────────────────────────────

// updatePreview reports how far behind origin/main the running checkout is,
// without changing anything. Read-only and safe to run any time.
func (h *Handler) updatePreview(delivery *DeliveryContext) {
	// Detached background task: the slash RPC has already returned, so there
	// is no request context to inherit. Bounded timeout keeps it from hanging.
	ctx, cancel := context.WithTimeout(context.Background(), updateFetchTimeout)
	defer cancel()

	root, err := updateRepoRoot(ctx)
	if err != nil {
		h.deliverSlashResponse(delivery, err.Error())
		return
	}
	if msg, ok := h.updatePrechecks(ctx, root); !ok {
		h.deliverSlashResponse(delivery, msg)
		return
	}

	commits, err := updatePendingCommits(ctx, root)
	if err != nil {
		h.logger.Warn("update preview: pending check failed", "error", err)
		h.deliverSlashResponse(delivery, "원격 저장소 확인에 실패했습니다. 네트워크 상태를 확인해 주세요.")
		return
	}

	versionNote := updateVersionNote(h.updateGatewayVersion())
	if strings.TrimSpace(commits) == "" {
		h.deliverSlashResponse(delivery, fmt.Sprintf("✅ 이미 최신 버전입니다%s.", versionNote))
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "📦 받을 수 있는 업데이트가 %d개 있습니다%s.\n\n", countLines(commits), versionNote)
	b.WriteString("```\n")
	b.WriteString(commits)
	if !strings.HasSuffix(commits, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n")
	b.WriteString("지금 업데이트하려면 `/update 확인`을 입력하세요. 업데이트하면 게이트웨이가 잠시 재시작됩니다.")
	h.deliverSlashResponse(delivery, b.String())
}

// ── execute ─────────────────────────────────────────────────────────────────

// updateExecute pulls origin/main, rebuilds the production binary, and
// restarts the gateway. Progress is reported to the user step by step.
func (h *Handler) updateExecute(delivery *DeliveryContext) {
	if !updateInFlight.CompareAndSwap(false, true) {
		h.deliverSlashResponse(delivery, "이미 업데이트가 진행 중입니다. 잠시만 기다려 주세요.")
		return
	}
	defer updateInFlight.Store(false)

	// Detached background task with a generous bound — the build dominates.
	ctx, cancel := context.WithTimeout(context.Background(), updateBuildTimeout)
	defer cancel()

	root, err := updateRepoRoot(ctx)
	if err != nil {
		h.deliverSlashResponse(delivery, err.Error())
		return
	}
	if msg, ok := h.updatePrechecks(ctx, root); !ok {
		h.deliverSlashResponse(delivery, msg)
		return
	}

	commits, err := updatePendingCommits(ctx, root)
	if err != nil {
		h.logger.Warn("update: pending check failed", "error", err)
		h.deliverSlashResponse(delivery, "원격 저장소 확인에 실패했습니다. 네트워크 상태를 확인해 주세요.")
		return
	}
	if strings.TrimSpace(commits) == "" {
		h.deliverSlashResponse(delivery, "✅ 이미 최신 버전입니다. 업데이트할 내용이 없습니다.")
		return
	}

	h.deliverSlashResponse(delivery, fmt.Sprintf(
		"🔄 업데이트를 시작합니다 (커밋 %d개). 최신 코드를 받고 빌드하는 중입니다 — 1~2분 정도 걸립니다...",
		countLines(commits)))

	// Step 1 — fast-forward pull. --ff-only refuses anything but a clean
	// fast-forward, matching scripts/deploy/deploy.sh; the worktree was
	// already verified clean by the prechecks above.
	if out, err := runUpdateGit(ctx, root, "pull", "--ff-only", "origin", "main"); err != nil {
		h.logger.Error("update: git pull failed", "error", err, "output", out)
		h.deliverSlashResponse(delivery, fmt.Sprintf(
			"❌ 코드 받기에 실패했습니다. 게이트웨이는 그대로 유지됩니다.\n\n```\n%s\n```",
			truncateUpdateOutput(out)))
		return
	}

	// Step 2 — production build. `make gateway-prod` rebuilds
	// dist/deneb-gateway, the binary the supervising wrapper relaunches after
	// the restart signal. (`make go` only compile-checks and would leave the
	// old binary in place — the restart would then run stale code.)
	if out, err := runUpdateMake(ctx, root); err != nil {
		h.logger.Error("update: build failed", "error", err, "output", out)
		h.deliverSlashResponse(delivery, fmt.Sprintf(
			"❌ 빌드에 실패했습니다. 코드는 받았지만 재시작하지 않습니다.\n\n```\n%s\n```",
			truncateUpdateOutput(out)))
		return
	}

	h.logger.Info("update: build complete, restarting gateway")
	h.deliverSlashResponse(delivery, "✅ 빌드 완료. 게이트웨이를 재시작합니다 — 몇 초 후 다시 사용할 수 있습니다.")

	// Step 3 — restart. SIGUSR1 → bootstrap.ExitCodeRestart (75) → the
	// supervising wrapper relaunches the freshly built binary.
	if err := signalGatewayRestart(); err != nil {
		h.logger.Error("update: restart signal failed", "error", err)
		h.deliverSlashResponse(delivery, "⚠️ 빌드는 끝났지만 재시작 신호 전송에 실패했습니다. 게이트웨이를 수동으로 재시작해 주세요.")
	}
}

// ── git / build helpers ─────────────────────────────────────────────────────

// updateRepoRoot resolves the git repository root from the gateway's working
// directory. `make` needs the repo root (where the Makefile lives); git would
// search upward on its own, but make does not.
func updateRepoRoot(ctx context.Context) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("작업 디렉토리를 확인할 수 없습니다: %w", err)
	}
	root, err := runUpdateGit(ctx, wd, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("git 저장소를 찾을 수 없어 업데이트할 수 없습니다")
	}
	return root, nil
}

// updatePrechecks verifies the worktree is on main and clean. Returns a
// Korean explanation + false when the update must not proceed.
func (h *Handler) updatePrechecks(ctx context.Context, root string) (string, bool) {
	branch, err := runUpdateGit(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "현재 브랜치를 확인하지 못했습니다.", false
	}
	if branch != "main" {
		return fmt.Sprintf("업데이트는 main 브랜치에서만 할 수 있습니다 (현재: %s).", branch), false
	}
	status, err := runUpdateGit(ctx, root, "status", "--porcelain")
	if err != nil {
		return "작업 디렉토리 상태를 확인하지 못했습니다.", false
	}
	if status != "" {
		return "커밋되지 않은 변경이 있어 업데이트할 수 없습니다. 먼저 변경 사항을 정리해 주세요.", false
	}
	return "", true
}

// updatePendingCommits fetches origin/main and returns the one-line log of
// the commits HEAD is behind by. Empty string means already up to date.
func updatePendingCommits(ctx context.Context, root string) (string, error) {
	if out, err := runUpdateGit(ctx, root, "fetch", "origin", "main"); err != nil {
		return "", fmt.Errorf("git fetch failed: %s", out)
	}
	commits, err := runUpdateGit(ctx, root, "log", "--oneline", "--no-decorate",
		fmt.Sprintf("-n%d", updatePreviewCommitCap), "HEAD..origin/main")
	if err != nil {
		return "", fmt.Errorf("git log failed: %s", commits)
	}
	return commits, nil
}

// runUpdateGit runs a git subcommand in repoDir and returns the trimmed
// combined output. On error the output is returned too so the caller can
// surface git's own message to the operator.
func runUpdateGit(ctx context.Context, repoDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runUpdateMake runs `make gateway-prod` at the repo root to rebuild the
// production binary.
func runUpdateMake(ctx context.Context, root string) (string, error) {
	cmd := exec.CommandContext(ctx, "make", "gateway-prod")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// signalGatewayRestart sends SIGUSR1 to this process. bootstrap.RunWithSignals
// catches it, shuts the gateway down gracefully, and exits with
// ExitCodeRestart (75) so the supervising wrapper relaunches it. Shared by the
// /update execute path and the /restart command.
func signalGatewayRestart() error {
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGUSR1)
}

// LatestAppliedRef identifies the deployed code for the /status dashboard. It
// reads the running checkout's HEAD and returns the PR reference when the
// commit is a GitHub squash-merge ("...(#1643)"), otherwise the short SHA.
// Returns "" when git is unavailable so the caller can fall back to the build
// version tag.
func LatestAppliedRef() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root, err := updateRepoRoot(ctx)
	if err != nil {
		return ""
	}
	out, err := runUpdateGit(ctx, root, "log", "-1", "--format=%h %s")
	if err != nil {
		return ""
	}
	shortSHA, subject, found := strings.Cut(out, " ")
	if !found {
		return strings.TrimSpace(out)
	}
	if pr := parsePRNumber(subject); pr != "" {
		return fmt.Sprintf("PR #%s (%s)", pr, shortSHA)
	}
	return shortSHA
}

// parsePRNumber extracts the trailing "(#1234)" reference GitHub appends to
// squash-merge commit subjects. Returns "" when the subject has none.
func parsePRNumber(subject string) string {
	subject = strings.TrimSpace(subject)
	if !strings.HasSuffix(subject, ")") {
		return ""
	}
	open := strings.LastIndexByte(subject, '(')
	if open < 0 {
		return ""
	}
	inner := subject[open+1 : len(subject)-1]
	if !strings.HasPrefix(inner, "#") || len(inner) < 2 {
		return ""
	}
	for _, r := range inner[1:] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return inner[1:]
}

// ── misc helpers ────────────────────────────────────────────────────────────

// updateGatewayVersion returns the running gateway's build version, or "" when
// the server has not wired status deps.
func (h *Handler) updateGatewayVersion() string {
	fn := h.StatusDeps()
	if fn == nil {
		return ""
	}
	return strings.TrimSpace(fn("").Version)
}

// updateVersionNote renders a " (현재 vX.Y.Z)" suffix for the preview. Empty
// for untagged ("dev") or unknown builds so the message stays clean.
func updateVersionNote(version string) string {
	if version == "" || version == "dev" {
		return ""
	}
	return " (현재 v" + version + ")"
}

// truncateUpdateOutput caps git/make failure output so an error fits inside a
// Telegram message. The tail is kept because build and pull failures put the
// actual error at the end.
func truncateUpdateOutput(s string) string {
	const maxRunes = 1000
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return "…(앞부분 생략)…\n" + string(r[len(r)-maxRunes:])
}
