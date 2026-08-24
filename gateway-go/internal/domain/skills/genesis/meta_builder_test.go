package genesis

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// builderFixture wires an evolver whose three model slots are distinguishable
// by model id, so the precedence is observable without an LLM call.
func builderFixture(t *testing.T) (*MetaEvolutionTask, *llm.Client) {
	t.Helper()
	client := llm.NewClient("http://127.0.0.1:1", "k")
	if client == nil {
		t.Fatal("llm client fixture is nil")
	}
	e := &Evolver{}
	e.SetPrimary(client, "producer-model")
	return &MetaEvolutionTask{Evolver: e}, client
}

// The builder must be the STRONGEST wired model, not whoever produces. The
// production shape that motivated this: a dedicated coding model owns rewrites,
// so the teacher is deliberately nil — and the revision then fell through to
// the producer revising its own operating prompt.
func TestMetaBuilderPrefersStrongerModelOverProducer(t *testing.T) {
	task, client := builderFixture(t)

	_, model, source := task.builderModel()
	if source != "primary" || model != "producer-model" {
		t.Fatalf("bare wiring = %s/%s, want primary/producer-model", source, model)
	}

	task.Evolver.SetJudge(client, "judge-model")
	_, model, source = task.builderModel()
	if source != "judge" || model != "judge-model" {
		t.Errorf("with a judge wired = %s/%s, want judge/judge-model", source, model)
	}

	task.Evolver.SetTeacher(client, "teacher-model")
	_, model, source = task.builderModel()
	if source != "teacher" || model != "teacher-model" {
		t.Errorf("with a teacher wired = %s/%s, want teacher/teacher-model", source, model)
	}
}

// An unwired slot must not shadow the next rung — a nil client with a stale
// model string would otherwise pick a model nothing can call.
func TestMetaBuilderSkipsHalfWiredSlots(t *testing.T) {
	task, client := builderFixture(t)
	task.Evolver.SetTeacher(nil, "teacher-model")
	task.Evolver.SetJudge(client, "judge-model")

	_, model, source := task.builderModel()
	if source != "judge" || model != "judge-model" {
		t.Errorf("half-wired teacher = %s/%s, want judge/judge-model", source, model)
	}

	task.Evolver.SetJudge(client, "")
	_, model, source = task.builderModel()
	if source != "primary" || model != "producer-model" {
		t.Errorf("empty judge model = %s/%s, want primary/producer-model", source, model)
	}
}
