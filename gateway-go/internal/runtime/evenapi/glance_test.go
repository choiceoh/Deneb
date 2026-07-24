package evenapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFormatGlanceComposesLines(t *testing.T) {
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.Local)
	text := FormatGlance(now, GlanceSources{
		Events: func(time.Time) []GlanceEvent {
			return []GlanceEvent{{
				Summary: "주간회의",
				Start:   now.Add(5 * time.Hour),
				End:     now.Add(6 * time.Hour),
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
	if !strings.Contains(text, "다음") || !strings.Contains(text, "주간회의") {
		t.Fatalf("missing event line: %q", text)
	}
	if !strings.Contains(text, "긴급") || !strings.Contains(text, "공문") {
		t.Fatalf("missing urgent line: %q", text)
	}
	if !strings.Contains(text, "할 일") || !strings.Contains(text, "견적") {
		t.Fatalf("missing todo line: %q", text)
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
	if !strings.Contains(text, "다음") || !strings.Contains(text, "저녁 회식") {
		t.Fatalf("missing next event: %q", text)
	}
	if !strings.Contains(text, "지난 할 일") || !strings.Contains(text, "어제 미완") {
		t.Fatalf("missing overdue todo: %q", text)
	}
}

func TestFormatGlanceEmpty(t *testing.T) {
	got := FormatGlance(time.Now(), GlanceSources{})
	if got != "지금 볼 일정·긴급·할 일은 없어요." {
		t.Fatalf("got %q", got)
	}
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
