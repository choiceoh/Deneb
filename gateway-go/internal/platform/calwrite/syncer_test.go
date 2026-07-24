package calwrite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// fakeRemote records calls and lets a test force failures.
type fakeRemote struct {
	inserts    []string // localIDs inserted
	patches    []string // googleIDs patched
	deletes    []string // googleIDs deleted
	seq        int
	failInsert bool
	failPatch  bool
	failDelete bool
}

func (f *fakeRemote) Insert(_ context.Context, localID string, _ calendar.Event) (string, error) {
	if f.failInsert {
		return "", errors.New("insert boom")
	}
	f.seq++
	f.inserts = append(f.inserts, localID)
	return fmt.Sprintf("g%d", f.seq), nil
}

func (f *fakeRemote) Patch(_ context.Context, googleID, _ string, _ calendar.Event) error {
	if f.failPatch {
		return errors.New("patch boom")
	}
	f.patches = append(f.patches, googleID)
	return nil
}

func (f *fakeRemote) Delete(_ context.Context, googleID string) error {
	if f.failDelete {
		return errors.New("delete boom")
	}
	f.deletes = append(f.deletes, googleID)
	return nil
}

func newSyncer(t *testing.T, r remote) (*Syncer, *[]string) {
	t.Helper()
	var warns []string
	s, err := NewSyncer(filepath.Join(t.TempDir(), "calendar-sync.json"), r, func(op string, err error) {
		warns = append(warns, op+": "+err.Error())
	})
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	return s, &warns
}

func ev(summary string) calendar.Event {
	return calendar.Event{Summary: summary, Start: time.Now(), End: time.Now().Add(time.Hour)}
}

func TestPush_FirstTimeInsertsAndMaps(t *testing.T) {
	r := &fakeRemote{}
	s, _ := newSyncer(t, r)
	if err := s.Push(context.Background(), "local:1", ev("A")); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(r.inserts) != 1 || r.inserts[0] != "local:1" {
		t.Errorf("inserts = %v", r.inserts)
	}
	if got := s.MirroredGoogleIDs(); len(got) != 1 {
		t.Errorf("mirrored ids = %v, want one", got)
	}
}

func TestPush_SecondTimePatchesSameGoogleEvent(t *testing.T) {
	r := &fakeRemote{}
	s, _ := newSyncer(t, r)
	_ = s.Push(context.Background(), "local:1", ev("A"))
	if err := s.Push(context.Background(), "local:1", ev("A edited")); err != nil {
		t.Fatalf("Push (2): %v", err)
	}
	if len(r.inserts) != 1 {
		t.Errorf("should insert once, got inserts=%v", r.inserts)
	}
	if len(r.patches) != 1 || r.patches[0] != "g1" {
		t.Errorf("second push should PATCH g1, got patches=%v", r.patches)
	}
}

func TestRemove_DeletesGoogleEventAndUnmaps(t *testing.T) {
	r := &fakeRemote{}
	s, _ := newSyncer(t, r)
	_ = s.Push(context.Background(), "local:1", ev("A"))
	if err := s.Remove(context.Background(), "local:1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(r.deletes) != 1 || r.deletes[0] != "g1" {
		t.Errorf("deletes = %v, want [g1]", r.deletes)
	}
	if got := s.MirroredGoogleIDs(); len(got) != 0 {
		t.Errorf("mapping should be dropped, got %v", got)
	}
}

func TestRemove_UnknownLocalIDIsNoop(t *testing.T) {
	r := &fakeRemote{}
	s, _ := newSyncer(t, r)
	if err := s.Remove(context.Background(), "local:never"); err != nil {
		t.Fatalf("Remove unknown: %v", err)
	}
	if len(r.deletes) != 0 {
		t.Errorf("unknown local id must not DELETE, got %v", r.deletes)
	}
}

func TestPush_InsertFailureReportsAndLeavesNoMapping(t *testing.T) {
	r := &fakeRemote{failInsert: true}
	s, warns := newSyncer(t, r)
	if err := s.Push(context.Background(), "local:1", ev("A")); err == nil {
		t.Fatal("expected error from failing insert")
	}
	if len(*warns) != 1 {
		t.Errorf("warn sink should fire once, got %v", *warns)
	}
	if got := s.MirroredGoogleIDs(); len(got) != 0 {
		t.Errorf("failed insert must not leave a mapping, got %v", got)
	}
}

func TestSyncer_MappingPersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "calendar-sync.json")
	r := &fakeRemote{}
	s, err := NewSyncer(path, r, nil)
	if err != nil {
		t.Fatalf("NewSyncer: %v", err)
	}
	if err := s.Push(context.Background(), "local:1", ev("A")); err != nil {
		t.Fatalf("Push: %v", err)
	}

	// A fresh syncer over the same file must see the pairing, so a later edit
	// patches (not re-inserts) the same Google event after a restart.
	r2 := &fakeRemote{}
	s2, err := NewSyncer(path, r2, nil)
	if err != nil {
		t.Fatalf("NewSyncer reload: %v", err)
	}
	if err := s2.Push(context.Background(), "local:1", ev("A again")); err != nil {
		t.Fatalf("Push after reload: %v", err)
	}
	if len(r2.inserts) != 0 || len(r2.patches) != 1 {
		t.Errorf("after reload expected a PATCH, got inserts=%v patches=%v", r2.inserts, r2.patches)
	}
}
