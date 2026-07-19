package classification

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// All people and company names in this file are synthetic fixtures. The
// package boundary must never need the operator's private roster to be tested.

func TestLaneConstantsAndDisplayOrderContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		lane Lane
		key  string
		name string
		real bool
	}{
		{lane: LaneTeam1, key: "team1", name: "기획조정실 1팀", real: true},
		{lane: LaneTeam2, key: "team2", name: "기획조정실 2팀", real: true},
		{lane: LaneTeam3, key: "team3", name: "기획조정실 3팀", real: true},
		{lane: LaneNamdo, key: "namdo", name: "남도에코", real: true},
		{lane: LanePersonal, key: "personal", name: "개인/기타", real: true},
		{lane: LaneUnclassified, key: "unclassified", name: "미분류", real: false},
	}
	for _, tc := range tests {
		if string(tc.lane) != tc.key {
			t.Errorf("lane key = %q, want %q", tc.lane, tc.key)
		}
		if got := DisplayName(tc.lane); got != tc.name {
			t.Errorf("DisplayName(%q) = %q, want %q", tc.lane, got, tc.name)
		}
		if !validLane(tc.lane) {
			t.Errorf("validLane(%q) = false", tc.lane)
		}
	}
	wantOrder := []Lane{LaneTeam1, LaneTeam2, LaneTeam3, LaneNamdo, LanePersonal}
	if !reflect.DeepEqual(AllLanes, wantOrder) {
		t.Fatalf("AllLanes = %v, want %v", AllLanes, wantOrder)
	}
	for _, lane := range AllLanes {
		if lane == LaneUnclassified {
			t.Fatal("AllLanes contains holding lane")
		}
		if DisplayName(lane) == "" {
			t.Fatalf("AllLanes entry %q has blank display name", lane)
		}
	}
}

func TestValidLaneRejectsUnknownBoundaryValues(t *testing.T) {
	t.Parallel()

	invalid := []Lane{
		"",
		" ",
		"TEAM1",
		"Team1",
		"team0",
		"team4",
		"team01",
		"team1 ",
		" team1",
		"team1\n",
		"남도에코",
		"none",
		"unknown",
		"unclassified-extra",
	}
	for _, lane := range invalid {
		if validLane(lane) {
			t.Errorf("validLane(%q) = true", lane)
		}
		if got := DisplayName(lane); got != string(lane) {
			t.Errorf("DisplayName(%q) = %q, want raw key", lane, got)
		}
	}
}

func TestConfidenceOrderingContract(t *testing.T) {
	t.Parallel()

	if ConfNone != 0 || ConfWeak != 1 || ConfMedium != 2 || ConfStrong != 3 {
		t.Fatalf("confidence values changed: none=%d weak=%d medium=%d strong=%d", ConfNone, ConfWeak, ConfMedium, ConfStrong)
	}
	if !(ConfNone < ConfWeak && ConfWeak < ConfMedium && ConfMedium < ConfStrong) {
		t.Fatal("confidence levels are not strictly ordered")
	}
}

func TestNormalizeCompanyBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "ascii spaces", in: "   ", want: ""},
		{name: "tabs and newlines", in: "\t\n\r", want: ""},
		{name: "simple korean", in: "가나다 에너지", want: "가나다에너지"},
		{name: "leading trailing", in: "  가나다 에너지  ", want: "가나다에너지"},
		{name: "ascii case", in: "ACME Energy", want: "acmeenergy"},
		{name: "mixed case", in: "AcMe SoLaR", want: "acmesolar"},
		{name: "tabs", in: "ACME\tEnergy", want: "acmeenergy"},
		{name: "newlines", in: "ACME\nEnergy", want: "acmeenergy"},
		{name: "carriage return", in: "ACME\r\nEnergy", want: "acmeenergy"},
		{name: "nonbreaking space", in: "ACME\u00a0Energy", want: "acmeenergy"},
		{name: "ideographic space", in: "가나다\u3000에너지", want: "가나다에너지"},
		{name: "parentheses retained", in: "(주) 가나다", want: "(주)가나다"},
		{name: "punctuation retained", in: "ACME-Energy.Co", want: "acme-energy.co"},
		{name: "digits retained", in: "Solar 2026", want: "solar2026"},
		{name: "unicode case", in: "ÄCME GmbH", want: "äcmegmbh"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeCompany(tc.in); got != tc.want {
				t.Fatalf("normalizeCompany(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got := NormalizeCompany(tc.in); got != tc.want {
				t.Fatalf("NormalizeCompany(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPickLaneBoundaryAndDeterministicSorting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		in     []Lane
		want   Lane
		wantOK bool
	}{
		{name: "nil", in: nil, want: "", wantOK: false},
		{name: "empty", in: []Lane{}, want: "", wantOK: false},
		{name: "single team1", in: []Lane{LaneTeam1}, want: LaneTeam1, wantOK: true},
		{name: "single unclassified", in: []Lane{LaneUnclassified}, want: LaneUnclassified, wantOK: true},
		{name: "two ordered", in: []Lane{LaneNamdo, LaneTeam3}, want: LaneNamdo, wantOK: true},
		{name: "two reversed", in: []Lane{LaneTeam3, LaneNamdo}, want: LaneNamdo, wantOK: true},
		{name: "all real lanes", in: []Lane{LaneTeam3, LaneTeam2, LanePersonal, LaneTeam1, LaneNamdo}, want: LaneNamdo, wantOK: true},
		{name: "duplicates", in: []Lane{LaneTeam2, LaneTeam2, LaneTeam1}, want: LaneTeam1, wantOK: true},
		{name: "unknown sorts first", in: []Lane{LaneTeam1, Lane("aaa")}, want: Lane("aaa"), wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := append([]Lane(nil), tc.in...)
			got, ok := pickLane(input)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("pickLane(%v) = (%q,%v), want (%q,%v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestMatchPersonNormalizesAndBreaksTiesDeterministically(t *testing.T) {
	t.Parallel()

	rules := map[string]Lane{
		"홍길동":      LaneTeam3,
		"김하늘":      LaneTeam1,
		"alicekim": LaneNamdo,
	}
	tests := []struct {
		name   string
		people []string
		want   Lane
		ok     bool
	}{
		{name: "nil people", people: nil, want: "", ok: false},
		{name: "empty people", people: []string{}, want: "", ok: false},
		{name: "blank person", people: []string{"  "}, want: "", ok: false},
		{name: "one rune ignored", people: []string{"김"}, want: "", ok: false},
		{name: "unknown", people: []string{"박바다"}, want: "", ok: false},
		{name: "plain match", people: []string{"홍길동"}, want: LaneTeam3, ok: true},
		{name: "honorific match", people: []string{"홍길동 부장"}, want: LaneTeam3, ok: true},
		{name: "affiliation match", people: []string{"홍길동(가나다)"}, want: LaneTeam3, ok: true},
		{name: "ascii case match", people: []string{"ALICE KIM"}, want: LaneNamdo, ok: true},
		{name: "same lane dedup", people: []string{"홍길동", "홍길동 부장"}, want: LaneTeam3, ok: true},
		{name: "tie sorted", people: []string{"홍길동", "김하늘"}, want: LaneTeam1, ok: true},
		{name: "tie reverse input", people: []string{"김하늘", "홍길동"}, want: LaneTeam1, ok: true},
		{name: "three lanes", people: []string{"홍길동", "김하늘", "ALICE KIM"}, want: LaneNamdo, ok: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := matchPerson(rules, tc.people)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("matchPerson(%v) = (%q,%v), want (%q,%v)", tc.people, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestMatchPersonEmptyRuleMapsNeverMatch(t *testing.T) {
	t.Parallel()

	for _, rules := range []map[string]Lane{nil, {}, make(map[string]Lane)} {
		if lane, ok := matchPerson(rules, []string{"홍길동"}); ok || lane != "" {
			t.Errorf("matchPerson(%#v) = (%q,%v)", rules, lane, ok)
		}
	}
}

func TestMatchCompanyNormalizesSubstringDirectionsAndTieBreak(t *testing.T) {
	t.Parallel()

	rules := map[string]Lane{
		"가나다에너지":    LaneTeam2,
		"acmesolar": LaneTeam3,
		"별빛":        LaneNamdo,
	}
	tests := []struct {
		name      string
		companies []string
		want      Lane
		ok        bool
	}{
		{name: "nil", companies: nil, want: "", ok: false},
		{name: "empty", companies: []string{}, want: "", ok: false},
		{name: "blank", companies: []string{" \t "}, want: "", ok: false},
		{name: "unknown", companies: []string{"없는회사"}, want: "", ok: false},
		{name: "exact", companies: []string{"가나다에너지"}, want: LaneTeam2, ok: true},
		{name: "decorated input", companies: []string{"(주) 가나다에너지"}, want: LaneTeam2, ok: true},
		{name: "input longer", companies: []string{"가나다에너지솔루션"}, want: LaneTeam2, ok: true},
		{name: "input shorter", companies: []string{"가나다"}, want: LaneTeam2, ok: true},
		{name: "ascii case and spaces", companies: []string{"ACME SOLAR"}, want: LaneTeam3, ok: true},
		{name: "same lane repeated", companies: []string{"ACME SOLAR", "acmesolar ltd"}, want: LaneTeam3, ok: true},
		{name: "two lanes", companies: []string{"가나다에너지", "별빛"}, want: LaneNamdo, ok: true},
		{name: "two lanes reversed", companies: []string{"별빛", "가나다에너지"}, want: LaneNamdo, ok: true},
		{name: "unknown then known", companies: []string{"없는회사", "별빛전기"}, want: LaneNamdo, ok: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := matchCompany(rules, tc.companies)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("matchCompany(%v) = (%q,%v), want (%q,%v)", tc.companies, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestMatchCompanyContractIgnoresMapConstructionOrder(t *testing.T) {
	t.Parallel()

	entries := []struct {
		key  string
		lane Lane
	}{
		{key: "solar", lane: LaneTeam3},
		{key: "energy", lane: LaneTeam1},
		{key: "company", lane: LaneNamdo},
	}
	for iteration := 0; iteration < 300; iteration++ {
		rules := make(map[string]Lane)
		for offset := range entries {
			entry := entries[(iteration+offset)%len(entries)]
			rules[entry.key] = entry.lane
		}
		lane, ok := matchCompany(rules, []string{"Solar Energy Company"})
		if !ok || lane != LaneNamdo {
			t.Fatalf("iteration %d: got (%q,%v), want (namdo,true)", iteration, lane, ok)
		}
	}
}

func TestMatchKeywordBoundaryMatrix(t *testing.T) {
	t.Parallel()

	rules := map[string]Lane{
		"루프탑":        LaneTeam2,
		"permit":     LaneTeam1,
		"케이블":        LaneNamdo,
		"":           LanePersonal,
		"multi word": LaneTeam3,
	}
	tests := []struct {
		name string
		text string
		want Lane
		ok   bool
	}{
		{name: "empty", text: "", want: "", ok: false},
		{name: "spaces", text: " \t\n ", want: "", ok: false},
		{name: "unknown", text: "일반 업무", want: "", ok: false},
		{name: "korean exact", text: "루프탑", want: LaneTeam2, ok: true},
		{name: "korean substring", text: "신규 루프탑발전 점검", want: LaneTeam2, ok: true},
		{name: "ascii lowercase", text: "permit review", want: LaneTeam1, ok: true},
		{name: "ascii uppercase", text: "PERMIT REVIEW", want: LaneTeam1, ok: true},
		{name: "ascii mixed case", text: "Permit Review", want: LaneTeam1, ok: true},
		{name: "multi word", text: "a MULTI WORD phrase", want: LaneTeam3, ok: true},
		{name: "empty key ignored", text: "anything", want: "", ok: false},
		{name: "two lane tie", text: "루프탑 케이블", want: LaneNamdo, ok: true},
		{name: "two lane reverse text", text: "케이블 루프탑", want: LaneNamdo, ok: true},
		{name: "three lanes", text: "PERMIT 루프탑 케이블", want: LaneNamdo, ok: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := matchKeyword(rules, tc.text)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("matchKeyword(%q) = (%q,%v), want (%q,%v)", tc.text, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestClassifyPrecedenceFallbackAcrossStrengthTiers(t *testing.T) {
	t.Parallel()

	rules := Rules{
		PersonToLane: map[string]Lane{
			"홍길동": LaneTeam3,
			"김하늘": LanePersonal,
		},
		CompanyToLane: map[string]Lane{
			"가나다에너지": LaneTeam2,
			"별빛전기":   LaneNamdo,
		},
		KeywordToLane: map[string]Lane{
			"인허가": LaneTeam1,
			"발주":  LaneTeam3,
		},
	}
	tests := []struct {
		name string
		sig  Signals
		lane Lane
		conf Confidence
	}{
		{
			name: "nothing",
			sig:  Signals{},
			lane: LaneUnclassified,
			conf: ConfNone,
		},
		{
			name: "unknown signals",
			sig: Signals{
				People:    []string{"박바다"},
				Companies: []string{"없는회사"},
				Text:      "일상 공유",
			},
			lane: LaneUnclassified,
			conf: ConfNone,
		},
		{
			name: "keyword only",
			sig:  Signals{Text: "인허가 검토"},
			lane: LaneTeam1,
			conf: ConfWeak,
		},
		{
			name: "company only",
			sig:  Signals{Companies: []string{"가나다 에너지"}},
			lane: LaneTeam2,
			conf: ConfMedium,
		},
		{
			name: "person only",
			sig:  Signals{People: []string{"홍길동 부장"}},
			lane: LaneTeam3,
			conf: ConfStrong,
		},
		{
			name: "company beats conflicting keyword",
			sig: Signals{
				Companies: []string{"가나다에너지"},
				Text:      "인허가 검토",
			},
			lane: LaneTeam2,
			conf: ConfMedium,
		},
		{
			name: "person beats conflicting company",
			sig: Signals{
				People:    []string{"홍길동"},
				Companies: []string{"가나다에너지"},
			},
			lane: LaneTeam3,
			conf: ConfStrong,
		},
		{
			name: "person beats all weaker tiers",
			sig: Signals{
				People:    []string{"김하늘"},
				Companies: []string{"별빛전기"},
				Text:      "인허가 발주",
			},
			lane: LanePersonal,
			conf: ConfStrong,
		},
		{
			name: "unknown person falls through to company",
			sig: Signals{
				People:    []string{"박바다"},
				Companies: []string{"별빛전기"},
				Text:      "인허가",
			},
			lane: LaneNamdo,
			conf: ConfMedium,
		},
		{
			name: "unknown person and company fall through to keyword",
			sig: Signals{
				People:    []string{"박바다"},
				Companies: []string{"없는회사"},
				Text:      "발주 협의",
			},
			lane: LaneTeam3,
			conf: ConfWeak,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lane, conf := rules.Classify(tc.sig)
			if lane != tc.lane || conf != tc.conf {
				t.Fatalf("Classify(%#v) = (%q,%d), want (%q,%d)", tc.sig, lane, conf, tc.lane, tc.conf)
			}
		})
	}
}

func TestClassifyPreservesSignalsAndRulesWithoutMutation(t *testing.T) {
	t.Parallel()

	rules := Rules{
		PersonToLane:  map[string]Lane{"홍길동": LaneTeam1},
		CompanyToLane: map[string]Lane{"가나다": LaneTeam2},
		KeywordToLane: map[string]Lane{"발주": LaneTeam3},
	}
	sig := Signals{
		People:    []string{"홍길동 부장", "박바다"},
		Companies: []string{"가나다 에너지", "없는회사"},
		Text:      "발주 업무",
	}
	wantRules := cloneRulesForTest(rules)
	wantSig := Signals{
		People:    append([]string(nil), sig.People...),
		Companies: append([]string(nil), sig.Companies...),
		Text:      sig.Text,
	}
	for i := 0; i < 100; i++ {
		lane, conf := rules.Classify(sig)
		if lane != LaneTeam1 || conf != ConfStrong {
			t.Fatalf("iteration %d = (%q,%d)", i, lane, conf)
		}
	}
	if !reflect.DeepEqual(rules, wantRules) {
		t.Fatalf("rules mutated: got=%#v want=%#v", rules, wantRules)
	}
	if !reflect.DeepEqual(sig, wantSig) {
		t.Fatalf("signals mutated: got=%#v want=%#v", sig, wantSig)
	}
}

func TestDefaultKeywordRulesCreatesIndependentCopies(t *testing.T) {
	t.Parallel()

	want := map[string]Lane{
		"인허가":  LaneTeam1,
		"개발행위": LaneTeam1,
		"허가":   LaneTeam1,
		"민원":   LaneTeam1,
		"루프탑":  LaneTeam2,
		"지붕":   LaneTeam2,
		"옥상":   LaneTeam2,
		"구매":   LaneTeam3,
		"발주":   LaneTeam3,
		"자재":   LaneTeam3,
		"모듈":   LaneTeam3,
		"케이블":  LaneNamdo,
		"전선":   LaneNamdo,
		"가공":   LaneNamdo,
		"지중":   LaneNamdo,
	}
	one := DefaultKeywordRules()
	two := DefaultKeywordRules()
	if !reflect.DeepEqual(one, want) || !reflect.DeepEqual(two, want) {
		t.Fatalf("defaults mismatch: one=%v two=%v want=%v", one, two, want)
	}
	one["루프탑"] = LanePersonal
	one["new"] = LanePersonal
	delete(one, "인허가")
	if !reflect.DeepEqual(two, want) {
		t.Fatalf("DefaultKeywordRules returned shared map: %#v", two)
	}
}

func TestDefaultRulesCreatesIndependentMapsAndClassifiesDefaults(t *testing.T) {
	t.Parallel()

	one := DefaultRules()
	two := DefaultRules()
	if one.PersonToLane == nil || one.CompanyToLane == nil || one.KeywordToLane == nil {
		t.Fatalf("DefaultRules contains nil maps: %#v", one)
	}
	if len(one.PersonToLane) != 0 || len(one.CompanyToLane) != 0 {
		t.Fatalf("DefaultRules leaked roster entries: %#v", one)
	}
	one.PersonToLane["가짜사람"] = LanePersonal
	one.CompanyToLane["가짜회사"] = LanePersonal
	one.KeywordToLane["루프탑"] = LanePersonal
	if len(two.PersonToLane) != 0 || len(two.CompanyToLane) != 0 || two.KeywordToLane["루프탑"] != LaneTeam2 {
		t.Fatalf("DefaultRules returned shared maps: %#v", two)
	}
	for keyword, lane := range DefaultKeywordRules() {
		got, conf := two.Classify(Signals{Text: "앞 " + keyword + " 뒤"})
		if got != lane || conf != ConfWeak {
			t.Errorf("default %q classified as (%q,%d), want (%q,%d)", keyword, got, conf, lane, ConfWeak)
		}
	}
}

func TestMergePersonsNormalizesDropsInvalidAndResolvesCollisions(t *testing.T) {
	t.Parallel()

	dst := map[string]Lane{"기존사람": LaneTeam1}
	src := map[string]string{
		"홍길동 부장":      "team2",
		"홍길동":         "team3",
		"김하늘(가나다에너지)": " namdo ",
		"박":           "team1",
		" ":           "team1",
		"이바다":         "TEAM1",
		"최태양":         "team99",
		"정별빛":         "",
	}
	mergePersons(dst, src)
	if dst["기존사람"] != LaneTeam1 {
		t.Fatal("existing destination entry lost")
	}
	if lane := dst["홍길동"]; lane != LaneTeam2 && lane != LaneTeam3 {
		t.Fatalf("normalized collision lane = %q, want one valid source value", lane)
	}
	if dst["김하늘"] != LaneNamdo {
		t.Fatalf("affiliation normalization = %q", dst["김하늘"])
	}
	for _, invalid := range []string{"박", "", "이바다", "최태양", "정별빛"} {
		if _, ok := dst[invalid]; ok {
			t.Errorf("invalid person entry %q merged", invalid)
		}
	}
}

func TestMergeCompaniesNormalizesOverridesAndDropsInvalid(t *testing.T) {
	t.Parallel()

	dst := map[string]Lane{
		"가나다에너지": LaneTeam1,
		"keep":   LanePersonal,
	}
	mergeCompanies(dst, map[string]string{
		"가나다 에너지":      LaneTeam2.StringForTest(),
		" ACME Solar ": "team3",
		"\t":           "team1",
		"Bad Lane":     "team999",
		"Case Invalid": " TEAM1 ",
	})
	if dst["가나다에너지"] != LaneTeam2 {
		t.Fatalf("normalized override = %q", dst["가나다에너지"])
	}
	if dst["acmesolar"] != LaneTeam3 {
		t.Fatalf("ASCII normalization = %q", dst["acmesolar"])
	}
	if dst["keep"] != LanePersonal {
		t.Fatalf("unrelated destination changed = %q", dst["keep"])
	}
	for _, invalid := range []string{"", "badlane", "caseinvalid"} {
		if _, ok := dst[invalid]; ok {
			t.Errorf("invalid company %q merged", invalid)
		}
	}
}

func TestMergeKeywordsNormalizesCaseAndDropsInvalid(t *testing.T) {
	t.Parallel()

	dst := map[string]Lane{
		"permit": LaneTeam1,
		"keep":   LanePersonal,
	}
	mergeKeywords(dst, map[string]string{
		" PERMIT ":   "team3",
		"Mixed CASE": "team2",
		" 한글 키워드 ":   "namdo",
		"":           "team1",
		" \t ":       "team1",
		"bad":        "team999",
		"bad-case":   "TEAM1",
	})
	if dst["permit"] != LaneTeam3 {
		t.Fatalf("keyword override = %q", dst["permit"])
	}
	if dst["mixed case"] != LaneTeam2 {
		t.Fatalf("keyword lowercase = %q", dst["mixed case"])
	}
	if dst["한글 키워드"] != LaneNamdo {
		t.Fatalf("keyword trim = %q", dst["한글 키워드"])
	}
	if dst["keep"] != LanePersonal {
		t.Fatalf("unrelated destination changed = %q", dst["keep"])
	}
	for _, invalid := range []string{"", "bad", "bad-case"} {
		if _, ok := dst[invalid]; ok {
			t.Errorf("invalid keyword %q merged", invalid)
		}
	}
}

func TestLoadFromFileJSONBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantError string
		check     func(*testing.T, Rules)
	}{
		{
			name: "empty object",
			body: `{}`,
			check: func(t *testing.T, got Rules) {
				if len(got.PersonToLane) != 0 || len(got.CompanyToLane) != 0 || got.KeywordToLane["루프탑"] != LaneTeam2 {
					t.Fatalf("empty object = %#v", got)
				}
			},
		},
		{
			name: "null object",
			body: `null`,
			check: func(t *testing.T, got Rules) {
				if got.KeywordToLane["인허가"] != LaneTeam1 {
					t.Fatalf("null lost defaults: %#v", got)
				}
			},
		},
		{
			name: "null maps",
			body: `{"personToLane":null,"companyToLane":null,"keywordToLane":null}`,
			check: func(t *testing.T, got Rules) {
				if got.KeywordToLane["발주"] != LaneTeam3 {
					t.Fatalf("null maps lost defaults: %#v", got)
				}
			},
		},
		{
			name: "unknown fields ignored",
			body: `{"unknown":true,"version":99,"personToLane":{"홍길동":"team1"}}`,
			check: func(t *testing.T, got Rules) {
				if got.PersonToLane["홍길동"] != LaneTeam1 {
					t.Fatalf("known field lost: %#v", got)
				}
			},
		},
		{
			name:      "empty document",
			body:      ``,
			wantError: "unexpected end of JSON input",
		},
		{
			name:      "whitespace document",
			body:      " \n\t ",
			wantError: "unexpected end of JSON input",
		},
		{
			name:      "array instead of object",
			body:      `[]`,
			wantError: "cannot unmarshal array",
		},
		{
			name:      "wrong map value type",
			body:      `{"personToLane":{"홍길동":1}}`,
			wantError: "cannot unmarshal number",
		},
		{
			name:      "trailing garbage",
			body:      `{} garbage`,
			wantError: "invalid character",
		},
		{
			name:      "two objects",
			body:      `{} {}`,
			wantError: "invalid character",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "rules.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := LoadFromFile(path)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantError)
				}
				if !strings.Contains(err.Error(), path) {
					t.Fatalf("error omits path %q: %v", path, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadFromFile: %v", err)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestLoadFromFileReadErrorIncludesPathAndCause(t *testing.T) {
	t.Parallel()

	path := t.TempDir() // reading a directory as a file is a stable error, even as root.
	got, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("LoadFromFile(directory) returned nil error")
	}
	if !strings.Contains(err.Error(), "classification: read rules") || !strings.Contains(err.Error(), path) {
		t.Fatalf("error lacks operation/path: %v", err)
	}
	if len(got.PersonToLane) != 0 || len(got.CompanyToLane) != 0 || got.KeywordToLane["루프탑"] != LaneTeam2 {
		t.Fatalf("error fallback rules = %#v", got)
	}
}

func TestLoadFromFileMissingParentAndDanglingSymlinkUseDefaults(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem boundary test")
	}
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing", "rules.json")
	got, err := LoadFromFile(missing)
	if err != nil {
		t.Fatalf("missing parent error = %v", err)
	}
	if got.KeywordToLane["루프탑"] != LaneTeam2 {
		t.Fatalf("missing parent defaults = %#v", got)
	}
	// The parent is missing, so the symlink result is irrelevant here; the
	// actual dangling-link boundary uses the valid parent created below.
	_ = os.Symlink(filepath.Join(t.TempDir(), "absent.json"), filepath.Join(filepath.Dir(missing), "link.json"))
	parent := t.TempDir()
	link := filepath.Join(parent, "rules-link.json")
	if err := os.Symlink(filepath.Join(parent, "does-not-exist.json"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	got, err = LoadFromFile(link)
	if err != nil {
		t.Fatalf("dangling symlink error = %v", err)
	}
	if got.KeywordToLane["인허가"] != LaneTeam1 {
		t.Fatalf("dangling link defaults = %#v", got)
	}
}

func TestResolveRulesPathTrimsOverrideAndLoadsIt(t *testing.T) {
	// Environment variables are process-global; keep this test serial.
	dir := t.TempDir()
	override := filepath.Join(dir, "operator-rules.json")
	t.Setenv(rulesEnvVar, "  "+override+" \t")
	if got := resolveRulesPath(); got != override {
		t.Fatalf("resolveRulesPath = %q, want %q", got, override)
	}
	if err := os.WriteFile(override, []byte(`{"personToLane":{"홍길동":"team2"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rules.PersonToLane["홍길동"] != LaneTeam2 {
		t.Fatalf("Load ignored override: %#v", rules.PersonToLane)
	}
}

func TestResolveRulesPathFallbackUsesStateDir(t *testing.T) {
	// Environment variables are process-global; keep this test serial.
	state := t.TempDir()
	t.Setenv(rulesEnvVar, " \t ")
	t.Setenv("DENEB_STATE_DIR", state)
	want := filepath.Join(config.ResolveStateDir(), rulesFileName)
	if got := resolveRulesPath(); got != want {
		t.Fatalf("resolveRulesPath = %q, want %q", got, want)
	}
}

func TestLoadedRulesMarshalOnlyThroughExplicitProjection(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "rules.json")
	body := `{
		"personToLane":{"홍길동 부장":"team1"},
		"companyToLane":{"ACME Solar":"team2"},
		"keywordToLane":{"PERMIT":"team3"}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	projection := rulesJSON{
		PersonToLane:  map[string]string{},
		CompanyToLane: map[string]string{},
		KeywordToLane: map[string]string{},
	}
	for key, lane := range rules.PersonToLane {
		projection.PersonToLane[key] = string(lane)
	}
	for key, lane := range rules.CompanyToLane {
		projection.CompanyToLane[key] = string(lane)
	}
	for key, lane := range rules.KeywordToLane {
		projection.KeywordToLane[key] = string(lane)
	}
	data, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || !utf8.Valid(data) {
		t.Fatalf("projection is not valid UTF-8 JSON: %q", data)
	}
	if !bytesContainAll(data, []string{"홍길동", "acmesolar", "permit", "team1", "team2", "team3"}) {
		t.Fatalf("projection missing normalized values: %s", data)
	}
}

func TestConcurrentClassificationIsRaceFreeAndStable(t *testing.T) {
	const (
		workers    = 64
		iterations = 1000
	)
	rules := Rules{
		PersonToLane: map[string]Lane{
			"홍길동": LaneTeam3,
			"김하늘": LaneNamdo,
		},
		CompanyToLane: map[string]Lane{
			"가나다에너지": LaneTeam2,
		},
		KeywordToLane: map[string]Lane{
			"인허가": LaneTeam1,
		},
	}
	tests := []struct {
		sig  Signals
		lane Lane
		conf Confidence
	}{
		{sig: Signals{People: []string{"홍길동 부장"}}, lane: LaneTeam3, conf: ConfStrong},
		{sig: Signals{People: []string{"김하늘"}, Text: "인허가"}, lane: LaneNamdo, conf: ConfStrong},
		{sig: Signals{Companies: []string{"(주) 가나다에너지"}}, lane: LaneTeam2, conf: ConfMedium},
		{sig: Signals{Text: "인허가 검토"}, lane: LaneTeam1, conf: ConfWeak},
		{sig: Signals{Text: "일반 공유"}, lane: LaneUnclassified, conf: ConfNone},
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				tc := tests[(worker+iteration)%len(tests)]
				lane, conf := rules.Classify(tc.sig)
				if lane != tc.lane || conf != tc.conf {
					t.Errorf("worker=%d iteration=%d got=(%q,%d) want=(%q,%d)", worker, iteration, lane, conf, tc.lane, tc.conf)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestClassificationTieBreakContractIgnoresMapIterationOrder(t *testing.T) {
	t.Parallel()

	lanes := []Lane{LaneTeam3, LanePersonal, LaneTeam2, LaneNamdo, LaneTeam1}
	for iteration := 0; iteration < 500; iteration++ {
		person := make(map[string]Lane)
		company := make(map[string]Lane)
		keyword := make(map[string]Lane)
		for offset := range lanes {
			lane := lanes[(iteration+offset)%len(lanes)]
			person[fmt.Sprintf("가짜이름%d", offset)] = lane
			company[fmt.Sprintf("company%d", offset)] = lane
			keyword[fmt.Sprintf("keyword%d", offset)] = lane
		}
		people := make([]string, 0, len(person))
		for name := range person {
			people = append(people, name)
		}
		companies := make([]string, 0, len(company))
		for name := range company {
			companies = append(companies, name)
		}
		keywords := make([]string, 0, len(keyword))
		for key := range keyword {
			keywords = append(keywords, key)
		}
		sort.Strings(keywords)
		rules := Rules{PersonToLane: person, CompanyToLane: company, KeywordToLane: keyword}
		if lane, conf := rules.Classify(Signals{People: people}); lane != LaneNamdo || conf != ConfStrong {
			t.Fatalf("iteration %d person tie = (%q,%d)", iteration, lane, conf)
		}
		if lane, conf := rules.Classify(Signals{Companies: companies}); lane != LaneNamdo || conf != ConfMedium {
			t.Fatalf("iteration %d company tie = (%q,%d)", iteration, lane, conf)
		}
		if lane, conf := rules.Classify(Signals{Text: strings.Join(keywords, " ")}); lane != LaneNamdo || conf != ConfWeak {
			t.Fatalf("iteration %d keyword tie = (%q,%d)", iteration, lane, conf)
		}
	}
}

func cloneRulesForTest(in Rules) Rules {
	out := Rules{
		PersonToLane:  make(map[string]Lane, len(in.PersonToLane)),
		CompanyToLane: make(map[string]Lane, len(in.CompanyToLane)),
		KeywordToLane: make(map[string]Lane, len(in.KeywordToLane)),
	}
	for key, lane := range in.PersonToLane {
		out.PersonToLane[key] = lane
	}
	for key, lane := range in.CompanyToLane {
		out.CompanyToLane[key] = lane
	}
	for key, lane := range in.KeywordToLane {
		out.KeywordToLane[key] = lane
	}
	return out
}

func bytesContainAll(data []byte, parts []string) bool {
	text := string(data)
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}

func (l Lane) StringForTest() string {
	return string(l)
}

func TestLoadFromFileErrorCanBeClassifiedByCaller(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	_, err := LoadFromFile(path)
	if err == nil {
		t.Fatal("expected read error")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("error %T does not preserve *os.PathError: %v", err, err)
	}
	if pathErr.Path != path {
		t.Fatalf("PathError.Path = %q, want %q", pathErr.Path, path)
	}
}
