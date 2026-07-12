// Package modelmaintenance owns construction of the gateway's model-quality
// background tasks. The server registers the resulting tasks but does not need
// to know which tuner, telemetry adapters, or persisted stores implement them.
package modelmaintenance

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modeltuner"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/regressionwatch"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compactuner"
)

const regressionWindow = 24 * time.Hour

// PeriodicTask is the scheduler surface returned to runtime/server. It matches
// autonomous.PeriodicTask without coupling this composition package to the
// autonomous service implementation.
type PeriodicTask interface {
	Name() string
	Interval() time.Duration
	Run(context.Context) error
}

// PromptTuner is the operator-triggered surface of the optional compaction
// task. The concrete task remains owned by this package.
type PromptTuner interface {
	RunWithReport(context.Context) compactuner.Report
}

// LogCapture is the narrow observation capability used by regression watch.
type LogCapture interface {
	Ring() *observe.Ring
}

// Deps are the runtime-owned inputs needed to construct model maintenance.
// StateDir determines the compaction-guideline store; model and regression
// tasks retain their own DENEB_STATE_DIR-aware default paths.
type Deps struct {
	Logs      *agentlog.Writer
	Registry  *modelrole.Registry
	Summaries compactuner.SummarySource
	Capture   LogCapture
	StateDir  string
	Notify    func(context.Context, string) error
	Logger    *slog.Logger
}

// Suite is the cohesive set of model-quality background tasks. The compaction
// tuner is also exposed for the operator-triggered prompt-tuner RPC.
type Suite struct {
	tasks           []PeriodicTask
	compactionTuner *compactuner.Task
}

// New constructs the enabled model-maintenance tasks in their stable
// registration order: model tuning, regression watch, then opt-in compaction
// tuning. Missing core telemetry inputs disable the entire suite, matching the
// gateway's previous fail-soft startup behavior.
func New(deps Deps) *Suite {
	suite := &Suite{}
	if deps.Logs == nil || deps.Registry == nil {
		return suite
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	suite.tasks = append(
		suite.tasks,
		modeltuner.NewTask(modeltuner.Deps{
			Logs:      deps.Logs,
			Registry:  deps.Registry,
			StatePath: modeltuner.DefaultStatePath(),
			Notify:    deps.Notify,
			Logger:    logger,
		}),
		regressionwatch.NewTask(regressionwatch.Deps{
			Sources:   regressionSources(deps),
			StatePath: regressionwatch.DefaultStatePath(),
			Notify:    deps.Notify,
			Logger:    logger,
		}),
	)

	if os.Getenv("DENEB_COMPACTION_TUNER") != "1" || deps.Summaries == nil {
		return suite
	}
	client := deps.Registry.Client(modelrole.RoleLightweight)
	if client == nil {
		return suite
	}
	suite.compactionTuner = compactuner.NewTask(compactuner.Deps{
		Summaries:  deps.Summaries,
		Guidelines: compaction.NewGuidelineStore(filepath.Join(deps.StateDir, compaction.GuidelineFileName)),
		Client:     client,
		Model:      deps.Registry.Model(modelrole.RoleLightweight),
		Notify:     deps.Notify,
		Logger:     logger,
	})
	suite.tasks = append(suite.tasks, suite.compactionTuner)
	logger.Info("compaction-tuner: enabled (DENEB_COMPACTION_TUNER)")
	return suite
}

func regressionSources(deps Deps) []regressionwatch.SignalSource {
	sources := []regressionwatch.SignalSource{
		regressionwatch.AgentLogSource{Logs: deps.Logs, Window: regressionWindow},
		regressionwatch.HealthSource{Registry: deps.Registry},
	}
	if deps.Capture != nil {
		sources = append(sources, observeLogSource{ring: deps.Capture.Ring(), window: regressionWindow})
	}
	return sources
}

// Tasks returns a copy so registration cannot mutate suite ownership or order.
func (suite *Suite) Tasks() []PeriodicTask {
	if suite == nil {
		return nil
	}
	return append([]PeriodicTask(nil), suite.tasks...)
}

// PromptTuner returns the opt-in compaction tuner exposed through the operator
// RPC, or nil when compaction tuning is disabled or unavailable.
func (suite *Suite) PromptTuner() PromptTuner {
	if suite == nil || suite.compactionTuner == nil {
		return nil
	}
	return suite.compactionTuner
}

// observeLogSource adapts the bounded in-memory log ring into one regression
// signal. It lives with the suite because it is model-maintenance telemetry,
// not HTTP server behavior.
type observeLogSource struct {
	ring   *observe.Ring
	window time.Duration
}

func (source observeLogSource) Name() string { return "observe-log" }

func (source observeLogSource) Sample() []regressionwatch.Signal {
	if source.ring == nil {
		return nil
	}
	since := time.Now().Add(-source.window).UnixMilli()
	errorLines := source.ring.Query(observe.QueryOpts{
		MinLevel: slog.LevelError,
		SinceMs:  since,
		Limit:    source.ring.Cap(),
	})
	return []regressionwatch.Signal{{
		Key:         "observe.error_lines",
		Value:       float64(len(errorLines)),
		Sample:      1,
		HigherWorse: true,
		Kind:        regressionwatch.KindCount,
		HardFloor:   10,
	}}
}
