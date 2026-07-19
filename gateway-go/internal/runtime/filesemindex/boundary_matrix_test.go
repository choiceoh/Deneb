package filesemindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

func TestFileServerModifiedMillisBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  int64
		valid bool
	}{
		{
			name:  "epoch",
			input: "1970-01-01T00:00:00Z",
			want:  0,
			valid: true,
		},
		{
			name:  "one millisecond",
			input: "1970-01-01T00:00:00.001Z",
			want:  1,
			valid: true,
		},
		{
			name:  "before epoch",
			input: "1969-12-31T23:59:59.999Z",
			want:  -1,
			valid: true,
		},
		{
			name:  "year 2000",
			input: "2000-01-01T00:00:00Z",
			want:  946684800000,
			valid: true,
		},
		{
			name:  "leap day",
			input: "2024-02-29T12:34:56Z",
			want:  1709210096000,
			valid: true,
		},
		{
			name:  "korea offset",
			input: "2026-07-11T17:15:30+09:00",
			want:  1783757730000,
			valid: true,
		},
		{
			name:  "negative offset",
			input: "2026-07-11T03:15:30-05:00",
			want:  1783757730000,
			valid: true,
		},
		{
			name:  "fraction micros",
			input: "2026-07-11T08:15:30.123456Z",
			want:  1783757730123,
			valid: true,
		},
		{
			name:  "fraction nanos",
			input: "2026-07-11T08:15:30.999999999Z",
			want:  1783757730999,
			valid: true,
		},
		{
			name:  "empty",
			input: "",
			want:  0,
			valid: false,
		},
		{
			name:  "space",
			input: " ",
			want:  0,
			valid: false,
		},
		{
			name:  "word",
			input: "not-a-time",
			want:  0,
			valid: false,
		},
		{
			name:  "date only",
			input: "2026-07-11",
			want:  0,
			valid: false,
		},
		{
			name:  "missing seconds",
			input: "2026-07-11T08:15Z",
			want:  0,
			valid: false,
		},
		{
			name:  "space separator",
			input: "2026-07-11 08:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "lowercase t",
			input: "2026-07-11t08:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "lowercase z",
			input: "2026-07-11T08:15:30z",
			want:  0,
			valid: false,
		},
		{
			name:  "no zone",
			input: "2026-07-11T08:15:30",
			want:  0,
			valid: false,
		},
		{
			name:  "bad month",
			input: "2026-13-11T08:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "zero month",
			input: "2026-00-11T08:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "bad day",
			input: "2026-07-32T08:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "zero day",
			input: "2026-07-00T08:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "bad hour",
			input: "2026-07-11T24:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "bad minute",
			input: "2026-07-11T08:60:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "bad second",
			input: "2026-07-11T08:15:60Z",
			want:  0,
			valid: false,
		},
		{
			name:  "offset hour too high",
			input: "2026-07-11T08:15:30+25:00",
			want:  0,
			valid: false,
		},
		{
			name:  "offset minute too high",
			input: "2026-07-11T08:15:30+09:99",
			want:  0,
			valid: false,
		},
		{
			name:  "trailing space",
			input: "2026-07-11T08:15:30Z ",
			want:  0,
			valid: false,
		},
		{
			name:  "leading space",
			input: " 2026-07-11T08:15:30Z",
			want:  0,
			valid: false,
		},
		{
			name:  "trailing data",
			input: "2026-07-11T08:15:30Zx",
			want:  0,
			valid: false,
		},
		{
			name:  "rfc1123",
			input: "Sat, 11 Jul 2026 08:15:30 GMT",
			want:  0,
			valid: false,
		},
		{
			name:  "unix seconds",
			input: "1783757730",
			want:  0,
			valid: false,
		},
		{
			name:  "json quoted",
			input: "\"2026-07-11T08:15:30Z\"",
			want:  0,
			valid: false,
		},
		{
			name:  "nul",
			input: "2026-07-11T08:15:30Z\u0000",
			want:  0,
			valid: false,
		},
		{
			name:  "newline",
			input: "2026-07-11T08:15:30Z\n",
			want:  0,
			valid: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fileServerModifiedMillis(tc.input)
			if got != tc.want {
				t.Fatalf("fileServerModifiedMillis(%q)=%d want=%d", tc.input, got, tc.want)
			}
			if tc.valid && tc.input != "1970-01-01T00:00:00Z" && got == 0 {
				t.Error("valid non-epoch timestamp collapsed to zero")
			}
		})
	}
}

func TestFileSemindexPathBoundary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	want := filepath.Join(dir, "files-semindex.json")
	if got := fileSemindexPath(); got != want {
		t.Fatalf("path=%q want=%q", got, want)
	}
	if filepath.Dir(fileSemindexPath()) != dir {
		t.Error("index escaped state directory")
	}
	if filepath.Base(fileSemindexPath()) != "files-semindex.json" {
		t.Error("unexpected sidecar name")
	}
}

func TestServiceConstructionDegradesToEmptyResultsWithoutEmbedding(t *testing.T) {
	if got := New(nil, nil, nil); got != nil {
		t.Fatalf("nil store enabled service: %#v", got)
	}
	store, err := filestore.NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	svc := New(store, nil, nil)
	if svc == nil {
		t.Fatal("non-nil store did not enable service")
	}
	if svc.store != store {
		t.Error("store boundary was not retained")
	}
	if svc.index == nil {
		t.Error("semantic index was not created")
	}
	if svc.embedding != nil {
		t.Error("nil embedding boundary changed")
	}
	if task := svc.Task(); task != nil {
		t.Fatalf("Task=%#v with no embedding", task)
	}
	if adapter := svc.KnowledgeAdapter(); adapter != nil {
		t.Fatalf("adapter=%#v with no embedding", adapter)
	}
	for _, tc := range []struct {
		query string
		limit int
	}{
		{query: "", limit: 0},
		{query: "proposal", limit: 1},
		{query: "한글", limit: 10},
		{query: strings.Repeat("x", 4096), limit: -1},
	} {
		hits, err := svc.Search(context.Background(), tc.query, tc.limit)
		if err != nil || len(hits) != 0 {
			t.Errorf("Search(%q,%d)=(%v,%v)", tc.query, tc.limit, hits, err)
		}
		if recall := svc.Recall(context.Background(), tc.query, tc.limit); len(recall) != 0 {
			t.Errorf("Recall(%q,%d)=%v", tc.query, tc.limit, recall)
		}
	}
}

func TestDisabledServiceMutationAndTaskReturnNil(t *testing.T) {
	svc := &Service{}
	paths := []string{"", "/", "/old.pdf", "relative.txt", "한글/문서.md", strings.Repeat("x", 4096)}
	for _, path := range paths {
		t.Run(fmt.Sprintf("remove_%x", len(path)), func(t *testing.T) { svc.Remove(path) })
	}
	for i, oldPath := range paths {
		newPath := fmt.Sprintf("/new-%d", i)
		t.Run(fmt.Sprintf("rename_%d", i), func(t *testing.T) { svc.Rename(oldPath, newPath) })
	}
	if task := svc.Task(); task != nil {
		t.Errorf("disabled task=%#v", task)
	}
	if adapter := svc.KnowledgeAdapter(); adapter != nil {
		t.Errorf("disabled adapter=%#v", adapter)
	}
	if recall := svc.Recall(context.Background(), "x", 5); recall != nil {
		t.Errorf("disabled recall=%#v want nil", recall)
	}
}

func TestSemindexTaskMetadataAndRunSucceedsWhenCanceled(t *testing.T) {
	task := &semindexTask{}
	if got := task.Name(); got != "file-semindex" {
		t.Errorf("Name=%q", got)
	}
	if got := task.Interval(); got != 15*time.Minute {
		t.Errorf("Interval=%s", got)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Errorf("Run without embedding=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := task.Run(ctx); err != nil {
		t.Errorf("canceled unhealthy Run=%v", err)
	}
	for _, tc := range []struct {
		name      string
		got, want time.Duration
	}{
		{name: "interval", got: semindexInterval, want: 15 * time.Minute},
		{name: "run timeout", got: semindexRunTimeout, want: 10 * time.Minute},
		{name: "query timeout", got: semindexQueryTimeout, want: 8 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got=%s want=%s", tc.got, tc.want)
			}
		})
	}
}

func TestFileSemindexExtractFormatsSupportedFileTypes(t *testing.T) {
	tests := []struct{ name, filename, body, want string }{
		{name: "txt", filename: "note.txt", body: "plain text", want: ""},
		{name: "markdown", filename: "note.md", body: "# Heading\nbody", want: "Heading"},
		{name: "csv", filename: "data.csv", body: "a,b\n1,2", want: "| a | b |"},
		{name: "json", filename: "data.json", body: `{"a":1}`, want: ""},
		{name: "log", filename: "app.log", body: "line one\nline two", want: ""},
		{name: "unknown", filename: "blob.unknown", body: "readable payload", want: ""},
		{name: "unicode", filename: "한글.txt", body: "업무 내용", want: ""},
		{name: "empty", filename: "empty.txt", body: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fileSemindexExtract(context.Background(), []byte(tc.body), tc.filename)
			if tc.want == "" {
				if got != "" {
					t.Errorf("extract=%q, want empty for unsupported/empty input", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("extract=%q want substring %q", got, tc.want)
			}
		})
	}
}

func TestFileSemindexConcurrentDegradedBoundaries(t *testing.T) {
	const workers = 128
	const iterations = 100
	svc := &Service{}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				query := fmt.Sprintf("q-%d-%d", worker, i)
				hits, err := svc.Search(context.Background(), query, i)
				if err != nil || len(hits) != 0 {
					errs <- fmt.Errorf("search worker=%d i=%d hits=%v err=%w", worker, i, hits, err)
					return
				}
				if got := fileServerModifiedMillis("2026-07-11T08:15:30Z"); got == 0 {
					errs <- fmt.Errorf("timestamp worker=%d", worker)
					return
				}
				svc.Remove(query)
				svc.Rename(query, query+"-new")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestFileSemindexPathCreatesNoFileOnRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	path := fileSemindexPath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("sidecar unexpectedly exists before service construction: %v", err)
	}
	_ = fileSemindexPath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path resolution created sidecar: %v", err)
	}
}
