package chat

import "testing"

func TestNotebookGroundingCache(t *testing.T) {
	const sk = "client:nbg-cache-test"
	clearNotebookGrounding(sk)

	if _, ok := cachedNotebookGrounding(sk, "nb1", 100); ok {
		t.Fatal("empty cache should miss")
	}
	storeNotebookGrounding(sk, "nb1", 100, "block-v1")
	if v, ok := cachedNotebookGrounding(sk, "nb1", 100); !ok || v != "block-v1" {
		t.Fatalf("hit expected, got %q ok=%v", v, ok)
	}
	// A bumped Updated stamp (any pin/unpin/mode change) invalidates.
	if _, ok := cachedNotebookGrounding(sk, "nb1", 101); ok {
		t.Fatal("newer Updated must miss (content changed)")
	}
	// A different notebook id invalidates (session switched notebooks).
	if _, ok := cachedNotebookGrounding(sk, "nb2", 100); ok {
		t.Fatal("different notebook id must miss")
	}
	clearNotebookGrounding(sk)
	if _, ok := cachedNotebookGrounding(sk, "nb1", 100); ok {
		t.Fatal("after clear should miss")
	}
}
