// recall_misses.go — the demand ledger: questions the wiki could NOT answer.
//
// The recall-utility ledger (recall_hits.go) records supply — which pages got
// used. The far more actionable signal is the other side: a turn where the user
// explicitly asked for recall and the search came back with nothing. That is a
// measured hole in the curated memory, and until now it only produced a log
// line ("recall preflight: no evidence") that nobody consumed.
//
// This ledger closes the loop the RSI roadmap calls 수요 생성: misses accumulate
// here, and the autonomous research lane (runtime/wikiwork) reads the aggregate
// to prefer pages the user has actually been asking about over pure round-robin
// staleness. Best-effort by contract — a ledger failure never affects a chat turn.
//
// Only CUE turns are recorded. Silent auto-recall runs on nearly every turn and
// legitimately finds nothing for smalltalk; recording those would bury the real
// demand signal in noise.
package wiki

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// recallMissesFile is the append-only demand ledger, a sibling of the pages
// (dot prefix + .jsonl keeps it out of the .md page index).
const recallMissesFile = ".recall-misses.jsonl"

const (
	// recallMissRetention bounds the ledger; compacted on the dream cycle.
	recallMissRetention = 90 * 24 * time.Hour
	// recallMissDemandWindow is how far back demand aggregation looks. Shorter
	// than retention: research should chase what the user is asking about NOW,
	// while the retained tail stays available for offline analysis.
	recallMissDemandWindow = 30 * 24 * time.Hour
	// recallMissQueryMaxRunes bounds one recorded question.
	recallMissQueryMaxRunes = 200
)

// RecallMiss is one unanswered question to record.
type RecallMiss struct {
	Query   string // the user's cue text (or the primary search query)
	Session string // chat session key ("" when the surface has none)
}

type recallMissLine struct {
	Query   string `json:"query"`
	At      int64  `json:"at"` // unix milli
	Session string `json:"session,omitempty"`
}

// RecordRecallMiss appends one demand line. Empty queries are dropped.
func (s *Store) RecordRecallMiss(miss RecallMiss) error {
	q := clipRunes(strings.TrimSpace(miss.Query), recallMissQueryMaxRunes)
	if q == "" {
		return nil
	}
	line, err := json.Marshal(recallMissLine{
		Query:   q,
		At:      time.Now().UnixMilli(),
		Session: strings.TrimSpace(miss.Session),
	})
	if err != nil {
		return err
	}
	s.recallMu.Lock()
	defer s.recallMu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, recallMissesFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// readRecallMissesLocked returns retained miss lines, oldest first. Caller MUST
// hold recallMu. Malformed lines are skipped — derived, best-effort data.
func (s *Store) readRecallMissesLocked() []recallMissLine {
	f, err := os.Open(filepath.Join(s.dir, recallMissesFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []recallMissLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m recallMissLine
		if json.Unmarshal([]byte(line), &m) != nil || strings.TrimSpace(m.Query) == "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// RecallDemandTerms returns the distinct content terms of recent unanswered
// questions, most-demanded first, capped at limit. Terms — not whole questions —
// because the consumer matches them against page titles/summaries: "완도 태양광
// 진행 상황?" and "완도 프로젝트 어디까지 갔지" are one demand for 완도.
func (s *Store) RecallDemandTerms(now time.Time, limit int) []string {
	if limit <= 0 {
		return nil
	}
	cutoff := now.Add(-recallMissDemandWindow).UnixMilli()
	s.recallMu.Lock()
	misses := s.readRecallMissesLocked()
	s.recallMu.Unlock()

	counts := map[string]int{}
	order := []string{}
	for _, m := range misses {
		if m.At < cutoff {
			continue
		}
		for _, term := range demandTerms(m.Query) {
			if _, seen := counts[term]; !seen {
				order = append(order, term)
			}
			counts[term]++
		}
	}
	// Stable ordering: count desc, then first-seen (no map iteration in output).
	sortedByCount(order, counts)
	if len(order) > limit {
		order = order[:limit]
	}
	return order
}

// sortedByCount orders terms by descending count, preserving first-seen order
// among ties (insertion sort — the slice is small and stability is the point).
func sortedByCount(terms []string, counts map[string]int) {
	for i := 1; i < len(terms); i++ {
		for j := i; j > 0 && counts[terms[j]] > counts[terms[j-1]]; j-- {
			terms[j], terms[j-1] = terms[j-1], terms[j]
		}
	}
}

// demandStopwords are question scaffolding that carries no topic. Matching a
// page on these would make every page "demanded".
var demandStopwords = map[string]bool{
	// Question scaffolding.
	"뭐야": true, "뭐지": true, "어디": true, "언제": true, "누가": true, "어떻게": true,
	"진행": true, "상황": true, "상태": true, "관련": true, "내용": true, "정리": true,
	"알려줘": true, "알려": true, "찾아": true, "찾아줘": true, "확인": true, "해줘": true,
	"그거": true, "저거": true, "이거": true, "우리": true, "지금": true, "최근": true,
	"what": true, "where": true, "when": true, "status": true, "about": true,
	// Recall cues — the phrases that MADE this a cue turn carry no topic
	// ("지난번에 얘기한 …", "… 기억나?").
	"기억": true, "기억나": true, "기억나요": true, "회상": true, "지난번": true, "저번": true,
	"예전": true, "예전에": true, "아까": true, "방금": true, "그때": true, "말했던": true,
	"말한": true, "얘기한": true, "했던": true, "논의했던": true,
	// Category words: they appear in every page path/title of that category, so
	// treating one as demand would mark the whole wiki as demanded.
	"프로젝트": true, "인물": true, "시스템": true, "사용자": true, "기타": true,
	"페이지": true, "위키": true, "문서": true, "project": true, "wiki": true,
}

// demandTerms splits a question into topic-bearing terms: 2+ character tokens
// that are not pure scaffolding. Korean particles are not stripped (no
// morphological analyzer here) — the consumer substring-matches, so "완도군의"
// still matches a 완도 page.
func demandTerms(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !isTermRune(r)
	})
	var out []string
	seen := map[string]bool{}
	for _, f := range fields {
		if len([]rune(f)) < 2 || demandStopwords[f] || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

func isTermRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r >= 0xAC00 && r <= 0xD7A3: // Hangul syllables
		return true
	case r >= 0x4E00 && r <= 0x9FFF: // CJK ideographs
		return true
	}
	return false
}

// compactRecallMisses drops lines past retention. Mirrors compactRecallHits
// (atomic replace) and is called from the same dream-cycle maintenance step.
func (s *Store) compactRecallMisses(now time.Time) (dropped int, err error) {
	cutoff := now.Add(-recallMissRetention).UnixMilli()
	s.recallMu.Lock()
	defer s.recallMu.Unlock()
	misses := s.readRecallMissesLocked()
	if len(misses) == 0 {
		return 0, nil
	}
	kept := misses[:0]
	for _, m := range misses {
		if m.At < cutoff {
			dropped++
			continue
		}
		kept = append(kept, m)
	}
	if dropped == 0 {
		return 0, nil
	}
	var buf []byte
	for _, m := range kept {
		line, merr := json.Marshal(m)
		if merr != nil {
			continue
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	tmp := filepath.Join(s.dir, recallMissesFile+".tmp")
	if werr := os.WriteFile(tmp, buf, 0o644); werr != nil {
		return 0, werr
	}
	if rerr := os.Rename(tmp, filepath.Join(s.dir, recallMissesFile)); rerr != nil {
		return 0, rerr
	}
	return dropped, nil
}
