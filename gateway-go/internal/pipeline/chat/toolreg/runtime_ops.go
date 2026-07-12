package toolreg

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"

// RuntimeOpsToolSet contains the already-wired executors whose registration
// policy belongs to the runtime-operations group. SpilloverRead is optional.
type RuntimeOpsToolSet struct {
	Gateway       toolctx.ToolFunc
	Observe       toolctx.ToolFunc
	Fleet         toolctx.ToolFunc
	SpilloverRead toolctx.ToolFunc
}

// RegisterRuntimeOpsTools registers gateway diagnostics and host operations.
func RegisterRuntimeOpsTools(registry toolctx.ToolRegistrar, set RuntimeOpsToolSet) {
	registry.RegisterTool(toolctx.ToolDef{
		Name: "gateway",
		Description: "Gateway self-management: status (버전/PID/포트/업타임/세션 수를 한 번에 반환), config_get/config_set (dotted paths), update (git pull + rebuild + restart), restart. " +
			"Destructive actions (restart/update/config_set) require approval — the first call returns a needs_approval envelope; relay the Korean summary to the user verbatim, and after approval call the .confirmed variant with the same action_token. " +
			"토큰/비밀번호/API 키는 절대 config_set으로 건드리지 마라.",
		InputSchema: gatewayToolSchema(),
		Fn:          set.Gateway,
		Deferred:    true,
	})
	registry.RegisterTool(toolctx.ToolDef{
		Name:        "observe",
		Description: "Self-diagnosis: why a turn was slow/failed, tool-usage stats, improvement-loop health. Observe your OWN runtime via the in-process observation plane: action=turn (runId → a past run's tokens/tools/cache + its captured logs), action=logs (recent log ring; filter by runId/session/level/contains), action=behavior (cross-session tool usage / proactive funnel / background-job health over N days, plus the local vLLM engine's prefix-cache hit rate), action=effort (adaptive effort-router scorecard: routed-off vs kept-on, escalation rate, savings), action=proactive (proactive-card engagement: FTR / over-intervention rate by source), action=health (self-improvement machinery digest: loop liveness, skill-decision mix, dreamer backlog, no-op frontier, silent-failure counts — same data as the loopback /api/observatory, read mid-reasoning).",
		InputSchema: observeToolSchema(),
		Fn:          set.Observe,
		Deferred:    true,
	})

	// Fleet: manage the SparkFleet GPU control plane (the machine's own model
	// servers) — the chat twin of the native 플릿 tab / the /api/v1/fleet
	// passthrough. Deferred like gateway/observe: niche but powerful, loaded via
	// fetch_tools when the user actually asks about the fleet.
	registry.RegisterTool(toolctx.ToolDef{
		Name: "fleet",
		Description: "SparkFleet GPU 컨트롤 플레인 관리 — 이 머신의 GPU 모델 서버를 띄우고 점검한다. " +
			"action=status (노드 GPU/메모리·레시피 실행 상태·최근 실패 작업 한눈에) · recipes (모델 레시피 목록) · jobs (백그라운드 작업) · " +
			"launch/stop/restart (recipe 이름으로 모델 기동·중지·재시작 — 실제 동작) · cancel (jobId로 작업 취소) · diagnose (실행 중 레시피 컨테이너 크래시 진단). " +
			"\"플릿 괜찮아?\" · \"qwen36 재시작해줘\" · \"왜 죽었어?\" 같은 요청에 사용.",
		InputSchema: fleetToolSchema(),
		Fn:          set.Fleet,
		Deferred:    true,
	})

	// Spillover: read full content of a previously spilled large tool result.
	// Registered eagerly so the trim marker's embedded spill ID can be used
	// in the same turn without a fetch_tools round-trip.
	if set.SpilloverRead != nil {
		registry.RegisterTool(toolctx.ToolDef{
			Name:        "read_spillover",
			Description: "Read a previous large tool result by spill ID, paged — offset/limit line window (default 400 lines) or grep to jump to matching lines. Use when a tool result was too large and was replaced with a preview; follow the [계속: offset=N] tail hint to page",
			InputSchema: readSpilloverToolSchema(),
			Fn:          set.SpilloverRead,
		})
	}
}
