package enginecontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeFleet is st_production.py on the head: a selection, a state, the profiles.
type fakeFleet struct {
	selected, serving, phase string
	calls                    [][]string
	failSelect               bool
}

func (f *fakeFleet) run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "show":
		return []byte(`{"selected": "` + f.selected + `", "default": "glm53",
		  "selection": {"by": "deneb", "at": 1789800000.5, "note": "why"},
		  "state": {"serving": "` + f.serving + `", "wanted": "` + f.selected + `", "phase": "` + f.phase + `", "detail": "", "at": 1789800010},
		  "profiles": {"qwen38": {"model": "qwen3.8-flash-next", "container": "st-qwen38"},
		               "glm53": {"model": "glm-5.3-flash", "container": "st-glm53"}}}`), nil
	case "select":
		if f.failSelect {
			return nil, errors.New("ssh: connection refused")
		}
		f.selected, f.phase = args[1], "switching"
		return []byte(`{}`), nil
	}
	return nil, errors.New("unexpected " + args[0])
}

func TestStateReadsTheFleetsAnswer(t *testing.T) {
	f := &fakeFleet{selected: "glm53", serving: "glm53", phase: "serving"}
	s, err := New("choiceoh@srv2", f.run).State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Selected != "glm53" || s.Serving != "glm53" || !s.Supervised || s.Default != "glm53" {
		t.Fatalf("state = %+v", s)
	}
	if len(s.Profiles) != 2 || s.Profiles[0].Name != "glm53" || s.ModelOf("qwen38") != "qwen3.8-flash-next" {
		t.Fatalf("profiles = %+v, want both, sorted", s.Profiles)
	}
	if s.ChosenBy != "deneb" || s.ChosenAt.UnixMilli() != 1789800000500 {
		t.Errorf("selection = %q at %v", s.ChosenBy, s.ChosenAt)
	}
}

// The engine screen polls every 15 s; the head is asked at most every stateTTL.
func TestStateIsCachedAndRefreshIsNot(t *testing.T) {
	f := &fakeFleet{selected: "glm53", serving: "glm53", phase: "serving"}
	c := New("h", f.run)
	now := time.Unix(1_000_000, 0)
	c.now = func() time.Time { return now }
	for range 3 {
		if _, err := c.State(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d, want one ssh for three reads inside the TTL", len(f.calls))
	}
	now = now.Add(stateTTL)
	_, _ = c.State(context.Background())
	_, _ = c.Refresh(context.Background())
	if len(f.calls) != 3 {
		t.Fatalf("calls = %d, want a re-ask after the TTL and on every Refresh", len(f.calls))
	}
}

func TestSelectWritesOnlyAProfileTheFleetOffers(t *testing.T) {
	f := &fakeFleet{selected: "glm53", serving: "glm53", phase: "serving"}
	c := New("h", f.run)

	s, err := c.Select(context.Background(), "qwen38", "deneb", "Deneb 앱에서 선택")
	if err != nil {
		t.Fatal(err)
	}
	if s.Selected != "qwen38" || s.Phase != "switching" {
		t.Fatalf("after select: %+v", s)
	}
	last := f.calls[len(f.calls)-2]
	if strings.Join(last, " ") != "select qwen38 --by deneb --note Deneb 앱에서 선택" {
		t.Fatalf("select call = %q", last)
	}

	for _, bad := range []string{"qwen39", "qwen38; rm -rf ~", ""} {
		before := len(f.calls)
		if _, err := c.Select(context.Background(), bad, "deneb", ""); !errors.Is(err, ErrUnknownProfile) {
			t.Errorf("Select(%q) err = %v, want ErrUnknownProfile", bad, err)
		}
		for _, call := range f.calls[before:] {
			if call[0] == "select" {
				t.Errorf("Select(%q) wrote a selection", bad)
			}
		}
	}

	// Choosing what is chosen writes nothing.
	before := len(f.calls)
	if _, err := c.Select(context.Background(), "qwen38", "deneb", ""); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != before+1 || f.calls[before][0] != "show" {
		t.Errorf("re-choosing the chosen profile made calls %v", f.calls[before:])
	}
}

func TestAWriteThatFailsSaysSoAndChangesNothing(t *testing.T) {
	f := &fakeFleet{selected: "glm53", serving: "glm53", phase: "serving", failSelect: true}
	s, err := New("h", f.run).Select(context.Background(), "qwen38", "deneb", "")
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("err = %v, want the transport's error", err)
	}
	if s.Selected != "glm53" {
		t.Errorf("state after a failed write = %q", s.Selected)
	}
}

// A supervisor from before the selection publishes no state: the choice is
// readable, but nobody says it took.
func TestAnUnsupervisedFleetIsSaidSo(t *testing.T) {
	f := &fakeFleet{selected: "glm53"}
	s, err := New("h", f.run).State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Supervised {
		t.Errorf("no phase published, yet Supervised: %+v", s)
	}
}

func TestShellQuoteSurvivesTheRemoteShell(t *testing.T) {
	if got := shellQuote("it's a note; $(x)"); got != `'it'\''s a note; $(x)'` {
		t.Errorf("shellQuote = %s", got)
	}
}

func TestFromEnvFindsTheHeadInTheEngineURL(t *testing.T) {
	t.Setenv(sshTargetEnv, "")
	c := FromEnv([]string{"http://100.125.220.117:8000/metrics"})
	if c == nil || !strings.HasSuffix(c.Target(), "@100.125.220.117") && c.Target() != "100.125.220.117" {
		t.Fatalf("target = %q", c.Target())
	}
	t.Setenv(sshTargetEnv, "choiceoh@10.10.10.2")
	if got := FromEnv(nil).Target(); got != "choiceoh@10.10.10.2" {
		t.Errorf("explicit target = %q", got)
	}
	t.Setenv(sshTargetEnv, "off")
	if FromEnv([]string{"http://100.125.220.117:8000/metrics"}) != nil {
		t.Error("off must disable the control")
	}
	t.Setenv(sshTargetEnv, "")
	if FromEnv(nil) != nil {
		t.Error("no engine, no control")
	}
}
