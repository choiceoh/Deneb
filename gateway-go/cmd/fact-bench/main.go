// fact-bench scores the canonical fact plane against a checked-in goldset.
//
// recall-bench deliberately runs page-only (QueryOptions.ExcludeFactResults), so
// the plane that #4653 introduced was never measured by a number — its parity
// claim proved page recall had not regressed, not that corrections and deletions
// actually hold. This scores the promise itself: after a sequence of asserts,
// corrections and tombstones, does the current value win, does the retired value
// disappear from search, and is old evidence still denied at the recall
// exposure boundary?
//
// Usage:
//
//	go run ./cmd/fact-bench                      # checked-in goldset, ratchet on
//	go run ./cmd/fact-bench -gold path.json      # another goldset
//	go run ./cmd/fact-bench -json                # machine-readable summary
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

//go:embed testdata/fact_lifecycle_gold.json
var defaultGoldset []byte

type goldOp struct {
	Op        string `json:"op"` // assert | forget
	Value     string `json:"value,omitempty"`
	Authority string `json:"authority,omitempty"`
	BasisAt   string `json:"basisAt,omitempty"`
}

type goldCase struct {
	ID      string   `json:"id"`
	Subject string   `json:"subject"`
	Key     string   `json:"key"`
	Kind    string   `json:"kind"`
	Ops     []goldOp `json:"ops"`
	Query   string   `json:"query"`
	// Current is the value that must be the winner, or "" when the case ends
	// tombstoned and nothing may remain current.
	Current string `json:"current,omitempty"`
	// Stale lists retired values that must not surface in search results.
	Stale []string `json:"stale,omitempty"`
	// Evidence are recall snippets carrying a retired value. Each must be denied
	// at the lifecycle exposure boundary chat recall uses.
	Evidence []string `json:"evidence,omitempty"`
}

type goldset struct {
	SchemaVersion int        `json:"schemaVersion"`
	Cases         []goldCase `json:"cases"`
}

type score struct {
	Cases int `json:"cases"`

	CurrentChecked int `json:"currentChecked"`
	CurrentWrong   int `json:"currentWrong"`

	StaleChecked int `json:"staleChecked"`
	StaleExposed int `json:"staleExposed"`
	// SearchChecked/SearchMissing keep this bench honest in the other direction:
	// suppressing everything would satisfy the stale checks alone. Each goldset
	// query must be phrased in the axis vocabulary the catalog publishes
	// (memory.FactKeyQueryAliases) — that is the contract a Korean query relies
	// on to reach an English-keyed fact at all.
	SearchChecked int `json:"searchChecked"`
	SearchMissing int `json:"searchMissing"`

	EvidenceChecked int `json:"evidenceChecked"`
	EvidenceLeaked  int `json:"evidenceLeaked"`

	Failures []string `json:"failures,omitempty"`
}

func (s score) clean() bool {
	return s.CurrentWrong == 0 && s.StaleExposed == 0 && s.EvidenceLeaked == 0 && s.SearchMissing == 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("fact-bench", flag.ContinueOnError)
	flags.SetOutput(stderr)
	goldPath := flags.String("gold", "", "goldset JSON path (default: checked-in goldset)")
	asJSON := flags.Bool("json", false, "print the summary as JSON")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	raw := defaultGoldset
	if *goldPath != "" {
		read, err := os.ReadFile(*goldPath)
		if err != nil {
			fmt.Fprintf(stderr, "fact-bench: read goldset: %v\n", err)
			return 2
		}
		raw = read
	}
	var gold goldset
	if err := json.Unmarshal(raw, &gold); err != nil {
		fmt.Fprintf(stderr, "fact-bench: parse goldset: %v\n", err)
		return 2
	}
	if gold.SchemaVersion != 1 {
		fmt.Fprintf(stderr, "fact-bench: unsupported goldset schema version %d\n", gold.SchemaVersion)
		return 2
	}
	if len(gold.Cases) == 0 {
		fmt.Fprintln(stderr, "fact-bench: goldset has no cases")
		return 2
	}

	result, err := evaluate(context.Background(), gold)
	if err != nil {
		fmt.Fprintf(stderr, "fact-bench: %v\n", err)
		return 2
	}
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintf(stderr, "fact-bench: encode: %v\n", err)
			return 2
		}
	} else {
		printSummary(stdout, result)
	}
	if !result.clean() {
		return 1
	}
	return 0
}

func printSummary(w io.Writer, s score) {
	fmt.Fprintf(w, "fact lifecycle bench — %d cases\n", s.Cases)
	fmt.Fprintf(w, "  current value correct : %d/%d\n", s.CurrentChecked-s.CurrentWrong, s.CurrentChecked)
	fmt.Fprintf(w, "  current value in search: %d/%d\n", s.SearchChecked-s.SearchMissing, s.SearchChecked)
	fmt.Fprintf(w, "  stale value suppressed : %d/%d\n", s.StaleChecked-s.StaleExposed, s.StaleChecked)
	fmt.Fprintf(w, "  stale evidence denied  : %d/%d\n", s.EvidenceChecked-s.EvidenceLeaked, s.EvidenceChecked)
	for _, failure := range s.Failures {
		fmt.Fprintf(w, "  ✗ %s\n", failure)
	}
	if s.clean() {
		fmt.Fprintln(w, "  ✓ current facts reachable, no stale exposure")
	}
}

// evaluate replays every case against its own store so one case's history can
// never mask another's, then scores the three boundaries the plane owns.
func evaluate(ctx context.Context, gold goldset) (score, error) {
	result := score{Cases: len(gold.Cases)}
	for _, c := range gold.Cases {
		store, cleanup, err := newCaseStore()
		if err != nil {
			return result, err
		}
		caseErr := scoreCase(ctx, store, c, &result)
		cleanup()
		if caseErr != nil {
			return result, fmt.Errorf("case %s: %w", c.ID, caseErr)
		}
	}
	return result, nil
}

func newCaseStore() (*wiki.Store, func(), error) {
	root, err := os.MkdirTemp("", "fact-bench-")
	if err != nil {
		return nil, nil, err
	}
	store, err := wiki.NewStore(filepath.Join(root, "wiki"), filepath.Join(root, "diary"))
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, nil, err
	}
	return store, func() {
		_ = store.Close()
		_ = os.RemoveAll(root)
	}, nil
}

func scoreCase(ctx context.Context, store *wiki.Store, c goldCase, result *score) error {
	if err := applyOps(store, c); err != nil {
		return err
	}

	_, active := store.ActiveFactSnapshot(c.Subject)
	result.CurrentChecked++
	switch {
	case c.Current == "":
		for _, claim := range active {
			if claim.Key == wikiKey(c.Key) {
				result.CurrentWrong++
				result.Failures = append(result.Failures,
					fmt.Sprintf("%s: tombstoned fact is still current (%q)", c.ID, claim.Value))
				break
			}
		}
	default:
		found := false
		for _, claim := range active {
			if claim.Key == wikiKey(c.Key) && claim.Value == c.Current {
				found = true
				break
			}
		}
		if !found {
			result.CurrentWrong++
			result.Failures = append(result.Failures,
				fmt.Sprintf("%s: current value %q is not the winner", c.ID, c.Current))
		}
	}

	if strings.TrimSpace(c.Query) != "" {
		report, err := store.SearchWithOptions(ctx, c.Query, 8, wiki.QueryOptions{Mode: wiki.SearchModeFull})
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		bodies := make([]string, 0, len(report.Results))
		for _, hit := range report.Results {
			bodies = append(bodies, hit.Content+"\n"+hit.ExpandedContent)
		}
		joined := strings.Join(bodies, "\n")
		if c.Current != "" {
			result.SearchChecked++
			if !strings.Contains(joined, c.Current) {
				result.SearchMissing++
				result.Failures = append(result.Failures,
					fmt.Sprintf("%s: current value is absent from top-8 for %q", c.ID, c.Query))
			}
		}
		if len(c.Stale) > 0 {
			result.StaleChecked++
			for _, stale := range c.Stale {
				if strings.Contains(joined, stale) {
					result.StaleExposed++
					result.Failures = append(result.Failures,
						fmt.Sprintf("%s: retired value %q still surfaces in search", c.ID, stale))
					break
				}
			}
		}
	}

	if len(c.Evidence) > 0 {
		snapshot := store.RecallFactSnapshot()
		items := make([]wiki.FactLifecycleEvidence, 0, len(c.Evidence))
		for _, text := range c.Evidence {
			items = append(items, wiki.FactLifecycleEvidence{
				Query: c.Query, Ref: "bench:" + c.ID, SubjectID: c.Subject, FactKey: c.Key, Text: text,
			})
		}
		allowed := store.FactLifecycleEvidencesAllowed(items, snapshot)
		for i, ok := range allowed {
			result.EvidenceChecked++
			if ok {
				result.EvidenceLeaked++
				result.Failures = append(result.Failures,
					fmt.Sprintf("%s: stale evidence was allowed through: %q", c.ID, c.Evidence[i]))
			}
		}
	}
	return nil
}

// wikiKey mirrors the store's key normalization closely enough for comparison:
// goldset keys are already canonical, so only case folding is needed.
func wikiKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func applyOps(store *wiki.Store, c goldCase) error {
	at := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	for i, op := range c.Ops {
		when := at.Add(time.Duration(i) * time.Hour)
		switch op.Op {
		case "assert":
			basis := time.Time{}
			if strings.TrimSpace(op.BasisAt) != "" {
				parsed, err := time.Parse("2006-01-02", op.BasisAt)
				if err != nil {
					return fmt.Errorf("op %d basisAt: %w", i, err)
				}
				basis = parsed
			}
			if _, err := store.UpsertFact(wiki.FactInput{
				Subject: c.Subject, Key: c.Key, Value: op.Value,
				Kind:      wiki.FactKind(c.Kind),
				Authority: wiki.FactAuthority(op.Authority),
				Actor:     "fact-bench", At: when, BasisAt: basis,
				Sources: []string{"bench:" + c.ID},
			}); err != nil {
				return fmt.Errorf("op %d assert: %w", i, err)
			}
		case "forget":
			if _, err := store.TombstoneFact(wiki.FactTombstoneInput{
				Subject: c.Subject, Key: c.Key,
				Authority: wiki.FactAuthority(op.Authority),
				Actor:     "fact-bench", At: when,
				Sources: []string{"bench:" + c.ID},
			}); err != nil {
				return fmt.Errorf("op %d forget: %w", i, err)
			}
		default:
			return fmt.Errorf("op %d has unknown kind %q", i, op.Op)
		}
	}
	return nil
}
