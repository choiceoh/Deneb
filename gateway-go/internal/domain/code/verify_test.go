package code

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectKind(t *testing.T) {
	cases := []struct {
		markers []string
		want    ProjectKind
	}{
		{[]string{"go.mod"}, KindGo},
		{[]string{"package.json"}, KindNode},
		{[]string{"Cargo.toml"}, KindRust},
		{[]string{"pyproject.toml"}, KindPython},
		{[]string{"requirements.txt"}, KindPython},
		{[]string{"Makefile"}, KindMake},
		{[]string{"README.md"}, KindUnknown},
		// go.mod wins over package.json (a Go repo with tooling configs).
		{[]string{"go.mod", "package.json"}, KindGo},
	}
	for _, c := range cases {
		set := map[string]bool{}
		for _, m := range c.markers {
			set[m] = true
		}
		got := detectKind(func(rel string) bool { return set[rel] })
		if got != c.want {
			t.Errorf("detectKind(%v) = %q, want %q", c.markers, got, c.want)
		}
	}
}

func TestVerifyPlan(t *testing.T) {
	if p := verifyPlan(KindGo); len(p) != 2 || p[0].cmd() != "go build ./..." {
		t.Errorf("go plan = %+v", p)
	}
	if p := verifyPlan(KindMake); len(p) != 1 || p[0].cmd() != "make" {
		t.Errorf("make plan = %+v", p)
	}
	if p := verifyPlan(KindUnknown); p != nil {
		t.Errorf("unknown plan should be nil, got %+v", p)
	}
}

func TestVerify_GoPasses(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "go.mod")
	m := &Manager{Runner: &fakeRunner{}}

	res, err := m.Verify(context.Background(), dir)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Kind != KindGo || !res.Passed {
		t.Errorf("want go+passed, got kind=%q passed=%v", res.Kind, res.Passed)
	}
	if len(res.Steps) != 2 || !res.Steps[0].OK || res.Steps[0].Label != "빌드" {
		t.Errorf("steps = %+v", res.Steps)
	}
}

func TestVerify_StopsAtFirstFailure(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "go.mod")
	// Fail the test step (args[0] == "test"); build (args[0] == "build") passes.
	m := &Manager{Runner: &fakeRunner{fail: map[string]bool{"test": true}}}

	res, _ := m.Verify(context.Background(), dir)
	if res.Passed {
		t.Error("a failing test step must fail verification")
	}
	// build (step 0) ran ok; test (step 1) failed → both recorded, stop after test.
	if len(res.Steps) != 2 || !res.Steps[0].OK || res.Steps[1].OK {
		t.Errorf("steps = %+v", res.Steps)
	}
}

func TestVerify_UnknownToolchain(t *testing.T) {
	dir := t.TempDir()
	writeMarker(t, dir, "README.md")
	m := &Manager{Runner: &fakeRunner{}}

	res, _ := m.Verify(context.Background(), dir)
	if res.Kind != KindUnknown || res.Passed || len(res.Steps) != 0 {
		t.Errorf("unknown toolchain: kind=%q passed=%v steps=%d", res.Kind, res.Passed, len(res.Steps))
	}
}

func TestVerifyPlan_NodeIgnoresScripts(t *testing.T) {
	p := verifyPlan(KindNode)
	if len(p) == 0 || p[0].cmd() != "npm install --no-audit --no-fund --ignore-scripts" {
		t.Errorf("node install step must use --ignore-scripts: %+v", p)
	}
}

func TestBoundedOutput_KeepsHeadAndTail(t *testing.T) {
	// >4000 runes: keep head + tail and drop the middle so end-of-output build
	// errors survive truncation.
	big := strings.Repeat("h", 2000) + "MIDDLE" + strings.Repeat("t", 3000) + "TAIL_ERROR"
	out := boundedOutput([]byte(big), nil)
	if !strings.HasPrefix(out, "hhh") {
		t.Error("should keep the head")
	}
	if !strings.HasSuffix(out, "TAIL_ERROR") {
		t.Error("should keep the tail (where build errors land)")
	}
	if strings.Contains(out, "MIDDLE") {
		t.Error("the middle should be dropped")
	}
	if !strings.Contains(out, "중략") {
		t.Error("should mark the elision")
	}
}

func writeMarker(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
