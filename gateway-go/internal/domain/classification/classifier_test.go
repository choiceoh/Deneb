package classification

// All test data here is FAKE — invented names/companies, never the real roster.
// The real roster lives in the operator's {stateDir}/classification_rules.json,
// which is not in the repo. (Privacy invariant for this package.)

import "testing"

// fakeRules builds a small ruleset from invented names for the classifier tests.
func fakeRules() Rules {
	return Rules{
		PersonToLane: map[string]Lane{
			"홍길동": LaneTeam1,
			"이영희": LaneTeam2,
			"임꺽정": LaneTeam3,
			"최지우": LaneNamdo,
		},
		CompanyToLane: map[string]Lane{
			"가나에너지": LaneNamdo,
			"마바솔라":  LaneTeam2,
		},
		KeywordToLane: map[string]Lane{
			"루프탑": LaneTeam2,
			"인허가": LaneTeam1,
			"케이블": LaneNamdo,
		},
	}
}

func TestClassify_PersonMatch(t *testing.T) {
	r := fakeRules()
	// Attendee name carries an honorific; NormalizePersonName should peel it so
	// "홍길동 부장" still matches the "홍길동" rule.
	lane, conf := r.Classify(Signals{People: []string{"홍길동 부장"}})
	if lane != LaneTeam1 {
		t.Fatalf("person match: lane = %q, want team1", lane)
	}
	if conf != ConfStrong {
		t.Fatalf("person match: confidence = %d, want ConfStrong(%d)", conf, ConfStrong)
	}
}

func TestClassify_PersonWithParentheticalAffiliation(t *testing.T) {
	r := fakeRules()
	// "이영희(마바솔라)" should normalize to "이영희" and match the person rule.
	lane, conf := r.Classify(Signals{People: []string{"이영희(마바솔라)"}})
	if lane != LaneTeam2 || conf != ConfStrong {
		t.Fatalf("person+affiliation: got (%q, %d), want (team2, ConfStrong)", lane, conf)
	}
}

func TestClassify_CompanyMatch(t *testing.T) {
	r := fakeRules()
	// No known person, but a known company → medium confidence.
	lane, conf := r.Classify(Signals{
		People:    []string{"알수없는사람"},
		Companies: []string{"가나에너지"},
	})
	if lane != LaneNamdo {
		t.Fatalf("company match: lane = %q, want namdo", lane)
	}
	if conf != ConfMedium {
		t.Fatalf("company match: confidence = %d, want ConfMedium(%d)", conf, ConfMedium)
	}
}

func TestClassify_CompanySubstringBothDirections(t *testing.T) {
	r := fakeRules()
	// Rule key "가나에너지" should match a decorated firm string.
	if lane, _ := r.Classify(Signals{Companies: []string{"가나에너지(주)"}}); lane != LaneNamdo {
		t.Fatalf("company substring (rule in string): lane = %q, want namdo", lane)
	}
	// Rule key "마바솔라" should match the bare-prefix firm "마바솔라에너지".
	if lane, _ := r.Classify(Signals{Companies: []string{"마바솔라에너지"}}); lane != LaneTeam2 {
		t.Fatalf("company substring (string extends rule): lane = %q, want team2", lane)
	}
}

func TestClassify_KeywordMatch(t *testing.T) {
	r := fakeRules()
	// No person/company, but the title mentions a domain keyword (substring).
	lane, conf := r.Classify(Signals{Text: "옥상 루프탑 발전 점검 일정"})
	if lane != LaneTeam2 {
		t.Fatalf("keyword match: lane = %q, want team2", lane)
	}
	if conf != ConfWeak {
		t.Fatalf("keyword match: confidence = %d, want ConfWeak(%d)", conf, ConfWeak)
	}
}

func TestClassify_PriorityPersonOverCompanyOverKeyword(t *testing.T) {
	r := fakeRules()
	// All three signals present but pointing at DIFFERENT lanes: person (team1)
	// must win over company (namdo) and keyword (team2). This is the core
	// strong→weak precedence guarantee.
	lane, conf := r.Classify(Signals{
		People:    []string{"홍길동"},   // team1
		Companies: []string{"가나에너지"}, // namdo
		Text:      "루프탑 케이블 작업",      // team2 / namdo keywords
	})
	if lane != LaneTeam1 || conf != ConfStrong {
		t.Fatalf("priority: got (%q, %d), want (team1, ConfStrong)", lane, conf)
	}
}

func TestClassify_CompanyOverKeyword(t *testing.T) {
	r := fakeRules()
	// No known person; company (namdo) must win over a keyword (team2).
	lane, conf := r.Classify(Signals{
		Companies: []string{"가나에너지"}, // namdo
		Text:      "루프탑 설치",          // team2 keyword
	})
	if lane != LaneNamdo || conf != ConfMedium {
		t.Fatalf("company>keyword: got (%q, %d), want (namdo, ConfMedium)", lane, conf)
	}
}

func TestClassify_Unclassified(t *testing.T) {
	r := fakeRules()
	// Nothing matches → holding lane, no confidence.
	lane, conf := r.Classify(Signals{
		People:    []string{"모르는사람"},
		Companies: []string{"없는회사"},
		Text:      "그냥 일반적인 회의",
	})
	if lane != LaneUnclassified {
		t.Fatalf("unclassified: lane = %q, want unclassified", lane)
	}
	if conf != ConfNone {
		t.Fatalf("unclassified: confidence = %d, want ConfNone(%d)", conf, ConfNone)
	}
}

func TestClassify_EmptyRulesAlwaysUnclassified(t *testing.T) {
	var empty Rules // zero value — all nil maps
	lane, conf := empty.Classify(Signals{
		People:    []string{"홍길동"},
		Companies: []string{"가나에너지"},
		Text:      "루프탑 인허가",
	})
	if lane != LaneUnclassified || conf != ConfNone {
		t.Fatalf("empty rules: got (%q, %d), want (unclassified, ConfNone)", lane, conf)
	}
}

func TestClassify_ShortNameIgnored(t *testing.T) {
	r := Rules{PersonToLane: map[string]Lane{"김": LaneTeam1}}
	// A 1-rune name is too ambiguous; matchPerson skips names < 2 runes, so even
	// though the (degenerate) rule exists, a 1-char attendee never matches.
	if lane, _ := r.Classify(Signals{People: []string{"김"}}); lane != LaneUnclassified {
		t.Fatalf("short name: lane = %q, want unclassified", lane)
	}
}

func TestClassify_MultiLaneDeterministicTieBreak(t *testing.T) {
	r := Rules{PersonToLane: map[string]Lane{
		"홍길동": LaneTeam3, // "team3"
		"이영희": LaneNamdo, // "namdo"
	}}
	// Two attendees map to different lanes; pickLane returns the lexicographically
	// smallest lane key ("namdo" < "team3") regardless of input order, so the
	// result is stable across reloads.
	want := LaneNamdo
	for _, order := range [][]string{
		{"홍길동", "이영희"},
		{"이영희", "홍길동"},
	} {
		if lane, _ := r.Classify(Signals{People: order}); lane != want {
			t.Fatalf("tie-break order=%v: lane = %q, want %q", order, lane, want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[Lane]string{
		LaneTeam1:        "기획조정실 1팀",
		LaneNamdo:        "남도에코",
		LanePersonal:     "개인/기타",
		LaneUnclassified: "미분류",
		Lane("future"):   "future", // unknown lane falls back to its key
	}
	for lane, want := range cases {
		if got := DisplayName(lane); got != want {
			t.Errorf("DisplayName(%q) = %q, want %q", lane, got, want)
		}
	}
}
