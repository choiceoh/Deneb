package corecache

import (
	"testing"
	"time"
)

func TestLRU_GetPut(t *testing.T) {
	c := NewLRU[string, int](3, 0)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	for _, tc := range []struct {
		key  string
		want int
	}{{"a", 1}, {"b", 2}, {"c", 3}} {
		v, ok := c.Get(tc.key)
		if !ok || v != tc.want {
			t.Errorf("Get(%q) = %d, %v; want %d, true", tc.key, v, ok, tc.want)
		}
	}
	if c.Len() != 3 {
		t.Errorf("Len() = %d; want 3", c.Len())
	}
}


func TestLRU_Eviction(t *testing.T) {
	c := NewLRU[string, int](2, 0)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // evicts "a" (oldest)

	if _, ok := c.Get("a"); ok {
		t.Fatal("expected 'a' to be evicted")
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Errorf("Get(b) = %d, %v; want 2, true", v, ok)
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Errorf("Get(c) = %d, %v; want 3, true", v, ok)
	}
}

func TestLRU_AccessPromotes(t *testing.T) {
	c := NewLRU[string, int](2, 0)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a")    // promote "a", "b" becomes LRU
	c.Put("c", 3) // evicts "b"

	if _, ok := c.Get("b"); ok {
		t.Fatal("expected 'b' to be evicted (LRU)")
	}
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Errorf("Get(a) = %d, %v; want 1, true", v, ok)
	}
}

func TestLRU_UpdateExisting(t *testing.T) {
	c := NewLRU[string, int](2, 0)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("a", 10) // update, promotes "a"

	if v, ok := c.Get("a"); !ok || v != 10 {
		t.Errorf("Get(a) = %d, %v; want 10, true", v, ok)
	}
	// "b" should still be there (update doesn't evict).
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Errorf("Get(b) = %d, %v; want 2, true", v, ok)
	}
}

func TestLRU_Delete(t *testing.T) {
	c := NewLRU[string, int](3, 0)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Delete("a")

	if _, ok := c.Get("a"); ok {
		t.Fatal("expected 'a' deleted")
	}
	if c.Len() != 1 {
		t.Errorf("Len() = %d; want 1", c.Len())
	}
}


func TestLRU_TTLExpiry(t *testing.T) {
	c := NewLRU[string, int](10, 50*time.Millisecond)
	c.Put("a", 1)

	if _, ok := c.Get("a"); !ok {
		t.Fatal("expected hit before TTL")
	}
	time.Sleep(60 * time.Millisecond)
	if _, ok := c.Get("a"); ok {
		t.Fatal("expected miss after TTL")
	}
	if c.Len() != 0 {
		t.Errorf("Len() = %d; want 0 (lazy delete)", c.Len())
	}
}

func TestLRU_Cleanup(t *testing.T) {
	c := NewLRU[string, int](10, 50*time.Millisecond)
	c.Put("a", 1)
	c.Put("b", 2)
	time.Sleep(60 * time.Millisecond)
	c.Put("c", 3) // not expired

	removed := c.Cleanup()
	if removed != 2 {
		t.Errorf("Cleanup() removed %d; want 2", removed)
	}
	if c.Len() != 1 {
		t.Errorf("Len() = %d; want 1", c.Len())
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("expected 'c' to survive cleanup")
	}
}



