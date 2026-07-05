package gmailpoll

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func anchorDomains(domains ...string) map[string]bool {
	out := map[string]bool{}
	for _, d := range domains {
		out[d] = true
	}
	return out
}

// The production failure case: a counterparty reply to our staff must label
// the sender external and our people as our side, and carry the reading rule.
func TestBuildPartyAnchor_CounterpartyReply(t *testing.T) {
	msg := &gmail.MessageDetail{
		From: "Taiwoo Yoo <taiwoo.yoo@hre-korea.com>",
		To:   "TopSolar - Mandyu <mandyu@topsolar.kr>",
		CC:   "오선택 <choiceoh@topsolar.kr>, JinHyung Kwon <jinhyung.kwon@hre-korea.com>",
	}
	got := buildPartyAnchor(msg, anchorDomains("topsolar.kr"))

	for _, want := range []string{
		"## 당사자 앵커",
		"보낸사람: Taiwoo Yoo <taiwoo.yoo@hre-korea.com> — 외부(hre-korea.com)",
		"받는사람: TopSolar - Mandyu <mandyu@topsolar.kr> — 우리 측(topsolar.kr)",
		"참조: 오선택 <choiceoh@topsolar.kr> — 우리 측(topsolar.kr)",
		"참조: JinHyung Kwon <jinhyung.kwon@hre-korea.com> — 외부(hre-korea.com)",
		"판독 규칙",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("anchor missing %q:\n%s", want, got)
		}
	}
}

// Korean business headers that violate RFC 5322 (unquoted names with '/')
// must fall back to tolerant parsing instead of dropping the whole list.
func TestBuildPartyAnchor_MalformedKoreanHeaderFallsBack(t *testing.T) {
	msg := &gmail.MessageDetail{
		From: "kelly@risen.com",
		CC:   "오선택전무/탑솔라 <choiceoh@topsolar.kr>,김세미과장/탑솔라 <kkssmm9358@topsolar.kr>",
	}
	got := buildPartyAnchor(msg, anchorDomains("topsolar.kr"))

	if !strings.Contains(got, "보낸사람: kelly@risen.com <kelly@risen.com> — 외부(risen.com)") {
		t.Errorf("bare sender address mishandled:\n%s", got)
	}
	if !strings.Contains(got, "오선택전무/탑솔라 <choiceoh@topsolar.kr> — 우리 측(topsolar.kr)") {
		t.Errorf("malformed CC entry lost:\n%s", got)
	}
	if !strings.Contains(got, "김세미과장/탑솔라 <kkssmm9358@topsolar.kr> — 우리 측(topsolar.kr)") {
		t.Errorf("second malformed CC entry lost:\n%s", got)
	}
}

// Our own outgoing mail (sender = our side) must still anchor — that is the
// case where models drifted into the counterparty's perspective.
func TestBuildPartyAnchor_OurOutgoingMail(t *testing.T) {
	msg := &gmail.MessageDetail{
		From: "고건 <taygun152@topsolar.kr>",
		To:   "KJ구조기술사사무소 <kj2390@hanmail.net>",
	}
	got := buildPartyAnchor(msg, anchorDomains("topsolar.kr"))
	if !strings.Contains(got, "보낸사람: 고건 <taygun152@topsolar.kr> — 우리 측(topsolar.kr)") {
		t.Errorf("our sender not labeled our side:\n%s", got)
	}
	if !strings.Contains(got, "외부(hanmail.net)") {
		t.Errorf("external recipient not labeled:\n%s", got)
	}
}

func TestBuildPartyAnchor_NoAddressesReturnsEmpty(t *testing.T) {
	if got := buildPartyAnchor(&gmail.MessageDetail{}, anchorDomains("topsolar.kr")); got != "" {
		t.Errorf("empty message produced anchor: %q", got)
	}
	if got := buildPartyAnchor(nil, anchorDomains("topsolar.kr")); got != "" {
		t.Errorf("nil message produced anchor: %q", got)
	}
}

func TestOurAnchorDomains_EnvOverride(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "example.co.kr, Sub.Example.com")
	got := ourAnchorDomains()
	if !got["example.co.kr"] || !got["sub.example.com"] {
		t.Fatalf("env domains not parsed: %v", got)
	}
	if got[defaultOurDomain] {
		t.Fatalf("default domain must not leak in when env is set: %v", got)
	}

	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "")
	got = ourAnchorDomains()
	if !got[defaultOurDomain] {
		t.Fatalf("default domain missing without env: %v", got)
	}
}
