package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailarchive"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
)

type personalSearchMailStub struct {
	rows  []gmail.MessageSummary
	err   error
	query string
	limit int
}

type personalMailMirrorStub struct {
	rows  []mailarchive.ContextMessage
	query string
	limit int
}

func (s *personalMailMirrorStub) Len() int { return len(s.rows) }

func (s *personalMailMirrorStub) SearchContext(_ context.Context, _ []string, query string, _ time.Time, limit int) []mailarchive.ContextMessage {
	s.query, s.limit = query, limit
	return s.rows
}

type failingPersonalSearchStore struct {
	filestore.Store
	err error
}

type oversizedFilenameStore struct {
	filestore.Store
}

type vanishedEvidenceStore struct {
	filestore.Store
}

type growingEvidenceStore struct {
	filestore.Store
}

type searchReadSeekCloser struct {
	*bytes.Reader
}

func (searchReadSeekCloser) Close() error { return nil }

func (s oversizedFilenameStore) SearchContent(
	context.Context,
	string,
	int,
	func(context.Context, []byte, string) string,
) ([]filestore.Entry, error) {
	return []filestore.Entry{{
		Tag: "file", Name: "needle-huge.pdf", PathDisplay: "/needle-huge.pdf",
		Size: filestore.MaxContentExtractBytes + 1,
	}}, nil
}

func (s oversizedFilenameStore) Get(context.Context, string) ([]byte, *filestore.Entry, error) {
	panic("oversized filename evidence must not read the payload")
}

func (s vanishedEvidenceStore) SearchContent(
	context.Context,
	string,
	int,
	func(context.Context, []byte, string) string,
) ([]filestore.Entry, error) {
	return []filestore.Entry{{Tag: "file", Name: "ordinary.txt", PathDisplay: "/ordinary.txt", Size: 12}}, nil
}

func (s vanishedEvidenceStore) Get(context.Context, string) ([]byte, *filestore.Entry, error) {
	return nil, nil, os.ErrNotExist
}

func (s growingEvidenceStore) SearchContent(
	context.Context,
	string,
	int,
	func(context.Context, []byte, string) string,
) ([]filestore.Entry, error) {
	return []filestore.Entry{{Tag: "file", Name: "ordinary.txt", PathDisplay: "/ordinary.txt", Size: 12}}, nil
}

func (s growingEvidenceStore) Open(context.Context, string) (io.ReadSeekCloser, *filestore.Entry, error) {
	data := bytes.Repeat([]byte("needle"), filestore.MaxContentExtractBytes/6+1)
	return searchReadSeekCloser{Reader: bytes.NewReader(data)}, &filestore.Entry{Size: 12}, nil
}

func (s growingEvidenceStore) Get(context.Context, string) ([]byte, *filestore.Entry, error) {
	panic("personal search evidence must use bounded Open, not Get")
}

func (s failingPersonalSearchStore) SearchContent(
	context.Context,
	string,
	int,
	func(context.Context, []byte, string) string,
) ([]filestore.Entry, error) {
	return nil, s.err
}

func (s *personalSearchMailStub) Search(_ context.Context, query string, limit int) ([]gmail.MessageSummary, error) {
	s.query, s.limit = query, limit
	return s.rows, s.err
}

func TestSearchPersonalFilesFallsBackToLexicalEvidence(t *testing.T) {
	store, err := filestore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "/계약/검토.txt", []byte("서문\n공급가 100만원\n확인 완료"), true); err != nil {
		t.Fatal(err)
	}

	hits, err := searchPersonalFiles(
		context.Background(),
		store,
		func(context.Context, string, int) ([]filestore.ScoredEntry, error) {
			return nil, errors.New("embedding unavailable")
		},
		"공급가",
		5,
	)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) {
		t.Fatalf("error = %v, want partial", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Path != "/계약/검토.txt" || hits[0].StartLine != 2 || hits[0].EndLine != 2 {
		t.Fatalf("locator = %+v", hits[0])
	}
	if !strings.Contains(hits[0].Snippet, "공급가 100만원") || hits[0].Kind != "txt" {
		t.Fatalf("evidence = %+v", hits[0])
	}
}

func TestSearchPersonalFilesMergesExactAndSemanticCoverage(t *testing.T) {
	store, err := filestore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "/새자료/정확한-계약.txt", []byte("계약 갱신일은 9월 1일"), true); err != nil {
		t.Fatal(err)
	}
	semantic := []filestore.ScoredEntry{{
		Entry:   filestore.Entry{Name: "관련검토.pdf", PathDisplay: "/법무/관련검토.pdf"},
		Score:   0.88,
		Snippet: "갱신 조건 전반을 검토한다.",
	}}
	hits, err := searchPersonalFiles(context.Background(), store, func(context.Context, string, int) ([]filestore.ScoredEntry, error) {
		return semantic, nil
	}, "계약 갱신", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].Path != "/새자료/정확한-계약.txt" || hits[1].Path != "/법무/관련검토.pdf" {
		t.Fatalf("merged exact+semantic hits = %+v", hits)
	}
}

func TestSearchPersonalFilesPreservesSemanticLocator(t *testing.T) {
	store, err := filestore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rows := []filestore.ScoredEntry{{
		Entry:     filestore.Entry{Name: "계약서.pdf", PathDisplay: "/법무/계약서.pdf"},
		Score:     0.91,
		Snippet:   "제4조 계약 기간은 1년이다.",
		StartLine: 40,
		EndLine:   43,
		Kind:      "pdf",
		Heading:   "제4조 계약기간",
	}}
	hits, err := searchPersonalFiles(context.Background(), store, func(context.Context, string, int) ([]filestore.ScoredEntry, error) {
		return rows, nil
	}, "계약 기간", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].StartLine != 40 || hits[0].EndLine != 43 || hits[0].Heading != "제4조 계약기간" {
		t.Fatalf("semantic evidence = %+v", hits)
	}
}

func TestSearchPersonalMailProjectsArchiveEvidence(t *testing.T) {
	stub := &personalSearchMailStub{rows: []gmail.MessageSummary{{
		ID: "mail-1", ThreadID: "thread-1", From: "법무팀", Subject: "계약 검토", Date: "2026-08-23",
		Snippet: "계약 기간 조항을 확인했습니다.", Mailbox: "INBOX",
	}}}
	hits, err := searchPersonalMail(context.Background(), stub, "계약", 7)
	if err != nil {
		t.Fatal(err)
	}
	if stub.query != "계약" || stub.limit != 7 || len(hits) != 1 {
		t.Fatalf("search projection query=%q limit=%d hits=%+v", stub.query, stub.limit, hits)
	}
	if hits[0].ID != "mail-1" || hits[0].ThreadID != "thread-1" || !strings.Contains(hits[0].Snippet, "계약 기간") {
		t.Fatalf("mail evidence = %+v", hits[0])
	}
}

func TestSearchPersonalMailMirrorUsesAuthoritativeCorpusAndMatchSnippet(t *testing.T) {
	stub := &personalMailMirrorStub{rows: []mailarchive.ContextMessage{{
		ID: "mirror-1", From: "법무팀", Subject: "계약 검토", Date: "2026-08-23", Mailbox: "INBOX",
		Body: "앞부분입니다.\n계속됩니다.\n지체상금은 계약금의 3%입니다.\n마지막입니다.",
	}}}
	hits, err := searchPersonalMailMirror(context.Background(), stub, "지체상금", 7)
	if err != nil {
		t.Fatal(err)
	}
	if stub.query != "지체상금" || stub.limit != 7 || len(hits) != 1 {
		t.Fatalf("query=%q limit=%d hits=%+v", stub.query, stub.limit, hits)
	}
	if hits[0].ID != "mirror-1" || !strings.Contains(hits[0].Snippet, "지체상금은 계약금의 3%") {
		t.Fatalf("mirror evidence = %+v", hits[0])
	}
}

func TestSearchPersonalMailMirrorPreservesHitsButMarksCanceledSearchPartial(t *testing.T) {
	stub := &personalMailMirrorStub{rows: []mailarchive.ContextMessage{{ID: "mirror-1", Body: "근거"}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hits, err := searchPersonalMailMirror(ctx, stub, "근거", 5)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) || len(hits) != 1 {
		t.Fatalf("hits=%+v err=%v, want preserved partial", hits, err)
	}
}

func TestSearchPersonalMailPreservesPartialArchiveEvidence(t *testing.T) {
	stub := &personalSearchMailStub{
		rows: []gmail.MessageSummary{{ID: "mail-1", Snippet: "usable"}},
		err:  mailarchive.ErrArchivePartial,
	}
	hits, err := searchPersonalMail(context.Background(), stub, "usable", 5)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) {
		t.Fatalf("error = %v, want partial", err)
	}
	if len(hits) != 1 || hits[0].ID != "mail-1" {
		t.Fatalf("partial hits = %+v", hits)
	}
}

func TestSearchPersonalSourcesReportUnavailable(t *testing.T) {
	if _, err := searchPersonalFiles(context.Background(), nil, nil, "계약", 5); !errors.Is(err, miniknowledge.ErrSearchSourceUnavailable) {
		t.Fatalf("file error = %v", err)
	}
	if _, err := searchPersonalMail(context.Background(), nil, "계약", 5); !errors.Is(err, miniknowledge.ErrSearchSourceUnavailable) {
		t.Fatalf("mail error = %v", err)
	}
}

func TestSearchPersonalFilesDoesNotHideLexicalFailureBehindEmptySemanticResult(t *testing.T) {
	wantErr := errors.New("lexical search failed")
	store := failingPersonalSearchStore{Store: mustPersonalSearchStore(t), err: wantErr}
	_, err := searchPersonalFiles(context.Background(), store, func(context.Context, string, int) ([]filestore.ScoredEntry, error) {
		return nil, nil
	}, "계약", 5)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) {
		t.Fatalf("error = %v, want partial coverage after lexical failure (%v)", err, wantErr)
	}
}

func TestSearchPersonalFilesPreservesSemanticHitsWhenLexicalFails(t *testing.T) {
	wantErr := errors.New("lexical search failed")
	store := failingPersonalSearchStore{Store: mustPersonalSearchStore(t), err: wantErr}
	hits, err := searchPersonalFiles(context.Background(), store, func(context.Context, string, int) ([]filestore.ScoredEntry, error) {
		return []filestore.ScoredEntry{{
			Entry: filestore.Entry{Tag: "file", Name: "indexed.pdf", PathDisplay: "/indexed.pdf"},
			Score: 0.9, Snippet: "indexed evidence",
		}}, nil
	}, "evidence", 5)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) {
		t.Fatalf("error = %v, want partial", err)
	}
	if len(hits) != 1 || hits[0].Path != "/indexed.pdf" {
		t.Fatalf("partial hits = %+v", hits)
	}
}

func TestSearchPersonalFilesKeepsPartialWhenOCRFileIsNotIndexed(t *testing.T) {
	store, err := filestore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "/new-scan.png", []byte("not-yet-indexed"), true); err != nil {
		t.Fatal(err)
	}
	hits, err := searchPersonalFiles(context.Background(), store, func(context.Context, string, int) ([]filestore.ScoredEntry, error) {
		return nil, nil
	}, "지체상금", 5)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) || len(hits) != 0 {
		t.Fatalf("hits=%+v err=%v, want empty partial", hits, err)
	}
}

func TestSearchPersonalFilesDoesNotReadOversizedFilenameHit(t *testing.T) {
	store := oversizedFilenameStore{Store: mustPersonalSearchStore(t)}
	hits, err := searchPersonalFilesLexically(context.Background(), store, "needle", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != "/needle-huge.pdf" || hits[0].Snippet != "" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchPersonalFilesDropsContentHitThatCannotBeReverified(t *testing.T) {
	store := vanishedEvidenceStore{Store: mustPersonalSearchStore(t)}
	hits, err := searchPersonalFilesLexically(context.Background(), store, "needle", 5)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) {
		t.Fatalf("error = %v, want partial", err)
	}
	if len(hits) != 0 {
		t.Fatalf("unverified content hit must be dropped: %+v", hits)
	}
}

func TestSearchPersonalFilesBoundsEvidenceReadWhenFileGrows(t *testing.T) {
	store := growingEvidenceStore{Store: mustPersonalSearchStore(t)}
	hits, err := searchPersonalFilesLexically(context.Background(), store, "needle", 5)
	if !errors.Is(err, miniknowledge.ErrSearchSourcePartial) {
		t.Fatalf("error = %v, want partial", err)
	}
	if len(hits) != 0 {
		t.Fatalf("oversized replacement must not become evidence: %+v", hits)
	}
}

func mustPersonalSearchStore(t *testing.T) filestore.Store {
	t.Helper()
	store, err := filestore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}
