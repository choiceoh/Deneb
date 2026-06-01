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
	var gotPayload []byte
	deps := Deps{
		EnrichContacts: func(b []byte) (wiki.ContactEnrichResult, error) {
			gotPayload = b
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
	// The handler must hand EnrichContacts a {"contacts":[...]} envelope, valid JSON.
	if !json.Valid(gotPayload) || !strings.HasPrefix(strings.TrimSpace(string(gotPayload)), `{"contacts":`) {
		t.Errorf("EnrichContacts payload not wrapped as expected: %q", gotPayload)
	}

	var payload struct {
		Text    string `json:"text"`
		Matched int    `json:"matched"`
		Updated int    `json:"updated"`
		Total   int    `json:"total"`
	}
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Total != 3 || payload.Matched != 1 || payload.Updated != 1 {
		t.Errorf("counts = %d/%d/%d, want total/matched/updated 3/1/1", payload.Total, payload.Matched, payload.Updated)
	}
	if !strings.Contains(payload.Text, "김민준") {
		t.Errorf("summary should name the enriched person, got %q", payload.Text)
	}
}

func TestMiniappCaptureContacts_MissingParam(t *testing.T) {
	called := false
	deps := Deps{EnrichContacts: func([]byte) (wiki.ContactEnrichResult, error) {
		called = true
		return wiki.ContactEnrichResult{}, nil
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
		t.Error("EnrichContacts must not run when contacts param is missing")
	}
}

func TestMiniappCaptureContacts_DependencyError(t *testing.T) {
	deps := Deps{EnrichContacts: func([]byte) (wiki.ContactEnrichResult, error) {
		return wiki.ContactEnrichResult{}, errors.New("wiki store unavailable")
	}}
	handler := handleMiniappCaptureContacts(deps)

	req := &protocol.RequestFrame{ID: "c-3", Params: json.RawMessage(`{"contacts":[{"name":"X"}]}`)}
	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatal("expected error when EnrichContacts fails")
	}
}

// contactsSummary wording must adapt to the three outcomes the native client
// shows inline: nothing matched, matched-but-already-current, and enriched.
func TestContactsSummary(t *testing.T) {
	noMatch := contactsSummary(wiki.ContactEnrichResult{Total: 200})
	if !strings.Contains(noMatch, "200") || !strings.Contains(noMatch, "위키에 이미 있는 사람") {
		t.Errorf("no-match summary unexpected: %q", noMatch)
	}
	current := contactsSummary(wiki.ContactEnrichResult{Total: 200, Matched: 5})
	if !strings.Contains(current, "변경은 없습니다") {
		t.Errorf("already-current summary unexpected: %q", current)
	}
	enriched := contactsSummary(wiki.ContactEnrichResult{
		Total: 200, Matched: 8, Updated: 8,
		Names: []string{"가", "나", "다", "라", "마", "바", "사", "아"},
	})
	if !strings.Contains(enriched, "외 2명") {
		t.Errorf("enriched summary should cap names and show overflow, got %q", enriched)
	}
}
