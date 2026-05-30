// Package promptguard is the single source of truth for prompt-injection /
// "Brainworm"-class threat signatures in UNTRUSTED content that flows back into
// the model: recalled memory (wiki/diary/session search) and tool output that
// originated outside the operator (web fetch, email, API responses).
//
// Deneb is single-user, so the threat here is not a malicious operator — it is
// an attacker who plants instructions in content the agent later ingests (a web
// page, an email, a calendar invite) hoping the model treats that text as
// commands. This package gives the recall path and the tool-execution path a
// shared, deterministic scanner so both chokepoints flag the same signatures.
//
// Mirrors NousResearch/hermes-agent tools/threat_patterns.py (single source +
// load-time scan + tool-result delimiters). Leaf package: stdlib only, so both
// internal/pipeline/chat and internal/agentsys/agent can import it without a
// dependency cycle.
package promptguard

import (
	"regexp"
	"strings"
)

// Match is one detected threat signature.
type Match struct {
	Label   string // short human label, e.g. "instruction-override"
	Snippet string // the matched text (truncated), for logging/annotation
}

type pattern struct {
	label string
	re    *regexp.Regexp
}

// patterns is the curated signature set. Kept deliberately small and
// high-precision: a false positive only adds an advisory delimiter around tool
// output (it never blocks the tool), so we bias toward catching the classic
// override/impersonation/exfiltration phrasings rather than exhaustive coverage.
var patterns = []pattern{
	// Instruction override (English + Korean).
	{"instruction-override", regexp.MustCompile(`(?i)\b(?:ignore|disregard|forget)\b[^.\n]{0,40}\b(?:previous|prior|above|earlier|all)\b[^.\n]{0,20}\b(?:instruction|prompt|message|context|rule)s?`)},
	{"instruction-override", regexp.MustCompile(`이전[^\n]{0,12}(?:지시|명령|프롬프트|규칙)[^\n]{0,8}(?:무시|잊)`)},
	// System-prompt / persona hijack.
	{"persona-hijack", regexp.MustCompile(`(?i)\byou are now\b|\bnew (?:instructions?|system prompt|persona)\s*[:：]|\b(?:developer|jailbreak|dan)\s+mode\b`)},
	{"persona-hijack", regexp.MustCompile(`(?i)\boverride\b[^.\n]{0,20}\b(?:system|safety|guard)`)},
	// Role/control-token impersonation embedded in data.
	{"role-impersonation", regexp.MustCompile(`(?i)<\|?\s*(?:im_start|im_end|system|assistant)\s*\|?>`)},
	{"role-impersonation", regexp.MustCompile(`(?im)^\s*(?:system|assistant|developer)\s*[:：]\s`)},
	// Credential / secret exfiltration.
	{"exfiltration", regexp.MustCompile(`(?i)\b(?:send|email|post|exfiltrate|leak|reveal|print|paste)\b[^.\n]{0,40}\b(?:api[\s_-]?key|password|token|secret|credential|private key|seed phrase)s?`)},
	{"exfiltration", regexp.MustCompile(`-{5}BEGIN (?:RSA |OPENSSH |EC |DSA )?PRIVATE KEY-{5}`)},
	// Remote-code / C2 fetch-and-run.
	{"c2-execution", regexp.MustCompile(`(?i)\bcurl\b[^\n|]{0,200}\|\s*(?:sudo\s+)?(?:ba)?sh\b`)},
	{"c2-execution", regexp.MustCompile(`(?i)\b(?:wget|curl)\b[^\n]{0,120}&&[^\n]{0,40}\b(?:chmod\s+\+x|\./)`)},
	{"c2-execution", regexp.MustCompile(`(?i)base64\s+(?:-d|--decode)[^\n|]{0,80}\|\s*(?:ba)?sh\b`)},
}

const snippetMax = 80

// Scan returns every distinct threat signature found in text, capped so a
// pathological input cannot produce an unbounded result. Order follows the
// pattern table (stable) and each label appears at most once.
func Scan(text string) []Match {
	if text == "" {
		return nil
	}
	var matches []Match
	seen := make(map[string]struct{}, len(patterns))
	for _, p := range patterns {
		if _, ok := seen[p.label]; ok {
			continue
		}
		loc := p.re.FindString(text)
		if loc == "" {
			continue
		}
		seen[p.label] = struct{}{}
		matches = append(matches, Match{Label: p.label, Snippet: truncate(loc, snippetMax)})
	}
	return matches
}

// HasThreat reports whether any signature matches. Cheaper than Scan when the
// caller only needs a boolean (it stops at the first hit).
func HasThreat(text string) bool {
	if text == "" {
		return false
	}
	for _, p := range patterns {
		if p.re.MatchString(text) {
			return true
		}
	}
	return false
}

// Labels returns the deduped label list for a set of matches, for compact
// logging / annotation (e.g. "instruction-override, exfiltration").
func Labels(matches []Match) string {
	if len(matches) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(matches))
	var out []string
	for _, m := range matches {
		if _, ok := seen[m.Label]; ok {
			continue
		}
		seen[m.Label] = struct{}{}
		out = append(out, m.Label)
	}
	return strings.Join(out, ", ")
}

func truncate(s string, limit int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	return string(r[:limit]) + "…"
}
