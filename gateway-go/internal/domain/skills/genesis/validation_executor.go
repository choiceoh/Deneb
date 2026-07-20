package genesis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// replayExecutorMaxTokens bounds the simulated tool-call plan. The plan output
// itself is short JSON, but this budget must ALSO cover chain-of-thought:
// runReplayExecutorWith requests Thinking=disabled, yet reasoning models with
// no honored off-switch (glm-5.2 via wormhole) ignore it and reason anyway
// (modelrole/thinking.go — "the caller must budget MaxTokens for reasoning +
// answer instead"). At 1024 the reasoning alone overran the cap and the call
// returned finish_reason=length with EMPTY content, failing every replay —
// evolve's behavioral gate (fail-open, so it silently stopped guarding) AND the
// skill-workout lane (observed 2026-07-09: reasoning_chars=4540, "replay
// executor failed, ending cycle"). 4096 was then overrun too: ablation replay
// starved 3× in one day (2026-07-20: reasoning_chars=17628 ≈ 5K tokens on one
// call, an 8300-char plan truncated at the cap on another), skipping utility
// measurement. Budget observed worst-case reasoning + plan with ~3× headroom;
// the model still stops at the plan, so a non-runaway call does not actually
// consume this whole cap (local GPU — the cap costs latency, not money).
const replayExecutorMaxTokens = 16384

// emittedToolCall is one tool invocation the executor model says it would make
// when following a skill. Args is intentionally a flat string so the existing
// substring-based assertion matching (InputIncludes/InputExcludes) works without
// brittle JSON-shape coupling.
type emittedToolCall struct {
	Name string   `json:"name"`
	Args flexArgs `json:"args"`
}

// flexArgs accepts the executor's tool-call arguments whether the model returns
// a JSON string ("action=read id=5"), an object, an array, or a number, and
// flattens it to text. Lightweight models drift between these shapes; coercing
// instead of rejecting keeps a single formatting quirk from failing the whole
// behavioral gate (the same flexStr lesson as the ASR segment parser).
type flexArgs string

// UnmarshalJSON decodes the supported flexible JSON representation.
func (a *flexArgs) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*a = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*a = flexArgs(s)
		return nil
	}
	*a = flexArgs(string(b))
	return nil
}

type replayPlan struct {
	ToolCalls []emittedToolCall `json:"tool_calls"`
}

// runReplayExecutorWith simulates an agent following skillBody on the replay
// case input and returns the emitted tool-call plan as a scoreable trace. The
// executor never sees the expected answer — it must derive the plan from the
// skill text alone, which is what makes the original-vs-candidate delta a real
// behavioral signal rather than an echo.
func (v *SkillValidationEngine) runReplayExecutorWith(ctx context.Context, executor *llm.Client, model, skillBody string, replay SkillReplayCaseRecord) (skillReplayTrace, error) {
	system, user := buildReplayExecutorPrompt(skillBody, replay)
	out, err := executor.Complete(ctx, llm.ChatRequest{
		Model:          model,
		Messages:       []llm.Message{llm.NewTextMessage("user", user)},
		System:         llm.SystemString(system),
		MaxTokens:      replayExecutorMaxTokens,
		Thinking:       &llm.ThinkingConfig{Type: "disabled"},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		return skillReplayTrace{}, err
	}
	calls, perr := parseEmittedToolCalls(out)
	if perr != nil {
		return skillReplayTrace{}, perr
	}
	return traceFromEmittedCalls(calls), nil
}

func buildReplayExecutorPrompt(skillBody string, replay SkillReplayCaseRecord) (system, user string) {
	if strings.TrimSpace(skillBody) == "" {
		system = `당신은 별도의 SKILL 문서를 제공받지 않은 에이전트를 시뮬레이션하는 채점기입니다.
주어진 사용자 작업과 컨텍스트만으로 호출하게 될 게이트웨이 도구들을 "순서대로" 출력하세요.
- 준비된 절차가 있다고 가정하지 말고, 작업을 수행하는 데 필요한 도구만 선택하세요.
- 실제 게이트웨이 도구 이름을 쓰세요 (예: exec, gmail, wiki, web, fs). 셸/CLI 명령은 exec로 감싸고 핵심 인자(명령어·경로·쿼리)를 args에 적으세요.
- 설명·주석·markdown 없이 JSON 객체 하나만 출력: {"tool_calls":[{"name":"<도구>","args":"<핵심 인자>"}]}`
	} else {
		system = `당신은 아래 SKILL 문서를 따르는 에이전트를 시뮬레이션하는 채점기입니다.
주어진 사용자 작업에 대해, 이 SKILL의 절차를 그대로 따랐을 때 호출하게 될 게이트웨이 도구들을 "순서대로" 출력하세요.
- 실제 게이트웨이 도구 이름을 쓰세요 (예: exec, gmail, wiki, web, fs). 셸/CLI 명령은 exec로 감싸고 핵심 인자(명령어·경로·쿼리)를 args에 적으세요.
- SKILL이 지시하는 것만 최소한으로 출력하고, 추측으로 도구를 늘리지 마세요.
- 설명·주석·markdown 없이 JSON 객체 하나만 출력: {"tool_calls":[{"name":"<도구>","args":"<핵심 인자>"}]}`
	}

	var b strings.Builder
	if strings.TrimSpace(skillBody) != "" {
		b.WriteString("## SKILL\n")
		b.WriteString(strings.TrimSpace(skillBody))
		b.WriteString("\n\n")
	}
	b.WriteString("## 사용자 작업\n")
	b.WriteString(strings.TrimSpace(replay.Input))
	if ctxLines := cleanReplayContextLines(replay.Context); len(ctxLines) > 0 {
		b.WriteString("\n\n## 컨텍스트\n")
		for _, line := range ctxLines {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return system, b.String()
}

func cleanReplayContextLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// parseEmittedToolCalls reads the executor's JSON plan. It accepts either the
// documented {"tool_calls":[...]} object or a bare [...] array (a common
// lightweight-model drift), and returns an empty slice with no error when the
// model legitimately reports no tools — only a genuinely unparseable body errors
// (which the caller treats as fail-open).
func parseEmittedToolCalls(raw string) ([]emittedToolCall, error) {
	plan, err := jsonutil.UnmarshalLLM[replayPlan](raw)
	if err == nil {
		return plan.ToolCalls, nil
	}
	arr, aerr := jsonutil.UnmarshalLLM[[]emittedToolCall](raw)
	if aerr == nil {
		return arr, nil
	}
	return nil, fmt.Errorf("parse tool-call plan: %w", err)
}

// traceFromEmittedCalls flattens the emitted plan into the same skillReplayTrace
// shape the deterministic body scorer uses, so scoreReplayAgainstTrace can apply
// every required/forbidden tool and ordering assertion against what the model
// would actually DO rather than what the skill text merely mentions.
func traceFromEmittedCalls(calls []emittedToolCall) skillReplayTrace {
	var b strings.Builder
	for _, c := range calls {
		name := strings.TrimSpace(c.Name)
		args := strings.TrimSpace(string(c.Args))
		if name == "" && args == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(name)
		if args != "" {
			b.WriteByte(' ')
			b.WriteString(args)
		}
	}
	text := normalizedValidationText(b.String())
	return skillReplayTrace{text: text, tokens: validationTokens(text)}
}
