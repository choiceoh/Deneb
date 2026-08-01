package genesis

import (
	"strings"
	"testing"
)

func toolCase(tools ...string) SkillValidationCaseRecord {
	return SkillValidationCaseRecord{Replay: SkillReplayCaseRecord{RequiredTools: tools}}
}

func TestPreflightRejectsDroppedToolMention(t *testing.T) {
	// The largest quality bucket in the evolve ledger, caught before a producer
	// rewrite and a judge call are spent on it.
	cases := []SkillValidationCaseRecord{toolCase("mail_archive")}
	ok, reason := requiredToolPreflight(cases, "1. call mail_archive first\n", "1. summarise the thread\n")
	if ok {
		t.Fatal("dropping a contracted tool must fail preflight")
	}
	if !strings.Contains(reason, "mail_archive") {
		t.Errorf("the reason must name the tool so the retry can act: %q", reason)
	}
}

func TestPreflightPassesWhenToolSurvives(t *testing.T) {
	cases := []SkillValidationCaseRecord{toolCase("mail_archive")}
	if ok, reason := requiredToolPreflight(cases, "call mail_archive", "first call mail_archive, then summarise"); !ok {
		t.Errorf("a rewrite that keeps the tool must pass: %q", reason)
	}
}

func TestPreflightIgnoresToolsTheOriginalNeverDocumented(t *testing.T) {
	// A case can name a tool the skill never mentioned (2026-07-21 weekly-report
	// was exactly that). Enforcing it would block every future evolve on a
	// defect in the CASE rather than in the candidate.
	cases := []SkillValidationCaseRecord{toolCase("no_reply")}
	if ok, reason := requiredToolPreflight(cases, "summarise the week", "summarise the week concisely"); !ok {
		t.Errorf("a tool absent from the original is not the candidate's fault: %q", reason)
	}
}

func TestPreflightIsCaseInsensitiveAndDeduped(t *testing.T) {
	cases := []SkillValidationCaseRecord{toolCase("Mail_Archive", "mail_archive", " ")}
	ok, reason := requiredToolPreflight(cases, "uses MAIL_ARCHIVE", "uses nothing")
	if ok {
		t.Fatal("case differences must not let a drop through")
	}
	// Reported once, in the casing the case declared.
	if strings.Count(strings.ToLower(reason), "mail_archive") != 1 {
		t.Errorf("the same tool must be reported once: %q", reason)
	}
}

func TestPreflightNoCasesIsNotARejection(t *testing.T) {
	// No contract means nothing to preserve; the later gates still apply.
	if ok, _ := requiredToolPreflight(nil, "anything", "anything else"); !ok {
		t.Error("an empty corpus must not reject")
	}
}
