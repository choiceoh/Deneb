package hooks

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegister_And_List(t *testing.T) {
	r := NewRegistry(testLogger())

	err := r.Register(Hook{
		ID:      "h1",
		Event:   EventGatewayStart,
		Command: "echo hello",
		Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	list := r.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(list))
	}
	if list[0].ID != "h1" {
		t.Errorf("expected h1, got %s", list[0].ID)
	}
}

func TestRegister_DuplicateID(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{ID: "h1", Event: EventGatewayStart, Command: "echo 1", Enabled: true})
	err := r.Register(Hook{ID: "h1", Event: EventGatewayStop, Command: "echo 2", Enabled: true})
	if err == nil {
		t.Error("expected error for duplicate ID")
	}
}

func TestRegister_Validation(t *testing.T) {
	r := NewRegistry(testLogger())

	err := r.Register(Hook{ID: "", Command: "echo", Enabled: true})
	if err == nil {
		t.Error("expected error for empty ID")
	}

	err = r.Register(Hook{ID: "h1", Command: "", Enabled: true})
	if err == nil {
		t.Error("expected error for empty command")
	}
}

func TestUnregister(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{ID: "h1", Event: EventGatewayStart, Command: "echo", Enabled: true})

	if !r.Unregister("h1") {
		t.Error("expected true")
	}
	if r.Unregister("h1") {
		t.Error("expected false for already removed")
	}
}

func TestUpdate(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{ID: "h1", Event: EventGatewayStart, Command: "echo 1", Enabled: true})

	err := r.Update(Hook{ID: "h1", Event: EventGatewayStop, Command: "echo 2", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}

	list := r.List()
	if list[0].Command != "echo 2" {
		t.Errorf("expected updated command, got %s", list[0].Command)
	}

	err = r.Update(Hook{ID: "nonexistent", Command: "echo"})
	if err == nil {
		t.Error("expected error for nonexistent hook")
	}
}

func TestListForEvent(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{ID: "h1", Event: EventGatewayStart, Command: "echo 1", Enabled: true})
	r.Register(Hook{ID: "h2", Event: EventGatewayStop, Command: "echo 2", Enabled: true})
	r.Register(Hook{ID: "h3", Event: EventGatewayStart, Command: "echo 3", Enabled: false}) // disabled

	hooks := r.ListForEvent(EventGatewayStart)
	if len(hooks) != 1 {
		t.Errorf("expected 1 enabled hook for gateway.start, got %d", len(hooks))
	}
}

func TestFire_Blocking(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{
		ID:       "h1",
		Event:    EventGatewayStart,
		Command:  "echo fired",
		Enabled:  true,
		Blocking: true,
	})

	results := r.Fire(context.Background(), EventGatewayStart, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", results[0].ExitCode)
	}
	if results[0].HookID != "h1" {
		t.Errorf("expected hookId h1, got %s", results[0].HookID)
	}
}

func TestFire_NonBlocking(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{
		ID:       "h1",
		Event:    EventGatewayStart,
		Command:  "echo async",
		Enabled:  true,
		Blocking: false,
	})

	results := r.Fire(context.Background(), EventGatewayStart, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestFire_EnvVars(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{
		ID:       "h1",
		Event:    EventGatewayStart,
		Command:  "echo $DENEB_HOOK_EVENT",
		Enabled:  true,
		Blocking: true,
	})

	results := r.Fire(context.Background(), EventGatewayStart, map[string]string{
		"CUSTOM_VAR": "custom_value",
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestFire_NoMatchingHooks(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{ID: "h1", Event: EventGatewayStart, Command: "echo", Enabled: true})

	results := r.Fire(context.Background(), EventGatewayStop, nil)
	if results != nil {
		t.Errorf("expected nil for no matching hooks, got %v", results)
	}
}

func TestDefaultTimeout(t *testing.T) {
	r := NewRegistry(testLogger())
	r.Register(Hook{ID: "h1", Event: EventGatewayStart, Command: "echo", Enabled: true})
	hooks := r.List()
	if hooks[0].TimeoutMs != 30000 {
		t.Errorf("expected default timeout 30000, got %d", hooks[0].TimeoutMs)
	}
}
