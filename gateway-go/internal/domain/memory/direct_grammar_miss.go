// direct_grammar_miss.go — the feedback edge of the direct-memory grammar.
//
// The catalog is deliberately precision-first: a phrasing it does not cover is
// dropped rather than guessed at. That is the right default and a silent one —
// the operator only finds out later, when the agent has forgotten something it
// was told. This records those near misses (an explicit remember/forward/forget
// lead that bound to no axis) so the gap is evidence a later pass can extend
// direct_grammar.json from, instead of folklore.
package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// directMemoryLeads name the openings that make a turn a memory COMMAND rather
// than conversation. They are intentionally broader than the binding grammar:
// this is a diagnostic, and a false positive here costs one ledger line.
var directMemoryLeads = []struct {
	name string
	re   *regexp.Regexp
}{
	{"remember", regexp.MustCompile(`(?i)^\s*(?:기억\s*(?:해|해줘|해줘요|해\s*둬)|(?:please\s+)?remember\b)`)},
	{"trailing_remember", regexp.MustCompile(`(?i)기억\s*(?:해|해줘|해줘요|해\s*둬)\s*[.!?。！？]*\s*$`)},
	// No trailing \b on the Korean alternatives: Go's \b is an ASCII word
	// boundary, and a Hangul syllable is not an ASCII word character, so
	// "앞으로는 보고서를…" and "아니야, 그건…" never matched while their English
	// twins did. In a Korean-first product that silently removed the two most
	// common ways to state a standing preference or a correction from the miss
	// ledger. \b stays where it belongs — attached to the ASCII alternative.
	// A looser Korean match is the documented trade above: this is a
	// diagnostic, and a false positive costs one ledger line.
	{"forward", regexp.MustCompile(`(?i)^\s*(?:앞으로(?:는)?|다음부터|from\s+now\s+on\b)`)},
	{"correction", regexp.MustCompile(`(?i)^\s*(?:아니(?:야|요)?|아님|정정|수정)`)},
	{"forget", regexp.MustCompile(`(?i)(?:기억.{0,20}(?:지워|삭제|잊)|\b(?:forget|delete|remove|erase)\b.{0,40}\b(?:memory|memories|preference|preferences|fact|facts|profile)\b)`)},
}

// DirectMemoryLead reports which command shape a message opens with, if any.
func DirectMemoryLead(message string) (string, bool) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", false
	}
	for _, lead := range directMemoryLeads {
		if lead.re.MatchString(message) {
			return lead.name, true
		}
	}
	return "", false
}

// DirectMemoryMiss is one command-shaped turn the catalog did not bind.
type DirectMemoryMiss struct {
	AtMs    int64  `json:"atMs"`
	Lead    string `json:"lead"`
	Target  string `json:"target"`
	Route   string `json:"route"`
	Message string `json:"message"`
}

// DefaultGrammarMissPath is the ledger the improvement loop reads to find
// phrasings worth adding to direct_grammar.json.
func DefaultGrammarMissPath(stateDir string) string {
	return filepath.Join(stateDir, "data", "memory_grammar_misses.jsonl")
}

const (
	directMemoryMissMaxBytes   = 1 << 20
	directMemoryMissMaxMessage = 400
)

// RecordDirectMemoryMiss appends one miss. It is a diagnostic, so it never
// blocks or fails a turn: an unwritable ledger returns an error the caller logs
// and moves past. The file is capped, and stops accepting rows rather than
// growing without bound — the tail is not more valuable than the head here,
// because the point is to notice a gap at all.
func RecordDirectMemoryMiss(path string, miss DirectMemoryMiss) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("memory: grammar miss ledger path is empty")
	}
	if info, err := os.Stat(path); err == nil && info.Size() >= directMemoryMissMaxBytes {
		return nil
	}
	if miss.AtMs == 0 {
		miss.AtMs = time.Now().UnixMilli()
	}
	miss.Message = truncateRunes(strings.Join(strings.Fields(miss.Message), " "), directMemoryMissMaxMessage)
	if miss.Message == "" {
		return nil
	}
	raw, err := json.Marshal(miss)
	if err != nil {
		return fmt.Errorf("memory: marshal grammar miss: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("memory: grammar miss ledger dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("memory: open grammar miss ledger: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("memory: append grammar miss: %w", err)
	}
	return nil
}

// DirectMemoryMissFor returns the ledger row for a command-shaped turn that did
// not reach the canonical fact plane, or false when the turn was bound (or was
// never a command in the first place).
func DirectMemoryMissFor(message string, ind *Induced) (DirectMemoryMiss, bool) {
	if ind == nil || ind.Route == RouteMemory {
		return DirectMemoryMiss{}, false
	}
	lead, found := DirectMemoryLead(message)
	if !found {
		return DirectMemoryMiss{}, false
	}
	return DirectMemoryMiss{
		Lead:    lead,
		Target:  string(ind.Candidate.Target),
		Route:   string(ind.Route),
		Message: message,
	}, true
}
