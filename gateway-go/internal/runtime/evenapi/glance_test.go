package evenapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildGlancePages(t *testing.T) {
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.Local)
	bundle := BuildGlance(now, GlanceSources{
		Events: func(time.Time) []GlanceEvent {
			return []GlanceEvent{{
				Summary: "주간회의",
				Start:   now.Add(30 * time.Minute),
				End:     now.Add(90 * time.Minute),
			}, {
				Summary: "저녁 회식",
				Start:   now.Add(10 * time.Hour),
				End:     now.Add(12 * time.Hour),
			}}
		},
		Urgent: func(time.Time) []GlanceUrgent {
			return []GlanceUrgent{
				{Title: "공문 회신 마감", Priority: 4},
				{Title: "결재 미결", Priority: 3},
			}
		},
		Todos: func(time.Time) []GlanceTodo {
			return []GlanceTodo{
				{Title: "견적 검토", Due: now.Add(2 * time.Hour)},
				{Title: "현장 전화"},
			}
		},
	})
	if !strings.Contains(bundle.Text, "09:00") {
		t.Fatalf("home missing clock: %q", bundle.Text)
	}
	if !strings.Contains(bundle.Text, "분 후") || !strings.Contains(bundle.Text, "주간회의") {
		t.Fatalf("home missing relative event: %q", bundle.Text)
	}
	if !strings.Contains(bundle.Text, "긴급") || !strings.Contains(bundle.Text, "공문") {
		t.Fatalf("home missing urgent: %q", bundle.Text)
	}
	if !strings.Contains(bundle.Text, "할 일") || !strings.Contains(bundle.Text, "견적") {
		t.Fatalf("home missing todo: %q", bundle.Text)
	}
	if len(bundle.Pages) != 4 {
		t.Fatalf("pages=%d want 4", len(bundle.Pages))
	}
	byID := map[string]GlancePage{}
	for _, p := range bundle.Pages {
		byID[p.ID] = p
	}
	for _, id := range []string{"home", "cal", "urgent", "todo"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing page %s", id)
		}
	}
	if !strings.Contains(byID["cal"].Text, "저녁 회식") {
		t.Fatalf("cal page: %q", byID["cal"].Text)
	}
	if !strings.Contains(byID["urgent"].Text, "결재") {
		t.Fatalf("urgent page: %q", byID["urgent"].Text)
	}
	if !strings.Contains(byID["todo"].Text, "현장") {
		t.Fatalf("todo page: %q", byID["todo"].Text)
	}
	if bundle.Text != byID["home"].Text {
		t.Fatalf("text should equal home page")
	}
}

func TestFormatGlanceShowsCurrentAndOverdue(t *testing.T) {
	now := time.Date(2026, 7, 24, 14, 0, 0, 0, time.Local)
	text := FormatGlance(now, GlanceSources{
		Events: func(time.Time) []GlanceEvent {
			return []GlanceEvent{{
				Summary: "고객 미팅",
				Start:   now.Add(-30 * time.Minute),
				End:     now.Add(30 * time.Minute),
			}, {
				Summary: "저녁 회식",
				Start:   now.Add(4 * time.Hour),
				End:     now.Add(6 * time.Hour),
			}}
		},
		Todos: func(time.Time) []GlanceTodo {
			return []GlanceTodo{{
				Title: "어제 미완",
				Due:   now.Add(-24 * time.Hour),
			}}
		},
	})
	if !strings.Contains(text, "지금") || !strings.Contains(text, "고객 미팅") {
		t.Fatalf("missing current event: %q", text)
	}
	if !strings.Contains(text, "지난 할 일") || !strings.Contains(text, "어제 미완") {
		t.Fatalf("missing overdue todo: %q", text)
	}
}

func TestFormatGlanceEmpty(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 15, 0, 0, time.Local)
	got := FormatGlance(now, GlanceSources{})
	if !strings.Contains(got, "10:15") {
		t.Fatalf("missing clock: %q", got)
	}
	if !strings.Contains(got, "지금 볼 일정·긴급·할 일은 없어요.") {
		t.Fatalf("got %q", got)
	}
	bundle := BuildGlance(now, GlanceSources{})
	if !strings.Contains(pageByID(bundle, "cal").Text, "없어요") {
		t.Fatalf("cal empty: %q", pageByID(bundle, "cal").Text)
	}
	if !strings.Contains(pageByID(bundle, "urgent").Text, "없어요") {
		t.Fatalf("urgent empty: %q", pageByID(bundle, "urgent").Text)
	}
	if !strings.Contains(pageByID(bundle, "todo").Text, "없어요") {
		t.Fatalf("todo empty: %q", pageByID(bundle, "todo").Text)
	}
}

func pageByID(b GlanceBundle, id string) GlancePage {
	for _, p := range b.Pages {
		if p.ID == id {
			return p
		}
	}
	return GlancePage{}
}

func TestGlanceHTTPAndCache(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.Local)
	calls := 0
	h := New(Config{
		Token: "secret",
		Now:   func() time.Time { return now },
		Sources: GlanceSources{
			Events: func(time.Time) []GlanceEvent {
				calls++
				return []GlanceEvent{{Summary: "미팅", Start: now.Add(time.Hour), End: now.Add(2 * time.Hour)}}
			},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/even/glance", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.Glance(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	text, _ := body["text"].(string)
	if !strings.Contains(text, "미팅") {
		t.Fatalf("text=%q", text)
	}
	pages, ok := body["pages"].([]any)
	if !ok || len(pages) != 4 {
		t.Fatalf("pages=%v", body["pages"])
	}
	if body["cached"] != false {
		t.Fatalf("first response should be uncached: %+v", body)
	}

	rec2 := httptest.NewRecorder()
	h.Glance(rec2, req)
	var body2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &body2)
	if body2["cached"] != true {
		t.Fatalf("second response should hit cache: %+v", body2)
	}
	if calls != 1 {
		t.Fatalf("source calls=%d want 1", calls)
	}

	reqFresh := httptest.NewRequest(http.MethodGet, "/api/even/glance?fresh=1", nil)
	reqFresh.Header.Set("Authorization", "Bearer secret")
	rec3 := httptest.NewRecorder()
	h.Glance(rec3, reqFresh)
	if calls != 2 {
		t.Fatalf("fresh bypass calls=%d want 2", calls)
	}
}

func TestStatusHTTP(t *testing.T) {
	h := New(Config{Token: "secret", Chat: &stubChat{ready: true}})
	req := httptest.NewRequest(http.MethodGet, "/api/even/status", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.Status(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["ok"] != true || body["chatReady"] != true {
		t.Fatalf("body=%+v", body)
	}
}

func TestBuildGlanceRicherHomeAndEmptyFlags(t *testing.T) {
	now := time.Date(2026, 7, 24, 14, 0, 0, 0, time.Local) // Friday
	bundle := BuildGlance(now, GlanceSources{
		Events: func(time.Time) []GlanceEvent {
			return []GlanceEvent{{
				Summary: "고객 미팅",
				Start:   now.Add(-30 * time.Minute),
				End:     now.Add(30 * time.Minute),
			}}
		},
		Urgent: func(time.Time) []GlanceUrgent {
			return []GlanceUrgent{{Title: "공문 회신", Preview: "오늘 17시까지", Priority: 4}}
		},
		Todos: func(time.Time) []GlanceTodo {
			return []GlanceTodo{{Title: "견적 검토", Due: now.Add(time.Hour)}}
		},
	})
	if !strings.Contains(bundle.Text, "금") || !strings.Contains(bundle.Text, "14:00") {
		t.Fatalf("clock header: %q", bundle.Text)
	}
	if !strings.Contains(bundle.Text, "일정 1") || !strings.Contains(bundle.Text, "긴급 1") {
		t.Fatalf("counts: %q", bundle.Text)
	}
	if !strings.Contains(bundle.Text, "종료 30분") {
		t.Fatalf("remaining: %q", bundle.Text)
	}
	byID := map[string]GlancePage{}
	for _, p := range bundle.Pages {
		byID[p.ID] = p
	}
	if byID["home"].Empty || byID["cal"].Empty || byID["urgent"].Empty || byID["todo"].Empty {
		t.Fatalf("unexpected empty flags: %+v", byID)
	}
	if !strings.Contains(byID["urgent"].Text, "오늘 17시") {
		t.Fatalf("urgent preview: %q", byID["urgent"].Text)
	}

	empty := BuildGlance(now, GlanceSources{})
	if !empty.Pages[0].Empty || !empty.Pages[1].Empty || !empty.Pages[2].Empty || !empty.Pages[3].Empty {
		t.Fatalf("want all empty: %+v", empty.Pages)
	}
}
