package toolctx

import "testing"

func TestActiveNotebookBinding(t *testing.T) {
	const sk = "client:nb-binding-test"
	ClearActiveNotebook(sk)

	if got := ActiveNotebook(sk); got != "" {
		t.Fatalf("unbound session should be empty, got %q", got)
	}
	SetActiveNotebook(sk, "topsolar")
	if got := ActiveNotebook(sk); got != "topsolar" {
		t.Fatalf("ActiveNotebook = %q, want topsolar", got)
	}
	// Last-write-wins: opening another notebook in the same session switches scope.
	SetActiveNotebook(sk, "wattradi")
	if got := ActiveNotebook(sk); got != "wattradi" {
		t.Fatalf("last-write-wins failed: %q", got)
	}
	ClearActiveNotebook(sk)
	if got := ActiveNotebook(sk); got != "" {
		t.Fatalf("after clear, want empty, got %q", got)
	}
}

func TestDedicatedNotebookSession(t *testing.T) {
	if got := DedicatedNotebookID("notebook:topsolar"); got != "topsolar" {
		t.Fatalf("DedicatedNotebookID = %q, want topsolar", got)
	}
	if got := DedicatedNotebookID("client:main"); got != "" {
		t.Fatalf("non-notebook session should derive empty, got %q", got)
	}
	if got := DedicatedNotebookID("notebook:"); got != "" {
		t.Fatalf("bare notebook: prefix should derive empty, got %q", got)
	}

	// A dedicated session grounds from the key alone — no explicit binding.
	const sk = "notebook:topsolar"
	ClearActiveNotebook(sk)
	if got := ActiveNotebook(sk); got != "topsolar" {
		t.Fatalf("dedicated session ActiveNotebook = %q, want topsolar", got)
	}
	// The key-derived notebook wins over any (stale) explicit binding.
	SetActiveNotebook(sk, "other")
	if got := ActiveNotebook(sk); got != "topsolar" {
		t.Fatalf("dedicated derivation must win over explicit binding, got %q", got)
	}
}

func TestActiveNotebookBindingEmptyKeySafe(t *testing.T) {
	// Empty session key / id are no-ops, never binding the empty session.
	SetActiveNotebook("", "x")
	if got := ActiveNotebook(""); got != "" {
		t.Fatalf("empty key must stay empty, got %q", got)
	}
	SetActiveNotebook("client:has-key", "")
	if got := ActiveNotebook("client:has-key"); got != "" {
		t.Fatalf("empty id must not bind, got %q", got)
	}
}
