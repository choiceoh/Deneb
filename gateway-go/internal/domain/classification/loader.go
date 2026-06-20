package classification

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// rulesFileName is the operator-maintained rules file under the state dir. It is
// deliberately OUTSIDE the repo: it holds real person/거래처 names, which must
// never be committed (privacy). The repo ships only a fake-name example template
// (classification_rules.example.json) for the operator to copy and fill.
const rulesFileName = "classification_rules.json"

// rulesEnvVar overrides the rules file path (tests, non-standard deployments).
// Mirrors the DENEB_*_URL/PATH override convention used across sidecars.
const rulesEnvVar = "DENEB_CLASSIFICATION_RULES"

// rulesJSON is the on-disk shape: three string→string maps. Kept separate from
// the typed Rules so the lane values can be validated (and bad lanes dropped) as
// they're folded into the typed maps.
type rulesJSON struct {
	PersonToLane  map[string]string `json:"personToLane"`
	CompanyToLane map[string]string `json:"companyToLane"`
	KeywordToLane map[string]string `json:"keywordToLane"`
}

// DefaultKeywordRules returns the generic domain-keyword → lane mapping that
// ships in code. These are *industry terms*, not operator data, so they're safe
// to hardcode and give the dashboard a useful baseline before any JSON exists:
//
//   - 1팀 (인허가/개발): 인허가, 개발행위, 허가, 인허가, 민원
//   - 2팀 (루프탑): 루프탑, 지붕, 옥상
//   - 3팀 (구매/발주): 구매, 발주, 자재, 모듈 (PV module sourcing)
//   - 남도에코 (케이블/전선): 케이블, 전선, 가공, 지중
//
// Keys are pre-normalized (lowercase) to match the load path. The operator's
// keywordToLane entries in the JSON merge on top of (and can override) these.
func DefaultKeywordRules() map[string]Lane {
	return map[string]Lane{
		// 1팀 — 인허가/개발행위.
		"인허가":  LaneTeam1,
		"개발행위": LaneTeam1,
		"허가":   LaneTeam1,
		"민원":   LaneTeam1,
		// 2팀 — 루프탑(지붕형).
		"루프탑": LaneTeam2,
		"지붕":  LaneTeam2,
		"옥상":  LaneTeam2,
		// 3팀 — 구매/발주(모듈 등 자재).
		"구매": LaneTeam3,
		"발주": LaneTeam3,
		"자재": LaneTeam3,
		"모듈": LaneTeam3,
		// 남도에코 — 케이블/전선.
		"케이블": LaneNamdo,
		"전선":  LaneNamdo,
		"가공":  LaneNamdo,
		"지중":  LaneNamdo,
	}
}

// DefaultRules returns the in-code ruleset: empty person/company maps (operator
// data only) plus the generic keyword defaults. This is what the classifier uses
// when no JSON file is present — keyword-only classification, everything else
// 미분류. Safe and privacy-clean.
func DefaultRules() Rules {
	return Rules{
		PersonToLane:  map[string]Lane{},
		CompanyToLane: map[string]Lane{},
		KeywordToLane: DefaultKeywordRules(),
	}
}

// resolveRulesPath returns the rules file path: the DENEB_CLASSIFICATION_RULES
// override if set, else {stateDir}/classification_rules.json (DENEB_STATE_DIR-
// aware via config.ResolveStateDir, so a dev gateway reads its own dir).
func resolveRulesPath() string {
	if v := strings.TrimSpace(os.Getenv(rulesEnvVar)); v != "" {
		return v
	}
	return filepath.Join(config.ResolveStateDir(), rulesFileName)
}

// Load reads the operator rules file and merges it over the in-code defaults,
// resolving the path itself (env override → state dir). A missing file is NOT an
// error: it returns DefaultRules() so a fresh install classifies by keyword and
// the operator opts into people/companies by creating the file. A present-but-
// corrupt file IS an error (so a typo'd JSON surfaces instead of silently
// reverting to keyword-only). Operator JSON entries override the keyword
// defaults on key collision.
func Load() (Rules, error) {
	return LoadFromFile(resolveRulesPath())
}

// LoadFromFile is Load against an explicit path (the testable core). Used by
// Load with the resolved path; tests pass a temp file.
func LoadFromFile(path string) (Rules, error) {
	rules := DefaultRules()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No operator rules yet — keyword-only defaults. Not an error.
			return rules, nil
		}
		return rules, fmt.Errorf("classification: read rules %s: %w", path, err)
	}

	var raw rulesJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return rules, fmt.Errorf("classification: parse rules %s: %w", path, err)
	}

	mergePersons(rules.PersonToLane, raw.PersonToLane)
	mergeCompanies(rules.CompanyToLane, raw.CompanyToLane)
	mergeKeywords(rules.KeywordToLane, raw.KeywordToLane)
	return rules, nil
}

// mergePersons folds JSON person entries into dst, normalizing each name with
// the wiki helper (so the rules file can hold display names like "김민준 부장" and
// still match an attendee "김민준"). Entries with an invalid/blank lane or a name
// that normalizes too short are skipped.
func mergePersons(dst map[string]Lane, src map[string]string) {
	for name, laneStr := range src {
		lane := Lane(strings.TrimSpace(laneStr))
		if !validLane(lane) {
			continue
		}
		key := wiki.NormalizePersonName(name)
		if len([]rune(key)) < 2 {
			continue
		}
		dst[key] = lane
	}
}

// mergeCompanies folds JSON company entries into dst, normalizing each firm name
// (lowercase + space-strip) to match the lookup form. Invalid lanes / blank keys
// are skipped.
func mergeCompanies(dst map[string]Lane, src map[string]string) {
	for name, laneStr := range src {
		lane := Lane(strings.TrimSpace(laneStr))
		if !validLane(lane) {
			continue
		}
		key := normalizeCompany(name)
		if key == "" {
			continue
		}
		dst[key] = lane
	}
}

// mergeKeywords folds JSON keyword entries into dst, lowercasing each keyword to
// match the substring-match form. Operator entries override the in-code keyword
// defaults on collision. Invalid lanes / blank keys are skipped.
func mergeKeywords(dst map[string]Lane, src map[string]string) {
	for kw, laneStr := range src {
		lane := Lane(strings.TrimSpace(laneStr))
		if !validLane(lane) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(kw))
		if key == "" {
			continue
		}
		dst[key] = lane
	}
}
