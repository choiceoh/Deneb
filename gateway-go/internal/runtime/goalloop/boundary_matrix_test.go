package goalloop

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseJudgeVerdictBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		wantDone   bool
		wantReason string
		wantOK     bool
	}{
		{
			name:       "true boolean",
			input:      "{\"done\":true}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "false boolean",
			input:      "{\"done\":false}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "true with reason",
			input:      "{\"done\":true,\"reason\":\"complete\"}",
			wantDone:   true,
			wantReason: "complete",
			wantOK:     true,
		},
		{
			name:       "false with reason",
			input:      "{\"done\":false,\"reason\":\"continue\"}",
			wantDone:   false,
			wantReason: "continue",
			wantOK:     true,
		},
		{
			name:       "true string",
			input:      "{\"done\":\"true\"}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "true string upper",
			input:      "{\"done\":\"TRUE\"}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "true string mixed",
			input:      "{\"done\":\"TrUe\"}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "true string spaced",
			input:      "{\"done\":\" true \"}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "false string",
			input:      "{\"done\":\"false\"}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "false string upper",
			input:      "{\"done\":\"FALSE\"}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "arbitrary string",
			input:      "{\"done\":\"yes\"}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "empty string",
			input:      "{\"done\":\"\"}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "string one",
			input:      "{\"done\":\"1\"}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "null done",
			input:      "{\"done\":null}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "numeric zero",
			input:      "{\"done\":0}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "numeric one",
			input:      "{\"done\":1}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "array done",
			input:      "{\"done\":[]}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "object done",
			input:      "{\"done\":{}}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "missing done",
			input:      "{\"reason\":\"none\"}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "empty object",
			input:      "{}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "empty input",
			input:      "",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "spaces",
			input:      "   ",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "null input",
			input:      "null",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "array input",
			input:      "[]",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "boolean input",
			input:      "true",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "number input",
			input:      "1",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "plain prose",
			input:      "done",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "malformed open",
			input:      "{",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "malformed close",
			input:      "}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "trailing comma",
			input:      "{\"done\":true,}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "single quotes",
			input:      "{'done':true}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "fenced json",
			input:      "```json\n{\"done\":true,\"reason\":\"fenced\"}\n```",
			wantDone:   true,
			wantReason: "fenced",
			wantOK:     true,
		},
		{
			name:       "fenced bare",
			input:      "```\n{\"done\":false,\"reason\":\"later\"}\n```",
			wantDone:   false,
			wantReason: "later",
			wantOK:     true,
		},
		{
			name:       "prose prefix",
			input:      "Result: {\"done\":true,\"reason\":\"ok\"}",
			wantDone:   true,
			wantReason: "ok",
			wantOK:     true,
		},
		{
			name:       "prose suffix",
			input:      "{\"done\":false,\"reason\":\"work\"} thanks",
			wantDone:   false,
			wantReason: "work",
			wantOK:     true,
		},
		{
			name:       "prose both",
			input:      "before {\"done\":true,\"reason\":\"yes\"} after",
			wantDone:   true,
			wantReason: "yes",
			wantOK:     true,
		},
		{
			name:       "newlines around",
			input:      "\n\t{\"done\":true}\r\n",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "extra field",
			input:      "{\"done\":true,\"extra\":42}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "reason before done",
			input:      "{\"reason\":\"ordered\",\"done\":true}",
			wantDone:   true,
			wantReason: "ordered",
			wantOK:     true,
		},
		{
			name:       "duplicate done last true",
			input:      "{\"done\":false,\"done\":true}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "duplicate done last false",
			input:      "{\"done\":true,\"done\":false}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "duplicate reason last",
			input:      "{\"done\":true,\"reason\":\"one\",\"reason\":\"two\"}",
			wantDone:   true,
			wantReason: "two",
			wantOK:     true,
		},
		{
			name:       "unicode reason",
			input:      "{\"done\":true,\"reason\":\"완료\"}",
			wantDone:   true,
			wantReason: "완료",
			wantOK:     true,
		},
		{
			name:       "emoji reason",
			input:      "{\"done\":false,\"reason\":\"🚧\"}",
			wantDone:   false,
			wantReason: "🚧",
			wantOK:     true,
		},
		{
			name:       "escaped reason",
			input:      "{\"done\":true,\"reason\":\"line\\nnext\"}",
			wantDone:   true,
			wantReason: "line\nnext",
			wantOK:     true,
		},
		{
			name:       "empty reason",
			input:      "{\"done\":true,\"reason\":\"\"}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "reason null",
			input:      "{\"done\":true,\"reason\":null}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "reason number",
			input:      "{\"done\":true,\"reason\":1}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "case done field",
			input:      "{\"Done\":true}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "uppercase field",
			input:      "{\"DONE\":true}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "unknown casing reason",
			input:      "{\"done\":true,\"Reason\":\"ok\"}",
			wantDone:   true,
			wantReason: "ok",
			wantOK:     true,
		},
		{
			name:       "nested object",
			input:      "{\"outer\":{\"done\":true}}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "leading brace in prose",
			input:      "prefix {not json} then {\"done\":true}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "two objects",
			input:      "{\"done\":false} {\"done\":true}",
			wantDone:   false,
			wantReason: "",
			wantOK:     false,
		},
		{
			name:       "braces in reason",
			input:      "{\"done\":true,\"reason\":\"use {x}\"}",
			wantDone:   true,
			wantReason: "use {x}",
			wantOK:     true,
		},
		{
			name:       "escaped quote reason",
			input:      "{\"done\":true,\"reason\":\"say \\\"done\\\"\"}",
			wantDone:   true,
			wantReason: "say \"done\"",
			wantOK:     true,
		},
		{
			name:       "whitespace true string",
			input:      "{\"done\":\"\\tTRUE\\n\"}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "false spaced string",
			input:      "{\"done\":\" false \"}",
			wantDone:   false,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "json BOM",
			input:      "\ufeff{\"done\":true}",
			wantDone:   true,
			wantReason: "",
			wantOK:     true,
		},
		{
			name:       "markdown heading",
			input:      "# verdict\n{\"done\":true,\"reason\":\"heading\"}",
			wantDone:   true,
			wantReason: "heading",
			wantOK:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			done, reason, ok := parseJudgeVerdict(tc.input)
			if done != tc.wantDone {
				t.Errorf("done=%v want=%v", done, tc.wantDone)
			}
			if reason != tc.wantReason {
				t.Errorf("reason=%q want=%q", reason, tc.wantReason)
			}
			if ok != tc.wantOK {
				t.Errorf("ok=%v want=%v", ok, tc.wantOK)
			}
		})
	}
}

func TestComposeGoalContinuationBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		goal     string
		subgoals []string
	}{
		{
			name:     "empty goal",
			goal:     "",
			subgoals: []string{},
		},
		{
			name:     "spaces goal",
			goal:     "   ",
			subgoals: []string{},
		},
		{
			name:     "ascii",
			goal:     "ship release",
			subgoals: []string{},
		},
		{
			name:     "trim ascii",
			goal:     "  ship release  ",
			subgoals: []string{},
		},
		{
			name:     "korean",
			goal:     "탑솔라 견적 정리",
			subgoals: []string{},
		},
		{
			name:     "emoji",
			goal:     "launch 🚀",
			subgoals: []string{},
		},
		{
			name:     "newline goal",
			goal:     "first\nsecond",
			subgoals: []string{},
		},
		{
			name:     "one criterion",
			goal:     "goal",
			subgoals: []string{"criterion one"},
		},
		{
			name:     "two criteria",
			goal:     "goal",
			subgoals: []string{"first", "second"},
		},
		{
			name:     "three criteria",
			goal:     "goal",
			subgoals: []string{"first", "second", "third"},
		},
		{
			name:     "empty criterion",
			goal:     "goal",
			subgoals: []string{""},
		},
		{
			name:     "spaced criterion",
			goal:     "goal",
			subgoals: []string{"  keep spaces  "},
		},
		{
			name:     "unicode criteria",
			goal:     "목표",
			subgoals: []string{"견적서 생성", "메일 초안", "일정 등록"},
		},
		{
			name:     "duplicate criteria",
			goal:     "goal",
			subgoals: []string{"same", "same"},
		},
		{
			name:     "newline criterion",
			goal:     "goal",
			subgoals: []string{"line1\nline2"},
		},
		{
			name:     "special criterion",
			goal:     "goal",
			subgoals: []string{"`code` and {json}"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := composeGoalContinuation(tc.goal, tc.subgoals)
			if !strings.Contains(got, "NO_REPLY") {
				t.Error("NO_REPLY instruction missing")
			}
			if !strings.Contains(got, "목표: "+strings.TrimSpace(tc.goal)) {
				t.Errorf("trimmed goal missing from %q", got)
			}
			if len(tc.subgoals) == 0 {
				if strings.Contains(got, "추가 완료 기준") {
					t.Error("criteria header present without subgoals")
				}
				return
			}
			if !strings.Contains(got, "추가 완료 기준") {
				t.Error("criteria header missing")
			}
			for i, sg := range tc.subgoals {
				needle := fmt.Sprintf("%d. %s", i+1, sg)
				if !strings.Contains(got, needle) {
					t.Errorf("criterion %q missing", needle)
				}
			}
		})
	}
}

func TestGoalTaskConstructionWithNilDepsRunsCleanly(t *testing.T) {
	task := NewTask(nil, nil, nil, nil, nil)
	if task == nil {
		t.Fatal("NewTask returned nil")
	}
	if got := task.Name(); got != "goal-loop" {
		t.Errorf("Name=%q", got)
	}
	if got := task.Interval(); got != 2*time.Minute {
		t.Errorf("Interval=%s", got)
	}
	if task.chatHandler != nil {
		t.Error("nil chat handler was not retained")
	}
	if task.store != nil {
		t.Error("nil store was not retained")
	}
	if task.activity != nil {
		t.Error("nil activity was not retained")
	}
	if task.notify != nil {
		t.Error("nil notifier was not retained")
	}
	for _, ctx := range []context.Context{context.Background(), context.TODO()} {
		if err := task.Run(ctx); err != nil {
			t.Errorf("disabled Run=%v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := task.Run(ctx); err != nil {
		t.Errorf("canceled disabled Run=%v", err)
	}
}

func TestGoalTaskMetadataConstantsPinDurationContract(t *testing.T) {
	tests := []struct {
		name      string
		got, want time.Duration
	}{
		{name: "tick", got: goalTickInterval, want: 2 * time.Minute},
		{name: "idle", got: goalIdleThreshold, want: time.Minute},
		{name: "step timeout", got: goalStepTimeout, want: 5 * time.Minute},
		{name: "judge timeout", got: goalJudgeTimeout, want: 45 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("duration=%s want=%s", tc.got, tc.want)
			}
			if tc.got <= 0 {
				t.Errorf("duration must be positive: %s", tc.got)
			}
		})
	}
}

func TestComposeGoalContinuationNumberingBoundary(t *testing.T) {
	criteria := make([]string, 125)
	for i := range criteria {
		criteria[i] = fmt.Sprintf("criterion-%03d", i+1)
	}
	got := composeGoalContinuation("large goal", criteria)
	for i, criterion := range criteria {
		needle := fmt.Sprintf("%d. %s", i+1, criterion)
		if count := strings.Count(got, needle); count != 1 {
			t.Errorf("%q count=%d", needle, count)
		}
	}
	if strings.Index(got, "1. criterion-001") > strings.Index(got, "125. criterion-125") {
		t.Error("criteria order was not preserved")
	}
}

func TestGoalPureBoundariesConcurrent(t *testing.T) {
	const workers = 128
	const iterations = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				input := fmt.Sprintf(`prefix {"done":%t,"reason":"w-%d-%d"} suffix`, i%2 == 0, worker, i)
				done, reason, ok := parseJudgeVerdict(input)
				if !ok || done != (i%2 == 0) || reason != fmt.Sprintf("w-%d-%d", worker, i) {
					errs <- fmt.Errorf("parse mismatch worker=%d i=%d", worker, i)
					return
				}
				prompt := composeGoalContinuation(fmt.Sprintf("goal-%d", worker), []string{fmt.Sprintf("step-%d", i)})
				if !strings.Contains(prompt, fmt.Sprintf("step-%d", i)) {
					errs <- fmt.Errorf("compose mismatch worker=%d i=%d", worker, i)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
