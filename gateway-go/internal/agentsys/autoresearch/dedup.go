package autoresearch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HypothesisTracker tracks SHA-256 hashes of previously tried hypotheses
// to prevent the LLM from proposing identical changes across iterations.
type HypothesisTracker struct {
	// Hashes maps content hash -> iteration number where it was first tried.
	Hashes map[string]int `json:"hashes"`
}

// NewHypothesisTracker creates an empty tracker.
func NewHypothesisTracker() *HypothesisTracker {
	return &HypothesisTracker{Hashes: make(map[string]int)}
}

// IsDuplicate checks if a hypothesis hash has been seen before.
// Returns the iteration number and true if duplicate.
func (ht *HypothesisTracker) IsDuplicate(hash string) (int, bool) {
	iter, ok := ht.Hashes[hash]
	return iter, ok
}

// Record stores a hypothesis hash with its iteration number.
func (ht *HypothesisTracker) Record(hash string, iteration int) {
	ht.Hashes[hash] = iteration
}

// HashFileChanges computes a SHA-256 hash of file changes (file-rewrite mode).
// Identical file contents produce the same hash regardless of iteration.
func HashFileChanges(changes map[string]string) string {
	h := sha256.New()

	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(changes[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// HashOverrideChanges computes a SHA-256 hash of override values (constants mode).
func HashOverrideChanges(overrides map[string]string) string {
	h := sha256.New()

	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte("="))
		h.Write([]byte(overrides[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// dedupPath returns the path to the dedup hashes file.
func dedupPath(workdir string) string {
	return filepath.Join(workdir, configDir, "dedup_hashes.json")
}

// LoadHypothesisTracker loads the tracker from disk, or returns a new empty one.
func LoadHypothesisTracker(workdir string) *HypothesisTracker {
	data, err := os.ReadFile(dedupPath(workdir))
	if err != nil {
		return NewHypothesisTracker()
	}
	var ht HypothesisTracker
	if err := json.Unmarshal(data, &ht); err != nil {
		return NewHypothesisTracker()
	}
	if ht.Hashes == nil {
		ht.Hashes = make(map[string]int)
	}
	return &ht
}

// SaveHypothesisTracker persists the tracker to disk.
func SaveHypothesisTracker(workdir string, ht *HypothesisTracker) error {
	data, err := json.Marshal(ht)
	if err != nil {
		return fmt.Errorf("marshal dedup hashes: %w", err)
	}
	return os.WriteFile(dedupPath(workdir), data, 0o644)
}

// FilterDuplicateHypotheses removes hypotheses from a batch that have
// already been tried. Returns the filtered list and a dedup hint string
// for the next prompt (empty if no duplicates found).
func FilterDuplicateHypotheses(hypotheses []hypothesisResult, tracker *HypothesisTracker, isConstantsMode bool) ([]hypothesisResult, string) {
	var filtered []hypothesisResult
	var dupIterations []int

	for _, hyp := range hypotheses {
		var hash string
		if isConstantsMode {
			hash = HashOverrideChanges(hyp.overrides)
		} else {
			hash = HashFileChanges(hyp.changes)
		}

		if iter, dup := tracker.IsDuplicate(hash); dup {
			dupIterations = append(dupIterations, iter)
		} else {
			filtered = append(filtered, hyp)
		}
	}

	var hint string
	if len(dupIterations) > 0 {
		parts := make([]string, len(dupIterations))
		for i, iter := range dupIterations {
			parts[i] = fmt.Sprintf("#%d", iter)
		}
		hint = fmt.Sprintf("Your proposal was identical to iteration(s) %s. Try something substantially different.",
			strings.Join(parts, ", "))
	}
	return filtered, hint
}
