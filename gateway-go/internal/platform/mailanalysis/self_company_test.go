package mailanalysis

import "testing"

func TestCompanyMatchKey(t *testing.T) {
	cases := map[string]string{
		"탑솔라":               "탑솔라",
		"탑솔라(주)":            "탑솔라",
		"탑솔라㈜":              "탑솔라",
		"  탑솔라 주식회사  ":      "탑솔라",
		"TOPSOLAR CO.,LTD":  "topsolar",
		"TOPSOLAR CO.,LTD.": "topsolar",
		"TopSolar":          "topsolar",
		"무림피앤피":             "무림피앤피",
		"현대차(주)":            "현대차",
		"현대自動車":             "현대自動車", // CJK ideographs pass through
		"":                  "",
		"   ":               "",
	}
	for in, want := range cases {
		if got := companyMatchKey(in); got != want {
			t.Errorf("companyMatchKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSelfCounterparty(t *testing.T) {
	// Pin to the deployment defaults so the domain-derived romanized stem
	// (topsolar) and the Korean default (탑솔라) are both deterministic
	// regardless of the ambient environment.
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "topsolar.kr")
	t.Setenv("DENEB_MAIL_OUR_NAMES", "탑솔라")

	// Every form the 2026-07-07 sweep saw (거래/탑솔라-주.md, 거래/탑솔라㈜.md) plus
	// the romanized/domain-carrying variants the tiny model emits.
	self := []string{
		"탑솔라", "탑솔라(주)", "탑솔라㈜", "탑솔라 주식회사",
		"TOPSOLAR CO.,LTD", "TopSolar", "topsolar.kr",
		"영업팀 <sales@topsolar.kr>", // domain parenthetical/address the model sometimes echoes
	}
	for _, name := range self {
		if !isSelfCounterparty(name) {
			t.Errorf("isSelfCounterparty(%q) = false, want true (self)", name)
		}
	}

	// The real counterparties whose documents were mis-filed under a self-ledger
	// must pass through as external. 탑솔라파트너스 shares a prefix but is a
	// distinct firm — equality (not substring) must not over-block it.
	external := []string{"무림피앤피", "인하공전", "현대차", "현대자동차(주)", "마바솔라", "탑솔라파트너스"}
	for _, name := range external {
		if isSelfCounterparty(name) {
			t.Errorf("isSelfCounterparty(%q) = true, want false (external)", name)
		}
	}
}

func TestOurCompanyNames_EnvOverride(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "topsolar.kr")
	t.Setenv("DENEB_MAIL_OUR_NAMES", "탑솔라, 탑쏠라")
	got := ourCompanyNames(ourAnchorDomains())
	for _, k := range []string{"탑솔라", "탑쏠라", "topsolar"} {
		if !got[k] {
			t.Errorf("expected self-name key %q in %v", k, got)
		}
	}

	// Unset → the const default (탑솔라) plus the domain-derived stem remain.
	t.Setenv("DENEB_MAIL_OUR_NAMES", "")
	got = ourCompanyNames(ourAnchorDomains())
	if !got["탑솔라"] || !got["topsolar"] {
		t.Errorf("default self-names missing without env: %v", got)
	}
}
