package hub

import (
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
)

func quietRPCLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func completeHubConfig() HubConfig {
	return HubConfig{
		Broadcaster:    events.NewBroadcaster(),
		GatewaySubs:    new(events.GatewayEventSubscriptions),
		Sessions:       session.NewManager(),
		Processes:      new(process.Manager),
		JobTracker:     agent.NewJobTracker(quietRPCLogger()),
		CronService:    new(cron.Service),
		CronPersistLog: new(cron.PersistentRunLog),
		Approvals:      approval.NewStore(),
		Skills:         skills.NewRegistry(),
		Logger:         quietRPCLogger(),
		Version:        "v-test",
	}
}

func TestGatewayHubAccessorsPreserveConfiguredDependencies(t *testing.T) {
	cfg := completeHubConfig()
	h := NewGatewayHub(cfg)
	if h == nil || h.Phase() != PhaseInit {
		t.Fatalf("hub/phase = %p/%d", h, h.Phase())
	}
	if h.Broadcaster() != cfg.Broadcaster || h.GatewaySubs() != cfg.GatewaySubs {
		t.Fatal("event accessors changed dependencies")
	}
	if h.Sessions() != cfg.Sessions || h.Processes() != cfg.Processes || h.JobTracker() != cfg.JobTracker {
		t.Fatal("runtime accessors changed dependencies")
	}
	if h.CronService() != cfg.CronService || h.CronPersistLog() != cfg.CronPersistLog {
		t.Fatal("cron accessors changed dependencies")
	}
	if h.Approvals() != cfg.Approvals || h.Skills() != cfg.Skills {
		t.Fatal("workflow accessors changed dependencies")
	}
	if h.Logger() != cfg.Logger || h.Version() != "v-test" {
		t.Fatalf("metadata = %p/%q", h.Logger(), h.Version())
	}
	if h.Opt == nil {
		t.Fatal("Opt bag not initialized")
	}

	wikiStore := new(wiki.Store)
	contactsStore := new(contacts.Store)
	insightsEngine := new(insights.Engine)
	h.Opt.WikiStore = wikiStore
	h.Opt.ContactsStore = contactsStore
	h.Opt.Insights = insightsEngine
	if h.Opt.WikiStore != wikiStore || h.Opt.ContactsStore != contactsStore || h.Opt.Insights != insightsEngine {
		t.Fatal("optional Opt fields did not preserve pointers")
	}
	if err := h.Validate(); err != nil {
		t.Fatalf("complete hub validation: %v", err)
	}
}

func TestGatewayHubValidateReportsAllMissingDependencies(t *testing.T) {
	var nilHub *GatewayHub
	if err := nilHub.Validate(); err == nil || err.Error() != "gatewayHub is nil" {
		t.Fatalf("nil hub validation = %v", err)
	}
	h := NewGatewayHub(HubConfig{})
	err := h.Validate()
	if err == nil {
		t.Fatal("empty hub passed validation")
	}
	wantNames := []string{"Broadcaster", "GatewaySubs", "Sessions", "Processes", "JobTracker", "CronService", "Approvals", "Skills", "Logger"}
	for _, name := range wantNames {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("validation error %q missing %q", err, name)
		}
	}
	for _, optional := range []string{"CronPersistLog", "Chat", "Version", "WikiStore", "ContactsStore", "Insights"} {
		if strings.Contains(err.Error(), optional) {
			t.Errorf("optional dependency %q reported missing: %v", optional, err)
		}
	}
}

func TestGatewayHubValidateReportsMissingDependencyIndividually(t *testing.T) {
	tests := []struct {
		name string
		drop func(*HubConfig)
	}{
		{name: "Broadcaster", drop: func(c *HubConfig) { c.Broadcaster = nil }},
		{name: "GatewaySubs", drop: func(c *HubConfig) { c.GatewaySubs = nil }},
		{name: "Sessions", drop: func(c *HubConfig) { c.Sessions = nil }},
		{name: "Processes", drop: func(c *HubConfig) { c.Processes = nil }},
		{name: "JobTracker", drop: func(c *HubConfig) { c.JobTracker = nil }},
		{name: "CronService", drop: func(c *HubConfig) { c.CronService = nil }},
		{name: "Approvals", drop: func(c *HubConfig) { c.Approvals = nil }},
		{name: "Skills", drop: func(c *HubConfig) { c.Skills = nil }},
		{name: "Logger", drop: func(c *HubConfig) { c.Logger = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := completeHubConfig()
			tt.drop(&cfg)
			err := NewGatewayHub(cfg).Validate()
			if err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("validation = %v", err)
			}
			for _, other := range tests {
				if other.name != tt.name && strings.Contains(err.Error(), other.name) {
					t.Errorf("validation unexpectedly reports %q: %v", other.name, err)
				}
			}
		})
	}
}

func expectPanicContaining(t *testing.T, substr string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(strings.TrimSpace(toString(r)), substr) {
			t.Fatalf("panic = %#v, want containing %q", r, substr)
		}
	}()
	fn()
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return ""
}

func TestGatewayHubAdvancePhaseRejectsOutOfOrderTransitions(t *testing.T) {
	h := NewGatewayHub(completeHubConfig())
	expectPanicContaining(t, "expected phase 1", func() { h.AdvancePhase(PhaseSession) })
	expectPanicContaining(t, "expected phase 1", func() { h.AdvancePhase(PhaseInit) })
	if h.Phase() != PhaseInit {
		t.Fatalf("invalid transition changed phase to %d", h.Phase())
	}
	h.AdvancePhase(PhaseEarly)
	if h.Phase() != PhaseEarly {
		t.Fatalf("phase = %d", h.Phase())
	}
	h.AdvancePhase(PhaseSession)
	h.AdvancePhase(PhaseLate)
	if h.Phase() != PhaseLate {
		t.Fatalf("phase = %d", h.Phase())
	}
	expectPanicContaining(t, "expected phase 4", func() { h.AdvancePhase(PhaseLate + 1) })
	if h.Phase() != PhaseLate {
		t.Fatal("out-of-range transition changed phase")
	}
}

type hubSubscriber struct {
	id       string
	mu       sync.Mutex
	received [][]byte
}

func (s *hubSubscriber) ID() string            { return s.id }
func (s *hubSubscriber) IsAuthenticated() bool { return true }
func (s *hubSubscriber) BufferedAmount() int64 { return 0 }
func (s *hubSubscriber) SendEvent(data []byte) error {
	s.mu.Lock()
	s.received = append(s.received, append([]byte(nil), data...))
	s.mu.Unlock()
	return nil
}

func TestGatewayHubBroadcastDeliversOrReturnsErrorWhenUnavailable(t *testing.T) {
	var nilHub *GatewayHub
	empty := events.EventPayload{}
	if sent, errs := nilHub.Broadcast("event", empty); sent != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "not available") {
		t.Fatalf("nil hub broadcast = %d/%v", sent, errs)
	}
	if sent, errs := NewGatewayHub(HubConfig{}).Broadcast("event", empty); sent != 0 || len(errs) != 1 {
		t.Fatalf("missing broadcaster = %d/%v", sent, errs)
	}
	cfg := completeHubConfig()
	sub := &hubSubscriber{id: "conn"}
	cfg.Broadcaster.Subscribe(sub, events.Filter{})
	h := NewGatewayHub(cfg)
	if sent, errs := h.Broadcast("hub.event", events.PayloadFromRaw([]byte(`{"ok":true}`))); sent != 1 || len(errs) != 0 {
		t.Fatalf("broadcast = %d/%v", sent, errs)
	}
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if len(sub.received) != 1 || !strings.Contains(string(sub.received[0]), `"event":"hub.event"`) {
		t.Fatalf("received = %q", sub.received)
	}
}
