package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestDetectMisclassificationsTemplateOffSwitchOmitsReasoningEffort(t *testing.T) {
	var body map[string]any
	var handlerErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			handlerErr = fmt.Errorf("path = %q, want /chat/completions", r.URL.Path)
			http.Error(w, handlerErr.Error(), http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			handlerErr = fmt.Errorf("decode request: %w", err)
			http.Error(w, handlerErr.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[]"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	wd := &WikiDreamer{
		client: llm.NewClient(srv.URL, ""),
		model:  "deepseek-v4-flash",
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		llmExtraBody: jsonObject{
			"chat_template_kwargs": map[string]any{"thinking": false},
		},
	}

	findings := wd.detectMisclassifications(context.Background(), map[string]IndexEntry{
		"인물/호수.md": {Title: "호수", Category: "인물", Summary: "장소"},
	})
	if handlerErr != nil {
		t.Fatal(handlerErr)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none from [] response", findings)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("request included reasoning_effort despite template off-switch: %#v", body)
	}
	kwargs, ok := body["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs = %#v, want object", body["chat_template_kwargs"])
	}
	if got := kwargs["thinking"]; got != false {
		t.Fatalf("chat_template_kwargs.thinking = %#v, want false", got)
	}
}

// TestDetectMisclassifications_RefusesMovesOnLayoutManagedAndUnknownPaths pins
// the guard that stopped the dream verify pass from shredding the deal ledger:
// a topic verdict may relocate a flat page, but never a path the layout owns
// (프로젝트/거래/…, project slots), never a `deal` page, and never a path the
// prompt snapshot does not contain (the model returned already-moved paths and
// each cycle retried the ENOENT move). Refused verdicts stay advisory.
func TestDetectMisclassifications_RefusesMovesOnLayoutManagedAndUnknownPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[` +
			`{\"path\":\"프로젝트/거래/김운.md\",\"currentCategory\":\"프로젝트\",\"correctCategory\":\"인물\",\"confidence\":\"high\",\"reason\":\"사람 이름\"},` +
			`{\"path\":\"프로젝트/거래/정종호.md\",\"currentCategory\":\"프로젝트\",\"correctCategory\":\"인물\",\"confidence\":\"high\",\"reason\":\"사람 이름\"},` +
			`{\"path\":\"프로젝트/pl1-bbw-wnd-001/spa.md\",\"currentCategory\":\"프로젝트\",\"correctCategory\":\"기타\",\"confidence\":\"high\",\"reason\":\"계약 문서\"},` +
			`{\"path\":\"기타/김부장.md\",\"currentCategory\":\"기타\",\"correctCategory\":\"인물\",\"confidence\":\"high\",\"reason\":\"사람 이름\"}` +
			`]"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	wd := &WikiDreamer{
		client: llm.NewClient(srv.URL, ""),
		model:  "deepseek-v4-flash",
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	// 김운 is deliberately absent from the snapshot: an already-moved path.
	entries := map[string]IndexEntry{
		"프로젝트/거래/정종호.md":              {Title: "정종호", Category: "프로젝트", Type: "deal"},
		"프로젝트/pl1-bbw-wnd-001/spa.md": {Title: "보배 SPA", Category: "프로젝트", Type: "entity"},
		"기타/김부장.md":                   {Title: "김부장", Category: "기타", Type: "entity"},
	}

	findings := wd.detectMisclassifications(context.Background(), entries)
	if len(findings) != 4 {
		t.Fatalf("findings = %d, want 4 (all verdicts stay advisory-or-fix)", len(findings))
	}
	fixes := map[string]*verifyFix{}
	for _, f := range findings {
		fixes[f.PageA] = f.Fix
	}
	for _, refused := range []string{"프로젝트/거래/김운.md", "프로젝트/거래/정종호.md", "프로젝트/pl1-bbw-wnd-001/spa.md"} {
		if fix := fixes[refused]; fix != nil {
			t.Errorf("%s carries an auto-move fix %+v, want advisory only", refused, fix)
		}
	}
	if fix := fixes["기타/김부장.md"]; fix == nil || fix.Kind != "move" || fix.NewPath != "인물/김부장.md" {
		t.Errorf("flat page fix = %+v, want move to 인물/김부장.md", fix)
	}
}
