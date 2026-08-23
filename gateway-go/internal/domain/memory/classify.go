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
	// Forget marks an explicit direct-user request to retire this fact key.
	// The trusted chat induction path maps it to a tombstone; legacy sinks do
	// not attempt destructive rewrites.
	Forget bool
	// FactKind selects the fact-plane conflict policy. It intentionally stays a
	// string so this leaf package does not depend on the wiki implementation.
	FactKind string
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
	forgetCueRe    = regexp.MustCompile(`(?i)(기억.{0,20}(지워|삭제|잊|하지\s*마)|(?:내|나의).{0,24}(선호|취향|사실|정보).{0,16}(지워|삭제|잊)|(?:선호|취향).{0,20}(지워|삭제|잊)|(?:forget|delete|remove|erase)\b.{0,40}\b(?:memory|memories|preference|preferences|fact|facts|profile|information)\b|(?:memory|memories|preference|preferences|fact|facts|profile|information)\b.{0,40}\b(?:forget|delete|remove|erase)\b)`)
	// A retention request can contain the same destructive verb as an explicit
	// forget. Keep this guard local to that verb so "기억하지 마" remains a
	// deletion request while "잊지 마" and "do not delete" do not tombstone.
	negatedForgetCueRe = regexp.MustCompile(`(?i)(?:(?:(?:지우|삭제하|잊)(?:거나|고)\s*)*(?:지우|삭제하|잊)지(?:는|만)?\s*(?:마|말)|(?:지우|삭제하|잊)면\s*안|(?:지워|삭제해|잊어)서는?\s*안|(?:do\s+not|don['’]t|never|not\s+to)(?:\s+[\p{L}'’.\-]+){0,3}\s+(?:forget|delete|remove|erase)(?:\s+(?:or|and)\s+(?:forget|delete|remove|erase))*\b)`)
	procedureCueRe     = regexp.MustCompile(`(?i)(앞으로는\s*이렇게|절차는|워크플로|워크플로우|SOP|표준\s*절차|항상\s*이렇게\s*해|매번\s*.{0,16}해줘|스킬로\s*만들어)`)
	episodicCueRe      = regexp.MustCompile(`(?i)(오늘|방금|어제|아까|이번\s*주|내일|모레|방금\s*전)`)
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
		FactKind:  "generic",
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
	withoutNegatedForget := negatedForgetCueRe.ReplaceAllString(msg, " ")
	hasForget := forgetCueRe.MatchString(withoutNegatedForget)
	hasRetentionRequest := !hasForget && forgetCueRe.MatchString(msg)
	switch {
	case hasRetentionRequest:
		// This is a request to retain an existing fact, not a new assertion.
		// Leave the fact plane untouched rather than replacing or tombstoning it.
		c.Target = TargetEpisodic
	case hasForget:
		c.Target = TargetProfile
		c.FactKind = profileFactKind(msg)
		c.Forget = true
	case procedureCueRe.MatchString(msg):
		c.Target = TargetProcedure
	case profileCueRe.MatchString(msg):
		c.Target = TargetProfile
		c.FactKind = profileFactKind(msg)
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
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, axis := range profileFactAxes {
		if axis.re.MatchString(lower) {
			return axis.key
		}
	}
	// Generic preference corrections often differ only in polarity ("커피를
	// 좋아해" → "커피 싫어"). Remove directive/polarity scaffolding before
	// deriving the fallback key so both statements address the same fact.
	lower = factKeyScaffoldingRE.ReplaceAllString(lower, " ")
	lower = factKeyParticleRE.ReplaceAllString(lower, "$1$3")
	var b strings.Builder
	n := 0
	for _, r := range lower {
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

type profileFactAxis struct {
	key  string
	kind string
	re   *regexp.Regexp
}

var profileFactAxes = []profileFactAxis{
	{key: "communication.response_length", kind: "preference", re: regexp.MustCompile(`(간결|짧게|짧은 답변|3줄|장황|길게|상세하게|자세하게|긴 답변|답변.{0,8}(길이|분량))`)},
	{key: "communication.language", kind: "preference", re: regexp.MustCompile(`(한국어|영어|한글).{0,12}(전용|답변|사용|말해|써|작성)`)},
	{key: "communication.answer_first", kind: "preference", re: regexp.MustCompile(`(즉답|답부터|결론부터|질문에는.{0,8}답|분석보다.{0,8}답)`)},
	{key: "communication.progress_updates", kind: "preference", re: regexp.MustCompile(`(진행 상황|진행상황|중간 보고|능동 공지|왜 멈췄)`)},
	{key: "communication.format", kind: "preference", re: regexp.MustCompile(`(불릿|목록|표 형식|산문|헤더|마크다운).{0,12}(선호|좋아|싫어|해줘|하지)`)},
	{key: "wiki.amount_vat_policy", kind: "preference", re: regexp.MustCompile(`(부가세|vat|공급가액).{0,24}(제외|별도|포함|기재|적지)`)},
	{key: "diet.vegan", kind: "identity", re: regexp.MustCompile(`(비건|채식주의|채식주의자)`)},
	{key: "identity.address", kind: "identity", re: regexp.MustCompile(`(나를|저를|호칭|이름).{0,16}(불러|불러줘|라고 해|호칭)`)},
}

var (
	factKeyScaffoldingRE = regexp.MustCompile(`(?i)(기억\s*(?:해(?:줘)?|하지\s*마|에서\s*(?:지워|삭제)|지워|삭제)|잊어(?:줘)?|앞으로(?:는)?|항상|다음부터|나는|내가|제가|저는|내\s+|나의\s+|취향(?:은|는|이|가|을|를)?|선호(?:해|한다|함|은|는|이|가|을|를)?|좋아(?:해|한다|함)?|싫어(?:해|한다|함)?|원해|아니야|아님|하지\s*마|해\s*줘|해주세요|부탁해|바꿔|변경해)`)
	factKeyParticleRE    = regexp.MustCompile(`([[:alnum:]가-힣]+)(은|는|이|가|을|를)(\s|$)`)
)

func profileFactKind(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, axis := range profileFactAxes {
		if axis.re.MatchString(lower) {
			return axis.kind
		}
	}
	return "preference"
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
