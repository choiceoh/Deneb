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
				{Title: "견적 검토"},
				{Title: "현장 전화"},
			}
		},
	})
	if !strings.Contains(text, "다음 일정") || !strings.Contains(text, "주간회의") {
		t.Fatalf("missing event line: %q", text)
	}
	if !strings.Contains(text, "긴급") || !strings.Contains(text, "공문") {
		t.Fatalf("missing urgent line: %q", text)
	}
	if !strings.Contains(text, "할 일") || !strings.Contains(text, "견적") {
		t.Fatalf("missing todo line: %q", text)
	}
}

func TestFormatGlanceEmpty(t *testing.T) {
	got := FormatGlance(time.Now(), GlanceSources{})
	if got != "지금 볼 일정·긴급·할 일은 없어요." {
		t.Fatalf("got %q", got)
	}
}

func TestGlanceHTTP(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.Local)
	h := New(Config{
		Token: "secret",
		Now:   func() time.Time { return now },
		Sources: GlanceSources{
			Events: func(time.Time) []GlanceEvent {
				return []GlanceEvent{{Summary: "미팅", Start: now.Add(time.Hour)}}
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
}
