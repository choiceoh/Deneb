package handlerminiapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakePeopleClient struct {
	searchFn func(ctx context.Context, q string, n int) ([]gmail.MessageSummary, error)
}

func (f *fakePeopleClient) Search(ctx context.Context, q string, n int) ([]gmail.MessageSummary, error) {
	if f.searchFn == nil {
		return nil, errors.New("Search not stubbed")
	}
	return f.searchFn(ctx, q, n)
}

func peopleDepsFor(c PeopleClient) PeopleDeps {
	return PeopleDeps{Client: func() (PeopleClient, error) { return c, nil }}
}

func TestPeopleList_HappyPath(t *testing.T) {
	now := time.Date(2026, 5, 26, 15, 0, 0, 0, time.UTC)
	earlier := now.Add(-3 * time.Hour)
	c := &fakePeopleClient{
		searchFn: func(_ context.Context, q string, n int) ([]gmail.MessageSummary, error) {
			// Default 30d window + me-exclusion.
			if q != "newer_than:30d -from:me" {
				t.Errorf("query = %q", q)
			}
			if n != maxPeopleScanMessages {
				t.Errorf("scan limit = %d, want %d", n, maxPeopleScanMessages)
			}
			return []gmail.MessageSummary{
				{From: "Alice <alice@example.com>", Subject: "Sync notes", Date: now.Format(time.RFC3339)},
				{From: "Alice <alice@example.com>", Subject: "Follow-up", Date: earlier.Format(time.RFC3339)},
				{From: "bob@example.com", Subject: "Quick question", Date: earlier.Format(time.RFC3339)},
				{From: "Promo Bot <noreply@promo.example>", Subject: "Sale!", Date: now.Format(time.RFC3339)},
			}, nil
		},
	}
	h := peopleList(peopleDepsFor(c))
	resp := h(authedCtx(), reqWith(t, "miniapp.people.list", map[string]any{}))
	var got struct {
		People       []map[string]any `json:"people"`
		WindowDays   int              `json:"windowDays"`
		ScannedCount int              `json:"scannedCount"`
	}
	decode(t, resp, &got)
	if got.WindowDays != defaultPeopleWindowDays {
		t.Errorf("windowDays = %d, want %d", got.WindowDays, defaultPeopleWindowDays)
	}
	if got.ScannedCount != 4 {
		t.Errorf("scannedCount = %d, want 4", got.ScannedCount)
	}
	if len(got.People) != 3 {
		t.Fatalf("people len = %d, want 3", len(got.People))
	}
	// Alice should be first (2 messages).
	if got.People[0]["email"] != "alice@example.com" {
		t.Errorf("top sender = %v, want alice", got.People[0]["email"])
	}
	if int(got.People[0]["messageCount"].(float64)) != 2 {
		t.Errorf("Alice count = %v, want 2", got.People[0]["messageCount"])
	}
	// Last-seen subject for Alice should be the more recent one.
	if got.People[0]["lastSubject"] != "Sync notes" {
		t.Errorf("Alice lastSubject = %v, want 'Sync notes'", got.People[0]["lastSubject"])
	}
	// Bob has no display name → name omitted.
	bobRow := findPerson(got.People, "bob@example.com")
	if bobRow == nil {
		t.Fatal("bob row missing")
	}
	if _, hasName := bobRow["name"]; hasName {
		t.Errorf("bob name should be omitted: %+v", bobRow)
	}
}

func findPerson(people []map[string]any, email string) map[string]any {
	for _, p := range people {
		if p["email"] == email {
			return p
		}
	}
	return nil
}

func TestPeopleList_CustomWindowAndLimit(t *testing.T) {
	var seenQuery string
	c := &fakePeopleClient{
		searchFn: func(_ context.Context, q string, _ int) ([]gmail.MessageSummary, error) {
			seenQuery = q
			return []gmail.MessageSummary{
				{From: "a@x.com", Subject: "S1", Date: "2026-05-26T10:00:00Z"},
				{From: "b@x.com", Subject: "S2", Date: "2026-05-26T11:00:00Z"},
				{From: "c@x.com", Subject: "S3", Date: "2026-05-26T12:00:00Z"},
			}, nil
		},
	}
	h := peopleList(peopleDepsFor(c))
	resp := h(authedCtx(), reqWith(t, "miniapp.people.list", map[string]any{
		"windowDays": 7,
		"limit":      2,
	}))
	if seenQuery != "newer_than:7d -from:me" {
		t.Errorf("query window not threaded: %q", seenQuery)
	}
	var got struct {
		People []map[string]any `json:"people"`
	}
	decode(t, resp, &got)
	if len(got.People) != 2 {
		t.Errorf("limit not applied: %d", len(got.People))
	}
}

func TestPeopleList_LimitClamp(t *testing.T) {
	c := &fakePeopleClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return nil, nil
		},
	}
	h := peopleList(peopleDepsFor(c))
	resp := h(authedCtx(), reqWith(t, "miniapp.people.list", map[string]any{"limit": 9999, "windowDays": 9999}))
	var got struct {
		WindowDays int `json:"windowDays"`
	}
	decode(t, resp, &got)
	if got.WindowDays != maxPeopleWindowDays {
		t.Errorf("windowDays clamp = %d, want %d", got.WindowDays, maxPeopleWindowDays)
	}
}

func TestPeopleList_DropsUnparseableSenders(t *testing.T) {
	c := &fakePeopleClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{
				{From: "(garbage no email)", Subject: "X", Date: "2026-05-26T10:00:00Z"},
				{From: "good@host.com", Subject: "Y", Date: "2026-05-26T11:00:00Z"},
			}, nil
		},
	}
	h := peopleList(peopleDepsFor(c))
	resp := h(authedCtx(), reqWith(t, "miniapp.people.list", map[string]any{}))
	var got struct {
		People []map[string]any `json:"people"`
	}
	decode(t, resp, &got)
	if len(got.People) != 1 || got.People[0]["email"] != "good@host.com" {
		t.Errorf("expected only valid sender, got: %+v", got.People)
	}
}

func TestPeopleList_RequiresAuth(t *testing.T) {
	h := peopleList(peopleDepsFor(&fakePeopleClient{}))
	resp := h(context.Background(), reqWith(t, "miniapp.people.list", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("auth not enforced: %+v", resp)
	}
}

func TestPeopleList_GmailUnavailable(t *testing.T) {
	deps := PeopleDeps{Client: func() (PeopleClient, error) {
		return nil, errors.New("oauth not configured")
	}}
	h := peopleList(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.people.list", map[string]any{}))
	if resp.OK || resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("expected UNAVAILABLE: %+v", resp)
	}
}

func TestPeopleMethods_NilClientReturnsNil(t *testing.T) {
	if got := PeopleMethods(PeopleDeps{Client: nil}); got != nil {
		t.Errorf("PeopleMethods(nil) = %v, want nil", got)
	}
}

func TestAggregatePeople_TiebreakDeterministic(t *testing.T) {
	// Two senders with the same count and same last-seen → tied on
	// frequency and on time, must fall back to email ASC for stability.
	msgs := []gmail.MessageSummary{
		{From: "b@x.com", Subject: "B", Date: "2026-05-26T10:00:00Z"},
		{From: "a@x.com", Subject: "A", Date: "2026-05-26T10:00:00Z"},
	}
	rows := aggregatePeople(msgs)
	if rows[0].Email != "a@x.com" {
		t.Errorf("tiebreak order = %v, want a first", rows[0].Email)
	}
}
