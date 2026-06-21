package wiki

import (
	"regexp"
	"strings"
)

// Composite project code: [부서]-[고객]-[거래타입]-[순번], every segment 3-char.
// The code is the FROZEN identity of a project. Folders, titles, and streams are
// mutable views derived from it; the code itself never changes once minted, so a
// cross-reference that points at the code survives renames and reclassification.
//
// Examples: pl3-tri-mod-001 (3팀·트리나·모듈·1), nde-ztt-cbl-001 (남도에코·ZTT·케이블·1).

// projectCodeRe matches a well-formed code: 3-char dept / 3-char client /
// 3-letter deal-type / 3-digit sequence. Dept and client allow a trailing digit
// (pl1, bs8); deal-type is letters only; sequence is digits only.
var projectCodeRe = regexp.MustCompile(`^[a-z][a-z0-9]{2}-[a-z0-9]{3}-[a-z]{3}-[0-9]{3}$`)

// DeptCodes are the fixed department segments. etc lumps every other division
// (전략사업본부·미래사업실·설계실); com is multi-division collaboration / JV.
var DeptCodes = map[string]string{
	"pl0": "기획조정실장 직할 (오선택 직접)",
	"pl1": "기획조정실 1팀 (사업개발)",
	"pl2": "기획조정실 2팀 (루프탑·RE100·자가소비)",
	"pl3": "기획조정실 3팀 (모듈·인버터 조달)",
	"nde": "남도에코 (케이블 조달)",
	"etc": "타부서 (전략사업본부·미래사업실·설계실)",
	"com": "다부서 협동 / JV",
}

// DealTypeCodes are the fixed deal-type segments.
var DealTypeCodes = map[string]string{
	"dev": "개발·인허가",
	"epc": "EPC 시공·O&M",
	"mod": "모듈 조달",
	"inv": "인버터 조달",
	"cbl": "케이블 조달",
	"bes": "BESS",
	"wnd": "풍력",
}

// normalizeProjectCode lowercases, trims, and strips any wikilink wrapper from a
// raw frontmatter code value. It does not reject malformed values (parsing stays
// lenient); callers use ValidProjectCode to gate.
func normalizeProjectCode(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	return strings.ToLower(strings.TrimSpace(s))
}

// ValidProjectCode reports whether s is a structurally well-formed project code
// with a known department and deal-type segment.
func ValidProjectCode(s string) bool {
	if !projectCodeRe.MatchString(s) {
		return false
	}
	parts := strings.Split(s, "-")
	if _, ok := DeptCodes[parts[0]]; !ok {
		return false
	}
	if _, ok := DealTypeCodes[parts[2]]; !ok {
		return false
	}
	return true
}
