package genesis

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

func saltEntries(t *testing.T, names ...string) []skills.SkillEntry {
	t.Helper()
	out := make([]skills.SkillEntry, 0, len(names))
	for _, n := range names {
		e := skills.SkillEntry{}
		e.Skill.Name = n
		out = append(out, e)
	}
	return out
}

func names(entries []skills.SkillEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Skill.Name)
	}
	return out
}

// The whole point of the salt: two judge versions must not retake the same
// exam, while ONE version must always get the same exam back — fairness within
// a cycle, rotation across cycles.
func TestSaltRotatesExamAcrossVersionsButNotWithinOne(t *testing.T) {
	entries := saltEntries(t, "a", "b", "c", "d", "e", "f", "g")

	one := names(rotateEntriesBySalt(entries, "judge-v1"))
	same := names(rotateEntriesBySalt(entries, "judge-v1"))
	for i := range one {
		if one[i] != same[i] {
			t.Fatalf("same salt produced different order: %v vs %v", one, same)
		}
	}

	// Across a range of salts the rotation must actually move — a hash that
	// happened to fix offset 0 forever would silently restore the frozen exam.
	moved := false
	for _, salt := range []string{"judge-v2", "judge-v3", "judge-v4", "judge-v5"} {
		other := names(rotateEntriesBySalt(entries, salt))
		if other[0] != one[0] {
			moved = true
			break
		}
	}
	if !moved {
		t.Fatalf("no salt in a version sequence rotated the exam away from %v", one)
	}
}

// Rotation must be a permutation — every skill still reachable, none repeated.
// A lossy "rotation" would shrink the exam pool instead of walking it.
func TestSaltRotationIsAPermutation(t *testing.T) {
	entries := saltEntries(t, "a", "b", "c", "d", "e")
	got := names(rotateEntriesBySalt(entries, "judge-v9"))
	if len(got) != len(entries) {
		t.Fatalf("rotation changed size: %v", got)
	}
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Fatalf("rotation duplicated %q: %v", n, got)
		}
		seen[n] = true
	}
	// And the original snapshot must be untouched (the catalog is shared).
	if entries[0].Skill.Name != "a" {
		t.Fatalf("rotation mutated the caller's slice: %v", names(entries))
	}
}

func TestSaltDegeneratesQuietlyOnTinyCatalogs(t *testing.T) {
	if got := rotateEntriesBySalt(nil, "v"); len(got) != 0 {
		t.Fatalf("nil entries rotated into %v", got)
	}
	one := saltEntries(t, "only")
	if got := names(rotateEntriesBySalt(one, "v")); len(got) != 1 || got[0] != "only" {
		t.Fatalf("single entry mangled: %v", got)
	}
}
