package evenapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildGlanceNotificationHome(t *testing.T) {
	now := time.Date(2026, 7, 24, 9, 0, 0, 0, time.Local)
	bundle := BuildGlance(now, GlanceSources{
		Events: func(time.Time) []GlanceEvent {
			return []GlanceEvent{{
				Summary: "주간회의",
				Start:   now.Add(30 * time.Minute),
				End:     now.Add(90 * time.Minute),
			}}
		},
		Urgent: func(time.Time) []GlanceUrgent {
			return []GlanceUrgent{
				{ID: "a1", Title: "공문 회신 마감", Preview: "오늘 17시", Body: "공문 회신 마감입니다. 오늘 17시까지 회신 바랍니다.", Priority: 4, CreatedAt: now.Add(-12 * time.Minute)},
				{ID: "a2", Title: "결재 미결", Body: "결재 대기 문서가 있습니다.", Priority: 3, CreatedAt: now.Add(-2 * time.Hour)},
			}
		},
		Todos: func(time.Time) []GlanceTodo {
			return []GlanceTodo{{Title: "견적 검토", Due: now.Add(2 * time.Hour)}}
		},
	})
	if !strings.Contains(bundle.Text, "알림 2건") {
		t.Fatalf("home count: %q", bundle.Text)
	}
	if !strings.Contains(bundle.Text, "공문") || !strings.Contains(bundle.Text, "결재") {
		t.Fatalf("home alerts: %q", bundle.Text)
	}
	if !strings.Contains(bundle.Text, "분 전") {
		t.Fatalf("home age: %q", bundle.Text)
	}
	if strings.Contains(bundle.Text, "주간회의") {
		t.Fatalf("home should be notification-first, got %q", bundle.Text)
	}
	byID := map[string]GlancePage{}
	for _, p := range bundle.Pages {
		byID[p.ID] = p
	}
	for _, id := range []string{"home", "alerts", "cal", "todo"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing page %s", id)
		}
	}
	if byID["home"].Title != "알림" {
		t.Fatalf("home title=%q", byID["home"].Title)
	}
	if !strings.Contains(byID["alerts"].Text, "오늘 17시") {
		t.Fatalf("alerts preview: %q", byID["alerts"].Text)
	}
	if !strings.Contains(byID["cal"].Text, "주간회의") {
		t.Fatalf("cal page: %q", byID["cal"].Text)
	}
	if bundle.Text != byID["home"].Text {
		t.Fatalf("text should equal home")
	}
	if len(bundle.Items) != 2 {
		t.Fatalf("items=%d want 2", len(bundle.Items))
	}
	if bundle.Items[0].ID != "a1" || !strings.Contains(bundle.Items[0].Body, "17시") {
		t.Fatalf("item0=%+v", bundle.Items[0])
	}
	if bundle.Items[0].Age == "" {
		t.Fatalf("item age empty")
	}
}

func TestFormatGlanceEmptyIsNoAlerts(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 15, 0, 0, time.Local)
	got := FormatGlance(now, GlanceSources{})
	if !strings.Contains(got, "새 알림 없음") {
		t.Fatalf("got %q", got)
	}
	bundle := BuildGlance(now, GlanceSources{})
	if !bundle.Pages[0].Empty || !pageByID(bundle, "alerts").Empty {
		t.Fatalf("want home/alerts empty: %+v", bundle.Pages)
	}
}

func TestFormatGlanceEmptyHintsCalTodo(t *testing.T) {
	now := time.Date(2026, 7, 24, 14, 0, 0, 0, time.Local)
	text := FormatGlance(now, GlanceSources{
		Events: func(time.Time) []GlanceEvent {
			return []GlanceEvent{{
				Summary: "고객 미팅",
				Start:   now.Add(-30 * time.Minute),
				End:     now.Add(30 * time.Minute),
			}}
		},
		Todos: func(time.Time) []GlanceTodo {
			return []GlanceTodo{{Title: "어제 미완", Due: now.Add(-24 * time.Hour)}}
		},
	})
	if !strings.Contains(text, "새 알림 없음") {
		t.Fatalf("want no-alerts home: %q", text)
	}
	if !strings.Contains(text, "일정") || !strings.Contains(text, "할 일") {
		t.Fatalf("want cal/todo hint: %q", text)
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
			Urgent: func(time.Time) []GlanceUrgent {
				calls++
				return []GlanceUrgent{{ID: "m1", Title: "새 메일", Body: "중요 메일 본문", Priority: 3, CreatedAt: now.Add(-5 * time.Minute)}}
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
	if !strings.Contains(text, "새 메일") {
		t.Fatalf("text=%q", text)
	}
	pages, ok := body["pages"].([]any)
	if !ok || len(pages) != 4 {
		t.Fatalf("pages=%v", body["pages"])
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items=%v", body["items"])
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
