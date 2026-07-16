package chat

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func difficultyTestRegistry(t *testing.T, withMain2 bool) *modelrole.Registry {
	t.Helper()
	opts := modelrole.RegistryOptions{
		MainModel: "kimi/kimi-for-coding",
		Providers: map[string]modelrole.ProviderResolved{
			"kimi": {BaseURL: "http://127.0.0.1:18800", APIKey: "t", APIMode: "anthropic"},
			"zai":  {BaseURL: "https://api.z.ai/api/anthropic", APIKey: "k", APIMode: "anthropic"},
		},
	}
	if withMain2 {
		opts.Main2Model = "zai/glm-5.2"
	}
	return modelrole.NewRegistryWithOptions(slog.Default(), opts)
}

func routeParams(msg string) RunParams {
	return RunParams{SessionKey: "client:main", Message: msg}
}

// resetDifficultyCounters seeds the package-level ratio-governor counters so
// each test starts from a known split (m1 main-tier turns, m2 routed turns).
func resetDifficultyCounters(t *testing.T, m1, m2 int64) {
	t.Helper()
	mainTierTurns.Store(m1)
	main2Turns.Store(m2)
	t.Cleanup(func() {
		mainTierTurns.Store(0)
		main2Turns.Store(0)
	})
}

func TestDifficultyModelRoute_SimpleTurnRidesMain2(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	reg := difficultyTestRegistry(t, true)
	resetDifficultyCounters(t, 2, 0) // main2 under its 1/3 share → eligible

	rt := difficultyModelRoute(reg, routeParams("고마워!"), "", nil,
		modelrole.RoleMain, "kimi", "kimi-for-coding", slog.Default())
	if rt == nil {
		t.Fatal("simple turn not routed to main2")
	}
	if rt.model != "glm-5.2" || rt.providerID != "zai" || rt.client == nil {
		t.Fatalf("route = %+v", rt)
	}
	if rt.reason == "" {
		t.Fatal("route carries no reason tag")
	}
}

// The 2:1 governor: with every turn simple, the deterministic cadence routes
// exactly every third main-tier turn to main2 — main1:main2 converges to 2:1.
// Analytical/automation turns count toward main1, so scarce simple turns
// leave main2 UNDER its share (never over).
func TestDifficultyModelRoute_RatioGovernorHoldsTwoToOne(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	reg := difficultyTestRegistry(t, true)
	resetDifficultyCounters(t, 0, 0)

	var pattern []bool
	for i := 0; i < 9; i++ {
		rt := difficultyModelRoute(reg, routeParams("고마워!"), "", nil,
			modelrole.RoleMain, "kimi", "kimi-for-coding", nil)
		pattern = append(pattern, rt != nil)
	}
	want := []bool{false, false, true, false, false, true, false, false, true}
	for i := range want {
		if pattern[i] != want[i] {
			t.Fatalf("routing pattern = %v, want %v (route every 3rd)", pattern, want)
		}
	}
	if m1, m2 := mainTierTurns.Load(), main2Turns.Load(); m1 != 6 || m2 != 3 {
		t.Fatalf("counters = %d:%d, want 6:3", m1, m2)
	}
}

func TestDifficultyModelRoute_KeepsMainWhenNotSimple(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	reg := difficultyTestRegistry(t, true)
	resetDifficultyCounters(t, 0, 0)

	analytical := "이번 분기 태양광 모듈 입찰 3건의 조건을 비교 분석해서 리스크 순위로 정리하고, " +
		"각 건의 지급 조건과 하자보수 조항 차이가 마진에 미치는 영향을 추정해줘. " +
		strings.Repeat("추가 맥락. ", 40)
	if rt := difficultyModelRoute(reg, routeParams(analytical), "", nil,
		modelrole.RoleMain, "kimi", "kimi-for-coding", slog.Default()); rt != nil {
		t.Fatalf("analytical turn routed off main: %+v", rt)
	}
}

func TestDifficultyModelRoute_HeavyRecentContextKeepsMain(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	reg := difficultyTestRegistry(t, true)
	resetDifficultyCounters(t, 2, 0)

	// A long assistant reply right before a short follow-up means mid-deep-work.
	// The assembled list includes the CURRENT user message last (the heuristic
	// excludes it when scanning the tail), mirroring production assembly.
	history := []llm.Message{
		llm.NewTextMessage("user", "보고서 분석해줘"),
		llm.NewTextMessage("assistant", strings.Repeat("깊은 분석 내용. ", 400)),
		llm.NewTextMessage("user", "응 계속해"),
	}
	if rt := difficultyModelRoute(reg, routeParams("응 계속해"), "", history,
		modelrole.RoleMain, "kimi", "kimi-for-coding", slog.Default()); rt != nil {
		t.Fatalf("mid-deep-work follow-up routed off main: %+v", rt)
	}
}

func TestDifficultyModelRoute_Guards(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	reg := difficultyTestRegistry(t, true)
	resetDifficultyCounters(t, 100, 0) // ratio never the blocker in this test
	simple := "고마워!"

	cases := []struct {
		name string
		run  func() *difficultyRoute
	}{
		{"explicit model override", func() *difficultyRoute {
			p := routeParams(simple)
			p.Model = "kimi/kimi-for-coding"
			return difficultyModelRoute(reg, p, "", nil, modelrole.RoleMain, "kimi", "kimi-for-coding", nil)
		}},
		{"sub-agent session", func() *difficultyRoute {
			return difficultyModelRoute(reg, routeParams(simple), "agent:parent", nil, modelrole.RoleMain, "kimi", "kimi-for-coding", nil)
		}},
		{"automation (cron) session", func() *difficultyRoute {
			p := routeParams(simple)
			p.SessionKey = "cron:morning-letter"
			return difficultyModelRoute(reg, p, "", nil, modelrole.RoleMain, "kimi", "kimi-for-coding", nil)
		}},
		{"attachments", func() *difficultyRoute {
			p := routeParams(simple)
			p.Attachments = []toolport.ChatAttachment{{Name: "photo.jpg", MimeType: "image/jpeg"}}
			return difficultyModelRoute(reg, p, "", nil, modelrole.RoleMain, "kimi", "kimi-for-coding", nil)
		}},
		{"non-main role", func() *difficultyRoute {
			return difficultyModelRoute(reg, routeParams(simple), "", nil, modelrole.RoleVision, "kimi", "kimi-for-coding", nil)
		}},
		{"resolution is not the flagship main", func() *difficultyRoute {
			return difficultyModelRoute(reg, routeParams(simple), "", nil, modelrole.RoleMain, "zai", "glm-5-turbo", nil)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rt := tc.run(); rt != nil {
				t.Fatalf("guard failed, routed: %+v", rt)
			}
		})
	}
}

func TestDifficultyModelRoute_OffWithoutMain2OrEffortFlag(t *testing.T) {
	t.Setenv("DENEB_ADAPTIVE_EFFORT", "1")
	resetDifficultyCounters(t, 2, 0)
	noMain2 := difficultyTestRegistry(t, false)
	if rt := difficultyModelRoute(noMain2, routeParams("고마워!"), "", nil,
		modelrole.RoleMain, "kimi", "kimi-for-coding", nil); rt != nil {
		t.Fatalf("routed without main2 configured: %+v", rt)
	}

	t.Setenv("DENEB_ADAPTIVE_EFFORT", "")
	withMain2 := difficultyTestRegistry(t, true)
	if rt := difficultyModelRoute(withMain2, routeParams("고마워!"), "", nil,
		modelrole.RoleMain, "kimi", "kimi-for-coding", nil); rt != nil {
		t.Fatalf("routed with effort router off: %+v", rt)
	}
}
