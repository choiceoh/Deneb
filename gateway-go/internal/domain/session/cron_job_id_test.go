package session

import "testing"

// The agent adapter logs a "jobId" for cron postmortems. It used to print
// AgentTurnParams.AgentID — which agent to run as, not which job — and no
// production job sets agentId, so every line read jobId="". The session key,
// logged on the same line, always carries it.
func TestCronJobIDExtractsTheJobFromTheSessionKey(t *testing.T) {
	cases := map[string]string{
		"cron:morning-letter:1787796819407":      "morning-letter",
		"cron:weekly-ref-audit:1787347800105":    "weekly-ref-audit",
		"cron:email-analysis-full:1787731200105": "email-analysis-full",
		// Defensive: a key without the timestamp suffix still yields the id.
		"cron:seat-probe": "seat-probe",
		// Non-cron keys yield nothing rather than a wrong id.
		"client:main":                     "",
		"system:skill-review:client:main": "",
		"":                                "",
		"cron:":                           "",
	}
	for key, want := range cases {
		if got := CronJobID(key); got != want {
			t.Errorf("CronJobID(%q) = %q, want %q", key, got, want)
		}
	}
}

// A job id is a cron job name, so the LAST colon separates the timestamp — an
// id must survive even if a future key gains more segments before it.
func TestCronJobIDUsesTheLastSeparator(t *testing.T) {
	if got := CronJobID("cron:a:b:1787796819407"); got != "a:b" {
		t.Errorf("CronJobID = %q, want %q", got, "a:b")
	}
}
