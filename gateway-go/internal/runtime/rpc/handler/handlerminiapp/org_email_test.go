package handlerminiapp

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
)

// membersToWire must prefer the EMAIL join (robust across 동명이인) and fall back
// to the NAME match only when no enriched address resolves to a page.
func TestMembersToWirePrefersEmailMatchFallsBackToNameWhenUnresolved(t *testing.T) {
	members := []org.Member{{Name: "김성훈"}, {Name: "차남두"}, {Name: "미상"}}

	lookup := func(name string) (phones, emails []string) {
		switch name {
		case "김성훈":
			return nil, []string{"akim@bohae.co.kr"} // 동명이인 — resolved by email
		case "미상":
			return nil, []string{"nomatch@x.com"} // an address that resolves to nothing
		default:
			return nil, nil // 차남두 — no address → name fallback
		}
	}
	// Email join: only bohae resolves (to the module 김성훈, not the Marsh one).
	resolveByEmail := func(email string) string {
		if email == "akim@bohae.co.kr" {
			return "인물/김성훈-보해.md"
		}
		return ""
	}
	// Name join (fallback): 차남두 has a page; 미상 does not.
	personPaths := map[string]string{"차남두": "인물/차남두.md"}

	out := membersToWire(members, lookup, personPaths, resolveByEmail)

	if out[0].PersonPath != "인물/김성훈-보해.md" {
		t.Errorf("김성훈 PersonPath = %q, want the email-resolved 보해 page", out[0].PersonPath)
	}
	if out[1].PersonPath != "인물/차남두.md" {
		t.Errorf("차남두 PersonPath = %q, want the name-fallback page", out[1].PersonPath)
	}
	if out[2].PersonPath != "" {
		t.Errorf("미상 PersonPath = %q, want empty (no email match, no name page)", out[2].PersonPath)
	}
}
