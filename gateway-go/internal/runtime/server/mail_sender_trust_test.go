package server

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

func TestMailSenderTrustDecisionDeterministicAllowlist(t *testing.T) {
	t.Setenv("DENEB_MAIL_OUR_DOMAINS", "ours.test")
	t.Setenv(trustedSendersEnv, "person@allowed.test")
	t.Setenv(trustedDomainsEnv, "partner.test")
	var server *Server

	for _, from := range []string{
		"Staff <staff@ours.test>",
		"Allowed <PERSON@ALLOWED.TEST>",
		"Vendor <sales@partner.test>",
	} {
		if got := server.mailSenderTrustDecision(&gmail.MessageDetail{From: from}); got.Disposition != mailanalysis.SenderTrusted {
			t.Errorf("%q decision = %+v", from, got)
		}
	}

	unknown := server.mailSenderTrustDecision(&gmail.MessageDetail{From: "New <new@unknown.test>"})
	if unknown.Disposition != mailanalysis.SenderReview || unknown.Reason != "미확인 발신자: new@unknown.test" {
		t.Fatalf("unknown decision = %+v", unknown)
	}
	missing := server.mailSenderTrustDecision(&gmail.MessageDetail{From: "Display Name Only"})
	if missing.Disposition != mailanalysis.SenderReview || missing.Reason == "" {
		t.Fatalf("missing address decision = %+v", missing)
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
