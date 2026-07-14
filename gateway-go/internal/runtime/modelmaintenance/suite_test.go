package modelmaintenance

import (
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

type summarySourceStub struct{}

func (summarySourceStub) RecentSummariesAcrossSessions(int) []polaris.SummaryNode { return nil }

func TestNewPreservesTaskOrderAndCompactionFallback(t *testing.T) {
	t.Setenv("DENEB_COMPACTION_TUNER", "")
	suite := New(testDeps(t))

	if got, want := taskNames(suite.Tasks()), []string{"model-tuner", "regression-watch"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("task names = %v, want %v", got, want)
	}
	if suite.PromptTuner() != nil {
		t.Fatal("prompt tuner must stay unavailable when the opt-in is disabled")
	}
}

func TestNewCreatesCompactionTaskWithPromptTuner(t *testing.T) {
	t.Setenv("DENEB_COMPACTION_TUNER", "1")
	deps := testDeps(t)
	deps.Summaries = summarySourceStub{}
	suite := New(deps)

	if got, want := taskNames(suite.Tasks()), []string{"model-tuner", "regression-watch", "compaction-tuner"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("task names = %v, want %v", got, want)
	}
	if suite.PromptTuner() == nil {
		t.Fatal("enabled compaction task must also back the prompt-tuner RPC")
	}
	if suite.Tasks()[2] != suite.compactionTuner || suite.PromptTuner() != suite.compactionTuner {
		t.Fatal("registered compaction task differs from the RPC tuner")
	}
}

func TestNewMissingCoreTelemetryDisablesSuite(t *testing.T) {
	tests := []struct {
		name string
		deps Deps
	}{
		{name: "missing logs", deps: Deps{Registry: modelrole.NewRegistry(discardLogger(), "", "test-model")}},
		{name: "missing registry", deps: Deps{Logs: agentlog.NewWriter(t.TempDir())}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suite := New(tt.deps)
			if tasks := suite.Tasks(); len(tasks) != 0 {
				t.Fatalf("tasks = %v, want disabled suite", taskNames(tasks))
			}
			if suite.PromptTuner() != nil {
				t.Fatal("disabled suite exposed a prompt tuner")
			}
		})
	}
}

func TestObserveLogSourceCountsOnlyRecentErrors(t *testing.T) {
	ring := observe.NewRing(8)
	logger := slog.New(observe.NewCapture(slog.NewTextHandler(io.Discard, nil), ring))
	logger.Info("healthy")
	logger.Error("first failure")
	logger.Error("second failure")

	signals := (observeLogSource{ring: ring, window: regressionWindow}).Sample()
	if len(signals) != 1 {
		t.Fatalf("signals = %+v, want one", signals)
	}
	signal := signals[0]
	if signal.Key != "observe.error_lines" || signal.Value != 2 || signal.Sample != 1 || !signal.HigherWorse || signal.HardFloor != 10 {
		t.Fatalf("signal = %+v, want stable recent error-count contract", signal)
	}
}

func testDeps(t *testing.T) Deps {
	t.Helper()
	stateDir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", stateDir)
	logger := discardLogger()
	return Deps{
		Logs:     agentlog.NewWriter(t.TempDir()),
		Registry: modelrole.NewRegistry(logger, "", "test-model"),
		StateDir: stateDir,
		Logger:   logger,
	}
}

func taskNames(tasks []PeriodicTask) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.Name())
	}
	return names
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
