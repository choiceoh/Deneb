package wiki

import (
	"strings"
	"testing"

	contactdomain "github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
)

// TestMergeContactsByName_KeepsTwoEmployersApart: contacts sync used to union
// phones and emails by name alone, so 동명이인 became one page carrying both
// companies' numbers (인물/김성환.md held topsolar.kr and bmenergy.co.kr) — that
// is how a call gets routed to the wrong person. A shared phone still means one
// person who changed jobs, and those merge.
func TestMergeContactsByName_KeepsTwoEmployersApart(t *testing.T) {
	merged := mergeContactsByName([]contactdomain.Contact{
		{Name: "김성환", Org: "탑솔라", Emails: []string{"upshgo@topsolar.kr"}, Phones: []string{"010-1111-2222"}},
		{Name: "김성환", Org: "BM에너지", Emails: []string{"shkim@bmenergy.co.kr"}, Phones: []string{"02-412-2721"}},
		// One person who moved companies: the mobile follows them.
		{Name: "조동욱", Org: "KPMG", Emails: []string{"cho@kr.kpmg.com"}, Phones: []string{"010-3333-4444"}},
		{Name: "조동욱", Org: "아르고에너지", Emails: []string{"cho@argo.co.kr"}, Phones: []string{"010-3333-4444"}},
	})

	homonym := merged[contactdomain.NormalizePersonName("김성환")]
	if homonym == nil {
		t.Fatal("김성환 dropped entirely")
	}
	if len(homonym.Emails) != 1 || strings.Contains(strings.Join(homonym.Emails, ","), "bmenergy") {
		t.Errorf("homonym emails merged: %v", homonym.Emails)
	}
	if strings.Contains(strings.Join(homonym.Phones, ","), "02-412-2721") {
		t.Errorf("homonym phones merged: %v", homonym.Phones)
	}

	moved := merged[contactdomain.NormalizePersonName("조동욱")]
	if moved == nil || len(moved.Emails) != 2 {
		t.Errorf("job change should keep both addresses: %+v", moved)
	}
}

// TestDetectHomonymPersonPages_FlagsTwoEmployersOnOnePage: pages merged before
// the guards still carry both identities, and a merged node answers "그 사람
// 연락처" with the wrong company's number. Detection only — splitting is the
// operator's call.
func TestDetectHomonymPersonPages_FlagsTwoEmployersOnOnePage(t *testing.T) {
	s, wd := newVerifyStore(t)
	writePageT(t, s, "인물/김성환.md", "김성환", "인물",
		"## 연락처\n\n- 이메일: upshgo@topsolar.kr, shkim@bmenergy.co.kr\n")
	writePageT(t, s, "인물/백창선.md", "백창선", "인물",
		"## 연락처\n\n- 이메일: baek@topsolar.kr\n- 개인: baek@gmail.com\n")

	var flagged []string
	for _, f := range wd.detectHomonymPersonPages() {
		flagged = append(flagged, f.PageA)
		if f.Fix != nil {
			t.Errorf("homonym finding carries a Fix — splitting must stay manual: %+v", f.Fix)
		}
	}
	if len(flagged) != 1 || flagged[0] != "인물/김성환.md" {
		t.Errorf("flagged = %v, want just 인물/김성환.md (freemail must not count)", flagged)
	}
}
