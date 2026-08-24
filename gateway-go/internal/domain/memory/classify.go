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
	otherSubjectRe             = regexp.MustCompile(`(?i)(아내|남편|배우자|아들|딸|어머니|아버지|부모님|동료|배우자분)`)
	profileCueRe               = regexp.MustCompile(`(?i)(기억해|기억해줘|기억\s*해\s*둬|내\s*선호|나는\s*.{0,12}(좋아|싫어|원해|선호)|앞으로\s*.{0,20}(불러|해줘|해\s*줘)|알레르기|못\s*먹|비건|채식|채식주의|금기|제약\s*사항|장기\s*목표)`)
	profileCorrectionLeadRe    = regexp.MustCompile(`(?i)^\s*(?:아니(?:야|요)?|아님|정정(?:할게(?:요)?)?|수정(?:할게(?:요)?)?|바꿔서?)(?:[,，:：.!?。！？\s-]+|$)`)
	genericProfileCorrectionRe = regexp.MustCompile(`(?i)(좋아|싫어|원해|선호|간결|짧게|짧은\s*답변|장황|길게|상세하게|자세하게|한국어|영어|한글|즉답|답부터|결론부터|불릿|목록|표\s*형식|산문|마크다운|비건|채식|호칭|불러)`)
	directRememberLeadRe       = regexp.MustCompile(`(?i)^\s*기억\s*(?:해|해줘|해줘요|해\s*둬)(?:[,，:：.!?。！？\s-]+|$)`)
	directForwardLeadRe        = regexp.MustCompile(`(?i)^\s*(?:앞으로(?:는)?|다음부터)(?:[,，:：.!?。！？\s-]+|$)`)
	directCorrectionValueRe    = regexp.MustCompile(`(?i)^\s*(?:(?:답변(?:은|는|을|를)?|응답(?:은|는|을|를)?|말투(?:는|를)?|언어(?:는|를)?)\s*)?(?:간결하게?|짧게|짧고\s*간결하게|짧은\s*답변|장황하게?|길게|상세하게|자세하게|한국어(?:로)?|영어(?:로)?|한글(?:로)?|즉답(?:으로)?|답부터|결론부터|불릿(?:으로)?|목록(?:으로)?|표\s*형식(?:으로)?|산문(?:으로)?|마크다운(?:으로)?)(?:\s*(?:답해줘|답변해줘|말해줘|해줘|바꿔줘))?\s*[.!?。！？]*\s*$`)
	directTrailingRememberRe   = regexp.MustCompile(`(?i)^\s*(?:내|나의|나는|저는|제가|나를|저를)[^:：\n]{0,60}기억\s*(?:해|해줘|해줘요|해\s*둬)\s*[.!?。！？]*\s*$`)
	directTrailingRememberEnd  = regexp.MustCompile(`(?i)\s*기억\s*(?:해|해줘|해줘요|해\s*둬)\s*[.!?。！？]*\s*$`)
	directKoreanForgetRe       = regexp.MustCompile(`(?i)^\s*(?:내|나의)\s+(?:(?:답변[^:：\n]{0,18}(?:선호|길이|분량|간결|짧|길게|상세|언어|형식))|(?:[^\s:：\n]{1,16}(?:\s+[^\s:：\n]{1,16}){0,2}\s+(?:선호|취향))|(?:(?:호칭|이름|언어|비건|채식|장기\s*목표|(?:[^\s:：\n]{1,16}\s+)?알레르기)(?:\s*정보)?))(?:은|는|이|가|을|를)?\s*(?:기억에서\s*)?(?:지워줘|삭제해줘|잊어줘)\s*[.!?。！？]*\s*$`)
	directPreferenceForgetRe   = regexp.MustCompile(`(?i)^\s*(?:내|나의)\s+([^\s:：\n]{1,16}(?:\s+[^\s:：\n]{1,16}){0,2})\s+(?:선호|취향)(?:은|는|이|가|을|를)?\s*(?:기억에서\s*)?(?:지워줘|삭제해줘|잊어줘)\s*[.!?。！？]*\s*$`)
	directKnownAxisForgetRe    = regexp.MustCompile(`(?i)^\s*(?:내|나의)\s+(?:(?:(?:답변\s*)?형식|불릿|목록|표\s*형식|산문|마크다운)\s*(?:선호|취향)|(?:부가세|vat|공급가액)(?:\s*(?:별도|제외|포함))?\s*(?:선호|정책)|(?:즉답|답부터|결론부터)\s*(?:선호|취향)|(?:진행\s*상황|중간\s*보고|능동\s*공지)\s*(?:선호|취향))(?:은|는|이|가|을|를)?\s*(?:기억에서\s*)?(?:지워줘|삭제해줘|잊어줘)\s*[.!?。！？]*\s*$`)
	embeddedPreferenceForgetRe = regexp.MustCompile(`(?i)^\s*(?:내가|제가|나는|저는)\s+(.{1,32}?)(?:을|를)\s+(?:좋아(?:해|한다)|싫어(?:해|한다)|선호(?:해|한다))(?:는|다는)\s+사실(?:은|는|을|를)?\s*(?:기억에서\s*)?(?:지워줘|삭제해줘|잊어줘)\s*[.!?。！？]*\s*$`)
	embeddedIdentityForgetRe   = regexp.MustCompile(`(?i)^\s*(?:내가|제가|나는|저는)\s+(비건|채식(?:주의자)?)(?:이란|이?라는|이라는)\s+사실(?:은|는|을|를)?\s*(?:기억에서\s*)?(?:지워줘|삭제해줘|잊어줘)\s*[.!?。！？]*\s*$`)
	// The per-axis English/Korean phrasings live in direct_grammar.json; only the
	// structural leads and guards remain here.
	directEnglishRememberRe       = regexp.MustCompile(`(?i)^\s*(?:please\s+)?remember\b`)
	directEnglishForwardRe        = regexp.MustCompile(`(?i)^\s*from\s+now\s+on\b`)
	directEnglishPreferenceRe     = regexp.MustCompile(`(?i)^\s*i\s+(?:like|dislike|prefer)\s+(.{1,40}?)\s*[.!?]*\s*$`)
	englishTemporaryScopeRe       = regexp.MustCompile(`(?i)\b(?:today|tomorrow|temporarily|for\s+now|for\s+(?:a\s+while|the\s+time\s+being)|until\s+[^,.!?\n]{1,24}|(?:just\s+)?this\s+once|(?:this|next)\s+(?:day|week|month|year|quarter)(?:\s+only)?|for\s+(?:the\s+)?next\s+(?:day|week|month|year|quarter)(?:\s+only)?|this\s+(?:time|answer|response|reply|turn|message|request|conversation|session)|(?:for\s+)?(?:one|the\s+next|this)\s+(?:answer|response|reply|turn|message|request|conversation|session)\s+only|for\s+(?:today|tomorrow|this\s+(?:time|week|month|year|answer|response|reply|turn|message|request|conversation|session)|one\s+(?:day|week|month))\s+only)\b`)
	englishReportedContentRe      = regexp.MustCompile(`(?i)(?:,\s*)?(?:according\s+to\b|(?:as\s+)?(?:[\p{L}\p{N}_'’-]+\s+){1,5}(?:says?|told\s+me)\b|i\s+was\s+told\b|it\s+(?:says|said)\b|was\s+(?:written|reported)\b)`)
	englishUncertainAssertionRe   = regexp.MustCompile(`(?i)(?:,\s*)?(?:i\s+(?:guess|think|suppose|believe)|apparently|supposedly|maybe|probably|perhaps)\b`)
	directDiscourseLeadRe         = regexp.MustCompile(`(?i)^\s*(?:잠깐(?:만)?|wait)(?:[,，:：.!?。！？\s-]+)`)
	directSelfProfileFrameRe      = regexp.MustCompile(`(?i)(?:(?:나는|저는|제가)\s*.{0,24}(?:좋아|싫어|원해|선호|비건|채식|알레르기)|(?:내|나의)\s*.{0,24}(?:선호|취향|답변|정보|사실|기억|호칭|이름|알레르기|장기\s*목표)|(?:나를|저를).{0,20}(?:불러|호칭)|\bi\s+(?:prefer|like|dislike|want)\b|\bi(?:['’]m|\s+am)\s+(?:vegan|vegetarian|allergic)\b|\bmy\s+(?:preference|preferences|profile|response|answer|language|name|address)\b)`)
	directSelfProfileStartRe      = regexp.MustCompile(`(?i)^\s*(?:(?:나는|저는|제가)(?:\s|$)|(?:내|나의|나를|저를)(?:\s|$)|i(?:\s|['’]m\s|\s+am\s)|my\s)`)
	thirdPartyReportFrameRe       = regexp.MustCompile(`(?i)(?:나는|저는|제가)\s+.{0,48}(?:가|이|는|은)\s+.{0,48}(?:라고|다고)\s*(?:들었|말했|전했|생각|알고|기억)`)
	reportedContentFrameRe        = regexp.MustCompile(`(?i)(?:라고|다고)\s*(?:들었|말했|전했|생각|알고|기억|적혀|쓰여|써\s*있|기재|작성)`)
	profilePayloadFrameRe         = regexp.MustCompile(`(?i)(?:번역(?:해|할)|요약(?:해|할)|검토(?:해|할)|설명(?:해|할)|문장|문구|메일|이메일|문서|페이지|파일|원문|\btranslate\b|\bsummarize\b|\bemail\b|\bdocument\b|\bsentence\b|\bpage\b|\bfile\b)`)
	temporaryProfileScopeRe       = regexp.MustCompile(`(?i)(?:오늘만|이번만|이번에만|이번에는|이번엔|다음에만|지금만|지금은|(?:(?:오늘|내일|모레)(?:\s*(?:하루|동안))?(?:에)?\s*만)|(?:이번\s*(?:주|달|개월|분기|해|연도)(?:\s*(?:에|동안))?\s*만)|(?:(?:한|두|세|\d+)\s*(?:시간|일|주|달|개월)|일주일|한달|한\s*달)(?:\s*동안)?\s*만|(?:이|이번|다음)\s*(?:답변|메시지|질문|요청|턴|대화|회의|세션)(?:에|에서|에는)?\s*만|(?:[\p{L}\p{N}_-]+(?:\s+[\p{L}\p{N}_-]+){0,3})(?:에서만|에게만|한테만|때만|경우에만))`)
	selfIdentityCueRe             = regexp.MustCompile(`(?i)(?:비건|채식|채식주의|알레르기|못\s*먹)`)
	directSelfIdentityRe          = regexp.MustCompile(`(?i)^\s*(?:(?:나는|저는|제가)\s+(?:(?:비건|채식(?:주의자)?)(?:이야|입니다|이에요|예요|야)?|(?:[\p{L}\p{N}_-]{1,16}\s+)?알레르기(?:가|는)?\s*(?:있어|있어요|있습니다)?)|(?:내|나의)\s+(?:(?:식단|식습관)(?:은|는|이|가)?\s*(?:비건|채식(?:주의)?)|알레르기(?:는|가)?\s+.{1,24}))\s*[.!?。！？]*\s*$`)
	directSelfIntoleranceRe       = regexp.MustCompile(`(?i)^\s*(?:나는|저는|제가)\s+[\p{L}\p{N}_-]{1,16}(?:을|를)\s+못\s*먹(?:어|어요|습니다)?\s*[.!?。！？]*\s*$`)
	directSelfCommunicationRe     = regexp.MustCompile(`(?i)^\s*(?:(?:나는|저는|제가)\s+(?:(?:(?:답변|응답|말투|언어)(?:은|는|을|를)?\s+(?:간결하게?|짧게|짧고\s*간결하게|장황하게?|길게|길고\s*상세하게|상세하게|자세하게|한국어(?:로)?|영어(?:로)?|한글(?:로)?)(?:\s*(?:원해|선호해|좋아해))?)|(?:(?:간결한|짧은|긴|상세한|자세한)\s+(?:답변|응답)(?:을|를)?\s*(?:원해|선호해|좋아해)))|(?:내|나의)\s+(?:답변|응답|말투|언어)(?:은|는|을|를)?\s+(?:간결하게?|짧게|짧고\s*간결하게|장황하게?|길게|길고\s*상세하게|상세하게|자세하게|한국어(?:로)?|영어(?:로)?|한글(?:로)?)(?:\s*(?:원해|선호해|좋아해))?|i\s+(?:prefer|want|like)\s+(?:my\s+)?(?:answers?|responses?|reply|replies|language)\s+.{1,24}|my\s+(?:answers?|responses?|reply|replies|language)\s+.{1,24})\s*[.!?。！？]*\s*$`)
	directSelfGenericPreferenceRe = regexp.MustCompile(`(?i)^\s*(?:나는|저는|제가)\s+(.{1,40}?)(?:을|를|은|는)?\s*(?:(?:정말|아주|별로|전혀|꽤|많이)\s*)?(?:좋아\s*하지\s*않(?:아|아요)?|안\s*좋아(?:해|해요)?|좋아(?:해|해요|한다)|싫어(?:해|해요|한다)|선호(?:해|해요|한다))\s*[.!?。！？]*\s*$`)
	directSelfLongTermGoalRe      = regexp.MustCompile(`(?i)^\s*(?:내|나의)\s*장기\s*목표(?:는|은|이|가)?\s+.{1,40}\s*[.!?。！？]*\s*$`)
	genericThirdPartyObjectRe     = regexp.MustCompile(`(?i)(?:^|\s)[\p{L}\p{N}_-]{1,24}(?:가|이|는|은|도|에게|한테|의)(?:\s|$)`)
	directSelfAddressRe           = regexp.MustCompile(`(?i)^\s*(?:(?:내|나의)\s*(?:호칭|이름)(?:은|는|을|를)?\s+(?:["“][^"”\n]{1,20}["”]|[\p{L}\p{N}_-]{1,20})(?:으로|로)?|(?:나를|저를)\s+(?:["“][^"”\n]{1,20}["”]|[\p{L}\p{N}_-]{1,20})(?:이라고|라고|로)\s*불러(?:줘)?)\s*[.!?。！？]*\s*$`)
	directForwardAddressRe        = regexp.MustCompile(`(?i)^\s*(?:나를|저를)\s+(?:["“][^"”\n]{1,20}["”]|[\p{L}\p{N}_-]{1,20})(?:이라고|라고|로)\s*불러(?:줘|주세요)?\s*[.!?。！？]*\s*$`)
	directForwardVATRe            = regexp.MustCompile(`(?i)^\s*(?:금액(?:은|는|을|를)?\s*)?(?:부가세|vat)(?:는|은|를|을)?\s*(?:별도|제외|포함)(?:로)?\s*(?:기재해줘|적어줘|해줘)?\s*[.!?。！？]*\s*$`)
	nestedSubjectWithinSelfRe     = regexp.MustCompile(`(?i)^\s*(?:나는|저는|제가)\s+.{0,32}[\p{L}\p{N}_-]{1,24}(?:가|이|는|은)\s+`)
	forgetCueRe                   = regexp.MustCompile(`(?i)(기억.{0,20}(지워|삭제|잊|하지\s*마)|(?:내|나의).{0,24}(선호|취향|사실|정보).{0,16}(지워|삭제|잊)|(?:선호|취향).{0,20}(지워|삭제|잊)|(?:forget|delete|remove|erase)\b.{0,40}\b(?:memory|memories|preference|preferences|fact|facts|profile|information)\b|(?:memory|memories|preference|preferences|fact|facts|profile|information)\b.{0,40}\b(?:forget|delete|remove|erase)\b)`)
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
	if otherSubjectRe.MatchString(msg) {
		// An explicit third-party subject wins over a first-person reporting frame
		// ("나는 아내가 비건이라고 들었어"). Ambiguous mixed-subject turns must
		// never mint a highest-authority self fact.
		c.SubjectID = "other:" + firstMatch(otherSubjectRe, msg)
	}
	englishKey, englishKind, hasEnglishProfile := explicitEnglishProfileCommand(msg)
	embeddedForgetKey, embeddedForgetKind, hasEmbeddedForget := embeddedSelfFactForget(msg)
	withoutNegatedForget := negatedForgetCueRe.ReplaceAllString(msg, " ")
	hasForget := hasEmbeddedForget || forgetCueRe.MatchString(withoutNegatedForget)
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
		if hasEmbeddedForget {
			c.FactKey = embeddedForgetKey
			c.FactKind = embeddedForgetKind
		}
	case procedureCueRe.MatchString(msg):
		c.Target = TargetProcedure
	case hasEnglishProfile:
		c.Target = TargetProfile
		c.FactKey = englishKey
		c.FactKind = englishKind
	case profileCueRe.MatchString(msg) || isProfileCorrection(msg):
		c.Target = TargetProfile
		c.FactKind = profileFactKind(msg)
		if c.SubjectID != SubjectSelf && !strings.HasPrefix(c.SubjectID, "other:") {
			c.SubjectID = SubjectSelf
		}
		// Explicit self-preference wins over incidental name mention.
		if !otherSubjectRe.MatchString(msg) && (strings.Contains(msg, "나는") || strings.Contains(msg, "제가") || strings.Contains(msg, "내 선호")) {
			c.SubjectID = SubjectSelf
		}
	case episodicCueRe.MatchString(msg):
		c.Target = TargetEpisodic
	default:
		c.Target = TargetEpisodic
	}
	return c
}

// HasDirectProfileMutationIntent reports whether the utterance itself is an
// explicit top-level remember/forward/correction/imperative-forget request.
// Casual self statements stay diary-only until the user asks to retain them.
// ClassifyHeuristics is
// intentionally recall-friendly and can find cues anywhere in a message; that
// is too permissive for granting direct_user authority because a translation,
// quotation, email, or fenced payload can contain the same words. This guard
// masks quoted payloads and requires a direct command/self frame outside them.
func HasDirectProfileMutationIntent(message string) bool {
	msg := strings.TrimSpace(message)
	candidate := ClassifyHeuristics(msg)
	if msg == "" || candidate.Target != TargetProfile {
		return false
	}
	outside := strings.TrimSpace(maskQuotedProfilePayloads(msg))
	outsideCandidate := ClassifyHeuristics(outside)
	if outside == "" || outsideCandidate.Target != TargetProfile {
		return false
	}
	if thirdPartyReportFrameRe.MatchString(msg) || reportedContentFrameRe.MatchString(msg) ||
		profilePayloadFrameRe.MatchString(outside) || temporaryProfileScopeRe.MatchString(outside) {
		return false
	}
	if candidate.Forget || outsideCandidate.Forget {
		// A deletion is canonical only when its noun phrase can be bound to the
		// same key as the active fact. English and unsupported Korean deletion
		// cues stay recall-friendly but diary-only; writing a tombstone for the
		// literal command text would leave the requested fact untouched.
		_, _, bound := embeddedSelfFactForget(outside)
		return bound
	}
	outside = directDiscourseLeadRe.ReplaceAllString(outside, "")
	if body, ok := directRememberPayload(msg); ok {
		return isDirectSelfProfileAssertion(body)
	}
	if directTrailingRememberRe.MatchString(outside) {
		body := strings.TrimSpace(directTrailingRememberEnd.ReplaceAllString(msg, ""))
		return isDirectSelfProfileAssertion(body)
	}
	if directForwardLeadRe.MatchString(outside) || directEnglishForwardRe.MatchString(outside) {
		return isDirectForwardProfileCommand(outside, outsideCandidate.FactKey)
	}
	if match := profileCorrectionLeadRe.FindStringIndex(outside); match != nil && match[0] == 0 {
		body := strings.TrimSpace(outside[match[1]:])
		if strings.ContainsAny(body, ":：\n") {
			return false
		}
		return (directForwardLeadRe.MatchString(body) && isDirectForwardProfileCommand(body, outsideCandidate.FactKey)) ||
			directCorrectionValueRe.MatchString(body)
	}
	return false
}

func explicitEnglishProfileCommand(message string) (key, kind string, ok bool) {
	if directEnglishRememberRe.MatchString(message) {
		body, found := directRememberPayload(message)
		if !found {
			return "", "", false
		}
		return englishSelfProfileAssertion(body)
	}
	if directEnglishForwardRe.MatchString(message) {
		return englishForwardProfileCommand(message)
	}
	return "", "", false
}

func englishSelfProfileAssertion(body string) (key, kind string, ok bool) {
	body = strings.TrimSpace(body)
	if body == "" || strings.HasSuffix(body, "?") || strings.HasSuffix(body, "？") ||
		profilePayloadFrameRe.MatchString(body) || englishTemporaryScopeRe.MatchString(body) ||
		reportedContentFrameRe.MatchString(body) || englishReportedContentRe.MatchString(body) ||
		englishUncertainAssertionRe.MatchString(body) {
		return "", "", false
	}
	if axis, found := axisForEnglishAssertion(body); found {
		return axis.Key, axis.Kind, true
	}
	match := directEnglishPreferenceRe.FindStringSubmatch(body)
	if len(match) != 2 {
		return "", "", false
	}
	object := strings.TrimSpace(match[1])
	if object == "" || profilePayloadFrameRe.MatchString(object) || englishTemporaryScopeRe.MatchString(object) {
		return "", "", false
	}
	return genericFactKeyFromText(object), "preference", true
}

func englishForwardProfileCommand(message string) (key, kind string, ok bool) {
	match := directEnglishForwardRe.FindStringIndex(message)
	if match == nil || match[0] != 0 {
		return "", "", false
	}
	body := strings.TrimSpace(message[match[1]:])
	body = strings.TrimLeftFunc(body, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",:;.!?-", r)
	})
	if body == "" || strings.HasSuffix(body, "?") || strings.HasSuffix(body, "？") ||
		profilePayloadFrameRe.MatchString(body) || englishTemporaryScopeRe.MatchString(body) {
		return "", "", false
	}
	if axis, found := axisForEnglishForward(body); found {
		return axis.Key, axis.Kind, true
	}
	return "", "", false
}

func embeddedSelfFactForget(message string) (key, kind string, ok bool) {
	if directKnownAxisForgetRe.MatchString(message) {
		lower := strings.ToLower(strings.TrimSpace(message))
		if axis, found := axisForKnownAxisForget(lower); found {
			return axis.Key, axis.Kind, true
		}
	}
	if directKoreanForgetRe.MatchString(message) {
		if match := directPreferenceForgetRe.FindStringSubmatch(message); len(match) == 2 {
			object := strings.TrimSpace(match[1])
			if object == "" || profilePayloadFrameRe.MatchString(object) {
				return "", "", false
			}
			normalizedObject := strings.Join(strings.Fields(strings.ToLower(object)), " ")
			if axis, found := axisForForgetObject(normalizedObject); found {
				return axis.Key, axis.Kind, true
			}
			return genericFactKeyFromText(object), "preference", true
		}
		lower := strings.ToLower(strings.TrimSpace(message))
		if axis, found := axisForForgetCue(lower); found {
			return axis.Key, axis.Kind, true
		}
		return "", "", false
	}
	if match := embeddedPreferenceForgetRe.FindStringSubmatch(message); len(match) == 2 {
		object := strings.TrimSpace(match[1])
		if object == "" {
			return "", "", false
		}
		return genericFactKeyFromText(object), "preference", true
	}
	if embeddedIdentityForgetRe.MatchString(message) {
		return "diet.vegan", "identity", true
	}
	return "", "", false
}

func directRememberPayload(message string) (string, bool) {
	for _, lead := range []*regexp.Regexp{directRememberLeadRe, directEnglishRememberRe} {
		match := lead.FindStringIndex(message)
		if match == nil || match[0] != 0 {
			continue
		}
		body := strings.TrimSpace(message[match[1]:])
		body = strings.TrimLeftFunc(body, func(r rune) bool {
			return unicode.IsSpace(r) || strings.ContainsRune(",，:：.!?。！？-", r)
		})
		return trimMatchingProfileQuotes(body), true
	}
	return "", false
}

func trimMatchingProfileQuotes(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	pairs := map[rune]rune{'"': '"', '“': '”', '‘': '’', '`': '`'}
	runes := []rune(value)
	if closing, ok := pairs[runes[0]]; ok && runes[len(runes)-1] == closing {
		return strings.TrimSpace(string(runes[1 : len(runes)-1]))
	}
	return value
}

func isDirectSelfProfileAssertion(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" || profilePayloadFrameRe.MatchString(body) || temporaryProfileScopeRe.MatchString(body) ||
		thirdPartyReportFrameRe.MatchString(body) || reportedContentFrameRe.MatchString(body) {
		return false
	}
	if _, _, ok := englishSelfProfileAssertion(body); ok {
		return true
	}
	if directSelfIntoleranceRe.MatchString(body) {
		return true
	}
	if !directSelfProfileStartRe.MatchString(body) || !directSelfProfileFrameRe.MatchString(body) {
		return false
	}
	if directSelfAddressRe.MatchString(body) {
		return true
	}
	candidate := ClassifyHeuristics(body)
	if candidate.Target != TargetProfile || candidate.SubjectID != SubjectSelf {
		return false
	}
	// Identity cues are especially easy to misattribute in first-person reports
	// ("나는 철수를 비건이라고 알고 있어"). Bind them to an explicit self
	// identity grammar instead of trying to enumerate every Korean subject or
	// object particle that a third-party clause may use.
	if selfIdentityCueRe.MatchString(body) {
		return directSelfIdentityRe.MatchString(body)
	}
	if nestedSubjectWithinSelfRe.MatchString(withoutPreferenceDegreeAdverbs(body)) {
		return false
	}
	if strings.HasPrefix(candidate.FactKey, "communication.") {
		return directSelfCommunicationRe.MatchString(body)
	}
	if candidate.FactKey == "goals.long_term" {
		return directSelfLongTermGoalRe.MatchString(body)
	}
	match := directSelfGenericPreferenceRe.FindStringSubmatch(body)
	if len(match) != 2 {
		return false
	}
	object := strings.TrimSpace(match[1])
	return object != "" && !genericThirdPartyObjectRe.MatchString(withoutPreferenceDegreeAdverbs(object))
}

func withoutPreferenceDegreeAdverbs(value string) string {
	fields := strings.Fields(value)
	kept := fields[:0]
	for _, field := range fields {
		switch field {
		case "정말", "아주", "별로", "전혀", "꽤", "많이":
			continue
		default:
			kept = append(kept, field)
		}
	}
	return strings.Join(kept, " ")
}

func isDirectForwardProfileCommand(message, key string) bool {
	if directEnglishForwardRe.MatchString(message) {
		parsedKey, _, ok := englishForwardProfileCommand(message)
		return ok && parsedKey == key
	}
	match := directForwardLeadRe.FindStringIndex(message)
	if match == nil || match[0] != 0 {
		return false
	}
	body := strings.TrimSpace(message[match[1]:])
	if body == "" || strings.ContainsAny(body, ":：\n") || profilePayloadFrameRe.MatchString(body) || temporaryProfileScopeRe.MatchString(body) {
		return false
	}
	switch {
	case strings.HasPrefix(key, "communication."):
		return directCorrectionValueRe.MatchString(body)
	case key == "identity.address":
		return directForwardAddressRe.MatchString(body)
	case key == "wiki.amount_vat_policy":
		return directForwardVATRe.MatchString(body)
	default:
		return false
	}
}

func isProfileCorrection(message string) bool {
	if !profileCorrectionLeadRe.MatchString(message) {
		return false
	}
	if _, found := axisForClassifiedText(message); found {
		return true
	}
	return genericProfileCorrectionRe.MatchString(message)
}

// maskQuotedProfilePayloads removes payload text while retaining the framing
// words around it. Straight apostrophes are deliberately not quote delimiters
// so English contractions such as "don't" remain parseable. Blockquote and
// fenced-code lines are payload-only and are removed in full.
func maskQuotedProfilePayloads(message string) string {
	var out strings.Builder
	inFence := false
	var quote rune
	for _, line := range strings.SplitAfter(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			out.WriteByte('\n')
			continue
		}
		if inFence || strings.HasPrefix(trimmed, ">") {
			out.WriteByte('\n')
			continue
		}
		for _, r := range line {
			if quote != 0 {
				if (quote == '“' && r == '”') || (quote == '‘' && r == '’') || r == quote {
					quote = 0
				}
				if r == '\n' {
					out.WriteRune(r)
				} else {
					out.WriteRune(' ')
				}
				continue
			}
			switch r {
			case '"', '“', '‘', '`':
				quote = r
				out.WriteRune(' ')
			default:
				out.WriteRune(r)
			}
		}
	}
	return out.String()
}

// FactKeyFromText builds a stable-ish key for supersession (latest-state).
// FactKindForKey returns the kind a profile axis key carries, and whether the key
// is one of those axes at all.
//
// The axis table defines key and kind as a pair on purpose: a key like
// communication.response_length IS a preference, whatever sentence produced it.
// Callers that derive the kind separately (a section heading, say) need this to
// avoid claiming a different kind for the same identity — which the fact store
// rejects, and which took the gateway down on 2026-08-23 when a bullet under
// "## 사용자 모델" was filed as identity against a preference key.
func FactKindForKey(key string) (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(key))
	for _, axis := range factAxes {
		if axis.Key == trimmed {
			return axis.Kind, true
		}
	}
	return "", false
}

func FactKeyFromText(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if axis, found := axisForClassifiedText(lower); found {
		return axis.Key
	}
	return genericFactKeyFromText(lower)
}

func genericFactKeyFromText(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
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

var (
	factKeyScaffoldingRE = regexp.MustCompile(`(?i)(기억\s*(?:해(?:줘)?|하지\s*마|에서\s*(?:지워|삭제)|지워|삭제)|잊어(?:줘)?|앞으로(?:는)?|항상|다음부터|나는|내가|제가|저는|내\s+|나의\s+|취향(?:은|는|이|가|을|를)?|선호(?:해|한다|함|은|는|이|가|을|를)?|좋아\s*하지\s*않(?:아|아요)?|안\s*좋아(?:해|한다)?|선호\s*하지\s*않(?:아|아요)?|원하지\s*않(?:아|아요)?|좋아(?:해|한다|함)?|싫어(?:해|한다|함)?|원해|정말|아주|별로|전혀|꽤|많이|아니야|아님|하지\s*마|해\s*줘|해주세요|부탁해|바꿔|변경해)`)
	factKeyParticleRE    = regexp.MustCompile(`([[:alnum:]가-힣]+)(은|는|이|가|을|를)(\s|$)`)
)

func profileFactKind(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if axis, found := axisForClassifiedText(lower); found {
		return axis.Kind
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
