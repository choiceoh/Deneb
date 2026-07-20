package memory

import (
	"regexp"
	"strings"
	"unicode"
)

// Candidate is one induced memory item before routing.
type Candidate struct {
	Text      string
	Target    WriteTarget
	SubjectID string
	FactKey   string
	// SupersedesKey, when set, marks that this candidate replaces a prior fact
	// with the same key (latest-state). Callers use it when rewriting MEMORY or
	// wiki pages.
	SupersedesKey string
}

var (
	excludeRe = regexp.MustCompile(`(?i)(주민등록|주민번호|여권번호|비밀번호|password|secret|api[_-]?key|토큰\s*[:：]|bearer\s+[a-z0-9]|보험증|카드번호|\b\d{6}-?\d{7}\b)`)
	// Third-party subject cues (family / named roles) — not exhaustive, heuristic.
	otherSubjectRe = regexp.MustCompile(`(?i)(아내|남편|배우자|아들|딸|어머니|아버지|부모님|동료|배우자분)`)
	profileCueRe   = regexp.MustCompile(`(?i)(기억해|기억해줘|기억\s*해\s*둬|내\s*선호|나는\s*.{0,12}(좋아|싫어|원해|선호)|앞으로\s*.{0,20}(불러|해줘|해\s*줘)|알레르기|못\s*먹|비건|채식|채식주의|금기|제약\s*사항|장기\s*목표)`)
	procedureCueRe = regexp.MustCompile(`(?i)(앞으로는\s*이렇게|절차는|워크플로|워크플로우|SOP|표준\s*절차|항상\s*이렇게\s*해|매번\s*.{0,16}해줘|스킬로\s*만들어)`)
	episodicCueRe  = regexp.MustCompile(`(?i)(오늘|방금|어제|아까|이번\s*주|내일|모레|방금\s*전)`)
)

// ClassifyHeuristics assigns a WriteTarget (and subject/fact key) from a raw
// user utterance. Pure and deterministic — no LLM. Prefer exclude over all
// other signals when sensitive patterns match.
func ClassifyHeuristics(message string) Candidate {
	msg := strings.TrimSpace(message)
	c := Candidate{
		Text:      truncateRunes(msg, 400),
		Target:    TargetEpisodic,
		SubjectID: SubjectSelf,
		FactKey:   FactKeyFromText(msg),
	}
	if msg == "" {
		c.Target = TargetExclude
		return c
	}
	if excludeRe.MatchString(msg) {
		c.Target = TargetExclude
		return c
	}
	if otherSubjectRe.MatchString(msg) && !strings.HasPrefix(msg, "나") && !strings.Contains(msg, "나는") && !strings.Contains(msg, "제가") {
		// Third-party mention without explicit self-frame → not self profile.
		c.SubjectID = "other:" + firstMatch(otherSubjectRe, msg)
	}
	switch {
	case procedureCueRe.MatchString(msg):
		c.Target = TargetProcedure
	case profileCueRe.MatchString(msg):
		c.Target = TargetProfile
		if c.SubjectID != SubjectSelf && !strings.HasPrefix(c.SubjectID, "other:") {
			c.SubjectID = SubjectSelf
		}
		// Explicit self-preference wins over incidental name mention.
		if strings.Contains(msg, "나는") || strings.Contains(msg, "제가") || strings.Contains(msg, "내 선호") {
			c.SubjectID = SubjectSelf
		}
	case episodicCueRe.MatchString(msg):
		c.Target = TargetEpisodic
	default:
		c.Target = TargetEpisodic
	}
	return c
}

// FactKeyFromText builds a stable-ish key for supersession (latest-state).
func FactKeyFromText(text string) string {
	var b strings.Builder
	n := 0
	for _, r := range strings.ToLower(text) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 48 {
			break
		}
	}
	if b.Len() == 0 {
		return "untitled"
	}
	return b.String()
}

func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindString(s)
	return strings.TrimSpace(m)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
