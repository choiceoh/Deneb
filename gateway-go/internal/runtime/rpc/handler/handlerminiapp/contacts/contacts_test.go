package contactsapi

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestContactsListRejectsUnauthenticatedAndReturnsProjectedRows(t *testing.T) {
	store, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceAll([]contacts.Contact{{
		Name: "홍길동", Phones: []string{"010-1234-5678"}, Emails: []string{"hong@example.com"}, Org: "샘플사",
	}}); err != nil {
		t.Fatal(err)
	}

	methods := ContactsMethods(ContactsDeps{Store: func() (*contacts.Store, error) { return store, nil }})
	h := methods["miniapp.contacts.list"]
	unauthorized := h(context.Background(), request(t, "miniapp.contacts.list", nil))
	if unauthorized.OK || unauthorized.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("unauthorized response = %+v", unauthorized)
	}

	resp := h(authenticatedContext(), request(t, "miniapp.contacts.list", nil))
	var got struct {
		Contacts []ContactRow `json:"contacts"`
		Count    int          `json:"count"`
	}
	decodeResponse(t, resp, &got)
	if got.Count != 1 || len(got.Contacts) != 1 || got.Contacts[0].Name != "홍길동" || got.Contacts[0].Org != "샘플사" {
		t.Fatalf("payload = %+v", got)
	}
}

func TestContactsMethodsNilStoreReturnsNilAndListReturnsUnavailableOnStoreError(t *testing.T) {
	if got := ContactsMethods(ContactsDeps{}); got != nil {
		t.Fatalf("nil factory registered methods: %v", got)
	}
	h := ContactsMethods(ContactsDeps{Store: func() (*contacts.Store, error) {
		return nil, errors.New("store down")
	}})["miniapp.contacts.list"]
	resp := h(authenticatedContext(), request(t, "miniapp.contacts.list", nil))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("unavailable response = %+v", resp)
	}
}

func TestContactsDedupReturnsSafeMergesAndCounts(t *testing.T) {
	store, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceAll([]contacts.Contact{
		{Name: "박한주", Phones: []string{"010-1111-2222"}},
		{Name: "#박한주 부장(솔라테크)", Phones: []string{"010-1111-2222"}},
		{Name: "이영희", Phones: []string{"010-3333-4444"}},
	}); err != nil {
		t.Fatal(err)
	}
	methods := ContactsMethods(ContactsDeps{Store: func() (*contacts.Store, error) { return store, nil }})
	h := methods["miniapp.contacts.dedup"]

	if u := h(context.Background(), request(t, "miniapp.contacts.dedup", nil)); u.OK || u.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("unauthorized = %+v", u)
	}
	resp := h(authenticatedContext(), request(t, "miniapp.contacts.dedup", nil))
	var got struct {
		Total     int `json:"total"`
		Distinct  int `json:"distinct"`
		Ambiguous int `json:"ambiguous"`
		Merges    []struct {
			Canonical string   `json:"canonical"`
			Names     []string `json:"names"`
			Phones    []string `json:"phones"`
		} `json:"merges"`
	}
	decodeResponse(t, resp, &got)
	if got.Total != 3 || got.Distinct != 2 || got.Ambiguous != 0 || len(got.Merges) != 1 {
		t.Fatalf("dedup payload = %+v", got)
	}
	m := got.Merges[0]
	if m.Canonical != "박한주" || len(m.Names) != 2 || len(m.Phones) != 1 {
		t.Fatalf("merge group = %+v", m)
	}
}

func TestContactsDedupMergePayloadUsesJoinKeysOnly(t *testing.T) {
	store, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceAll([]contacts.Contact{
		{Name: "김철수", Phones: []string{"010-1111-2222", "010-3333-4444"}},
		{Name: "김철수", Phones: []string{"010-1111-2222"}},
		{Name: "이영희", Phones: []string{"010-3333-4444"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := ContactsMethods(ContactsDeps{Store: func() (*contacts.Store, error) { return store, nil }})["miniapp.contacts.dedup"]
	resp := h(authenticatedContext(), request(t, "miniapp.contacts.dedup", nil))
	var got struct {
		Ambiguous int `json:"ambiguous"`
		Merges    []struct {
			Phones []string `json:"phones"`
		} `json:"merges"`
	}
	decodeResponse(t, resp, &got)
	if got.Ambiguous != 1 || len(got.Merges) != 1 {
		t.Fatalf("ambiguous=%d merges=%d, want 1/1", got.Ambiguous, len(got.Merges))
	}
	if len(got.Merges[0].Phones) != 1 || got.Merges[0].Phones[0] != "01011112222" {
		t.Fatalf("merge phones = %v, want only shared join key 01011112222", got.Merges[0].Phones)
	}
}

func TestContactsDedupReturnsAmbiguousPairsForAdjudication(t *testing.T) {
	store, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	// two people sharing one personal number under incompatible names -> ambiguous
	if _, err := store.ReplaceAll([]contacts.Contact{
		{Name: "강상민", Phones: []string{"010-9999-8888"}},
		{Name: "브라이트강상민", Phones: []string{"010-9999-8888"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := ContactsMethods(ContactsDeps{Store: func() (*contacts.Store, error) { return store, nil }})["miniapp.contacts.dedup"]
	resp := h(authenticatedContext(), request(t, "miniapp.contacts.dedup", nil))
	var got struct {
		Ambiguous      int `json:"ambiguous"`
		AmbiguousPairs []struct {
			A      struct{ Name string } `json:"a"`
			B      struct{ Name string } `json:"b"`
			Shared string                `json:"shared"`
		} `json:"ambiguous_pairs"`
	}
	decodeResponse(t, resp, &got)
	if got.Ambiguous != 1 || len(got.AmbiguousPairs) != 1 {
		t.Fatalf("ambiguous=%d pairs=%d, want 1/1", got.Ambiguous, len(got.AmbiguousPairs))
	}
	p := got.AmbiguousPairs[0]
	names := p.A.Name + "|" + p.B.Name
	if !strings.Contains(names, "강상민") || !strings.Contains(names, "브라이트강상민") || p.Shared == "" {
		t.Fatalf("pair = %+v", p)
	}
}

type fakeAdjudicator struct{ verdicts []contacts.Verdict }

func (f fakeAdjudicator) Adjudicate(_ context.Context, _ []contacts.Contact, pairs []contacts.AmbiguousPair) ([]contacts.Verdict, error) {
	return f.verdicts, nil
}

func TestContactsAdjudicateRegistersOnlyWithAdjudicatorAndReturnsVerdicts(t *testing.T) {
	store, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	storeFn := func() (*contacts.Store, error) { return store, nil }

	// without an adjudicator, the method is not registered
	if ContactsMethods(ContactsDeps{Store: storeFn})["miniapp.contacts.adjudicate"] != nil {
		t.Fatal("adjudicate registered without an adjudicator")
	}

	h := ContactsMethods(ContactsDeps{
		Store:       storeFn,
		Adjudicator: fakeAdjudicator{verdicts: []contacts.Verdict{contacts.VerdictSame, contacts.VerdictDifferent}},
	})["miniapp.contacts.adjudicate"]
	if h == nil {
		t.Fatal("adjudicate not registered with an adjudicator")
	}
	params := map[string]any{"pairs": []map[string]any{
		{"a": map[string]any{"name": "강상민"}, "b": map[string]any{"name": "브라이트강상민"}, "shared": "01099998888"},
		{"a": map[string]any{"name": "서장원"}, "b": map[string]any{"name": "박태경"}, "shared": "0212345678"},
	}}
	resp := h(authenticatedContext(), request(t, "miniapp.contacts.adjudicate", params))
	var got struct {
		Verdicts []string `json:"verdicts"`
	}
	decodeResponse(t, resp, &got)
	if len(got.Verdicts) != 2 || got.Verdicts[0] != "same" || got.Verdicts[1] != "diff" {
		t.Fatalf("verdicts = %+v, want [same diff]", got.Verdicts)
	}
}

func TestContactsAdjudicateRejectsOversizeBatch(t *testing.T) {
	store, _ := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	h := ContactsMethods(ContactsDeps{
		Store:       func() (*contacts.Store, error) { return store, nil },
		Adjudicator: fakeAdjudicator{},
	})["miniapp.contacts.adjudicate"]
	pairs := make([]map[string]any, maxAdjudicatePairs+1)
	for i := range pairs {
		pairs[i] = map[string]any{"a": map[string]any{"name": "A"}, "b": map[string]any{"name": "B"}}
	}
	resp := h(authenticatedContext(), request(t, "miniapp.contacts.adjudicate", map[string]any{"pairs": pairs}))
	if resp.OK || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("oversize batch response = %+v", resp)
	}
}

func authenticatedContext() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{User: &clientauth.User{ID: 42}})
}

func request(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("test", method, params)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func decodeResponse(t *testing.T, resp *protocol.ResponseFrame, out any) {
	t.Helper()
	if resp == nil || !resp.OK {
		t.Fatalf("response = %+v", resp)
	}
	if err := json.Unmarshal(resp.Payload, out); err != nil {
		t.Fatal(err)
	}
}
