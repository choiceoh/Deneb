package mcpclient

import "strings"

// stderrRepeatLogEvery is how often a repeating stderr line is re-logged
// (with its repeat count) after its first verbatim appearance.
const stderrRepeatLogEvery = 100

// stderrDedupeMaxKeys bounds the per-spawn key set; a child that emits endless
// distinct lines resets the set instead of growing it.
const stderrDedupeMaxKeys = 512

// stderrDeduper counts occurrences of stderr lines per child spawn. Keys are
// digit-normalised so a line that only differs by a timestamp, pid, or retry
// counter collapses onto the same key.
type stderrDeduper struct {
	counts map[string]int
}

func newStderrDeduper() *stderrDeduper {
	return &stderrDeduper{counts: make(map[string]int)}
}

// observe records one line and returns how many times its key has been seen
// (1 on first sight).
func (d *stderrDeduper) observe(line string) int {
	key := stderrDedupeKey(line)
	if _, seen := d.counts[key]; !seen && len(d.counts) >= stderrDedupeMaxKeys {
		d.counts = make(map[string]int)
	}
	d.counts[key]++
	return d.counts[key]
}

// stderrDedupeKey replaces every digit run with "#" so timestamped or counted
// variants of one message share a key.
func stderrDedupeKey(line string) string {
	var b strings.Builder
	b.Grow(len(line))
	inDigits := false
	for _, r := range line {
		if r >= '0' && r <= '9' {
			if !inDigits {
				b.WriteByte('#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		b.WriteRune(r)
	}
	return b.String()
}
