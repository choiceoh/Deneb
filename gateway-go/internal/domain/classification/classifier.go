// Package classification buckets work items into the operator's managed parts
// (레인) for the "파트별 업무 현황" dashboard. The user is a solar-group executive
// who oversees five parts; this engine answers "which part does this item belong
// to?" so the native dashboard can group calendar events, work-feed cards, and
// (later) mail/todos by part.
//
// Design intent (rule-based, no LLM — like domain/mailpriority): the signal that
// actually identifies a part is the *person* attached to an item (a meeting
// attendee, a mail sender). People map to parts. Company and keyword are weaker
// supporting signals. When nothing matches with enough confidence the item lands
// in the 미분류 (unclassified) lane rather than being force-fit into a wrong part.
//
// ★ Privacy: the real person/company rosters are operator data and MUST NOT be
// compiled into this repo. The maps below are populated at runtime from
// {stateDir}/classification_rules.json (see loader.go). Code ships only the
// generic domain keyword defaults (루프탑→2팀, 인허가→1팀, …) — never a name.
package classification

import (
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// Lane is the part bucket an item is sorted into.
type Lane string

const (
	// LaneTeam1 — 기획조정실 1팀.
	LaneTeam1 Lane = "team1"
	// LaneTeam2 — 기획조정실 2팀.
	LaneTeam2 Lane = "team2"
	// LaneTeam3 — 기획조정실 3팀.
	LaneTeam3 Lane = "team3"
	// LaneNamdo — 남도에코.
	LaneNamdo Lane = "namdo"
	// LanePersonal — 개인/기타 (the operator's own items: solo events, personal
	// admin) that belong to no team but are still real work.
	LanePersonal Lane = "personal"
	// LaneUnclassified — no signal reached the confidence floor. A holding lane,
	// not a part; the dashboard shows it last so the operator can triage.
	LaneUnclassified Lane = "unclassified"
)

// Confidence ranks how strong the matched signal was, so the dashboard can show
// (or the caller can act on) a weak vs. strong assignment. Higher is stronger.
type Confidence int

const (
	// ConfNone — nothing matched; the item is LaneUnclassified.
	ConfNone Confidence = 0
	// ConfWeak — only a keyword in the title/body matched. Keywords are the
	// noisiest signal (a subject can mention 루프탑 in passing), so this is the
	// lowest non-zero rank.
	ConfWeak Confidence = 1
	// ConfMedium — a company name matched. A counterparty firm is a fairly
	// reliable part signal but less direct than a named person.
	ConfMedium Confidence = 2
	// ConfStrong — a named person (attendee/organizer/sender) mapped to a part.
	// This is the primary signal the whole engine is built around.
	ConfStrong Confidence = 3
)

// AllLanes lists the real part lanes in display order (excludes the holding
// LaneUnclassified, which the dashboard appends last). Exposed so the handler
// can render every part — even empty ones — in a stable order.
var AllLanes = []Lane{LaneTeam1, LaneTeam2, LaneTeam3, LaneNamdo, LanePersonal}

// laneNames are the Korean display labels for each lane. DisplayName falls back
// to the raw key for an unknown lane so a future lane never renders blank.
var laneNames = map[Lane]string{
	LaneTeam1:        "기획조정실 1팀",
	LaneTeam2:        "기획조정실 2팀",
	LaneTeam3:        "기획조정실 3팀",
	LaneNamdo:        "남도에코",
	LanePersonal:     "개인/기타",
	LaneUnclassified: "미분류",
}

// DisplayName returns the Korean label for a lane.
func DisplayName(l Lane) string {
	if n, ok := laneNames[l]; ok {
		return n
	}
	return string(l)
}

// validLane reports whether s names a real lane (so a typo in the rules JSON,
// e.g. "team22", is dropped at load time rather than silently routing items to a
// non-existent lane).
func validLane(s Lane) bool {
	switch s {
	case LaneTeam1, LaneTeam2, LaneTeam3, LaneNamdo, LanePersonal, LaneUnclassified:
		return true
	default:
		return false
	}
}

// Rules is the data-driven mapping from work signals to lanes. Every map is
// keyed by a *normalized* form (see normalization in Classify): person names go
// through wiki.NormalizePersonName, companies/keywords are lowercased + space-
// stripped. Loaded from JSON at runtime; the zero value (all-nil) is a valid
// empty ruleset that classifies everything as LaneUnclassified.
type Rules struct {
	// PersonToLane maps a normalized person name → lane. The strong signal.
	PersonToLane map[string]Lane
	// CompanyToLane maps a normalized company/거래처 name → lane. Medium signal.
	CompanyToLane map[string]Lane
	// KeywordToLane maps a normalized keyword → lane. Weak signal; matched as a
	// substring of the item's text.
	KeywordToLane map[string]Lane
}

// Signals carries everything known about one work item that could identify its
// part. Any field may be empty; the classifier uses whichever are present in
// strength order. Keeping this a plain value (not tied to calendar/workfeed
// types) is the decoupling seam — every data source projects its rows into
// Signals, so adding mail/todos later needs no change here.
type Signals struct {
	// People are the names attached to the item: meeting attendees + organizer,
	// or a mail sender's display name. The primary classification input.
	People []string
	// Companies are counterparty/거래처 names mentioned by the item (e.g. parsed
	// from an organizer's org field). Supporting signal.
	Companies []string
	// Text is the free-form title/subject/body the keyword pass scans. Lowercased
	// internally before matching.
	Text string
}

// Classify returns the best lane for the given signals plus the confidence of
// that decision. Resolution order mirrors signal reliability (strong → weak):
//
//  1. person → lane   (ConfStrong)  — a named attendee/sender maps to a part
//  2. company → lane  (ConfMedium)  — a counterparty firm maps to a part
//  3. keyword → lane  (ConfWeak)    — a domain keyword appears in the text
//
// The first tier that produces a match wins; lower tiers are not consulted. When
// no tier matches, the item is LaneUnclassified / ConfNone. A nil/empty ruleset
// always yields LaneUnclassified — the engine never guesses without data.
//
// Within a tier, multiple matches are resolved deterministically (see the per-
// tier helpers) so the same item always lands in the same lane.
func (r Rules) Classify(sig Signals) (Lane, Confidence) {
	// Tier 1 — person (strongest). A meeting's attendees/organizer or a mail's
	// sender are the most direct part signal we have.
	if lane, ok := matchPerson(r.PersonToLane, sig.People); ok {
		return lane, ConfStrong
	}
	// Tier 2 — company. Less direct than a person but still a real counterparty
	// signal (e.g. the organizer's firm).
	if lane, ok := matchCompany(r.CompanyToLane, sig.Companies); ok {
		return lane, ConfMedium
	}
	// Tier 3 — keyword (weakest). A domain term in the title/body. Substring
	// match, so "루프탑 점검 일정" hits the 루프탑 keyword.
	if lane, ok := matchKeyword(r.KeywordToLane, sig.Text); ok {
		return lane, ConfWeak
	}
	return LaneUnclassified, ConfNone
}

// matchPerson resolves people against the person→lane map. Each name is
// normalized with the same wiki helper the contacts sync and mail-sender lookup
// use, so "김민준 부장" and "김민준대표님" both match a "김민준" rule. To stay
// deterministic when several attendees map to *different* lanes, candidate lanes
// are collected and the lexicographically smallest lane key wins (a stable,
// explainable tie-break — not input order, which can shuffle).
func matchPerson(m map[string]Lane, people []string) (Lane, bool) {
	if len(m) == 0 {
		return "", false
	}
	var hits []Lane
	seen := map[Lane]bool{}
	for _, name := range people {
		key := wiki.NormalizePersonName(name)
		if len([]rune(key)) < 2 {
			continue // too short/ambiguous to match a person
		}
		if lane, ok := m[key]; ok && !seen[lane] {
			seen[lane] = true
			hits = append(hits, lane)
		}
	}
	return pickLane(hits)
}

// matchCompany resolves companies against the company→lane map. Company names
// are normalized by lowercasing + stripping spaces (no honorific peeling — a
// firm name has none). Substring match in *both* directions: a rule "탑솔라"
// matches a company string "탑솔라에너지(주)", and a rule "탑솔라에너지" matches a
// bare "탑솔라에너지". This catches the common 주식회사/(주)/Co. decorations
// without enumerating them.
func matchCompany(m map[string]Lane, companies []string) (Lane, bool) {
	if len(m) == 0 {
		return "", false
	}
	var hits []Lane
	seen := map[Lane]bool{}
	for _, raw := range companies {
		c := normalizeCompany(raw)
		if c == "" {
			continue
		}
		for key, lane := range m {
			if seen[lane] {
				continue
			}
			if strings.Contains(c, key) || strings.Contains(key, c) {
				seen[lane] = true
				hits = append(hits, lane)
			}
		}
	}
	return pickLane(hits)
}

// matchKeyword resolves the item text against the keyword→lane map by substring.
// The text is lowercased once; each keyword (already normalized at load) is
// tested as a substring. Deterministic tie-break via pickLane when keywords for
// different lanes both appear.
func matchKeyword(m map[string]Lane, text string) (Lane, bool) {
	if len(m) == 0 {
		return "", false
	}
	t := strings.ToLower(text)
	if strings.TrimSpace(t) == "" {
		return "", false
	}
	var hits []Lane
	seen := map[Lane]bool{}
	for key, lane := range m {
		if seen[lane] || key == "" {
			continue
		}
		if strings.Contains(t, key) {
			seen[lane] = true
			hits = append(hits, lane)
		}
	}
	return pickLane(hits)
}

// pickLane reduces a set of candidate lanes to one deterministic winner: the
// lexicographically smallest lane key. With a single hit this just returns it;
// with several (an item that legitimately touches two parts) it picks a stable,
// reproducible lane so the dashboard never flickers between groupings on reload.
func pickLane(hits []Lane) (Lane, bool) {
	switch len(hits) {
	case 0:
		return "", false
	case 1:
		return hits[0], true
	default:
		sort.Slice(hits, func(i, j int) bool { return hits[i] < hits[j] })
		return hits[0], true
	}
}

// normalizeCompany reduces a 거래처/firm name to a match key: lowercase, all
// whitespace removed. Mirrors the key form used when the rules JSON is loaded so
// lookups line up. (Intentionally simpler than NormalizePersonName — firms carry
// no honorifics to peel.)
func normalizeCompany(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

// NormalizePersonName returns the person match-key form used by every rule
// source (the loader's JSON merge and Classify's lookup): the shared wiki
// normalization (honorific peel + space strip + lowercase). Exported so an
// alternate rule producer — e.g. the org chart's DeriveRules — keys its
// PersonToLane entries identically, instead of re-implementing (and drifting
// from) the matcher's key form.
func NormalizePersonName(s string) string {
	return wiki.NormalizePersonName(s)
}

// NormalizeCompany returns the company match-key form (lowercase +
// whitespace-stripped) used by CompanyToLane lookups. Exported for the same
// single-source-of-truth reason as NormalizePersonName.
func NormalizeCompany(s string) string {
	return normalizeCompany(s)
}
