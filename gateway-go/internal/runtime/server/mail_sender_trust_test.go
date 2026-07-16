package server

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

func TestMailSenderTrustDecisionDefaultAnalyzesUnknownBusiness(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "ours.test")
	t.Setenv(trustedSendersEnv, "person@allowed.test")
	t.Setenv(trustedDomainsEnv, "partner.test")
	var server *Server

	for _, from := range []string{
		"Staff <staff@ours.test>",
		"Allowed <PERSON@ALLOWED.TEST>",
		"Vendor <sales@partner.test>",
		// Unknown counterparty — must still auto-analyze.
		"New <new@unknown.test>",
		"황세호 <sayho@kia.com>",
	} {
		if got := server.mailSenderTrustDecision(&gmail.MessageDetail{From: from, Subject: "견적 요청"}); got.Disposition != mailanalysis.SenderTrusted {
			t.Errorf("%q decision = %+v", from, got)
		}
	}

	missing := server.mailSenderTrustDecision(&gmail.MessageDetail{From: "Display Name Only", Subject: "hello"})
	if missing.Disposition != mailanalysis.SenderTrusted {
		t.Fatalf("missing address must default to analyze = %+v", missing)
	}
}

func TestMailSenderTrustDecisionReviewsOnlyBulkNoise(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "ours.test")
	t.Setenv(trustedSendersEnv, "")
	t.Setenv(trustedDomainsEnv, "")
	var server *Server

	cases := []struct {
		from, subject string
		labels        []string
	}{
		{from: "News <newsletter@vendor.test>", subject: "이번 주 업데이트"},
		{from: "Bot <no-reply@saas.test>", subject: "비밀번호 재설정"},
		{from: "Ads <ads@vendor.test>", subject: "[광고] 여름 세일"},
		{from: "Promo <hello@vendor.test>", subject: "혜택 안내", labels: []string{"CATEGORY_PROMOTIONS"}},
		{from: "Spam <x@y.test>", subject: "win", labels: []string{"SPAM"}},
	}
	for _, tc := range cases {
		got := server.mailSenderTrustDecision(&gmail.MessageDetail{
			From: tc.from, Subject: tc.subject, Labels: tc.labels,
		})
		if got.Disposition != mailanalysis.SenderReview || got.Reason == "" {
			t.Fatalf("noise %q / %q = %+v", tc.from, tc.subject, got)
		}
	}
}

func TestMailSenderTrustDecisionExplicitTrustOverridesNoise(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "ours.test")
	t.Setenv(trustedSendersEnv, "")
	t.Setenv(trustedDomainsEnv, "")
	store, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceAll([]contacts.Contact{{
		Name:   "Vendor bot",
		Emails: []string{"no-reply@partner.test"},
	}}); err != nil {
		t.Fatal(err)
	}
	server := &Server{MemorySubsystem: &MemorySubsystem{contactsStore: store}}
	got := server.mailSenderTrustDecision(&gmail.MessageDetail{
		From: "Vendor <no-reply@partner.test>", Subject: "주문 확인",
	})
	if got.Disposition != mailanalysis.SenderTrusted {
		t.Fatalf("contacts exact email must override noise = %+v", got)
	}
}

func TestMailSenderTrustDecisionAcceptsExactWikiPerson(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "ours.test")
	t.Setenv(trustedSendersEnv, "")
	t.Setenv(trustedDomainsEnv, "")
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person := wiki.NewPage("김성훈", "인물", nil)
	person.Meta.Emails = []string{"known@counterparty.test"}
	if err := store.WritePage("인물/김성훈.md", person); err != nil {
		t.Fatal(err)
	}

	server := &Server{MemorySubsystem: &MemorySubsystem{wikiStore: store}}
	got := server.mailSenderTrustDecision(&gmail.MessageDetail{From: "김성훈 <KNOWN@COUNTERPARTY.TEST>"})
	if got.Disposition != mailanalysis.SenderTrusted {
		t.Fatalf("wiki person decision = %+v", got)
	}
}

func TestMailSenderTrustHelpers(t *testing.T) {
	if got := emailDomain("Person@Example.COM "); got != "example.com" {
		t.Fatalf("domain = %q", got)
	}
	if got := splitEnvSet(" A@example.com, ,B.TEST "); len(got) != 2 || got[0] != "a@example.com" || got[1] != "b.test" {
		t.Fatalf("env set = %#v", got)
	}
	if emailDomain("not-an-address") != "" || stringSetContains(nil, "x") {
		t.Fatal("empty helper contract")
	}
}
