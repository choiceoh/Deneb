// recall_longitudinal_test.go — E1 scaffold: seed a fact, query, supersede, re-query.
//
// Measures latest-state hit rate and cross-subject isolation after the
// HealthClaw-style induction/recall gates (M3/M4/M6). Lexical path only (CI).
package recall

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

type longProbe struct {
	name     string
	question string
	want     string // must appear in formatted recall (empty = no positive assert)
	forbid   string // must NOT appear
}

func TestRecallLongitudinal_LatestStateAndSubjectGate(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	old := &wiki.Page{
		Meta: wiki.Frontmatter{
			ID: "pref-dinner-old", Title: "저녁 선호 구버전", Category: "사용자",
			Summary: "저녁은 파스타 선호", Importance: 0.9, SubjectID: "self",
		},
		Body: "운영자 저녁 선호는 파스타다. 파스타 파스타.",
	}
	cur := &wiki.Page{
		Meta: wiki.Frontmatter{
			ID: "pref-dinner", Title: "저녁 선호", Category: "사용자",
			Summary: "저녁은 비건 한식 선호", Importance: 0.9, SubjectID: "self",
		},
		Body: "운영자 저녁 선호는 비건 한식이다.",
	}
	other := &wiki.Page{
		Meta: wiki.Frontmatter{
			ID: "spouse-allergy", Title: "배우자 알레르기", Category: "사용자",
			Summary: "배우자 해산물 알레르기", Importance: 0.9, SubjectID: "other:배우자",
		},
		Body: "배우자는 해산물 알레르기가 있다.",
	}
	if err := store.WritePage("사용자/dinner-old.md", old); err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("사용자/dinner.md", cur); err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("사용자/spouse-allergy.md", other); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkSuperseded("사용자/dinner-old", "사용자/dinner.md"); err != nil {
		t.Fatal(err)
	}

	probes := []longProbe{
		{
			name:     "latest-dinner",
			question: "내 저녁 선호 뭐였지 기억해줘",
			want:     "비건",
			forbid:   "파스타",
		},
		{
			name:     "self-query-excludes-spouse",
			question: "내 알레르기 기억나?",
			forbid:   "해산물",
		},
		{
			name:     "named-spouse-allows",
			question: "배우자 해산물 알레르기 기억나?",
			want:     "해산물",
		},
	}

	hits, total := 0, 0
	for _, p := range probes {
		total++
		out, _ := Build(context.Background(),
			Params{SessionKey: "client:main", Message: p.question},
			Deps{Wiki: store}, nil)
		ok := true
		if p.want != "" && !strings.Contains(out, p.want) {
			ok = false
			t.Errorf("%s: want %q in recall, got %q", p.name, p.want, out)
		}
		if p.forbid != "" && strings.Contains(out, p.forbid) {
			ok = false
			t.Errorf("%s: forbid %q still present:\n%s", p.name, p.forbid, out)
		}
		if ok {
			hits++
		}
	}

	rate := float64(hits) / float64(total)
	fmt.Printf("RECALL_LONG_HIT_RATE=%.4f hits=%d total=%d\n", rate, hits, total)
	if rate < 1.0 {
		t.Fatalf("longitudinal hit rate %.2f < 1.0", rate)
	}
}

func TestRecallLongitudinal_SubjectIDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	page := &wiki.Page{
		Meta: wiki.Frontmatter{ID: "x", Title: "t", SubjectID: "other:아내"},
		Body: "body",
	}
	raw := page.Render()
	if !strings.Contains(string(raw), "subject_id: other:아내") {
		t.Fatalf("render missing subject_id: %s", raw)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := wiki.ParsePageFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.SubjectID != "other:아내" {
		t.Fatalf("parse SubjectID=%q", got.Meta.SubjectID)
	}
}
