package contacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestNewStoreAbsentCorruptAndReadFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.json")
	store, err := NewStore(missing)
	if err != nil {
		t.Fatalf("missing store: %v", err)
	}
	if store.Count() != 0 || store.path != missing {
		t.Fatalf("missing store = %+v", store)
	}

	corrupt := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(corrupt); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("corrupt error = %v", err)
	}

	if _, err := NewStore(t.TempDir()); err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("directory error = %v", err)
	}
}

func TestReplaceAllOwnsInputAndResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	input := []Contact{{
		Name:   "Kim",
		Phones: []string{"010-1111-2222"},
		Emails: []string{"kim@example.com"},
		Org:    "Original",
	}}
	if n, err := store.ReplaceAll(input); err != nil || n != 1 {
		t.Fatalf("ReplaceAll = %d,%v", n, err)
	}

	input[0].Name = "Mutated"
	input[0].Org = "Changed"
	input[0].Phones[0] = "010-9999-9999"
	input[0].Emails[0] = "mutated@example.com"
	got := store.All()
	if len(got) != 1 || got[0].Name != "Kim" || got[0].Org != "Original" ||
		got[0].Phones[0] != "010-1111-2222" || got[0].Emails[0] != "kim@example.com" {
		t.Fatalf("caller mutation leaked into store: %+v", got)
	}
	if store.LookupPhone("01099999999") != nil || store.HasEmail("mutated@example.com") {
		t.Fatal("caller mutation leaked into indexes")
	}

	got[0].Name = "Output mutation"
	got[0].Phones[0] = "010-3333-4444"
	got[0].Emails[0] = "output@example.com"
	again := store.All()
	if again[0].Name != "Kim" || again[0].Phones[0] != "010-1111-2222" || again[0].Emails[0] != "kim@example.com" {
		t.Fatalf("All result aliases store state: %+v", again)
	}

	phone := store.LookupPhone("01011112222")
	phone[0].Phones[0] = "broken"
	email := store.LookupEmail("kim@example.com")
	email[0].Emails[0] = "broken@example.com"
	search := store.Search("Kim", 1)
	search[0].Org = "broken"
	final := store.All()[0]
	if final.Phones[0] == "broken" || final.Emails[0] == "broken@example.com" || final.Org == "broken" {
		t.Fatalf("lookup result aliases store state: %+v", final)
	}
}

func TestReplaceAllPersistenceFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "contacts.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	original := []Contact{{Name: "Original", Phones: []string{"010-1111-2222"}, Emails: []string{"one@example.com"}}}
	if _, err := store.ReplaceAll(original); err != nil {
		t.Fatalf("seed ReplaceAll: %v", err)
	}

	parentFile := filepath.Join(t.TempDir(), "not-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(parentFile, "contacts.json")
	replacement := []Contact{{Name: "Replacement", Phones: []string{"010-9999-8888"}, Emails: []string{"two@example.com"}}}
	if n, err := store.ReplaceAll(replacement); err == nil || n != 1 {
		t.Fatalf("failed ReplaceAll = %d,%v", n, err)
	}
	if got := store.All(); !reflect.DeepEqual(got, original) {
		t.Fatalf("failed ReplaceAll changed snapshot: %+v", got)
	}
	if len(store.LookupPhone("01011112222")) != 1 || store.HasEmail("two@example.com") {
		t.Fatal("failed ReplaceAll changed indexes")
	}
}

func TestReplaceAllEmptyAndNilSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contacts []Contact
	}{
		{name: "nil", contacts: nil},
		{name: "empty", contacts: []Contact{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "contacts.json")
			store, _ := NewStore(path)
			if n, err := store.ReplaceAll(tc.contacts); err != nil || n != 0 {
				t.Fatalf("ReplaceAll = %d,%v", n, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var decoded []Contact
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("persisted JSON %q: %v", data, err)
			}
			if len(decoded) != 0 || store.Count() != 0 {
				t.Fatalf("decoded=%+v count=%d", decoded, store.Count())
			}
		})
	}
}

func TestNormalizePhoneAndTailDigitsBoundaries(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: "+82 (10) 1234-5678", want: "01012345678"},
		{input: "82-2-555-0000", want: "025550000"},
		{input: "010.1234.5678 ext 99", want: "0101234567899"},
		{input: "abc", want: ""},
		{input: "82", want: "82"},
	} {
		if got := normalizePhone(tc.input); got != tc.want {
			t.Errorf("normalizePhone(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
	for _, tc := range []struct {
		input string
		n     int
		want  string
	}{
		{input: "12345678", n: 8, want: "12345678"},
		{input: "0012345678", n: 8, want: "12345678"},
		{input: "", n: 8, want: ""},
		{input: "123", n: 8, want: "123"},
	} {
		if got := tailDigits(tc.input, tc.n); got != tc.want {
			t.Errorf("tailDigits(%q,%d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}

func TestLookupPhoneFallbackDeduplicatesAndPreservesOrder(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	contacts := []Contact{
		{Name: "First", Phones: []string{"+1 212 5555 1234", "001-212-5555-1234"}},
		{Name: "Second", Phones: []string{"+44 20 5555 1234"}},
		{Name: "Other", Phones: []string{"010-0000-0000"}},
	}
	if _, err := store.ReplaceAll(contacts); err != nil {
		t.Fatal(err)
	}
	got := store.LookupPhone("55551234")
	if len(got) != 2 {
		t.Fatalf("fallback results = %+v", got)
	}
	if got[0].Name != "First" || got[1].Name != "Second" {
		t.Fatalf("fallback order = %+v", got)
	}
	if got := store.LookupPhone("letters only"); got != nil {
		t.Fatalf("empty normalized lookup = %+v", got)
	}
}

func TestSearchEveryFieldLimitAndCase(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	contacts := []Contact{
		{Name: "Alice Kim", Org: "Solar One", Emails: []string{"ALICE@EXAMPLE.COM"}, Phones: []string{"010-1111-2222"}},
		{Name: "Alice Park", Org: "Solar Two", Emails: []string{"park@example.com"}, Phones: []string{"010-3333-4444"}},
		{Name: "Bob", Org: "Wind", Emails: []string{"bob@example.com"}, Phones: []string{"010-5555-6666"}},
	}
	store.ReplaceAll(contacts)
	for _, tc := range []struct {
		query string
		want  string
	}{
		{query: "alice", want: "Alice Kim"},
		{query: "SOLAR", want: "Alice Kim"},
		{query: "example.com", want: "Alice Kim"},
		{query: "1111", want: "Alice Kim"},
	} {
		got := store.Search(tc.query, 1)
		if len(got) != 1 || got[0].Name != tc.want {
			t.Errorf("Search(%q) = %+v, want %q", tc.query, got, tc.want)
		}
	}
	if got := store.Search("alice", 1); len(got) != 1 {
		t.Fatalf("limited search = %+v", got)
	}
	if got := store.Search("   ", 3); got != nil {
		t.Fatalf("blank search = %+v", got)
	}
}

func TestEmailIndexTrimsCasesDuplicatesAndEmpty(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	store.ReplaceAll([]Contact{
		{Name: "One", Emails: []string{" Shared@Example.com ", ""}},
		{Name: "Two", Emails: []string{"shared@example.com"}},
	})
	if !store.HasEmail(" SHARED@example.COM ") || store.HasEmail(" ") {
		t.Fatalf("HasEmail normalization failed")
	}
	got := store.LookupEmail("shared@example.com")
	if len(got) != 2 || got[0].Name != "One" || got[1].Name != "Two" {
		t.Fatalf("shared email lookup = %+v", got)
	}
	if got := store.LookupEmail(" "); got != nil {
		t.Fatalf("blank email lookup = %+v", got)
	}
}

func TestHotwordHintsDeduplicatesAndBoundsTerms(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	store.ReplaceAll([]Contact{
		{Name: "Alice", Org: "Solar"},
		{Name: "Alice", Org: "Solar"},
		{Name: "Bob"},
		{Name: "", Org: ""},
	})
	hints := store.HotwordHints(10)
	if strings.Count(hints, "Alice") != 1 || strings.Count(hints, "Solar") != 1 || !strings.Contains(hints, "Bob") {
		t.Fatalf("hotword deduplication = %q", hints)
	}
	if got := store.HotwordHints(2); strings.Count(got, ",") > 1 {
		t.Fatalf("two-term cap = %q", got)
	}
}

func TestStoreConcurrentReplaceAndRead(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	books := [][]Contact{
		{{Name: "One", Phones: []string{"010-1111-2222"}, Emails: []string{"one@example.com"}}},
		{{Name: "Two", Phones: []string{"010-3333-4444"}, Emails: []string{"two@example.com"}}},
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				if i%4 == 0 {
					_, _ = store.ReplaceAll(books[(i+j)%2])
					continue
				}
				_ = store.Count()
				_ = store.All()
				_ = store.LookupPhone("01011112222")
				_ = store.LookupEmail("one@example.com")
				_ = store.HasEmail("two@example.com")
				_ = store.Search("o", 20)
				_ = store.HotwordHints(20)
			}
		}(i)
	}
	wg.Wait()
	if store.Count() != 1 {
		t.Fatalf("final count = %d", store.Count())
	}
}
