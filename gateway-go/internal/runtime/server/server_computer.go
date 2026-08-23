// server_computer.go — delivers computer-use commands (the `computer` chat
// tool: screenshot / click / type / key on the host OS) to the connected
// Andromeda desktop over the events push channel and waits for its execution
// report. Unlike the fire-and-forget workstation (screen arrangement) loop,
// every computer command has a result round trip: the desktop executes through
// its Tauri shell and reports back over miniapp.computer.result (correlated by
// the frame's Ref id), carrying a PNG for screenshots. Tool results are text,
// so a screenshot is read by the vision chain (main model → dedicated vision
// model, OCR as last resort) into a UI description with image-space
// coordinates the model then uses for its next click — the desktop maps those
// coordinates back onto the real screen.
package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/hostops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	handlerchatminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat/miniapp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// computerResultWait bounds how long a dispatch waits for the desktop's
// execution report. A screenshot (capture + PNG encode + upload) lands in a
// second or two on a healthy desktop; the cap only bites on a hidden/asleep
// app or a build that predates the executor — both degrade to a clear error
// instead of hanging the turn. The wait action extends it by its own delay.
const computerResultWait = 30 * time.Second

// computerScreenDescribeTokens bounds the vision description of one screen —
// a UI inventory with coordinates is longer than a photo caption.
const computerScreenDescribeTokens = 1500

// computerScreenPrompt is the vision instruction for a desktop screenshot: a
// structured UI read with image-space coordinates, because the model's next
// action (click x,y) is only as good as this description's positions.
const computerScreenPrompt = "너는 컴퓨터 화면 분석기다. 데스크톱 스크린샷을 보고 한국어로, 다른 에이전트가 마우스·키보드로 조작할 수 있을 만큼 정확하게 서술한다. " +
	"출력 형식: (1) 한 줄 요약 — 어떤 앱/창이 떠 있고 무엇을 하는 화면인지. (2) 주요 UI 요소 목록 — 버튼·메뉴·탭·입력창·링크·아이콘·체크박스 등 조작 가능한 요소마다 '이름/표시 텍스트 — (x,y)' 형태로 중심점 좌표를 적는다(좌표는 이 이미지의 픽셀 기준, 좌상단 원점). " +
	"(3) 화면의 글자(제목·본문·표·숫자·에러 메시지·다이얼로그)는 원문 그대로 옮긴다. (4) 포커스/선택/활성 상태, 스크롤 위치, 열린 팝업·알림이 있으면 명시한다. 추측·서론 없이 사실만 출력한다."

// computerResult is the desktop's execution report for one dispatched command.
type computerResult struct {
	ID     string
	OK     bool
	Error  string
	Image  []byte // PNG; screenshot only
	Width  int
	Height int
	Text   string // small textual payload (cursor position, notes)
}

// computerWaiter tracks one in-flight dispatch with the same success-biased
// aggregation as phone actions: the frame fans out to EVERY connected desktop
// (normally one; a second is a verification harness or a stale window), any
// ok=true resolves immediately, an ok=false resolves only once every
// subscriber has failed — so a desktop with computer use switched off cannot
// mask the one that actually executed.
type computerWaiter struct {
	ch       chan computerResult
	expected int
	fails    int
}

// computerAwaiter correlates dispatched computer commands with desktop
// reports. One independent mutex, never held while blocking; waiter channels
// are buffered(1) and the map entry is removed before the sole send, so
// resolve never blocks and a late report after the wait window is discarded.
type computerAwaiter struct {
	mu sync.Mutex
	m  map[string]*computerWaiter
}

func newComputerAwaiter() *computerAwaiter {
	return &computerAwaiter{m: make(map[string]*computerWaiter)}
}

// register creates the waiter for a dispatch fanned out to `expected`
// desktops. The caller MUST `defer drop(id)`.
func (a *computerAwaiter) register(id string, expected int) <-chan computerResult {
	if expected < 1 {
		expected = 1
	}
	w := &computerWaiter{ch: make(chan computerResult, 1), expected: expected}
	a.mu.Lock()
	a.m[id] = w
	a.mu.Unlock()
	return w.ch
}

// drop removes a waiter without resolving it (timeout / turn canceled). Idempotent.
func (a *computerAwaiter) drop(id string) {
	a.mu.Lock()
	delete(a.m, id)
	a.mu.Unlock()
}

// resolve feeds one report into the matching waiter; false when no waiter
// holds the id (late report, or gateway restarted between dispatch and report).
func (a *computerAwaiter) resolve(res computerResult) bool {
	a.mu.Lock()
	w, ok := a.m[res.ID]
	if !ok {
		a.mu.Unlock()
		return false
	}
	if !res.OK {
		w.fails++
		if w.fails < w.expected {
			a.mu.Unlock()
			return true // absorbed; another desktop may still succeed
		}
	}
	delete(a.m, res.ID)
	a.mu.Unlock()
	w.ch <- res // buffered(1), sole sender after map removal — never blocks
	return true
}

// newComputerDispatchID returns a random correlation id (same reasoning as
// newPhoneActionID: unguessable, never wraps into a live waiter).
func newComputerDispatchID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("cu-%d", time.Now().UnixNano())
	}
	return "cu-" + hex.EncodeToString(b)
}

// resolveComputerResult is the miniapp.computer.result entry point (wired in
// method_registry_late.go). Returns false when nothing was waiting.
func (s *Server) resolveComputerResult(res handlerchatminiapp.ComputerResult) bool {
	if s.computerActions == nil {
		return false
	}
	ok := s.computerActions.resolve(computerResult{
		ID: res.ID, OK: res.OK, Error: res.Error,
		Image: res.Image, Width: res.Width, Height: res.Height, Text: res.Text,
	})
	if !ok {
		s.logger.Info("computer result arrived with no waiter (late or restarted)", "id", res.ID, "ok", res.OK)
	}
	return ok
}

// dispatchComputerCommand is wired into the computer tool as its
// ComputerCommandFunc (chat_pipeline.go). It publishes the frame to connected
// desktops, waits for the execution report, and turns it into the tool text.
func (s *Server) dispatchComputerCommand(ctx context.Context, action string, args map[string]string) (string, error) {
	if s.pushHub == nil || s.computerActions == nil {
		return "", fmt.Errorf("연결된 데스크톱 워크스테이션이 없어 컴퓨터를 조종할 수 없습니다")
	}
	fanout := s.pushHub.DesktopSubscriberCount()
	if fanout == 0 {
		return "", fmt.Errorf("연결된 데스크톱 워크스테이션(Andromeda)이 없습니다 — 데스크톱 앱이 켜져 있어야 컴퓨터 조종이 가능합니다")
	}
	data := make(map[string]string, len(args)+1)
	for k, v := range args {
		data[k] = v
	}
	data["action"] = action

	id := newComputerDispatchID()
	result := s.computerActions.register(id, fanout)
	defer s.computerActions.drop(id)

	label := hostops.DescribeComputerCommand(action, args)
	s.pushHub.Publish(nativepush.Event{
		Kind:  nativepush.PushKindComputer,
		Title: "컴퓨터 조종",
		Body:  label,
		Ref:   id,
		Data:  data,
	})
	s.recordUsageLedger("computer_usage.json", action)
	s.logger.Info("computer command dispatched", "action", action, "id", id, "desktops", fanout)

	wait := computerResultWait
	if action == "wait" {
		if ms, err := strconv.Atoi(args["wait_ms"]); err == nil && ms > 0 {
			wait += time.Duration(ms) * time.Millisecond
		}
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()

	var res computerResult
	select {
	case res = <-result:
	case <-ctx.Done():
		return "", fmt.Errorf("컴퓨터 조작(%s)을 기다리는 중 턴이 종료되었습니다", label)
	case <-timer.C:
		// Boundary drain: a report that landed as the timer fired must not be
		// misreported as a timeout.
		select {
		case res = <-result:
		default:
			s.logger.Warn("computer command not reported in time", "action", action, "id", id, "wait", wait)
			return "", fmt.Errorf("데스크톱이 %s 안에 응답하지 않았습니다(%s) — Andromeda 창이 백그라운드이거나 설정에서 '컴퓨터 조종 허용'이 꺼져 있을 수 있습니다", wait.Round(time.Second), label)
		}
	}
	if !res.OK {
		msg := strings.TrimSpace(res.Error)
		if msg == "" {
			msg = "데스크톱이 실행하지 못했습니다"
		}
		// User-observable (the agent relays it), recoverable → Warn.
		s.logger.Warn("computer command failed on desktop", "action", action, "id", id, "detail", msg)
		return "", fmt.Errorf("컴퓨터 조작 실패(%s): %s", label, msg)
	}
	s.logger.Info("computer command confirmed", "action", action, "id", id, "image", len(res.Image) > 0)
	if len(res.Image) > 0 {
		return s.describeComputerScreen(ctx, action, args, res), nil
	}
	out := "컴퓨터 조작 완료: " + label
	if t := strings.TrimSpace(res.Text); t != "" {
		out += "\n" + t
	}
	return out, nil
}

// describeComputerScreen turns the desktop's PNG into the text the model acts
// on, keeping the raw capture on disk for the operator (cache/computer/last.png).
func (s *Server) describeComputerScreen(ctx context.Context, action string, args map[string]string, res computerResult) string {
	s.saveComputerScreenshot(res.Image)
	size := fmt.Sprintf("%d×%d", res.Width, res.Height)
	userText := fmt.Sprintf("이미지 크기: %d×%d 픽셀. 모든 좌표는 이 이미지의 픽셀 좌표(좌상단 원점)로, 요소의 중심점을 추정해 (x,y)로 적어라.", res.Width, res.Height)
	desc := pilot.DescribeImage(ctx, computerScreenPrompt, userText, "image/png", base64.StdEncoding.EncodeToString(res.Image), computerScreenDescribeTokens)
	head := "화면 스크린샷 " + size + " (좌표계: 이 이미지의 픽셀 — 다음 click/move/drag/scroll 좌표는 이 기준으로)"
	if action == "screenshot" && args["width"] != "" {
		head += " · 확대 영역 (" + args["x"] + "," + args["y"] + ") " + args["width"] + "×" + args["height"] + " — 좌표는 이 확대 이미지 기준"
	}
	if strings.TrimSpace(desc) != "" {
		return head + "\n" + strings.TrimSpace(desc)
	}
	// No vision model (or it failed): OCR glyphs are still better than nothing,
	// but say so — the model must not mistake text-only output for a full read.
	if text, err := toolbind.OCRImage(ctx, res.Image); err == nil && strings.TrimSpace(text) != "" {
		return head + "\n(비전 모델 응답 없음 — OCR 텍스트만, 좌표 없음)\n" + strings.TrimSpace(text)
	}
	s.logger.Error("computer screenshot could not be described (vision and OCR both empty)", "size", size)
	return head + "\n(화면 서술 실패: 비전 모델과 OCR 모두 응답이 없습니다 — 잠시 후 screenshot을 다시 시도하거나 사용자에게 화면 상태를 물어보세요)"
}

// saveComputerScreenshot keeps the latest capture for operator inspection.
// Best-effort: a write failure must not fail the tool call.
func (s *Server) saveComputerScreenshot(png []byte) {
	if s.denebDir == "" || len(png) == 0 {
		return
	}
	path := filepath.Join(s.denebDir, "cache", "computer", "last.png")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.logger.Warn("computer screenshot dir create failed", "path", path, "error", err)
		return
	}
	if err := atomicfile.WriteFile(path, png, nil); err != nil {
		s.logger.Warn("computer screenshot save failed", "path", path, "error", err)
	}
}
