package backup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestExpandTargets_ResolvesGlobsAndLeavesPlainNames: the recall gold sets are
// added as a glob because new sets appear over time (meeting, facet, traffic…),
// and their ".bak-*" copies must stay out. A pattern that matches nothing has
// to behave like a store that is not deployed.
func TestExpandTargets_ResolvesGlobsAndLeavesPlainNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"wiki-qa-gold.jsonl",
		"wiki-qa-gold-meeting-xl.jsonl",
		"wiki-qa-gold.jsonl.bak-1783451388", // a backup of a backup: not wanted
		"contacts.json",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := expandTargets(dir, []string{"wiki", "contacts.json", "wiki-qa-gold*.jsonl", "absent*.json"})

	want := []string{"wiki", "contacts.json", "wiki-qa-gold-meeting-xl.jsonl", "wiki-qa-gold.jsonl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandTargets = %v, want %v", got, want)
	}
}
