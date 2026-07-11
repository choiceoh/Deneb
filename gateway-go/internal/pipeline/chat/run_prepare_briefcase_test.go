package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

func TestPrepareContextAndPromptBriefcaseWithholdsHostContext(t *testing.T) {
	t.Cleanup(prompt.Cache.Reset)
	var calendarCalled, goalCalled atomic.Bool
	reg := NewToolRegistry()
	reg.Register("wiki", func(context.Context, json.RawMessage) (string, error) { return "", nil })
	reg.Register("read", func(context.Context, json.RawMessage) (string, error) { return "", nil })

	parent := t.TempDir()
	workspace := filepath.Join(parent, "isolated", "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(parent, "USER.md"),
		filepath.Join(parent, "MEMORY.md"),
		filepath.Join(workspace, "SOUL.md"),
		filepath.Join(workspace, "AGENTS.md"),
	} {
		if err := os.WriteFile(path, []byte("HOST-AMBIENT-SENTINEL"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	fixedNow := time.Date(2040, time.December, 31, 16, 30, 0, 0, time.UTC)
	deps := runDeps{
		logger:             discardLogger(),
		tools:              reg,
		transcript:         NewMemoryTranscriptStore(),
		contextCfg:         DefaultContextConfig(),
		briefcaseMode:      true,
		disableTier1Wiki:   true,
		promptWorkspaceDir: "/briefcase/workspace",
		semanticTimezone:   "UTC",
		semanticNow:        func() time.Time { return fixedNow },
		strictErrors:       &strictRunErrorSink{},
		ambient: AmbientDeps{
			CalendarGlance: func(context.Context, string, string) string {
				calendarCalled.Store(true)
				return "HOST-CALENDAR-SENTINEL"
			},
			GoalGlance: func(context.Context, string) string {
				goalCalled.Store(true)
				return "HOST-GOAL-SENTINEL"
			},
			PersonaOverride: func() string { return "HOST-PERSONA-SENTINEL" },
		},
	}

	prep := prepareContextAndPrompt(context.Background(), RunParams{
		SessionKey: "client:briefcase-isolation",
		Message:    "기억을 확인해줘",
	}, deps, workspace, "briefcase", deps.logger)
	system := string(prep.SystemPrompt)
	for _, leaked := range []string{
		"HOST-AMBIENT-SENTINEL",
		"HOST-CALENDAR-SENTINEL",
		"HOST-GOAL-SENTINEL",
		"HOST-PERSONA-SENTINEL",
		"너의 외부 메모리",
	} {
		if strings.Contains(system, leaked) {
			t.Fatalf("Briefcase prompt leaked host context %q", leaked)
		}
	}
	if prep.ContextFiles != nil {
		t.Fatalf("Briefcase loaded workspace context files: %+v", prep.ContextFiles)
	}
	if calendarCalled.Load() || goalCalled.Load() {
		t.Fatal("Briefcase invoked ambient calendar or goal providers")
	}
	if !strings.Contains(system, "Deneb-Briefcase") ||
		!strings.Contains(system, "Workspace: /briefcase/workspace") ||
		!strings.Contains(system, "Monday, December 31, 2040") {
		t.Fatalf("Briefcase prompt missing isolated provenance: %s", system)
	}
}
