package memory

import "strings"

// SubjectSelf is the operator's own subject id for profile facts.
const SubjectSelf = "self"

// NormalizeSubject collapses empty/aliases to SubjectSelf and lowercases the rest.
func NormalizeSubject(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	switch s {
	case "", "self", "me", "operator", "user", "나", "저":
		return SubjectSelf
	default:
		return s
	}
}

// CrossSubjectBlocked reports whether evidence about evidenceSubject should be
// withheld for a query that only mentions querySubjects (plus implicit self).
//
// Rules:
//   - empty / self evidence is always allowed
//   - if the query names the evidence subject (exact or prefix match on PID-like ids), allow
//   - otherwise block — prevents family/colleague facts leaking into self-directed answers
func CrossSubjectBlocked(evidenceSubject string, querySubjects []string) bool {
	ev := NormalizeSubject(evidenceSubject)
	if ev == SubjectSelf {
		return false
	}
	for _, q := range querySubjects {
		nq := NormalizeSubject(q)
		if nq == SubjectSelf {
			continue
		}
		if nq == ev || strings.HasPrefix(ev, nq+"-") || strings.HasPrefix(nq, ev+"-") {
			return false
		}
		// Path-style subject ids: "인물/김민수" vs "김민수"
		if strings.Contains(ev, nq) || strings.Contains(nq, ev) {
			return false
		}
	}
	return true
}
