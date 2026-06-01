// Package contacts mirrors the device address book on the gateway as a flat,
// fully-replaced snapshot. It's deliberately separate from the wiki: the wiki
// stays a curated knowledge base of the people/projects the user actively works
// on, while this is a bulk lookup table that answers "whose number is this?",
// powers name search, and feeds ASR proper-noun bias — none of which should
// pollute the wiki with hundreds of contact pages.
//
// Single-user, single-writer; a process-wide RWMutex is sufficient.
package contacts

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// Contact is one address-book entry synced from the native client. The shape
// matches the client's ContactData and the gateway's wiki.Contact.
type Contact struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones,omitempty"`
	Emails []string `json:"emails,omitempty"`
	Org    string   `json:"org,omitempty"`
}

// Store holds the whole address book. The client sends the entire book on each
// sync, so writes fully replace the snapshot rather than merging.
type Store struct {
	mu      sync.RWMutex
	path    string
	all     []Contact
	byPhone map[string][]int // normalized phone -> indices into all
}

// NewStore loads the snapshot from path (an empty store if the file is absent).
func NewStore(path string) (*Store, error) {
	s := &Store{path: path, byPhone: map[string][]int{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("contacts: read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &s.all); err != nil {
		return nil, fmt.Errorf("contacts: parse %s: %w", path, err)
	}
	s.reindexLocked()
	return s, nil
}

// ReplaceAll swaps the entire snapshot and persists it. Returns the stored count.
func (s *Store) ReplaceAll(contacts []Contact) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.all = contacts
	s.reindexLocked()
	if err := s.persistLocked(); err != nil {
		return len(s.all), err
	}
	return len(s.all), nil
}

// Count returns the number of stored contacts.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.all)
}

// LookupPhone returns contacts whose number matches phone. It first tries an
// exact normalized hit, then falls back to matching the trailing local digits so
// country-code/formatting differences (+82 vs 0, dashes/spaces) don't matter.
func (s *Store) LookupPhone(phone string) []Contact {
	key := normalizePhone(phone)
	if key == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if idxs, ok := s.byPhone[key]; ok {
		return s.collectLocked(idxs)
	}
	tail := tailDigits(key, 8)
	if tail == "" {
		return nil
	}
	seen := map[int]bool{}
	var out []Contact
	for k, idxs := range s.byPhone {
		if tailDigits(k, 8) != tail {
			continue
		}
		for _, i := range idxs {
			if !seen[i] {
				seen[i] = true
				out = append(out, s.all[i])
			}
		}
	}
	return out
}

// Search returns contacts whose name, org, email, or phone contains query
// (case-insensitive substring), capped at limit (default 20).
func (s *Store) Search(query string, limit int) []Contact {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	digits := normalizePhone(query)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Contact
	for i := range s.all {
		c := &s.all[i]
		if strings.Contains(strings.ToLower(c.Name), q) ||
			strings.Contains(strings.ToLower(c.Org), q) ||
			anyContains(c.Emails, q) ||
			(digits != "" && anyPhoneContains(c.Phones, digits)) {
			out = append(out, *c)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// HotwordHints builds a comma-separated proper-noun bias list (names + orgs) for
// ASR, capped at maxTerms/2500 chars. Contacts that carry an org (work contacts)
// rank first so the most useful names survive the cap.
func (s *Store) HotwordHints(maxTerms int) string {
	if maxTerms <= 0 {
		maxTerms = 200
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	withOrg := make([]int, 0, len(s.all))
	withoutOrg := make([]int, 0, len(s.all))
	for i := range s.all {
		if strings.TrimSpace(s.all[i].Org) != "" {
			withOrg = append(withOrg, i)
		} else {
			withoutOrg = append(withoutOrg, i)
		}
	}

	const maxChars = 2500
	seen := make(map[string]bool)
	terms := make([]string, 0, maxTerms)
	chars := 0
	add := func(raw string) bool {
		t := strings.TrimSpace(raw)
		if t == "" {
			return true
		}
		key := strings.ToLower(t)
		if seen[key] {
			return true
		}
		if len(terms) >= maxTerms || chars+len(t) > maxChars {
			return false
		}
		seen[key] = true
		terms = append(terms, t)
		chars += len(t) + 2
		return true
	}
	for _, group := range [][]int{withOrg, withoutOrg} {
		for _, i := range group {
			if !add(s.all[i].Name) {
				return strings.Join(terms, ", ")
			}
			if !add(s.all[i].Org) {
				return strings.Join(terms, ", ")
			}
		}
	}
	return strings.Join(terms, ", ")
}

func (s *Store) reindexLocked() {
	idx := make(map[string][]int, len(s.all))
	for i := range s.all {
		for _, p := range s.all[i].Phones {
			if key := normalizePhone(p); key != "" {
				idx[key] = append(idx[key], i)
			}
		}
	}
	s.byPhone = idx
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.all, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // G306 — world-readable is intentional
		return fmt.Errorf("contacts: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("contacts: rename: %w", err)
	}
	return nil
}

func (s *Store) collectLocked(idxs []int) []Contact {
	out := make([]Contact, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, s.all[i])
	}
	return out
}

// normalizePhone strips to digits and maps a Korean +82 country code to the
// national 0 prefix ("+82 10-1234-5678" -> "01012345678").
func normalizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteByte(byte(r))
		}
	}
	d := b.String()
	if strings.HasPrefix(d, "82") && len(d) > 2 {
		d = "0" + d[2:]
	}
	return d
}

func tailDigits(d string, n int) string {
	if len(d) <= n {
		return d
	}
	return d[len(d)-n:]
}

func anyContains(ss []string, q string) bool {
	for _, s := range ss {
		if strings.Contains(strings.ToLower(s), q) {
			return true
		}
	}
	return false
}

func anyPhoneContains(phones []string, digits string) bool {
	for _, p := range phones {
		if strings.Contains(normalizePhone(p), digits) {
			return true
		}
	}
	return false
}
