// plaud_glossary.go — meeting-scoped glossary slice, do-not-correct list,
// auto-promotion from 「표기 교정」, and ASR hotword extraction.
package meeting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

const (
	// PlaudDoNotCorrectFile lists forbidden ASR "corrections" (A must not become B).
	PlaudDoNotCorrectFile = "plaud-do-not-correct.md"
	// PlaudPromotePendingFile tracks pair sightings before glossary promotion.
	PlaudPromotePendingFile = "plaud-promote-pending.json"

	plaudDoNotCorrectMaxRunes  = 4_000
	plaudGlossarySliceMaxRunes = 4_500
	plaudAutoPromoteHeading    = "## 자동 승격"
	plaudPromoteMaxPerMeeting  = 20
	// plaudPromoteMinSightings: pair must appear in this many distinct recordings.
	plaudPromoteMinSightings = 2
	// plaudAutoPromoteMaxLines caps the promote section growth.
	plaudAutoPromoteMaxLines = 60
)

type promotePendingState struct {
	Version   int                 `json:"version"`
	Sightings map[string][]string `json:"sightings"` // "from\x00to" → recording ids
}

// CorrectionPair is one 원문 → 교정 mapping.
type CorrectionPair struct {
	From string
	To   string
}

// LoadPlaudDoNotCorrect returns the operator do-not-correct body (may be empty).
func LoadPlaudDoNotCorrect(topicsDir string) string {
	return loadPlaudTopicFile(topicsDir, PlaudDoNotCorrectFile, plaudDoNotCorrectMaxRunes)
}

// ParseCorrectionPairs extracts "A → B" / "A -> B" bullet pairs from markdown.
func ParseCorrectionPairs(body string) []CorrectionPair {
	var out []CorrectionPair
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Estimates and struck-through notes are not durable glossary material.
		if strings.Contains(line, "추정") || strings.Contains(line, "~~") {
			continue
		}
		sep := "→"
		if !strings.Contains(line, sep) {
			sep = "->"
		}
		if !strings.Contains(line, sep) {
			continue
		}
		parts := strings.SplitN(line, sep, 2)
		from := cleanPairSide(parts[0])
		to := cleanPairSide(parts[1])
		if from == "" || to == "" || from == to {
			continue
		}
		if strings.Contains(to, "유지") || strings.Contains(from, "~~") {
			continue
		}
		key := from + "\x00" + to
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, CorrectionPair{From: from, To: to})
	}
	return out
}

func cleanPairSide(s string) string {
	s = strings.TrimSpace(s)
	// Drop trailing notes: (추정), （사용자…）
	if i := strings.IndexAny(s, "(（"); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.Trim(s, "*_`\"'")
	return strings.TrimSpace(s)
}

// ExtractReportCorrectionPairs reads the ## 표기 교정 section of a meeting report.
func ExtractReportCorrectionPairs(report string) []CorrectionPair {
	sec := sectionAfterHeading(report, "표기 교정")
	if sec == "" {
		return nil
	}
	var kept []CorrectionPair
	for _, p := range ParseCorrectionPairs(sec) {
		if strings.Contains(p.To, "추정") || strings.Contains(p.From, "추정") {
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

func sectionAfterHeading(md, headingFragment string) string {
	lines := strings.Split(md, "\n")
	start := -1
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "##") && strings.Contains(trim, headingFragment) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	var b strings.Builder
	for _, line := range lines[start:] {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "##") && !strings.HasPrefix(trim, "###") {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// ForbiddenCorrection reports whether from→to is blocked by the do-not-correct list.
func ForbiddenCorrection(doNotBody string, from, to string) bool {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	for _, p := range ParseCorrectionPairs(doNotBody) {
		if equalFoldSpace(p.From, from) && equalFoldSpace(p.To, to) {
			return true
		}
		// Also block reversing a protected identity: "오형석 ≠ 오선택" style
		// stored as 오형석 → 오선택 in the forbid file.
		if equalFoldSpace(p.From, to) && equalFoldSpace(p.To, from) {
			return true
		}
	}
	// Lines like "오형석 ≠ 오선택"
	for _, line := range strings.Split(doNotBody, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if !strings.Contains(line, "≠") && !strings.Contains(line, "!=") {
			continue
		}
		sep := "≠"
		if !strings.Contains(line, sep) {
			sep = "!="
		}
		parts := strings.SplitN(line, sep, 2)
		a, b := cleanPairSide(parts[0]), cleanPairSide(parts[1])
		if a == "" || b == "" {
			continue
		}
		if (equalFoldSpace(a, from) && equalFoldSpace(b, to)) ||
			(equalFoldSpace(a, to) && equalFoldSpace(b, from)) {
			return true
		}
	}
	return false
}

func equalFoldSpace(a, b string) bool {
	return strings.EqualFold(strings.ReplaceAll(a, " ", ""), strings.ReplaceAll(b, " ", ""))
}

// GlossaryHints drives meeting-scoped slicing.
type GlossaryHints struct {
	RecordingName string
	Candidates    []mailanalysis.ProjectCandidate
	// ExtraTokens are names pulled from mentioned-project wiki pages
	// (people/places/orgs) — merged into hint matching.
	ExtraTokens []string
}

// SlicePlaudGlossary keeps always-on sections + hint-matching lines, capped.
func SlicePlaudGlossary(full, doNot string, hints GlossaryHints) string {
	full = strings.TrimSpace(full)
	doNot = strings.TrimSpace(doNot)
	if full == "" && doNot == "" {
		return ""
	}

	hintTokens := glossaryHintTokens(hints)
	var kept []string
	if doNot != "" {
		kept = append(kept, "# 교정 금지", "", doNot, "", "---", "")
	}
	if full == "" {
		return trimRunes(strings.Join(kept, "\n"), plaudGlossarySliceMaxRunes)
	}

	alwaysSection := false
	for _, line := range strings.Split(full, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			alwaysSection = alwaysOnGlossarySection(trim)
		}
		if alwaysSection || glossaryLineMatches(line, hintTokens) || strings.HasPrefix(trim, "# ") {
			kept = append(kept, line)
			continue
		}
		// Keep blank lines only while inside an always section (handled above).
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	return trimRunes(out, plaudGlossarySliceMaxRunes)
}

func alwaysOnGlossarySection(heading string) bool {
	switch {
	case strings.Contains(heading, "사용자 확인"),
		// High-trust ASR pairs (메가→MW, 헥사→Hexa, …) — not project-specific.
		strings.Contains(heading, "고신뢰"),
		strings.Contains(heading, "회의록에서 반복"),
		strings.Contains(heading, "단위"),
		// ## 자동 승격 is hint-matched only (slice budget).
		strings.Contains(heading, "탑솔라 그룹"),
		strings.Contains(heading, "핵심 인물"),
		strings.Contains(heading, "본사 임원"),
		strings.Contains(heading, "고신뢰 교정"):
		return true
	default:
		return false
	}
}

func glossaryHintTokens(hints GlossaryHints) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		for _, part := range splitHintTokens(s) {
			key := strings.ToLower(part)
			if seen[key] || utf8.RuneCountInString(part) < 2 {
				continue
			}
			seen[key] = true
			out = append(out, part)
		}
	}
	add(hints.RecordingName)
	for _, c := range hints.Candidates {
		add(c.Title)
		add(c.Summary)
		add(c.Path)
		if base := filepath.Base(c.Path); base != "" {
			add(strings.TrimSuffix(base, filepath.Ext(base)))
		}
		// project folder name
		dir := filepath.Base(filepath.Dir(c.Path))
		if dir != "" && dir != "." && dir != "프로젝트" {
			add(dir)
		}
	}
	for _, t := range hints.ExtraTokens {
		add(t)
	}
	return out
}

func splitHintTokens(s string) []string {
	s = strings.ReplaceAll(s, "/", " ")
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, "(", " ")
	s = strings.ReplaceAll(s, ")", " ")
	fields := strings.Fields(s)
	var out []string
	for _, f := range fields {
		f = strings.Trim(f, ".,·|")
		if f != "" {
			out = append(out, f)
		}
	}
	// Also keep the original compact string for paths like 당진솔라빌리지
	compact := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
	if utf8.RuneCountInString(compact) >= 2 {
		out = append(out, compact)
	}
	return out
}

func glossaryLineMatches(line string, tokens []string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	lower := strings.ToLower(line)
	for _, t := range tokens {
		if strings.Contains(lower, strings.ToLower(t)) {
			return true
		}
	}
	return false
}

func trimRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max]))
}

// PromotePlaudCorrections records sightings and appends pairs under ## 자동 승격
// once they appear in plaudPromoteMinSightings distinct recordings.
func PromotePlaudCorrections(topicsDir string, pairs []CorrectionPair, sourceID string) (int, error) {
	topicsDir = strings.TrimSpace(topicsDir)
	if topicsDir == "" || len(pairs) == 0 {
		return 0, nil
	}
	path := filepath.Join(topicsDir, PlaudGlossaryFile)
	doNot := LoadPlaudDoNotCorrect(topicsDir)
	existing := LoadPlaudGlossary(topicsDir)
	existingPairs := ParseCorrectionPairs(existing)

	have := map[string]bool{}
	for _, p := range existingPairs {
		have[p.From+"\x00"+p.To] = true
	}

	pending := loadPromotePending(topicsDir)
	srcID := strings.TrimSpace(sourceID)
	if srcID == "" {
		srcID = "unknown"
	}

	var add []CorrectionPair
	for _, p := range pairs {
		if len(add) >= plaudPromoteMaxPerMeeting {
			break
		}
		if !promotablePair(p) || ForbiddenCorrection(doNot, p.From, p.To) {
			continue
		}
		key := p.From + "\x00" + p.To
		if have[key] {
			continue
		}
		n := recordPromoteSighting(&pending, key, srcID)
		if n < plaudPromoteMinSightings {
			continue
		}
		have[key] = true
		add = append(add, p)
		delete(pending.Sightings, key)
	}
	if err := savePromotePending(topicsDir, pending); err != nil {
		return 0, err
	}
	if len(add) == 0 {
		return 0, nil
	}

	body := existing
	if body == "" {
		body = "# Plaud 회의 전사 용어집\n"
	}
	if !strings.Contains(body, plaudAutoPromoteHeading) {
		body = strings.TrimRight(body, "\n") + "\n\n" + plaudAutoPromoteHeading + "\n\n" +
			"> 회의록 「표기 교정」에서 자동 승격(≥2회 관측). 잘못된 항목은 지우고 `plaud-do-not-correct.md`에 넣으세요.\n"
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteByte('\n')
	day := time.Now().In(time.FixedZone("KST", 9*3600)).Format("2006-01-02")
	for _, p := range add {
		fmt.Fprintf(&b, "- %s → %s (%s · plaud:%s)\n", p.From, p.To, day, srcID)
	}
	out := trimAutoPromoteSection(b.String(), plaudAutoPromoteMaxLines)
	if err := atomicfile.WriteFile(path, []byte(out), nil); err != nil {
		return 0, err
	}
	return len(add), nil
}

func loadPromotePending(topicsDir string) promotePendingState {
	st := promotePendingState{Version: 1, Sightings: map[string][]string{}}
	data, err := os.ReadFile(filepath.Join(topicsDir, PlaudPromotePendingFile))
	if err != nil {
		return st
	}
	if json.Unmarshal(data, &st) != nil || st.Sightings == nil {
		return promotePendingState{Version: 1, Sightings: map[string][]string{}}
	}
	return st
}

func savePromotePending(topicsDir string, st promotePendingState) error {
	st.Version = 1
	if st.Sightings == nil {
		st.Sightings = map[string][]string{}
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(filepath.Join(topicsDir, PlaudPromotePendingFile), data, &atomicfile.Options{Perm: 0o600})
}

func recordPromoteSighting(st *promotePendingState, key, sourceID string) int {
	if st.Sightings == nil {
		st.Sightings = map[string][]string{}
	}
	for _, id := range st.Sightings[key] {
		if id == sourceID {
			return len(st.Sightings[key])
		}
	}
	st.Sightings[key] = append(st.Sightings[key], sourceID)
	return len(st.Sightings[key])
}

// trimAutoPromoteSection keeps the newest bullet lines under ## 자동 승격.
func trimAutoPromoteSection(body string, maxLines int) string {
	if maxLines <= 0 || !strings.Contains(body, plaudAutoPromoteHeading) {
		return body
	}
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), plaudAutoPromoteHeading) {
			start = i
			break
		}
	}
	if start < 0 {
		return body
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trim, "## ") {
			end = i
			break
		}
	}
	var bullets []string
	var head []string
	for _, line := range lines[start+1 : end] {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "- ") {
			bullets = append(bullets, line)
		} else {
			head = append(head, line)
		}
	}
	if len(bullets) <= maxLines {
		return body
	}
	bullets = bullets[len(bullets)-maxLines:]
	var out []string
	out = append(out, lines[:start+1]...)
	out = append(out, head...)
	out = append(out, bullets...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// promotablePair rejects noisy LLM correction lines before they enter the glossary.
func promotablePair(p CorrectionPair) bool {
	fromN := utf8.RuneCountInString(p.From)
	toN := utf8.RuneCountInString(p.To)
	if fromN < 2 || toN < 2 || fromN > 40 || toN > 40 {
		return false
	}
	// Explanations / multi-clause "corrections" are not glossary material.
	if strings.ContainsAny(p.To, ";；") || strings.Count(p.To, " ") > 6 {
		return false
	}
	if strings.Contains(p.From, "없음") || strings.Contains(p.To, "없음") {
		return false
	}
	return true
}

// LoadPlaudGlossaryHotwords extracts canonical terms for ASR hotword bias.
func LoadPlaudGlossaryHotwords(topicsDir string, maxTerms int) string {
	if maxTerms <= 0 {
		maxTerms = 80
	}
	terms := textutil.NewLimitedTerms(maxTerms, 2000)
	addFrom := func(body string) {
		for _, p := range ParseCorrectionPairs(body) {
			_ = terms.Add(p.To)
			_ = terms.Add(p.From) // mishearing forms help ASR bias
		}
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || line == "" {
				continue
			}
			line = strings.TrimPrefix(line, "-")
			line = strings.TrimSpace(line)
			if strings.Contains(line, "→") || strings.Contains(line, "->") || strings.Contains(line, "≠") {
				continue
			}
			// "남도에코에너지: 오선택 (대표) / 박민수" → split on / and :
			for _, part := range splitHotwordFrags(line) {
				if !terms.Add(part) {
					return
				}
			}
		}
	}
	addFrom(LoadPlaudGlossary(topicsDir))
	addFrom(LoadPlaudDoNotCorrect(topicsDir))
	for _, p := range ParseCorrectionPairs(LoadPlaudDoNotCorrect(topicsDir)) {
		_ = terms.Add(p.From)
		_ = terms.Add(p.To)
	}
	return terms.String()
}

func splitHotwordFrags(line string) []string {
	line = strings.Split(line, "—")[0]
	line = strings.Split(line, " - ")[0]
	var frags []string
	for _, chunk := range strings.FieldsFunc(line, func(r rune) bool {
		return r == '/' || r == ':' || r == ',' || r == '·' || r == '|'
	}) {
		chunk = strings.TrimSpace(chunk)
		chunk = strings.Trim(chunk, "()（）")
		// Drop role-only tokens
		if chunk == "" || chunk == "대표" || chunk == "전무" || chunk == "이사" {
			continue
		}
		// "오선택 (대표)" already trimmed
		if i := strings.IndexAny(chunk, "(（"); i > 0 {
			chunk = strings.TrimSpace(chunk[:i])
		}
		if utf8.RuneCountInString(chunk) < 2 || utf8.RuneCountInString(chunk) > 24 {
			continue
		}
		frags = append(frags, chunk)
	}
	return frags
}
