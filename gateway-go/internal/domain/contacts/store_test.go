package contacts

import (
	"path/filepath"
	"strings"
	"testing"
)

func sample() []Contact {
	return []Contact{
		{Name: "김민준", Phones: []string{"010-1234-5678"}, Emails: []string{"minjun@topsolar.kr"}, Org: "탑솔라"},
		{Name: "이서연", Phones: []string{"+82 10-9876-5432"}, Org: "에코프로"},
		{Name: "박지호", Phones: []string{"02-555-0000"}},
	}
}

func TestReplaceAllAndCount(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.ReplaceAll(sample()); err != nil || n != 3 || s.Count() != 3 {
		t.Fatalf("ReplaceAll = %d,%v; Count = %d", n, err, s.Count())
	}
	// A sync fully replaces (not merges): a shorter book shrinks the store.
	if n, _ := s.ReplaceAll(sample()[:1]); n != 1 || s.Count() != 1 {
		t.Fatalf("replace count = %d, want 1", s.Count())
	}
}

func TestLookupPhone(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "c.json"))
	s.ReplaceAll(sample())

	if got := s.LookupPhone("01012345678"); len(got) != 1 || got[0].Name != "김민준" {
		t.Errorf("exact lookup = %v", got)
	}
	if got := s.LookupPhone("010-1234-5678"); len(got) != 1 || got[0].Name != "김민준" {
		t.Errorf("formatted lookup = %v", got)
	}
	// +82 stored, national queried.
	if got := s.LookupPhone("010-9876-5432"); len(got) != 1 || got[0].Name != "이서연" {
		t.Errorf("+82-stored lookup = %v", got)
	}
	// national stored, +82 queried (trailing-digit fallback).
	if got := s.LookupPhone("+82 10 1234 5678"); len(got) != 1 || got[0].Name != "김민준" {
		t.Errorf("+82-query lookup = %v", got)
	}
	if got := s.LookupPhone("010-0000-0000"); len(got) != 0 {
		t.Errorf("unknown number should miss, got %v", got)
	}
}

func TestSearch(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "c.json"))
	s.ReplaceAll(sample())

	if got := s.Search("탑솔라", 0); len(got) != 1 || got[0].Name != "김민준" {
		t.Errorf("org search = %v", got)
	}
	if got := s.Search("서연", 0); len(got) != 1 || got[0].Name != "이서연" {
		t.Errorf("name search = %v", got)
	}
	if got := s.Search("9876", 0); len(got) != 1 || got[0].Name != "이서연" {
		t.Errorf("phone-fragment search = %v", got)
	}
	if got := s.Search("없는키워드XYZ", 0); len(got) != 0 {
		t.Errorf("no-match search should be empty, got %v", got)
	}
}

func TestHotwordHints(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "c.json"))
	s.ReplaceAll(sample())

	h := s.HotwordHints(0)
	for _, want := range []string{"김민준", "탑솔라", "이서연", "에코프로", "박지호"} {
		if !strings.Contains(h, want) {
			t.Errorf("hotwords missing %q: %q", want, h)
		}
	}
	// Org-bearing contacts rank first, so the org-less 박지호 trails 탑솔라/에코프로.
	if strings.Index(h, "박지호") < strings.Index(h, "탑솔라") {
		t.Errorf("org contacts should rank before org-less: %q", h)
	}
	// maxTerms cap.
	if h2 := s.HotwordHints(1); strings.Count(h2, ",") > 0 {
		t.Errorf("maxTerms=1 should yield one term: %q", h2)
	}
}

func TestPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	s, _ := NewStore(path)
	s.ReplaceAll(sample())

	s2, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Count() != 3 {
		t.Errorf("reloaded count = %d, want 3", s2.Count())
	}
	if got := s2.LookupPhone("010-1234-5678"); len(got) != 1 {
		t.Errorf("reloaded lookup failed: %v", got)
	}
}
