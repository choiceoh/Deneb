package localtodo

import (
	"path/filepath"
	"testing"
)

func newTempStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "todos.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestCreateIfAbsent_DedupsBySource(t *testing.T) {
	s := newTempStore(t)
	in := CreateInput{Title: "계약서 검토", Source: "mail:m1|계약서 검토"}

	td1, created1, err := s.CreateIfAbsent(in)
	if err != nil || !created1 {
		t.Fatalf("first CreateIfAbsent: created=%v err=%v", created1, err)
	}
	td2, created2, err := s.CreateIfAbsent(in)
	if err != nil {
		t.Fatalf("second CreateIfAbsent: %v", err)
	}
	if created2 {
		t.Error("second CreateIfAbsent with same Source should not create")
	}
	if td2.ID != td1.ID {
		t.Errorf("dedup should return the existing to-do: %s vs %s", td2.ID, td1.ID)
	}
	if got := len(s.List()); got != 1 {
		t.Errorf("expected 1 to-do after dedup, got %d", got)
	}
	if td1.Source != "mail:m1|계약서 검토" {
		t.Errorf("Source not persisted on created to-do: %q", td1.Source)
	}
}

func TestCreateIfAbsent_EmptySourceAlwaysCreates(t *testing.T) {
	s := newTempStore(t)
	if _, c1, _ := s.CreateIfAbsent(CreateInput{Title: "A"}); !c1 {
		t.Error("first empty-source create should succeed")
	}
	if _, c2, _ := s.CreateIfAbsent(CreateInput{Title: "A"}); !c2 {
		t.Error("second empty-source create should also create (no dedup)")
	}
	if got := len(s.List()); got != 2 {
		t.Errorf("expected 2 to-dos with empty source, got %d", got)
	}
}

func TestCreateIfAbsent_PersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "todos.json")
	s1, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := s1.CreateIfAbsent(CreateInput{Title: "x", Source: "mail:m9|x"}); err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	// Fresh store from the same file must see the persisted Source and dedup.
	s2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := s2.CreateIfAbsent(CreateInput{Title: "x", Source: "mail:m9|x"}); err != nil || created {
		t.Errorf("reloaded store should dedup by persisted Source: created=%v err=%v", created, err)
	}
}
