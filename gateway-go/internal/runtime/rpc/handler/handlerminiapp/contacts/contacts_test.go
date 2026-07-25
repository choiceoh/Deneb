package contactsapi

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
