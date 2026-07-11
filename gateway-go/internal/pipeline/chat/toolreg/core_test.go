package toolreg

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// mockRegistrar collects registered tools for assertion.
type mockRegistrar struct {
	tools []toolctx.ToolDef
}

func (m *mockRegistrar) RegisterTool(def toolctx.ToolDef) {
	m.tools = append(m.tools, def)
}

func (m *mockRegistrar) toolNames() []string {
	names := make([]string, len(m.tools))
	for i, t := range m.tools {
		names[i] = t.Name
	}
	return names
}

// ─── RegisterFSTools ──────────────────────────────────────────────────────────

func TestRegisterFSTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	deps := &toolctx.CoreToolDeps{WorkspaceDir: t.TempDir()}
	RegisterFSTools(reg, deps)

	if len(reg.tools) == 0 {
		t.Fatal("expected RegisterFSTools to register at least one tool")
	}
	// Verify well-known FS tools are present.
	names := reg.toolNames()
	for _, want := range []string{"read", "write", "edit", "grep"} {
		if !containsName(names, want) {
			t.Errorf("missing expected tool %q", want)
		}
	}
	for _, name := range []string{"gateway", "observe", "fleet"} {
		assertRegisteredContract(t, registeredTool(t, reg, name), true)
	}
}

// ─── RegisterProcessTools ─────────────────────────────────────────────────────

func TestRegisterProcessTools_registersTools(t *testing.T) {
	reg := &mockRegistrar{}
	deps := &toolctx.ProcessDeps{WorkspaceDir: t.TempDir()}
	RegisterProcessTools(reg, deps)

	assertRegisteredContract(t, registeredTool(t, reg, "exec"), false)
	assertRegisteredContract(t, registeredTool(t, reg, "process"), true)
}

func TestRegisterSessionToolsContracts(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterSessionTools(reg, &toolctx.SessionDeps{})

	assertRegisteredContract(t, registeredTool(t, reg, "sessions"), true)
	assertRegisteredContract(t, registeredTool(t, reg, "sessions_spawn"), false)
	assertRegisteredContract(t, registeredTool(t, reg, "subagents"), true)
}

func TestRegisterHeartbeatContract(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterChronoTools(reg)

	assertRegisteredContract(t, registeredTool(t, reg, "heartbeat_update"), false)
}

func TestRegisterScheduleAndRoutineTools(t *testing.T) {
	calendarReg := &mockRegistrar{}
	RegisterCalendarTool(calendarReg, &toolctx.CalendarDeps{
		Client: func() (toolctx.CalendarReader, error) { return nil, nil },
	})
	calendar := registeredTool(t, calendarReg, "calendar")
	assertRegisteredContract(t, calendar, false)

	routineReg := &mockRegistrar{}
	RegisterRoutineTools(routineReg, &toolctx.ChronoDeps{}, t.TempDir(), t.TempDir(), nil)
	for _, name := range []string{"files", "morning_letter", "evening_letter"} {
		def := registeredTool(t, routineReg, name)
		assertRegisteredContract(t, def, true)
	}
}

func TestRegisterMediaTools(t *testing.T) {
	reg := &mockRegistrar{}
	RegisterMediaTools(reg, t.TempDir())

	for _, name := range []string{"send_file", "chart", "diagram", "watch"} {
		def := registeredTool(t, reg, name)
		assertRegisteredContract(t, def, true)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func containsName(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}

func registeredTool(t *testing.T, registrar *mockRegistrar, name string) toolctx.ToolDef {
	t.Helper()
	for _, def := range registrar.tools {
		if def.Name == name {
			return def
		}
	}
	t.Fatalf("tool %q was not registered", name)
	return toolctx.ToolDef{}
}

func assertRegisteredContract(t *testing.T, def toolctx.ToolDef, deferred bool) {
	t.Helper()
	if def.Fn == nil {
		t.Fatalf("tool %q has no implementation", def.Name)
	}
	if def.InputSchema["type"] != "object" {
		t.Fatalf("tool %q schema type = %v, want object", def.Name, def.InputSchema["type"])
	}
	if def.Deferred != deferred {
		t.Fatalf("tool %q deferred = %v, want %v", def.Name, def.Deferred, deferred)
	}
}
