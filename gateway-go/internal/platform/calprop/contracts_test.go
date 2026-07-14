package calprop

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func validAllDayProposal(source string) CreateInput {
	return CreateInput{
		Title:  "Review contract",
		Start:  "2026-07-15",
		AllDay: true,
		Kind:   "deadline",
		Source: source,
	}
}

func TestCreateValidationContract(t *testing.T) {
	s := newTestStore(t)
	for _, tt := range []struct {
		name string
		in   CreateInput
		want string
	}{
		{name: "blank title", in: CreateInput{Title: " ", Start: "2026-07-15", AllDay: true}, want: "title"},
		{name: "blank start", in: CreateInput{Title: "x", Start: " "}, want: "start"},
		{name: "invalid all day", in: CreateInput{Title: "x", Start: "2026-02-30", AllDay: true}, want: "all-day"},
		{name: "timed date only", in: CreateInput{Title: "x", Start: "2026-07-15"}, want: "timed"},
		{name: "invalid timed", in: CreateInput{Title: "x", Start: "tomorrow"}, want: "timed"},
		{name: "unknown kind", in: CreateInput{Title: "x", Start: "2026-07-15", AllDay: true, Kind: "reminder"}, want: "kind"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, created, err := s.CreateIfAbsent(tt.in); err == nil || created || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CreateIfAbsent = created=%v err=%v", created, err)
			}
		})
	}
	if pending, err := s.ListPending(); err != nil || len(pending) != 0 {
		t.Fatalf("invalid creates changed store: %+v/%v", pending, err)
	}

	for _, in := range []CreateInput{
		{Title: "meeting", Start: "2026-07-15T09:00:00Z", Kind: "meeting"},
		{Title: "deadline", Start: "2026-07-15", AllDay: true, Kind: "deadline"},
		{Title: "legacy kindless", Start: "2026-07-15", AllDay: true},
	} {
		if _, created, err := s.CreateIfAbsent(in); err != nil || !created {
			t.Errorf("valid create = created=%v err=%v input=%+v", created, err, in)
		}
	}
}

func TestCreateNormalizesFieldsAndCopiesDocuments(t *testing.T) {
	s := newTestStore(t)
	docs := []string{"quote.pdf", "contract.docx"}
	p, created, err := s.CreateIfAbsent(CreateInput{
		Title:         "  Kickoff  ",
		Start:         " 2026-07-15 ",
		AllDay:        true,
		Kind:          " meeting ",
		Source:        " mail:m1|kickoff ",
		SourceSubject: " Mail subject ",
		SourceFrom:    " Vendor <vendor@example.test> ",
		Docs:          docs,
	})
	if err != nil || !created {
		t.Fatalf("create = %+v/%v/%v", p, created, err)
	}
	if p.Title != "Kickoff" || p.Start != "2026-07-15" || p.Kind != "meeting" {
		t.Fatalf("core fields not normalized: %+v", p)
	}
	if p.Source != "mail:m1|kickoff" || p.SourceSubject != "Mail subject" || p.SourceFrom != "Vendor <vendor@example.test>" {
		t.Fatalf("source fields not normalized: %+v", p)
	}
	docs[0] = "mutated.pdf"
	p.Docs[1] = "mutated.docx"
	got, err := s.Get(p.ID)
	if err != nil || got == nil || !reflect.DeepEqual(got.Docs, []string{"quote.pdf", "contract.docx"}) {
		t.Fatalf("document copy/persistence = %+v/%v", got, err)
	}
}

func TestNormalizedSourceDeduplicates(t *testing.T) {
	s := newTestStore(t)
	first, created, err := s.CreateIfAbsent(CreateInput{Title: "first", Start: "2026-07-15", AllDay: true, Source: " mail:one "})
	if err != nil || !created {
		t.Fatal(err)
	}
	second, created, err := s.CreateIfAbsent(CreateInput{Title: "second", Start: "2026-07-16", AllDay: true, Source: "mail:one"})
	if err != nil || created || second.ID != first.ID || second.Title != "first" {
		t.Fatalf("dedup = %+v created=%v err=%v", second, created, err)
	}
	// Empty source intentionally disables deduplication.
	for i := 0; i < 2; i++ {
		if _, created, err := s.CreateIfAbsent(CreateInput{Title: "manual", Start: "2026-07-17", AllDay: true, Source: " "}); err != nil || !created {
			t.Fatalf("empty-source create %d = %v/%v", i, created, err)
		}
	}
}

func TestDecide_RejectsInvalidStatusAndClearsEventIDOnReopen(t *testing.T) {
	s := newTestStore(t)
	p, _, _ := s.CreateIfAbsent(validAllDayProposal("source"))
	if got, err := s.Decide(p.ID, Status("unknown"), "local:wrong"); err == nil || got != nil {
		t.Fatalf("invalid status = %+v/%v", got, err)
	}
	unchanged, _ := s.Get(p.ID)
	if unchanged.Status != StatusPending || unchanged.CalendarEventID != "" || unchanged.DecidedAtMs != 0 {
		t.Fatalf("invalid decision mutated proposal: %+v", unchanged)
	}

	accepted, err := s.Decide(p.ID, StatusAccepted, " local:event ")
	if err != nil || accepted.Status != StatusAccepted || accepted.CalendarEventID != "local:event" || accepted.DecidedAtMs == 0 {
		t.Fatalf("accepted = %+v/%v", accepted, err)
	}
	reopened, err := s.Decide(p.ID, StatusPending, "must-be-cleared")
	if err != nil || reopened.Status != StatusPending || reopened.CalendarEventID != "" || reopened.DecidedAtMs != 0 {
		t.Fatalf("reopened = %+v/%v", reopened, err)
	}
	rejected, err := s.Decide(p.ID, StatusRejected, "must-also-be-cleared")
	if err != nil || rejected.Status != StatusRejected || rejected.CalendarEventID != "" || rejected.DecidedAtMs == 0 {
		t.Fatalf("rejected = %+v/%v", rejected, err)
	}
}

func TestConcurrentClaimForAcceptHasExactlyOneWinner(t *testing.T) {
	s := newTestStore(t)
	p, _, _ := s.CreateIfAbsent(validAllDayProposal("claim"))
	const callers = 64
	type result struct {
		proposal *Proposal
		claimed  bool
		err      error
	}
	results := make(chan result, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, claimed, err := s.ClaimForAccept(p.ID)
			results <- result{proposal: got, claimed: claimed, err: err}
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for r := range results {
		if r.err != nil || r.proposal == nil || r.proposal.ID != p.ID {
			t.Fatalf("claim result = %+v", r)
		}
		if r.claimed {
			winners++
		}
		if r.proposal.Status != StatusAccepted {
			t.Fatalf("claim observed status %q", r.proposal.Status)
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners = %d, want 1", winners)
	}
	stored, _ := s.Get(p.ID)
	if stored.Status != StatusAccepted || stored.DecidedAtMs == 0 {
		t.Fatalf("stored claim = %+v", stored)
	}
}

func TestConcurrentCreateIfAbsentDeduplicatesEachSource(t *testing.T) {
	s := newTestStore(t)
	const perSource = 12
	const sources = 8
	type result struct {
		source  int
		id      string
		created bool
		err     error
	}
	results := make(chan result, perSource*sources)
	var wg sync.WaitGroup
	for source := 0; source < sources; source++ {
		for i := 0; i < perSource; i++ {
			wg.Add(1)
			go func(source int) {
				defer wg.Done()
				in := validAllDayProposal(fmt.Sprintf("mail:%d", source))
				p, created, err := s.CreateIfAbsent(in)
				results <- result{source: source, id: p.ID, created: created, err: err}
			}(source)
		}
	}
	wg.Wait()
	close(results)
	createdBySource := make(map[int]int)
	idsBySource := make(map[int]map[string]struct{})
	for r := range results {
		if r.err != nil {
			t.Fatalf("create: %v", r.err)
		}
		if r.created {
			createdBySource[r.source]++
		}
		if idsBySource[r.source] == nil {
			idsBySource[r.source] = make(map[string]struct{})
		}
		idsBySource[r.source][r.id] = struct{}{}
	}
	for source := 0; source < sources; source++ {
		if createdBySource[source] != 1 || len(idsBySource[source]) != 1 {
			t.Errorf("source %d: created=%d ids=%#v", source, createdBySource[source], idsBySource[source])
		}
	}
	pending, err := s.ListPending()
	if err != nil || len(pending) != sources {
		t.Fatalf("pending = %d/%v, want %d", len(pending), err, sources)
	}
}

func TestPruneTerminalBoundaryAndPendingRetention(t *testing.T) {
	originalNow := nowMs
	defer func() { nowMs = originalNow }()
	base := int64(2_000_000_000_000)
	nowMs = func() int64 { return base }
	m := &fileModel{Proposals: []Proposal{
		{ID: "old", Status: StatusRejected, DecidedAtMs: base - terminalRetention.Milliseconds() - 1},
		{ID: "boundary", Status: StatusRejected, DecidedAtMs: base - terminalRetention.Milliseconds()},
		{ID: "recent", Status: StatusAccepted, DecidedAtMs: base - terminalRetention.Milliseconds() + 1},
		{ID: "pending-old", Status: StatusPending, DecidedAtMs: base - terminalRetention.Milliseconds() - 1},
		{ID: "legacy-terminal", Status: StatusRejected, DecidedAtMs: 0},
	}}
	if !pruneTerminalLocked(m) {
		t.Fatal("prune reported no change")
	}
	ids := make([]string, 0, len(m.Proposals))
	for _, p := range m.Proposals {
		ids = append(ids, p.ID)
	}
	want := []string{"boundary", "recent", "pending-old", "legacy-terminal"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("kept = %#v, want %#v", ids, want)
	}
	if pruneTerminalLocked(m) {
		t.Fatal("second prune unexpectedly changed model")
	}
}

func TestSortByStartStableAndLexical(t *testing.T) {
	proposals := []Proposal{
		{ID: "late", Start: "2026-07-20"},
		{ID: "tie-a", Start: "2026-07-10"},
		{ID: "early", Start: "2026-07-01"},
		{ID: "tie-b", Start: "2026-07-10"},
	}
	sortByStart(proposals)
	ids := make([]string, 0, len(proposals))
	for _, p := range proposals {
		ids = append(ids, p.ID)
	}
	want := []string{"early", "tie-a", "tie-b", "late"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("sort = %#v, want %#v", ids, want)
	}
}

func TestStoreMissingCorruptAndPermissionContracts(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("empty path accepted")
	}
	path := filepath.Join(t.TempDir(), "calendar_proposals.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("missing"); err != nil || got != nil {
		t.Fatalf("Get missing = %+v/%v", got, err)
	}
	if got, claimed, err := s.ClaimForAccept("missing"); err != nil || got != nil || claimed {
		t.Fatalf("Claim missing = %+v/%v/%v", got, claimed, err)
	}
	if got, err := s.Decide("missing", StatusRejected, ""); err != nil || got != nil {
		t.Fatalf("Decide missing = %+v/%v", got, err)
	}
	p, _, err := s.CreateIfAbsent(validAllDayProposal("persist"))
	if err != nil || p.ID == "" {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("store permissions = %o, want 600", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte(`{"proposals":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListPending(); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("corrupt store error = %v", err)
	}
}

func TestListPendingReturnsOnlyPendingAndFreshCopies(t *testing.T) {
	s := newTestStore(t)
	one, _, _ := s.CreateIfAbsent(validAllDayProposal("one"))
	twoIn := validAllDayProposal("two")
	twoIn.Start = "2026-07-14"
	two, _, _ := s.CreateIfAbsent(twoIn)
	threeIn := validAllDayProposal("three")
	threeIn.Start = "2026-07-16"
	three, _, _ := s.CreateIfAbsent(threeIn)
	_, _ = s.Decide(one.ID, StatusAccepted, "local:one")
	_, _ = s.Decide(three.ID, StatusRejected, "")

	pending, err := s.ListPending()
	if err != nil || len(pending) != 1 || pending[0].ID != two.ID {
		t.Fatalf("pending = %+v/%v", pending, err)
	}
	pending[0].Title = "mutated"
	again, err := s.ListPending()
	if err != nil || again[0].Title == "mutated" {
		t.Fatalf("ListPending leaked mutation: %+v/%v", again, err)
	}
}

func TestNewID_GeneratesUniqueFormattedIDs(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		id := newID()
		if !strings.HasPrefix(id, "cp_") || len(id) != len("cp_")+18 {
			t.Fatalf("id shape = %q", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("duplicate id = %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestLoadReadErrorsAreWrapped(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListPending(); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("directory read error = %v", err)
	}
	if _, _, err := s.CreateIfAbsent(validAllDayProposal("x")); err == nil {
		t.Fatal("create on directory path succeeded")
	}
	if _, err := s.Get("x"); err == nil {
		t.Fatal("get on directory path succeeded")
	}
	if _, _, err := s.ClaimForAccept("x"); err == nil {
		t.Fatal("claim on directory path succeeded")
	}
	if _, err := s.Decide("x", StatusRejected, ""); err == nil {
		t.Fatal("decide on directory path succeeded")
	}
}
