package subagent

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// The notification told every parent "the user is waiting for this answer".
// For a cron or system parent nobody is waiting — the claim is simply false,
// and a false statement in a system instruction is the kind the model has no
// way to check.
func TestNotificationVoiceMatchesTheParentKind(t *testing.T) {
	items := []notifyItem{{label: "조사", status: session.StatusDone, lastOutput: "결과"}}

	userFacing := formatBatchNotificationFor("client:main", items)
	if !strings.Contains(userFacing, "the user is waiting") {
		t.Errorf("a user-facing parent lost the waiting-user reason:\n%s", userFacing)
	}

	for _, parent := range []string{"cron:morning-letter:1", "system:skill-review:client:main"} {
		out := formatBatchNotificationFor(parent, items)
		if strings.Contains(out, "the user is waiting") {
			t.Errorf("%s: told an autonomous parent a user is waiting:\n%s", parent, out)
		}
		// The instruction itself must survive — NO_REPLY suppresses delivery,
		// so a cron job that reports would silently drop its children's work.
		if !strings.Contains(out, "NO_REPLY") {
			t.Errorf("%s: dropped the NO_REPLY prohibition:\n%s", parent, out)
		}
		if !strings.Contains(out, "산출물") {
			t.Errorf("%s: did not give the autonomous reason:\n%s", parent, out)
		}
	}
}

// A client-spawned sub-agent is still user-facing: the chain ends at a person.
func TestNotificationTreatsSubAgentParentsAsUserFacing(t *testing.T) {
	out := formatBatchNotificationFor("client:sub:1787722337766",
		[]notifyItem{{label: "조사", status: session.StatusDone, lastOutput: "결과"}})
	if !strings.Contains(out, "the user is waiting") {
		t.Errorf("a client-spawned sub-agent parent was treated as autonomous:\n%s", out)
	}
}

// Every header shape carries the clause — failures, mixed batches and the
// all-succeeded batch alike.
func TestNotificationVoiceAppliesToEveryHeaderShape(t *testing.T) {
	cases := map[string][]notifyItem{
		"single failure": {{label: "a", status: session.StatusFailed, failureReason: "timeout"}},
		"all failed":     {{label: "a", status: session.StatusFailed}, {label: "b", status: session.StatusKilled}},
		"mixed":          {{label: "a", status: session.StatusFailed}, {label: "b", status: session.StatusDone, lastOutput: "ok"}},
		"all done":       {{label: "a", status: session.StatusDone, lastOutput: "x"}, {label: "b", status: session.StatusDone, lastOutput: "y"}},
	}
	for name, items := range cases {
		out := formatBatchNotificationFor("cron:email-analysis:1", items)
		if strings.Contains(out, "the user is waiting") {
			t.Errorf("%s: autonomous parent got the waiting-user claim:\n%s", name, out)
		}
		if !strings.Contains(out, "NO_REPLY") {
			t.Errorf("%s: lost the NO_REPLY prohibition:\n%s", name, out)
		}
	}
}
