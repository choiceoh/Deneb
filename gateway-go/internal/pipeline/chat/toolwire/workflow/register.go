package workflow

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/workflowops"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire/schema"
)

// ToolSet is the narrow implementation port for workflow tools.
type ToolSet struct {
	Goal       toolport.ToolFunc
	Blackboard toolport.ToolFunc
}

// DefaultToolSet wires the production workflow tool implementations.
func DefaultToolSet() ToolSet {
	return ToolSet{
		Goal:       workflowops.ToolGoal(),
		Blackboard: workflowops.ToolBlackboard(),
	}
}

// Register wires goal-oriented workflow tools.
func Register(registry toolport.ToolRegistrar) {
	RegisterTools(registry, DefaultToolSet())
}

// RegisterTools registers workflow tools from an already-wired tool set.
func RegisterTools(registry toolport.ToolRegistrar, set ToolSet) {
	// Standing goal (Ralph loop). Once set, the server's goalTask advances it
	// one run per idle tick, judges completion with the lightweight model, and a
	// per-goal idempotency ledger blocks repeated destructive actions.
	//
	// Deferred (prompt audit 2026-08-29): it was eager so the agent could
	// discover it and volunteer a goal on a multi-step request. It never did —
	// zero calls across the recorded transcript history — so the eager slot was
	// buying no behavior at ~770 wire bytes a turn. If standing goals should
	// actually fire, the lever is the prompt that decides to set one, not the
	// tool's placement.
	registry.RegisterTool(toolport.ToolDef{
		Name:        "goal",
		Description: "다단계·장기 작업을 여러 턴에 걸쳐 끝까지 진행해야 할 때 표준 목표(standing goal)를 설정·관리한다. action=set(목표 설정) | subgoal(완료 기준 추가) | status | pause | resume | stop. 설정하면 사용자가 자리를 비운 동안 자동으로 한 단계씩 진행하고 완료를 판정한다. 이미 실행한 작업은 멱등 가드로 중복되지 않는다.",
		InputSchema: schema.GoalToolSchema(),
		Fn:          set.Goal,
		Deferred:    true,
	})

	// Typed blackboard: fail-closed I/O contracts for multi-tool workflows.
	// Prefer named keys over free-text handoffs between mail/wiki/web/etc.
	registry.RegisterTool(toolport.ToolDef{
		Name: "blackboard",
		Description: "크로스툴·다단계 워크플로에서 중간값을 typed key로 넘긴다(free-text 요약 대체). " +
			"action=plan(steps[{id,goal,inputs,outputs}]) → begin(step) → 작업 → end(step,outputs) | put/get/require/list/clear. " +
			"필수 입력·출력이 없으면 실패로 닫힌다. 다른 툴 인자에는 문자열 \"$board.<key>\"로 주입 가능.",
		InputSchema: schema.BlackboardToolSchema(),
		Fn:          set.Blackboard,
	})
}

// NewGoalGlanceFunc builds the ambient standing-goal glance for the dynamic
// system-prompt block.
func NewGoalGlanceFunc() func(ctx context.Context, sessionKey string) string {
	return workflowops.NewGoalGlanceFunc()
}

// HandleGoalCommand processes the /goal slash command against the process goal
// store.
func HandleGoalCommand(sessionKey, args string, respond func(text string)) {
	workflowops.HandleGoalCommand(sessionKey, args, respond)
}
