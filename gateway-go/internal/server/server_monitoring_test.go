package server

import (
	"context"
	"log/slog"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
)

type monitoringTestPlugin struct {
	id string
}

func (p *monitoringTestPlugin) ID() string                     { return p.id }
func (p *monitoringTestPlugin) Meta() channel.Meta             { return channel.Meta{ID: p.id} }
func (p *monitoringTestPlugin) Capabilities() channel.Capabilities { return channel.Capabilities{} }
func (p *monitoringTestPlugin) Start(context.Context) error    { return nil }
func (p *monitoringTestPlugin) Stop(context.Context) error     { return nil }
func (p *monitoringTestPlugin) Status() channel.Status         { return channel.Status{} }

func TestExpectedWatchdogChannelCount_OnlyStartedChannels(t *testing.T) {
	reg := channel.NewRegistry()
	if err := reg.Register(&monitoringTestPlugin{id: "telegram"}); err != nil {
		t.Fatalf("register telegram: %v", err)
	}
	if err := reg.Register(&monitoringTestPlugin{id: "discord"}); err != nil {
		t.Fatalf("register discord: %v", err)
	}

	lm := channel.NewLifecycleManager(reg, slog.Default())
	if err := lm.StartChannel(context.Background(), "telegram"); err != nil {
		t.Fatalf("start telegram: %v", err)
	}

	s := &Server{
		channels:         reg,
		channelLifecycle: lm,
	}

	if got := s.expectedWatchdogChannelCount(); got != 1 {
		t.Fatalf("expected watchdog channel count=1, got %d", got)
	}
}

func TestExpectedWatchdogChannelCount_FallbackNoLifecycle(t *testing.T) {
	reg := channel.NewRegistry()
	if err := reg.Register(&monitoringTestPlugin{id: "telegram"}); err != nil {
		t.Fatalf("register telegram: %v", err)
	}
	if err := reg.Register(&monitoringTestPlugin{id: "discord"}); err != nil {
		t.Fatalf("register discord: %v", err)
	}

	s := &Server{channels: reg}
	if got := s.expectedWatchdogChannelCount(); got != 2 {
		t.Fatalf("expected watchdog channel count=2 without lifecycle, got %d", got)
	}
}
