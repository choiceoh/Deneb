package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type countingSearchFactLifecycle struct {
	store         *wiki.Store
	snapshotCalls int
	matcherCalls  int
	batchSizes    []int
}

func (l *countingSearchFactLifecycle) RecallFactSnapshot() wiki.FactRecallSnapshot {
	l.snapshotCalls++
	return l.store.RecallFactSnapshot()
}

func (l *countingSearchFactLifecycle) FactLifecycleEvidencesAllowed(
	evidence []wiki.FactLifecycleEvidence,
	snapshot wiki.FactRecallSnapshot,
) []bool {
	l.matcherCalls++
	l.batchSizes = append(l.batchSizes, len(evidence))
	return l.store.FactLifecycleEvidencesAllowed(evidence, snapshot)
}

func newSearchFactStore(t *testing.T) *wiki.Store {
	t.Helper()
	root := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func upsertSearchFact(t *testing.T, store *wiki.Store, subject, key, value string, at time.Time) wiki.FactWriteResult {
	t.Helper()
	result, err := store.UpsertFact(wiki.FactInput{
		Subject: subject, Key: key, Value: value,
		Kind: wiki.FactKindSystemState, Authority: wiki.FactAuthorityRuntime, At: at,
	})
	if err != nil || !result.Committed {
		t.Fatalf("UpsertFact(%s/%s=%q) = %+v, err=%v", subject, key, value, result, err)
	}
	return result
}

func searchDepsFor(store MemorySearcher, client PeopleClient) SearchDeps {
	deps := SearchDeps{}
	if store != nil {
		deps.Store = func() (MemorySearcher, error) { return store, nil }
	}
	if client != nil {
		deps.Client = func() (PeopleClient, error) { return client, nil }
	}
	return deps
}

func correctedFactSearchDeps(
	t *testing.T,
	lifecycle SearchFactLifecycle,
	oldFactID string,
	currentFactID string,
) SearchDeps {
	t.Helper()
	store := &fakeMemoryStore{
		searchFn: func(context.Context, string, int) ([]wiki.SearchResult, error) {
			return []wiki.SearchResult{
				{Path: "projects/alpha-stale.md", Content: "project:alpha release status A stale-alpha-marker"},
				{Path: "projects/alpha-current.md", Content: "project:alpha release status B current-alpha-marker"},
				{
					Path: "@facts/" + oldFactID + ".md", Content: "project:alpha release status A stale-fact-marker",
					FactID: oldFactID, SubjectID: "project:alpha",
				},
				{
					Path: "@facts/" + currentFactID + ".md", Content: "project:alpha release status B current-fact-marker",
					FactID: currentFactID, SubjectID: "project:alpha",
				},
			}, nil
		},
		readPageFn: func(path string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Title: path}}, nil
		},
		searchDiaryFn: func(context.Context, string, int) ([]wiki.DiaryHit, error) {
			return []wiki.DiaryHit{
				{File: "diary/alpha-stale.md", Header: "project:alpha release status", Content: "A stale-alpha-marker"},
				{File: "diary/alpha-current.md", Header: "project:alpha release status", Content: "B current-alpha-marker"},
			}, nil
		},
	}
	client := &fakePeopleClient{searchFn: func(context.Context, string, int) ([]gmail.MessageSummary, error) {
		return []gmail.MessageSummary{
			{
				From:    "Project Alpha Stale <stale-alpha@example.com>",
				Subject: "project:alpha release status A stale-alpha-marker",
				Date:    "2026-08-23T01:00:00Z",
			},
			{
				From:    "Project Alpha Current <current-alpha@example.com>",
				Subject: "project:alpha release status B current-alpha-marker",
				Date:    "2026-08-23T02:00:00Z",
			},
		}, nil
	}}
	deps := searchDepsFor(store, client)
	deps.FactLifecycle = func() SearchFactLifecycle { return lifecycle }
	deps.Files = func(context.Context, string, int) ([]SearchFileHit, error) {
		return []SearchFileHit{
			{Path: "files/alpha-stale.txt", Name: "project alpha release status", Snippet: "A stale-alpha-marker"},
			{Path: "files/alpha-current.txt", Name: "project alpha release status", Snippet: "B current-alpha-marker"},
		}, ErrSearchSourcePartial
	}
	deps.Mail = func(context.Context, string, int) ([]SearchMailHit, error) {
		return []SearchMailHit{
			{ID: "mail-stale", Subject: "project:alpha release status", Snippet: "A stale-alpha-marker"},
			{ID: "mail-current", Subject: "project:alpha release status", Snippet: "B current-alpha-marker"},
		}, nil
	}
	return deps
}

func TestSearchMethods_BothNilReturnsNil(t *testing.T) {
	if got := SearchMethods(SearchDeps{}); got != nil {
		t.Errorf("SearchMethods(empty) = %v, want nil", got)
	}
}

func TestSearchMethods_NewSourceAloneRegisters(t *testing.T) {
	deps := SearchDeps{
		Files: func(context.Context, string, int) ([]SearchFileHit, error) { return nil, nil },
	}
	if got := SearchMethods(deps); got == nil {
		t.Fatal("SearchMethods(files only) = nil, want registered method")
	}
}

func TestSearchAll_ReturnsResultsFromWikiDiaryAndGmailDomains(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, q string, _ int) ([]wiki.SearchResult, error) {
			if q != "peter" {
				t.Errorf("wiki query = %q", q)
			}
			return []wiki.SearchResult{
				{Path: "people/peter.md", Content: "Peter notes.", Score: 0.9},
			}, nil
		},
		readPageFn: func(_ string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Title: "Peter", Summary: "ops lead", Category: "people"}}, nil
		},
		searchDiaryFn: func(_ context.Context, q string, _ int) ([]wiki.DiaryHit, error) {
			if q != "peter" {
				t.Errorf("diary query = %q", q)
			}
			return []wiki.DiaryHit{
				{File: "diary-2026-05-26.md", Header: "14:30", Content: "met peter", Snippet: "met **peter**", At: 1700000000, Score: 0.75},
			}, nil
		},
	}
	now := time.Date(2026, 5, 26, 15, 0, 0, 0, time.UTC)
	client := &fakePeopleClient{
		searchFn: func(_ context.Context, q string, _ int) ([]gmail.MessageSummary, error) {
			if q != "newer_than:30d -from:me" {
				t.Errorf("gmail query = %q", q)
			}
			return []gmail.MessageSummary{
				{From: "Peter Park <peter@example.com>", Subject: "hi", Date: now.Format(time.RFC3339)},
				{From: "Other <other@example.com>", Subject: "x", Date: now.Format(time.RFC3339)},
			}, nil
		},
	}

	h := searchAll(searchDepsFor(store, client))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "peter"}))

	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Wiki) != 1 || got.Wiki[0].Title != "Peter" {
		t.Errorf("wiki = %+v, want Peter card", got.Wiki)
	}
	if len(got.Diary) != 1 || got.Diary[0].File != "diary-2026-05-26.md" {
		t.Errorf("diary = %+v", got.Diary)
	}
	if len(got.Diary) > 0 && got.Diary[0].Content != "met **peter**" {
		t.Errorf("diary content = %q, want snippet-preferred", got.Diary[0].Content)
	}
	if len(got.People) != 1 || got.People[0].Email != "peter@example.com" {
		t.Errorf("people = %+v, want only peter@example.com", got.People)
	}
	if got.Sources.Wiki != searchSourceOK || got.Sources.Diary != searchSourceOK || got.Sources.People != searchSourcePartial {
		t.Errorf("core source states = %+v, want wiki/diary ok and people partial", got.Sources)
	}
	if got.Sources.Files != searchSourceUnavailable || got.Sources.Mail != searchSourceUnavailable {
		t.Errorf("optional source states = %+v, want files/mail unavailable", got.Sources)
	}
	if got.Files == nil || got.Mail == nil {
		t.Errorf("new result slices must be non-nil for backward-compatible empty rendering: files=%#v mail=%#v", got.Files, got.Mail)
	}
}

func TestSearchAllPreservesFactIdentityAsReadOnlyWithoutPageLookup(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) {
			return []wiki.SearchResult{{
				Path:      "@facts/fact-456.md",
				Content:   "project:alpha owner is B",
				Score:     0.93,
				FactID:    "fact-456",
				SubjectID: "project:alpha",
			}}, nil
		},
		searchDiaryFn: func(context.Context, string, int) ([]wiki.DiaryHit, error) { return nil, nil },
		readPageFn: func(path string) (*wiki.Page, error) {
			t.Fatalf("ReadPage called for fact hit %q", path)
			return nil, nil
		},
	}
	resp := searchAll(searchDepsFor(store, nil))(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "alpha"}))

	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Wiki) != 1 {
		t.Fatalf("wiki = %+v", got.Wiki)
	}
	hit := got.Wiki[0]
	if hit.ResultKind != "fact" || !hit.ReadOnly || hit.FactID != "fact-456" || hit.SubjectID != "project:alpha" {
		t.Fatalf("fact identity = %+v", hit)
	}
}

func TestSearchAllFinalFactLifecycleFiltersCorrectionAndTombstoneAcrossSources(t *testing.T) {
	store := newSearchFactStore(t)
	base := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	oldFact := upsertSearchFact(t, store, "project:alpha", "release.status", "A", base)
	currentFact := upsertSearchFact(t, store, "project:alpha", "release.status", "B", base.Add(time.Hour))
	lifecycle := &countingSearchFactLifecycle{store: store}
	deps := correctedFactSearchDeps(t, lifecycle, oldFact.ClaimID, currentFact.ClaimID)

	resp := searchAll(deps)(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "alpha"}))
	var corrected SearchAllResult
	decode(t, resp, &corrected)
	if len(corrected.Wiki) != 2 || len(corrected.Diary) != 1 || len(corrected.People) != 1 ||
		len(corrected.Files) != 1 || len(corrected.Mail) != 1 {
		t.Fatalf("corrected result counts = wiki:%d diary:%d people:%d files:%d mail:%d; result=%+v",
			len(corrected.Wiki), len(corrected.Diary), len(corrected.People), len(corrected.Files), len(corrected.Mail), corrected)
	}
	encoded, err := json.Marshal(corrected)
	if err != nil {
		t.Fatalf("marshal corrected result: %v", err)
	}
	if strings.Contains(string(encoded), "stale-alpha-marker") || !strings.Contains(string(encoded), "current-alpha-marker") {
		t.Fatalf("correction boundary leaked stale or lost current evidence: %s", encoded)
	}
	if corrected.Sources.Wiki != searchSourceOK || corrected.Sources.Diary != searchSourceOK ||
		corrected.Sources.People != searchSourcePartial || corrected.Sources.Files != searchSourcePartial ||
		corrected.Sources.Mail != searchSourceOK {
		t.Fatalf("correction changed source status: %+v", corrected.Sources)
	}
	if lifecycle.snapshotCalls != 1 {
		t.Fatalf("RecallFactSnapshot calls after correction = %d, want 1", lifecycle.snapshotCalls)
	}
	if lifecycle.matcherCalls != 1 || len(lifecycle.batchSizes) != 1 || lifecycle.batchSizes[0] != 10 {
		t.Fatalf("correction matcher calls/batches = %d/%v, want one 10-row batch", lifecycle.matcherCalls, lifecycle.batchSizes)
	}

	tombstone, err := store.TombstoneFact(wiki.FactTombstoneInput{
		Subject: "project:alpha", Key: "release.status",
		Authority: wiki.FactAuthorityRuntime, At: base.Add(2 * time.Hour),
	})
	if err != nil || !tombstone.Committed {
		t.Fatalf("TombstoneFact = %+v, err=%v", tombstone, err)
	}
	resp = searchAll(deps)(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "alpha"}))
	var forgotten SearchAllResult
	decode(t, resp, &forgotten)
	if len(forgotten.Wiki) != 0 || len(forgotten.Diary) != 0 || len(forgotten.People) != 0 ||
		len(forgotten.Files) != 0 || len(forgotten.Mail) != 0 {
		t.Fatalf("tombstoned evidence survived final boundary: %+v", forgotten)
	}
	if forgotten.Sources.Wiki != searchSourceOK || forgotten.Sources.Diary != searchSourceOK ||
		forgotten.Sources.People != searchSourcePartial || forgotten.Sources.Files != searchSourcePartial ||
		forgotten.Sources.Mail != searchSourceOK {
		t.Fatalf("tombstone changed source status: %+v", forgotten.Sources)
	}
	if lifecycle.snapshotCalls != 2 {
		t.Fatalf("RecallFactSnapshot calls after tombstone = %d, want 2 total", lifecycle.snapshotCalls)
	}
	if lifecycle.matcherCalls != 2 || len(lifecycle.batchSizes) != 2 || lifecycle.batchSizes[1] != 10 {
		t.Fatalf("tombstone matcher calls/batches = %d/%v, want one new 10-row batch", lifecycle.matcherCalls, lifecycle.batchSizes)
	}
}

func TestSearchAllFactLifecycleKeepsSameValueForDifferentSubject(t *testing.T) {
	store := newSearchFactStore(t)
	base := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	upsertSearchFact(t, store, "project:alpha", "release.status", "A", base)
	upsertSearchFact(t, store, "project:alpha", "release.status", "B", base.Add(time.Hour))
	upsertSearchFact(t, store, "project:beta", "quality.grade", "A", base.Add(2*time.Hour))
	lifecycle := &countingSearchFactLifecycle{store: store}
	deps := SearchDeps{
		FactLifecycle: func() SearchFactLifecycle { return lifecycle },
		Files: func(context.Context, string, int) ([]SearchFileHit, error) {
			return []SearchFileHit{
				{
					Path: "files/alpha-release.txt", Name: "project alpha release status",
					Snippet: "A alpha-stale-marker",
				},
				{
					Path: "files/beta-grade.txt", Name: "project beta quality grade",
					Snippet: "A beta-current-marker",
				},
			}, ErrSearchSourcePartial
		},
	}

	resp := searchAll(deps)(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "beta"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Files) != 1 || got.Files[0].Path != "files/beta-grade.txt" ||
		!strings.Contains(got.Files[0].Snippet, "beta-current-marker") {
		t.Fatalf("cross-subject same value was flattened: %+v", got.Files)
	}
	if got.Sources.Files != searchSourcePartial {
		t.Fatalf("files status = %q, want partial", got.Sources.Files)
	}
	if lifecycle.snapshotCalls != 1 {
		t.Fatalf("RecallFactSnapshot calls = %d, want 1", lifecycle.snapshotCalls)
	}
	if lifecycle.matcherCalls != 1 || len(lifecycle.batchSizes) != 1 || lifecycle.batchSizes[0] != 2 {
		t.Fatalf("matcher calls/batches = %d/%v, want one 2-row batch", lifecycle.matcherCalls, lifecycle.batchSizes)
	}
}

func TestSearchAllRevalidatesAfterSlowSourceMutation(t *testing.T) {
	store := newSearchFactStore(t)
	base := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	oldFact := upsertSearchFact(t, store, "project:alpha", "release.status", "A", base)
	lifecycle := &countingSearchFactLifecycle{store: store}
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseSource := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseSource()
	memory := &fakeMemoryStore{
		searchFn: func(context.Context, string, int) ([]wiki.SearchResult, error) {
			return []wiki.SearchResult{
				{
					Path: "@facts/" + oldFact.ClaimID + ".md", Content: "project:alpha release status A slow-stale-marker",
					FactID: oldFact.ClaimID, SubjectID: "project:alpha",
				},
				{Path: "projects/alpha-stale.md", Content: "project:alpha release status A slow-stale-marker"},
			}, nil
		},
		readPageFn: func(path string) (*wiki.Page, error) {
			return &wiki.Page{Meta: wiki.Frontmatter{Title: path}}, nil
		},
		searchDiaryFn: func(context.Context, string, int) ([]wiki.DiaryHit, error) { return nil, nil },
	}
	deps := searchDepsFor(memory, nil)
	deps.FactLifecycle = func() SearchFactLifecycle { return lifecycle }
	deps.Files = func(context.Context, string, int) ([]SearchFileHit, error) {
		close(started)
		<-release
		return []SearchFileHit{{
			Path: "files/alpha-stale.txt", Name: "project alpha release status", Snippet: "A slow-stale-marker",
		}}, ErrSearchSourcePartial
	}
	handler := searchAll(deps)
	request := reqWith(t, "miniapp.search.all", map[string]any{"query": "alpha"})
	response := make(chan *protocol.ResponseFrame, 1)
	go func() {
		response <- handler(authedCtx(), request)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("slow file source did not start")
	}
	upsertSearchFact(t, store, "project:alpha", "release.status", "B", base.Add(time.Hour))
	releaseSource()
	var resp *protocol.ResponseFrame
	select {
	case resp = <-response:
	case <-time.After(5 * time.Second):
		t.Fatal("unified search did not finish after releasing slow source")
	}
	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Wiki) != 0 || len(got.Files) != 0 {
		t.Fatalf("pre-mutation search evidence escaped final revalidation: wiki=%+v files=%+v", got.Wiki, got.Files)
	}
	if got.Sources.Wiki != searchSourceOK || got.Sources.Files != searchSourcePartial {
		t.Fatalf("final revalidation changed source status: %+v", got.Sources)
	}
	if lifecycle.snapshotCalls != 1 {
		t.Fatalf("RecallFactSnapshot calls = %d, want exactly one final snapshot", lifecycle.snapshotCalls)
	}
	if lifecycle.matcherCalls != 1 || len(lifecycle.batchSizes) != 1 || lifecycle.batchSizes[0] != 2 {
		t.Fatalf("matcher calls/batches = %d/%v, want one 2-row batch", lifecycle.matcherCalls, lifecycle.batchSizes)
	}
}

func TestSearchAll_GmailMissingDegrades(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn:      func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) { return nil, nil },
		searchDiaryFn: func(_ context.Context, _ string, _ int) ([]wiki.DiaryHit, error) { return nil, nil },
	}
	h := searchAll(searchDepsFor(store, nil))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "x"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if got.People == nil {
		t.Error("People should be non-nil empty slice, got nil")
	}
	if len(got.People) != 0 {
		t.Errorf("People should be empty when client absent, got %+v", got.People)
	}
	if got.Sources.People != searchSourceUnavailable {
		t.Errorf("people status = %q, want unavailable", got.Sources.People)
	}
}

func TestSearchAll_PerDomainFailureNonFatal(t *testing.T) {
	store := &fakeMemoryStore{
		searchFn: func(_ context.Context, _ string, _ int) ([]wiki.SearchResult, error) {
			return nil, errors.New("wiki index unavailable")
		},
		searchDiaryFn: func(_ context.Context, _ string, _ int) ([]wiki.DiaryHit, error) {
			return []wiki.DiaryHit{{File: "d.md", Header: "10:00", Content: "x"}}, nil
		},
	}
	h := searchAll(searchDepsFor(store, nil))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "x"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Wiki) != 0 {
		t.Errorf("wiki should degrade to empty on error, got %+v", got.Wiki)
	}
	if len(got.Diary) != 1 {
		t.Errorf("diary should succeed independently, got %+v", got.Diary)
	}
	if got.Sources.Wiki != searchSourceError || got.Sources.Diary != searchSourceOK {
		t.Errorf("source states = %+v, want wiki error and diary ok", got.Sources)
	}
}

func TestSearchAll_PreservesFileLocatorFields(t *testing.T) {
	want := SearchFileHit{
		Path:      "/reports/forecast.md",
		Name:      "forecast.md",
		Snippet:   "revenue reaches 42",
		Score:     0.87,
		StartLine: 41,
		EndLine:   48,
		Kind:      "section",
		Heading:   "Q4 forecast",
	}
	deps := SearchDeps{
		Files: func(_ context.Context, query string, limit int) ([]SearchFileHit, error) {
			if query != "revenue" || limit != defaultUnifiedSearchLimit {
				t.Errorf("files callback args = %q/%d", query, limit)
			}
			return []SearchFileHit{want}, nil
		},
	}

	resp := searchAll(deps)(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "revenue"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if len(got.Files) != 1 {
		t.Fatalf("files = %+v, want one locator-bearing hit", got.Files)
	}
	hit := got.Files[0]
	if hit != want {
		t.Errorf("file hit = %+v, want %+v", hit, want)
	}
	if got.Sources.Files != searchSourceOK {
		t.Errorf("files source status = %q, want ok", got.Sources.Files)
	}
}

func TestSearchAll_PreservesHitsFromPartialFileSource(t *testing.T) {
	deps := SearchDeps{
		Files: func(context.Context, string, int) ([]SearchFileHit, error) {
			return []SearchFileHit{{Path: "/partial.txt", Name: "partial.txt", Snippet: "usable evidence"}}, ErrSearchSourcePartial
		},
	}
	resp := searchAll(deps)(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "evidence"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if got.Sources.Files != searchSourcePartial {
		t.Fatalf("files status = %q, want partial", got.Sources.Files)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "/partial.txt" {
		t.Fatalf("partial file evidence was lost: %+v", got.Files)
	}
}

func TestSearchAll_SourceTimeoutPreservesCompletedSources(t *testing.T) {
	deps := SearchDeps{
		Files: func(ctx context.Context, _ string, _ int) ([]SearchFileHit, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		Mail: func(_ context.Context, _ string, _ int) ([]SearchMailHit, error) {
			return []SearchMailHit{{ID: "mail-1", ThreadID: "thread-1", Subject: "Ready"}}, nil
		},
	}
	ctx, cancel := context.WithTimeout(authedCtx(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	resp := searchAll(deps)(ctx, reqWith(t, "miniapp.search.all", map[string]any{"query": "ready"}))
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("timed-out source held unified search for %s", elapsed)
	}

	var got SearchAllResult
	decode(t, resp, &got)
	if got.Sources.Files != searchSourceTimeout {
		t.Errorf("files source status = %q, want timeout", got.Sources.Files)
	}
	if got.Sources.Mail != searchSourceOK || len(got.Mail) != 1 || got.Mail[0].ID != "mail-1" {
		t.Errorf("completed mail source was lost: status=%q hits=%+v", got.Sources.Mail, got.Mail)
	}
}

func TestSearchAll_SourceErrorIsGenericAndPartialResultsSurvive(t *testing.T) {
	const privateMarker = "should-not-leak-marker"
	deps := SearchDeps{
		Files: func(context.Context, string, int) ([]SearchFileHit, error) {
			return nil, errors.New("backend rejected " + privateMarker)
		},
		Mail: func(context.Context, string, int) ([]SearchMailHit, error) {
			return []SearchMailHit{
				{
					ID:       "mail-2",
					ThreadID: "thread-2",
					From:     "sender@example.com",
					Subject:  "Forecast",
					Date:     "2026-08-23T00:00:00Z",
					Snippet:  "revenue forecast",
					Mailbox:  "Archive",
				},
			}, nil
		},
	}

	resp := searchAll(deps)(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "forecast"}))
	var got SearchAllResult
	decode(t, resp, &got)
	if got.Sources.Files != searchSourceError {
		t.Errorf("files source status = %q, want error", got.Sources.Files)
	}
	if got.Sources.Mail != searchSourceOK || len(got.Mail) != 1 || got.Mail[0].Mailbox != "Archive" {
		t.Errorf("mail partial result = status %q, hits %+v", got.Sources.Mail, got.Mail)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(raw), privateMarker) || strings.Contains(string(raw), "backend rejected") {
		t.Fatalf("dependency error leaked into response: %s", raw)
	}
}

func TestSearchAll_ClampsNewSourceLimitAndResultCount(t *testing.T) {
	seenLimit := 0
	deps := SearchDeps{
		Files: func(_ context.Context, _ string, limit int) ([]SearchFileHit, error) {
			seenLimit = limit
			hits := make([]SearchFileHit, limit+3)
			for i := range hits {
				hits[i].Path = "/file"
			}
			return hits, nil
		},
	}
	resp := searchAll(deps)(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{"query": "x", "limit": 100}))
	var got SearchAllResult
	decode(t, resp, &got)
	if seenLimit != maxUnifiedSearchLimit || len(got.Files) != maxUnifiedSearchLimit {
		t.Errorf("files limit = callback %d/result %d, want %d", seenLimit, len(got.Files), maxUnifiedSearchLimit)
	}
}

func TestSearchAll_MissingQuery(t *testing.T) {
	h := searchAll(searchDepsFor(&fakeMemoryStore{}, nil))
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{}))
	if resp.Error == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestSearchAll_RejectsOversizedQueryBeforeFanout(t *testing.T) {
	called := false
	h := searchAll(SearchDeps{Files: func(context.Context, string, int) ([]SearchFileHit, error) {
		called = true
		return nil, nil
	}})
	resp := h(authedCtx(), reqWith(t, "miniapp.search.all", map[string]any{
		"query": strings.Repeat("가", maxUnifiedSearchQueryRunes+1),
	}))
	if resp.Error == nil || called {
		t.Fatalf("error=%+v called=%v, want rejected before fanout", resp.Error, called)
	}
}

func TestMatchesPersonReturnsTrueForCaseInsensitiveSubstringMatch(t *testing.T) {
	p := PersonRow{Email: "peter@example.com", Name: "Peter Park"}
	cases := []struct {
		needle string
		want   bool
	}{
		{"peter", true},
		{"PARK", true},
		{"example.com", true},
		{"alice", false},
		{"", true},
	}
	for _, c := range cases {
		if got := matchesPerson(p, c.needle); got != c.want {
			t.Errorf("matchesPerson(%q) = %v, want %v", c.needle, got, c.want)
		}
	}
}
