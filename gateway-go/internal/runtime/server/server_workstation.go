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

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
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
	s.pushHub.Publish(proactive.Event{
		Kind:  proactive.PushKindWorkspace,
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
	if data, err := json.MarshalIndent(usage, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, data, 0o644)
	}
}
