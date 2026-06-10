package handlerminiapp

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
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

// peopleWikiStore stubs the 인물 directory: ListPages("인물") returns the
// page paths, ReadPage serves each page.
func peopleWikiStore(pages map[string]*wiki.Page) *fakeMemoryStore {
	paths := make([]string, 0, len(pages))
	for p := range pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return &fakeMemoryStore{
		listPagesFn: func(category string) ([]string, error) {
			if category != peopleWikiCategory {
				return nil, nil
			}
			return paths, nil
		},
		readPageFn: func(rel string) (*wiki.Page, error) {
			page, ok := pages[rel]
			if !ok {
				return nil, errors.New("no such page")
			}
			return page, nil
		},
	}
}

func TestPeopleList_MergesWikiPeople(t *testing.T) {
	c := &fakePeopleClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{
				// Matches 인물/kim by 연락처 email despite the unrelated display name.
				{From: "MJ <mj.kim@topsolar.kr>", Subject: "견적", Date: "2026-06-08T10:00:00Z"},
				// Matches 인물/lee by normalized name (honorific stripped).
				{From: "이수민 차장 <soomin@partner.co>", Subject: "회의", Date: "2026-06-09T10:00:00Z"},
				// No wiki page.
				{From: "noreply@promo.example", Subject: "Sale", Date: "2026-06-07T10:00:00Z"},
			}, nil
		},
	}
	store := peopleWikiStore(map[string]*wiki.Page{
		"인물/kim.md": {
			Meta: wiki.Frontmatter{Title: "김민준", Summary: "탑솔라 대표", Category: "인물", Updated: "2026-06-01"},
			Body: "# 김민준\n\n## 연락처\n\n- 이메일: MJ.Kim@topsolar.kr\n",
		},
		"인물/lee.md": {
			Meta: wiki.Frontmatter{Title: "이수민", Summary: "파트너사 차장", Category: "인물", Updated: "2026-05-20"},
			Body: "# 이수민\n",
		},
		// No recent mail → wiki-only tail row.
		"인물/park.md": {
			Meta: wiki.Frontmatter{Title: "박지훈", Summary: "법무 자문", Category: "인물", Updated: "2026-05-30"},
			Body: "# 박지훈\n",
		},
		// Newer than park → must sort first in the tail.
		"인물/choi.md": {
			Meta: wiki.Frontmatter{Title: "최은서", Summary: "회계사", Category: "인물", Updated: "2026-06-05"},
			Body: "# 최은서\n",
		},
		// Stray non-person page under 인물/ — must be skipped.
		"인물/stray.md": {
			Meta: wiki.Frontmatter{Title: "메모", Summary: "잘못 들어온 페이지", Category: "토픽"},
			Body: "# 메모\n",
		},
	})
	deps := peopleDepsFor(c)
	deps.WikiStore = func() (MemorySearcher, error) { return store, nil }

	h := peopleList(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.people.list", map[string]any{}))
	var got struct {
		People []PersonRow `json:"people"`
	}
	decode(t, resp, &got)
	if len(got.People) != 5 {
		t.Fatalf("people len = %d, want 5 (3 gmail + 2 wiki-only): %+v", len(got.People), got.People)
	}

	kim := findPersonRow(got.People, "mj.kim@topsolar.kr")
	if kim == nil || kim.WikiPath != "인물/kim.md" || kim.WikiSummary != "탑솔라 대표" {
		t.Errorf("email match not applied: %+v", kim)
	}
	lee := findPersonRow(got.People, "soomin@partner.co")
	if lee == nil || lee.WikiPath != "인물/lee.md" {
		t.Errorf("normalized-name match not applied: %+v", lee)
	}
	promo := findPersonRow(got.People, "noreply@promo.example")
	if promo == nil || promo.WikiPath != "" {
		t.Errorf("unmatched sender gained wiki fields: %+v", promo)
	}

	// Wiki-only tail: updated desc → choi (06-05) before park (05-30);
	// both after every Gmail row, with no email/count.
	tail := got.People[3:]
	if tail[0].Name != "최은서" || tail[1].Name != "박지훈" {
		t.Errorf("wiki-only tail order = [%s, %s], want [최은서, 박지훈]", tail[0].Name, tail[1].Name)
	}
	for _, row := range tail {
		if row.Email != "" || row.MessageCount != 0 || row.WikiPath == "" {
			t.Errorf("wiki-only row malformed: %+v", row)
		}
	}
}

func findPersonRow(rows []PersonRow, email string) *PersonRow {
	for i := range rows {
		if rows[i].Email == email {
			return &rows[i]
		}
	}
	return nil
}

func TestPeopleList_WikiUnavailableDegradesToGmailOnly(t *testing.T) {
	c := &fakePeopleClient{
		searchFn: func(_ context.Context, _ string, _ int) ([]gmail.MessageSummary, error) {
			return []gmail.MessageSummary{
				{From: "a@x.com", Subject: "S", Date: "2026-06-09T10:00:00Z"},
			}, nil
		},
	}
	deps := peopleDepsFor(c)
	deps.WikiStore = func() (MemorySearcher, error) { return nil, errors.New("wiki disabled") }

	h := peopleList(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.people.list", map[string]any{}))
	var got struct {
		People []PersonRow `json:"people"`
	}
	decode(t, resp, &got)
	if len(got.People) != 1 || got.People[0].Email != "a@x.com" {
		t.Errorf("wiki failure must not break gmail rows: %+v", got.People)
	}
}

func TestContactSectionEmails_OnlyContactSection(t *testing.T) {
	page := &wiki.Page{
		Body: "# 김민준\n\n## 요약\n\n다른사람 other@x.com 언급.\n\n## 연락처\n\n- 전화: 010-0000-0000\n- 이메일: A@topsolar.kr, b@topsolar.kr\n\n_주소록에서 동기화됨_\n",
	}
	got := contactSectionEmails(page)
	want := []string{"a@topsolar.kr", "b@topsolar.kr"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("contactSectionEmails = %v, want %v", got, want)
	}
}
