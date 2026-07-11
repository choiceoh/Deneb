// letter_tokens.go — digit-free market tokens for LLM-composed letters.
//
// The morning-letter LLM transcribes numbers unreliably when it writes them
// itself (2026-07-07: usd_krw 1530.98 became "1,331원" in the delivered
// letter). So the letter tool hands the model digit-free placeholder tokens
// ("{{market:usd_krw}}") alongside the raw floats it may reason about, records
// the fetched display strings here, and the proactive relay substitutes the
// tokens mechanically just before delivery. The model never types a digit:
// the failure mode collapses from "silently wrong number" to a visible "—".
package market

import (
	"regexp"
	"strings"
	"sync"
	"time"
)

// LetterToken* are the placeholder tokens the letter tool hands the model.
//
//nolint:gosec // G101 false positive — template placeholders in letter text, not credentials.
const (
	LetterTokenUSDKRW = "{{market:usd_krw}}"
	LetterTokenEURKRW = "{{market:eur_krw}}"
	LetterTokenCopper = "{{market:copper}}"
)

// letterTokenTTL bounds how long recorded values stay substitutable. The
// record and the substitution happen within one agent turn (minutes apart);
// anything older is a leftover from a previous run and must not leak into a
// letter composed after a failed fetch.
const letterTokenTTL = 6 * time.Hour

// letterTokenRe matches any market token, known or not, so an unrecorded or
// stale token degrades to a visible placeholder instead of leaking template
// syntax into the letter.
var letterTokenRe = regexp.MustCompile(`\{\{market:[a-z_]+\}\}`)

var letterTokens struct {
	sync.Mutex
	values map[string]string
	asOf   time.Time
}

// RecordLetterTokens merges fetched display numbers ("1,531" — digits only,
// units stay in the model's prose) under their tokens for later substitution.
// Merging (not replacing) lets the exchange and copper collectors record
// independently within the same letter run.
func RecordLetterTokens(values map[string]string) {
	if len(values) == 0 {
		return
	}
	letterTokens.Lock()
	defer letterTokens.Unlock()
	if letterTokens.values == nil || time.Since(letterTokens.asOf) > letterTokenTTL {
		letterTokens.values = make(map[string]string, len(values))
	}
	for k, v := range values {
		if k != "" && v != "" {
			letterTokens.values[k] = v
		}
	}
	letterTokens.asOf = time.Now()
}

// SubstituteLetterTokens replaces market tokens in a composed letter with the
// recorded display strings. Unknown or expired tokens become "—" so a fetch
// failure reads as a missing value, never as template syntax or a wrong number.
func SubstituteLetterTokens(s string) string {
	if !strings.Contains(s, "{{market:") {
		return s
	}
	letterTokens.Lock()
	values := make(map[string]string, len(letterTokens.values))
	if time.Since(letterTokens.asOf) > letterTokenTTL {
		values = nil
	} else {
		for token, value := range letterTokens.values {
			values[token] = value
		}
	}
	letterTokens.Unlock()
	return letterTokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		if v, ok := values[tok]; ok {
			return v
		}
		return "—"
	})
}
