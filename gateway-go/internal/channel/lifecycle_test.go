package channel

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// lifecycleMockPlugin extends mockPlugin with start/stop tracking.
type lifecycleMockPlugin struct {
	id          string
	meta        Meta
	caps        Capabilities
	status      Status
	startErr    error
	stopErr     error
	startCalled bool
	stopCalled  bool
}

func newLifecycleMock(id string) *lifecycleMockPlugin {
	return &lifecycleMockPlugin{
		id:     id,
		meta:   Meta{ID: id, Label: id},
		caps:   Capabilities{ChatTypes: []string{"text"}},
		status: Status{Connected: true},
	}
}

func (m *lifecycleMockPlugin) ID() string                       { return m.id }
func (m *lifecycleMockPlugin) Meta() Meta                       { return m.meta }
func (m *lifecycleMockPlugin) Capabilities() Capabilities       { return m.caps }
func (m *lifecycleMockPlugin) Status() Status                   { return m.status }

func (m *lifecycleMockPlugin) Start(_ context.Context) error {
	m.startCalled = true
	if m.startErr != nil {
		return m.startErr
	}
	m.status.Connected = true
	return nil
}

func (m *lifecycleMockPlugin) Stop(_ context.Context) error {
	m.stopCalled = true
	if m.stopErr != nil {
		return m.stopErr
	}
	m.status.Connected = false
	return nil
}

func TestLifecycleStartAll(t *testing.T) {
	reg := NewRegistry()
	p1 := newLifecycleMock("telegram")
	p2 := newLifecycleMock("discord")
	_ = reg.Register(p1)
	_ = reg.Register(p2)

	lm := NewLifecycleManager(reg, slog.Default())
	errs := lm.StartAll(context.Background())

	if errs != nil {
		t.Fatalf("expected nil errors, got %v", errs)
	}
	if !p1.startCalled || !p2.startCalled {
		t.Error("expected both plugins to be started")
	}
}

func TestLifecycleStartAllWithError(t *testing.T) {
	reg := NewRegistry()
	p1 := newLifecycleMock("telegram")
	p2 := newLifecycleMock("discord")
	p2.startErr = errors.New("connection refused")
	_ = reg.Register(p1)
	_ = reg.Register(p2)

	lm := NewLifecycleManager(reg, slog.Default())
	errs := lm.StartAll(context.Background())

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if errs["discord"] == nil {
		t.Error("expected error for discord")
	}
}

func TestLifecycleStopAll(t *testing.T) {
	reg := NewRegistry()
	p1 := newLifecycleMock("telegram")
	_ = reg.Register(p1)

	lm := NewLifecycleManager(reg, slog.Default())
	_ = lm.StartAll(context.Background())
	errs := lm.StopAll(context.Background())

	if errs != nil {
		t.Fatalf("expected nil errors, got %v", errs)
	}
	if !p1.stopCalled {
		t.Error("expected stop to be called")
	}
}

func TestLifecycleHealthCheck(t *testing.T) {
	reg := NewRegistry()
	p1 := newLifecycleMock("telegram")
	p2 := newLifecycleMock("discord")
	p2.status = Status{Connected: false, Error: "auth failed"}
	_ = reg.Register(p1)
	_ = reg.Register(p2)

	lm := NewLifecycleManager(reg, slog.Default())
	health := lm.HealthCheck()

	if len(health) != 2 {
		t.Fatalf("expected 2 health entries, got %d", len(health))
	}

	for _, h := range health {
		if h.ID == "discord" {
			if h.Connected {
				t.Error("discord should not be connected")
			}
			if h.Error != "auth failed" {
				t.Errorf("expected 'auth failed', got %q", h.Error)
			}
		}
	}
}

func TestLifecycleSingleChannel(t *testing.T) {
	reg := NewRegistry()
	p1 := newLifecycleMock("telegram")
	_ = reg.Register(p1)

	lm := NewLifecycleManager(reg, slog.Default())

	if err := lm.StartChannel(context.Background(), "telegram"); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	if !p1.startCalled {
		t.Error("expected start to be called")
	}

	if err := lm.StopChannel(context.Background(), "telegram"); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if !p1.stopCalled {
		t.Error("expected stop to be called")
	}

	if err := lm.StartChannel(context.Background(), "unknown"); err == nil {
		t.Error("expected error for unknown channel")
	}
}
