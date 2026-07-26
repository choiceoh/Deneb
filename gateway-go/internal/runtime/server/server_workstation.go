// server_workstation.go — delivers workstation (desktop workspace) commands to
// connected Andromeda clients over the events push channel.
//
// The chat tool `workstation` validates the verb (tools/runtimeops/
// workstation.go); this side owns the transport: gate on a connected DESKTOP
// subscriber (a mobile-only connection must not read as "screen arranged"),
// then publish the command in the frame's Data under Kind="workspace". The
// desktop re-validates through its command bus (andromeda/src/commands.ts) and
// shows a visible "화면 조정" nudge, so machine-driven rearrangement is never
// silent. Fire-and-forget by design — a screen arrangement is idempotent and
// low-stakes, unlike phone actions there is no execution-result round trip.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// dispatchWorkstationCommand is wired into the workstation tool as its
// WorkstationCommandFunc (chat_pipeline.go).
func (s *Server) dispatchWorkstationCommand(_ context.Context, action string, args map[string]string) error {
	if s.pushHub == nil {
		return fmt.Errorf("연결된 데스크톱 워크스테이션이 없어 화면을 조종할 수 없습니다")
	}
	if s.pushHub.DesktopSubscriberCount() == 0 {
		return fmt.Errorf("연결된 데스크톱 워크스테이션(Andromeda)이 없습니다 — 데스크톱 앱이 켜져 있어야 화면 조종이 가능합니다")
	}
	data := make(map[string]string, len(args)+1)
	for k, v := range args {
		data[k] = v
	}
	data["action"] = action
	s.pushHub.Publish(nativepush.Event{
		Kind:  nativepush.PushKindWorkspace,
		Title: "화면 조정",
		Body:  action,
		Data:  data,
	})
	s.recordWorkstationUsage(action)
	s.logger.Info("workstation command dispatched", "action", action, "desktops", s.pushHub.DesktopSubscriberCount())
	return nil
}

// recordWorkstationUsage keeps a tiny on-disk tally per action — the utility
// grounding for the workstation loop ("대대적 개선이 실제로 쓰이는가"를 2주 뒤
// 숫자로 답하기 위한 원장). Best-effort; dispatch rate is human-scale.
func (s *Server) recordWorkstationUsage(action string) {
	if s.denebDir == "" {
		return
	}
	s.workstationUsageMu.Lock()
	defer s.workstationUsageMu.Unlock()
	path := filepath.Join(s.denebDir, "cache", "workstation_usage.json")
	usage := struct {
		Total    int            `json:"total"`
		ByAction map[string]int `json:"byAction"`
		LastAt   string         `json:"lastAt"`
	}{ByAction: map[string]int{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &usage)
		if usage.ByAction == nil {
			usage.ByAction = map[string]int{}
		}
	}
	usage.Total++
	usage.ByAction[action]++
	usage.LastAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(usage, "", "  ")
	if err != nil {
		s.logger.Warn("workstation usage ledger marshal failed", "error", err)
		return
	}
	// Atomic write: a mid-write crash must not truncate the two-week ledger
	// (the next dispatch would silently reset the tally from zero).
	if err := atomicfile.WriteFile(path, data, nil); err != nil {
		s.logger.Warn("workstation usage ledger write failed", "path", path, "error", err)
	}
}

// workstationUsageHint turns the desktop's kept/dropped feedback tally into a
// short self-adjustment note for the workstation tool result. Only speaks when
// there is enough signal AND the keep-rate is poor — silence is the default so
// well-adopted actions stay noise-free. Reading the tiny tally per tool call
// beats caching: the file changes out-of-band (desktop reports) and a screen
// command is a low-frequency, high-latency operation anyway.
func (s *Server) workstationUsageHint(action string) string {
	const minSamples, poorKeepRate = 5, 0.5
	data, err := os.ReadFile(filepath.Join(s.denebDir, "cache", "workstation_feedback.json"))
	if err != nil {
		return ""
	}
	var tally struct {
		ByAction map[string]struct {
			Kept    int `json:"kept"`
			Dropped int `json:"dropped"`
		} `json:"byAction"`
	}
	if json.Unmarshal(data, &tally) != nil {
		return ""
	}
	entry := tally.ByAction[action]
	total := entry.Kept + entry.Dropped
	if total < minSamples {
		return ""
	}
	rate := float64(entry.Kept) / float64(total)
	if rate >= poorKeepRate {
		return ""
	}
	return fmt.Sprintf(
		"참고(효용 원장): 최근 '%s' 조종 %d회 중 %d회는 사용자가 2분 내에 화면을 닫았습니다(유지율 %.0f%%) — 이 액션은 더 아껴 쓰거나, 띄우기 전에 정말 필요한지 판단하세요.",
		action, total, entry.Dropped, rate*100,
	)
}
