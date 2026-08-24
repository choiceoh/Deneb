// recall_fact_match.go — binding a turn to canonical facts. Answers "which
// subjects and claims did this message actually name?" so non-self facts and
// their retired values stay scoped to the identity that owns them.
package recall

import (
	"strings"
	"unicode"

	mem "github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

func combineCurrentFactContext(currentFacts, recallContext string) string {
	currentFacts = strings.TrimSpace(currentFacts)
	recallContext = strings.TrimSpace(recallContext)
	switch {
	case currentFacts == "":
		return recallContext
	case recallContext == "":
		return currentFacts
	default:
		return currentFacts + "\n\n" + recallContext
	}
}

// explicitlyMatchedFactSubjects returns only non-self fact subjects whose
// identity tokens are explicitly present in the natural-language message.
// Subject suffixes are exact-token matched (project:alpha -> alpha), never
// substring matched, so project:alpha cannot expose project:alphabet. When the
// same identity exists under multiple namespaces, the namespace must also be
// named ("project alpha" vs "person alpha") or neither subject is selected.
func explicitlyMatchedFactSubjects(message string, claims []wiki.FactClaim) map[string]struct{} {
	messageTokens := normalizedFactSubjectTokens(message)
	if len(messageTokens) == 0 {
		return nil
	}
	type candidate struct {
		subject   string
		namespace string
		signature string
	}
	seenSubjects := make(map[string]struct{})
	var candidates []candidate
	for _, claim := range claims {
		subject := strings.ToLower(strings.TrimSpace(claim.Subject))
		if subject == "" || subject == mem.SubjectSelf {
			continue
		}
		if _, seen := seenSubjects[subject]; seen {
			continue
		}
		seenSubjects[subject] = struct{}{}
		namespace, identityTokens := factSubjectIdentityTokens(subject)
		if len(identityTokens) == 0 || !containsEveryFactSubjectToken(messageTokens, identityTokens) {
			continue
		}
		// A lone one-rune identity is too collision-prone without its namespace.
		// A multi-part slug such as a-1 remains distinctive because every token
		// must match; splitting slug separators must not make such IDs unusable.
		shortID := len(identityTokens) == 1 && len([]rune(identityTokens[0])) < 2
		if shortID {
			if _, explicitNamespace := messageTokens[namespace]; namespace == "" || !explicitNamespace {
				continue
			}
		}
		candidates = append(candidates, candidate{
			subject: subject, namespace: namespace,
			signature: strings.Join(identityTokens, "\x00"),
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	signatureCount := make(map[string]int, len(candidates))
	for _, item := range candidates {
		signatureCount[item.signature]++
	}
	matched := make(map[string]struct{}, len(candidates))
	for _, item := range candidates {
		if signatureCount[item.signature] > 1 {
			if _, namesNamespace := messageTokens[item.namespace]; item.namespace == "" || !namesNamespace {
				continue
			}
		}
		matched[item.subject] = struct{}{}
	}
	return matched
}

func normalizedFactSubjectTokens(value string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, token := range splitFactSubjectTokens(value) {
		token = normalizeFactSubjectToken(token)
		if token != "" {
			tokens[token] = struct{}{}
		}
	}
	return tokens
}

func normalizeFactSubjectToken(token string) string {
	token = normalizeRecallToken(token)
	if strings.HasSuffix(token, "의") {
		candidate := strings.TrimSuffix(token, "의")
		if len([]rune(candidate)) >= 2 {
			return candidate
		}
	}
	return token
}

// splitFactSubjectTokens treats slug separators as natural-language word
// boundaries. Recall search keeps hyphenated terms intact, but fact subjects are
// canonical IDs: person:john-smith and project:alpha_beta must match (and be
// isolated by) the natural forms "John Smith" and "alpha beta".
func splitFactSubjectTokens(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func factSubjectIdentityTokens(subject string) (string, []string) {
	subject = strings.ToLower(strings.TrimSpace(subject))
	namespace := ""
	identity := subject
	if index := strings.IndexAny(subject, ":/"); index >= 0 {
		namespace = normalizeRecallToken(subject[:index])
		identity = subject[index+1:]
	}
	var tokens []string
	for _, token := range splitFactSubjectTokens(identity) {
		if token = normalizeFactSubjectToken(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	return namespace, textutil.DedupeStrings(tokens)
}

func containsEveryFactSubjectToken(messageTokens map[string]struct{}, subjectTokens []string) bool {
	for _, token := range subjectTokens {
		if _, ok := messageTokens[token]; !ok {
			return false
		}
	}
	return true
}

// unmatchedNonSelfFactValues forms a deny set for untyped diary/transcript
// evidence. Those sources cannot carry a reliable subject ID, so an exact live
// value from an unmentioned project/person must not bypass subject isolation.
func unmatchedNonSelfFactValues(claims []wiki.FactClaim, matched map[string]struct{}) []string {
	allowed := make(map[string]struct{})
	for _, claim := range claims {
		if _, ok := matched[strings.ToLower(strings.TrimSpace(claim.Subject))]; ok {
			if value := normalizeFactMatchText(claim.Value); value != "" {
				allowed[value] = struct{}{}
			}
		}
	}
	seen := make(map[string]struct{})
	var values []string
	for _, claim := range claims {
		subject := strings.ToLower(strings.TrimSpace(claim.Subject))
		if subject == "" || subject == mem.SubjectSelf {
			continue
		}
		if _, ok := matched[subject]; ok {
			continue
		}
		value := strings.Join(strings.Fields(claim.Value), " ")
		folded := normalizeFactMatchText(value)
		if folded == "" {
			continue
		}
		if _, alsoAllowed := allowed[folded]; alsoAllowed {
			continue
		}
		if _, duplicate := seen[folded]; duplicate {
			continue
		}
		seen[folded] = struct{}{}
		values = append(values, value)
	}
	return values
}

type factSubjectAlias struct {
	subject string
	tokens  []string
}

// unmatchedNonSelfFactAliases lets subject-less diary/transcript/file rows
// enforce the same isolation as typed wiki rows. A paraphrase may omit the
// exact current value, but when it explicitly names an unrequested subject it
// still must not cross into another subject's turn.
func unmatchedNonSelfFactAliases(claims []wiki.FactClaim, matched map[string]struct{}) []factSubjectAlias {
	seen := make(map[string]struct{})
	var aliases []factSubjectAlias
	for _, claim := range claims {
		subject := strings.ToLower(strings.TrimSpace(claim.Subject))
		if subject == "" || subject == mem.SubjectSelf {
			continue
		}
		if _, ok := matched[subject]; ok {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		_, tokens := factSubjectIdentityTokens(subject)
		if len(tokens) > 0 {
			aliases = append(aliases, factSubjectAlias{subject: subject, tokens: tokens})
		}
	}
	return aliases
}

func normalizeFactMatchText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func filterUnmatchedSubjectFactEvidence(evidence []recallEvidence, values []string) []recallEvidence {
	if len(evidence) == 0 || len(values) == 0 {
		return evidence
	}
	out := evidence[:0]
	for _, item := range evidence {
		blocked := false
		for _, value := range values {
			// Subject-less short/common values cannot safely enforce privacy by
			// themselves. Alias/SubjectID guards below still block rows that name
			// the unmatched subject; typed lifecycle rules handle same-identity
			// short corrections without turning "A" into a global deny.
			if !wiki.IsFactLifecycleGlobalStalePhrase(value) {
				continue
			}
			if evidenceContainsNormalizedFactValue(item.Note, value) {
				blocked = true
				break
			}
		}
		if !blocked {
			out = append(out, item)
		}
	}
	return out
}

func filterUnmatchedSubjectAliases(
	evidence []recallEvidence,
	aliases []factSubjectAlias,
	matched map[string]struct{},
) []recallEvidence {
	if len(evidence) == 0 || len(aliases) == 0 {
		return evidence
	}
	out := evidence[:0]
	for _, item := range evidence {
		blocked := false
		if subject := strings.ToLower(strings.TrimSpace(item.SubjectID)); subject != "" && subject != mem.SubjectSelf {
			_, allowed := matched[subject]
			blocked = !allowed
		}
		if !blocked {
			for _, alias := range aliases {
				all := true
				for _, token := range alias.tokens {
					if !evidenceContainsNormalizedFactValue(item.Note, token) {
						all = false
						break
					}
				}
				if all {
					blocked = true
					break
				}
			}
		}
		if !blocked {
			out = append(out, item)
		}
	}
	return out
}

func evidenceContainsNormalizedFactValue(note, value string) bool {
	note = strings.ToLower(strings.Join(strings.Fields(note), " "))
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	if note == "" || value == "" {
		return false
	}
	// Values need real boundaries regardless of length: raw substring matching
	// makes A poison every Latin word, Bob poison Bobcat, and a multi-word value
	// match a longer token at its right edge. Korean particles and numeric units
	// attach without whitespace, so the boundary matcher admits only those
	// explicit suffixes.
	return containsBoundedFactValue(note, value)
}

func containsBoundedFactValue(note, value string) bool {
	noteRunes, valueRunes := []rune(note), []rune(value)
	if len(noteRunes) == 0 || len(valueRunes) == 0 || len(valueRunes) > len(noteRunes) {
		return false
	}
	for start := 0; start+len(valueRunes) <= len(noteRunes); start++ {
		match := true
		for i := range valueRunes {
			if noteRunes[start+i] != valueRunes[i] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if !recallFactValueLeftBoundary(noteRunes, valueRunes, start) {
			continue
		}
		end := start + len(valueRunes)
		if recallFactValueRightBoundary(noteRunes, valueRunes, end) {
			return true
		}
	}
	return false
}

func recallFactValueLeftBoundary(note, value []rune, start int) bool {
	if start == 0 || !unicode.IsLetter(value[0]) && !unicode.IsDigit(value[0]) {
		return true
	}
	previous := note[start-1]
	if unicode.IsLetter(previous) || unicode.IsDigit(previous) {
		return false
	}
	return start < 2 || !recallFactValueConnector(previous) || !unicode.IsLetter(note[start-2]) && !unicode.IsDigit(note[start-2])
}

func recallFactValueRightBoundary(note, value []rune, end int) bool {
	if end == len(note) || !unicode.IsLetter(value[len(value)-1]) && !unicode.IsDigit(value[len(value)-1]) {
		return true
	}
	next := note[end]
	if !unicode.IsLetter(next) && !unicode.IsDigit(next) {
		if recallFactValueConnector(next) && end+1 < len(note) && (unicode.IsLetter(note[end+1]) || unicode.IsDigit(note[end+1])) {
			return false
		}
		return true
	}
	return factValueAllowsAttachedSuffix(value[len(value)-1], string(note[end:]))
}

func recallFactValueConnector(value rune) bool {
	switch value {
	case '-', '_', '/', '.', ':':
		return true
	default:
		return false
	}
}

func factValueAllowsAttachedSuffix(last rune, suffix string) bool {
	particles := []string{
		"이었어요", "이었는데", "이었던", "였어요", "였는데", "였던",
		"이었다", "입니다", "이라고", "에서", "에게", "한테", "부터", "까지", "으로",
		"였다", "이다", "라고", "은", "는", "이", "가", "을", "를", "의", "도", "만", "와", "과", "에", "로",
	}
	for _, particle := range particles {
		if strings.HasPrefix(suffix, particle) {
			return true
		}
	}
	if !unicode.IsDigit(last) {
		return false
	}
	units := []string{"억원", "만원", "천원", "원", "퍼센트", "시", "분", "초", "일", "월", "년", "개", "명", "건", "회", "차"}
	for _, unit := range units {
		if strings.HasPrefix(suffix, unit) {
			return true
		}
	}
	return false
}

func currentFactsResolveMessage(message string, claims []wiki.FactClaim, matched map[string]struct{}) bool {
	messageTokens := normalizedFactSubjectTokens(message)
	if len(matched) == 0 {
		for _, claim := range claims {
			if strings.EqualFold(strings.TrimSpace(claim.Subject), mem.SubjectSelf) &&
				liveSelfTurnFact(claim) && selfFactClaimMatchesMessage(message, messageTokens, claim) {
				return true
			}
		}
		return false
	}
	if factQueryOnlyNamesMatchedSubjects(message, matched) {
		return true
	}
	for _, claim := range claims {
		if _, ok := matched[strings.ToLower(strings.TrimSpace(claim.Subject))]; !ok {
			continue
		}
		if factClaimMatchesMessage(message, messageTokens, claim) {
			return true
		}
	}
	return false
}

// selfFactClaimMatchesMessage deliberately excludes broad kind aliases such as
// "preference" or "선호". Those aliases are useful after an explicit subject
// match, but on a self-only turn they would let any preference satisfy a
// question about a different preference axis.
func selfFactClaimMatchesMessage(message string, messageTokens map[string]struct{}, claim wiki.FactClaim) bool {
	if evidenceContainsNormalizedFactValue(message, claim.Value) {
		return true
	}
	claimKey := strings.ToLower(strings.TrimSpace(claim.Key))
	queryKey := strings.ToLower(strings.TrimSpace(mem.FactKeyFromText(message)))
	if claimKey != "" && queryKey != "" && queryKey != "untitled" && claimKey == queryKey {
		return true
	}
	for _, token := range selfFactKeyQueryTokens(claim.Key) {
		if _, ok := messageTokens[token]; ok {
			return true
		}
	}
	for _, alias := range selfFactCanonicalQueryAliases(claimKey) {
		if _, ok := messageTokens[alias]; ok {
			return true
		}
	}
	return false
}

func selfFactKeyQueryTokens(key string) []string {
	var specific []string
	for _, token := range factKeyQueryTokens(key) {
		switch token {
		case "communication", "response", "identity", "profile", "preference", "fact", "setting":
			continue
		default:
			specific = append(specific, token)
		}
	}
	return specific
}

// selfFactCanonicalQueryAliases returns the words a user searches one self axis
// by. The vocabulary lives in the direct-grammar catalog so this surface and the
// wiki fact search cannot drift apart — they used to keep separate tables, and
// the search one simply did not exist, leaving every Korean query unable to
// reach its own axis.
func selfFactCanonicalQueryAliases(key string) []string {
	return mem.FactKeyQueryAliases(key)
}

func factClaimMatchesMessage(message string, messageTokens map[string]struct{}, claim wiki.FactClaim) bool {
	if evidenceContainsNormalizedFactValue(message, claim.Value) {
		return true
	}
	recognized := make(map[string]struct{})
	for _, token := range factKeyQueryTokens(claim.Key) {
		recognized[token] = struct{}{}
	}
	for _, alias := range factKindQueryAliases(claim.Kind) {
		recognized[alias] = struct{}{}
	}
	// A parent key token is useful context only when the user did not name a
	// competing fact aspect. For example quote.amount must not satisfy
	// "alpha quote owner" merely because both contain quote; likewise a short
	// "alpha PM" query must remain an honest miss unless the claim models PM.
	for token := range messageTokens {
		if !isSpecificFactAspectToken(token) {
			continue
		}
		if _, ok := recognized[token]; !ok {
			return false
		}
	}
	for token := range recognized {
		if _, ok := messageTokens[token]; ok {
			return true
		}
	}
	return false
}

func factKeyQueryTokens(key string) []string {
	var tokens []string
	for _, token := range strings.FieldsFunc(strings.ToLower(key), func(r rune) bool {
		return r == '.' || r == '_' || r == ':' || r == '/' || r == '-'
	}) {
		token = normalizeRecallToken(token)
		if len([]rune(token)) >= 2 {
			tokens = append(tokens, token)
		}
	}
	return textutil.DedupeStrings(tokens)
}

func isSpecificFactAspectToken(token string) bool {
	switch token {
	case "amount", "price", "cost", "금액", "가격", "비용", "예산",
		"deadline", "due", "납기", "마감", "기한", "일정",
		"status", "state", "상태",
		"name", "role", "title", "이름", "소속", "직함", "역할",
		"preference", "prefer", "선호", "취향",
		"owner", "assignee", "manager", "pm", "po", "담당", "담당자", "책임자",
		"attendee", "attendees", "participant", "participants", "참석", "참석자", "참여자",
		"author", "writer", "작성자":
		return true
	default:
		return false
	}
}

func factQueryOnlyNamesMatchedSubjects(message string, matched map[string]struct{}) bool {
	queryTokens := make(map[string]struct{})
	// Use the full message rather than searchQueries: its retrieval-oriented
	// minimum lengths intentionally drop short but meaningful aspects such as PM,
	// QA, PO, or IP. Losing them here turns a specific question into a broad
	// subject-only query and suppresses the no-evidence warning.
	for _, token := range splitFactSubjectTokens(message) {
		token = normalizeFactSubjectToken(token)
		if token == "" || isRecallCueToken(token) {
			continue
		}
		queryTokens[token] = struct{}{}
	}
	for subject := range matched {
		namespace, identityTokens := factSubjectIdentityTokens(subject)
		delete(queryTokens, namespace)
		for _, token := range identityTokens {
			delete(queryTokens, token)
		}
		if namespace == "project" {
			delete(queryTokens, "프로젝트")
		}
		if namespace == "person" {
			delete(queryTokens, "사람")
			delete(queryTokens, "인물")
		}
	}
	return len(queryTokens) == 0
}

func factKindQueryAliases(kind wiki.FactKind) []string {
	switch kind {
	case wiki.FactKindAmount:
		return []string{"amount", "quote", "price", "cost", "금액", "견적", "가격", "비용", "예산"}
	case wiki.FactKindDeadline:
		return []string{"deadline", "due", "납기", "마감", "기한", "일정"}
	case wiki.FactKindContract:
		return []string{"contract", "계약", "조건", "조항"}
	case wiki.FactKindSystemState:
		return []string{"status", "state", "system", "상태", "시스템", "배포", "실행", "헬스"}
	case wiki.FactKindIdentity:
		return []string{"identity", "name", "role", "title", "이름", "소속", "직함", "역할"}
	case wiki.FactKindPreference:
		return []string{"preference", "prefer", "선호", "취향", "좋아", "싫어"}
	default:
		return nil
	}
}

// subjectAwareCurrentFactContext keeps the established self-only renderer for
// ordinary/vague turns. Only an explicit non-self match switches to a combined
// renderer, where matched non-self claims are placed before self facts so a
