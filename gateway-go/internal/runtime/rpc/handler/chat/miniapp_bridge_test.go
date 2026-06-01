package chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestMiniappCaptureContacts_Success(t *testing.T) {
	var savedPayload, enrichPayload []byte
	deps := Deps{
		SaveContacts: func(b []byte) (int, error) {
			savedPayload = b
			return 3, nil
		},
		EnrichContacts: func(b []byte) (wiki.ContactEnrichResult, error) {
			enrichPayload = b
			return wiki.ContactEnrichResult{Total: 3, Matched: 1, Updated: 1, Names: []string{"김민준"}}, nil
		},
	}
	handler := handleMiniappCaptureContacts(deps)

	req := &protocol.RequestFrame{
		ID:     "c-1",
		Params: json.RawMessage(`{"contacts":[{"name":"김민준 부장","phones":["010-1234-5678"]}]}`),
	}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("got error: %+v", resp.Error)
	}
	// Both deps must receive a {"contacts":[...]} envelope, valid JSON.
	for _, got := range [][]byte{savedPayload, enrichPayload} {
		if !json.Valid(got) || !strings.HasPrefix(strings.TrimSpace(string(got)), `{"contacts":`) {
			t.Errorf("payload not wrapped as expected: %q", got)
		}
	}

	var payload struct {
		Text     string `json:"text"`
		Saved    int    `json:"saved"`
		Enriched int    `json:"enriched"`
		Matched  int    `json:"matched"`
		Total    int    `json:"total"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Saved != 3 {
		t.Errorf("saved = %d, want 3", payload.Saved)
	}
	if payload.Enriched != 1 || payload.Matched != 1 {
		t.Errorf("enriched/matched = %d/%d, want 1/1", payload.Enriched, payload.Matched)
	}
	if !strings.Contains(payload.Text, "저장") {
		t.Errorf("summary should headline the save, got %q", payload.Text)
	}
	if !strings.Contains(payload.Text, "김민준") {
		t.Errorf("summary should name the enriched person, got %q", payload.Text)
	}
}

// Save alone (no wiki) is enough to register and succeed — the wiki enrichment is
// an optional bonus.
func TestMiniappCaptureContacts_SaveOnly(t *testing.T) {
	deps := Deps{
		SaveContacts: func([]byte) (int, error) { return 2798, nil },
	}
	handler := handleMiniappCaptureContacts(deps)

	req := &protocol.RequestFrame{ID: "c-1b", Params: json.RawMessage(`{"contacts":[{"name":"X"}]}`)}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("got error: %+v", resp.Error)
	}
	var payload struct {
		Saved    int `json:"saved"`
		Enriched int `json:"enriched"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Saved != 2798 || payload.Enriched != 0 {
		t.Errorf("saved/enriched = %d/%d, want 2798/0", payload.Saved, payload.Enriched)
	}
}

func TestMiniappCaptureContacts_MissingParam(t *testing.T) {
	called := false
	deps := Deps{SaveContacts: func([]byte) (int, error) {
		called = true
		return 0, nil
	}}
	handler := handleMiniappCaptureContacts(deps)

	req := &protocol.RequestFrame{ID: "c-2", Params: json.RawMessage(`{}`)}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error for missing contacts param")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("got %+v, want MISSING_PARAM", resp.Error)
	}
	if called {
		t.Error("SaveContacts must not run when contacts param is missing")
	}
}

// A SaveContacts failure is the primary failure path and surfaces as an RPC error.
func TestMiniappCaptureContacts_SaveError(t *testing.T) {
	deps := Deps{SaveContacts: func([]byte) (int, error) {
		return 0, errors.New("contacts store unavailable")
	}}
	handler := handleMiniappCaptureContacts(deps)

	req := &protocol.RequestFrame{ID: "c-3", Params: json.RawMessage(`{"contacts":[{"name":"X"}]}`)}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error when SaveContacts fails")
	}
}

// A wiki enrichment failure must NOT fail the sync once the book is already stored
// — enrichment is best-effort.
func TestMiniappCaptureContacts_EnrichErrorTolerated(t *testing.T) {
	deps := Deps{
		SaveContacts: func([]byte) (int, error) { return 5, nil },
		EnrichContacts: func([]byte) (wiki.ContactEnrichResult, error) {
			return wiki.ContactEnrichResult{}, errors.New("wiki store unavailable")
		},
	}
	handler := handleMiniappCaptureContacts(deps)

	req := &protocol.RequestFrame{ID: "c-4", Params: json.RawMessage(`{"contacts":[{"name":"X"}]}`)}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("wiki enrichment failure must not fail the sync: %+v", resp.Error)
	}
	var payload struct {
		Saved    int `json:"saved"`
		Enriched int `json:"enriched"`
	}
	_ = json.Unmarshal(resp.Payload, &payload)
	if payload.Saved != 5 || payload.Enriched != 0 {
		t.Errorf("saved/enriched = %d/%d, want 5/0", payload.Saved, payload.Enriched)
	}
}

// contactsSummary headlines the store save; wiki enrichment, when any people were
// updated, is appended as a parenthetical bonus (with name capping/overflow).
func TestContactsSummary(t *testing.T) {
	saveOnly := contactsSummary(2798, wiki.ContactEnrichResult{Total: 2798})
	if !strings.Contains(saveOnly, "2798") || !strings.Contains(saveOnly, "저장") {
		t.Errorf("save-only summary unexpected: %q", saveOnly)
	}
	if strings.Contains(saveOnly, "위키") {
		t.Errorf("save-only summary should not mention wiki when nothing enriched: %q", saveOnly)
	}
	enriched := contactsSummary(200, wiki.ContactEnrichResult{
		Total: 200, Matched: 8, Updated: 8,
		Names: []string{"가", "나", "다", "라", "마", "바", "사", "아"},
	})
	if !strings.Contains(enriched, "200") || !strings.Contains(enriched, "저장") {
		t.Errorf("enriched summary should still headline the save: %q", enriched)
	}
	if !strings.Contains(enriched, "위키 인물 8명") {
		t.Errorf("enriched summary should report the wiki bonus: %q", enriched)
	}
	if !strings.Contains(enriched, "외 2명") {
		t.Errorf("enriched summary should cap names and show overflow, got %q", enriched)
	}
}
