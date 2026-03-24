package channel

import (
	"context"
	"testing"
)

type mockPlugin struct {
	id     string
	meta   Meta
	caps   Capabilities
	status Status
}

func (m *mockPlugin) ID() string                       { return m.id }
func (m *mockPlugin) Meta() Meta                       { return m.meta }
func (m *mockPlugin) Capabilities() Capabilities       { return m.caps }
func (m *mockPlugin) Start(ctx context.Context) error  { return nil }
func (m *mockPlugin) Stop(ctx context.Context) error   { return nil }
func (m *mockPlugin) Status() Status                   { return m.status }

func TestRegistryRegisterAndGet(t *testing.T) {
	reg := NewRegistry()
	p := &mockPlugin{id: "telegram", meta: Meta{ID: "telegram", Label: "Telegram"}}
	if err := reg.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := reg.Get("telegram")
	if got == nil {
		t.Fatal("telegram not found")
	}
	if got.ID() != "telegram" {
		t.Errorf("ID = %q, want %q", got.ID(), "telegram")
	}
}

func TestRegistryDuplicateRegister(t *testing.T) {
	reg := NewRegistry()
	p := &mockPlugin{id: "telegram"}
	reg.Register(p)
	if err := reg.Register(p); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewRegistry()
	if reg.Get("nonexistent") != nil {
		t.Error("should not find nonexistent channel")
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockPlugin{id: "telegram"})
	reg.Register(&mockPlugin{id: "discord"})

	list := reg.List()
	if len(list) != 2 {
		t.Errorf("expected 2 channels, got %d", len(list))
	}
}

func TestRegistryStatusAll(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockPlugin{id: "telegram", status: Status{Connected: true}})
	reg.Register(&mockPlugin{id: "discord", status: Status{Connected: false, Error: "token expired"}})

	statuses := reg.StatusAll()
	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d", len(statuses))
	}
	if !statuses["telegram"].Connected {
		t.Error("telegram should be connected")
	}
	if statuses["discord"].Error != "token expired" {
		t.Errorf("discord error = %q, want %q", statuses["discord"].Error, "token expired")
	}
}
