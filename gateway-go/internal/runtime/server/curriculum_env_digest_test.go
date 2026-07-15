package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
)

// The server adapter wires its stores into the curriculumenv digest. Digest
// FORMAT is covered by runtime/curriculumenv; this only proves the wiring
// (a wired feed store reaches the digest, no store yields "").
func TestCurriculumEnvDigestReturnsWiredFeedContent(t *testing.T) {
	dir := t.TempDir()
	store := domainbind.NewWorkFeedStore(filepath.Join(dir, "workfeed.jsonl"))
	if _, err := store.Append(domainbind.Item{Source: "test", Title: "계약 검토 — NDA 초안"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		MemorySubsystem:     &MemorySubsystem{workFeedStore: store},
		AutonomousSubsystem: &AutonomousSubsystem{}, // always set in New(); agentLogWriter stays nil here
	}
	if got := s.curriculumEnvDigest(context.Background()); !strings.Contains(got, "계약 검토") {
		t.Fatalf("wired feed store should reach the digest:\n%s", got)
	}
}

// No stores wired (dev/test) → empty digest, quiet.
func TestCurriculumEnvDigest_Empty(t *testing.T) {
	s := &Server{MemorySubsystem: &MemorySubsystem{}, AutonomousSubsystem: &AutonomousSubsystem{}}
	if got := s.curriculumEnvDigest(context.Background()); got != "" {
		t.Fatalf("empty stores should yield empty digest, got %q", got)
	}
}
