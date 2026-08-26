package subagent

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

// The batch header tells the parent to "synthesize the result below". A done
// child that left no text produced no Result line at all, so the parent's only
// exits were inventing a summary or dropping the child's work silently.
//
// Not hypothetical: LastOutput can be unset, and the transcript fallback also
// returns "" for a child whose whole run was tool calls with no prose.
func TestNotifyItemNamesAMissingResult(t *testing.T) {
	var sb strings.Builder
	writeNotifyItem(&sb, notifyItem{
		label:  "조사",
		status: session.StatusDone,
	})
	out := sb.String()
	if !strings.Contains(out, "- Result:") {
		t.Errorf("a done child with no output produced no Result line:\n%s", out)
	}
	if !strings.Contains(out, "없음") || !strings.Contains(out, "지어내지 말고") {
		t.Errorf("the notice does not tell the parent what to do:\n%s", out)
	}
}

// A child that DID produce output is unchanged — the notice must not ride along.
func TestNotifyItemKeepsRealResultsUntouched(t *testing.T) {
	var sb strings.Builder
	writeNotifyItem(&sb, notifyItem{
		label:      "조사",
		status:     session.StatusDone,
		lastOutput: "탑솔라 관련 위키 3건을 찾았다.",
	})
	out := sb.String()
	if !strings.Contains(out, "탑솔라 관련 위키 3건") {
		t.Errorf("real result lost:\n%s", out)
	}
	if strings.Contains(out, "없음 —") {
		t.Errorf("missing-result notice fired for a child that had output:\n%s", out)
	}
}

// A failure already carries its own Failure line and a status-aware header; the
// missing-output notice would only repeat what the parent was told.
func TestNotifyItemDoesNotAddTheNoticeToFailures(t *testing.T) {
	for _, status := range []session.RunStatus{session.StatusFailed, session.StatusKilled, session.StatusTimeout} {
		var sb strings.Builder
		writeNotifyItem(&sb, notifyItem{
			label:         "조사",
			status:        status,
			failureReason: "타임아웃",
		})
		out := sb.String()
		if strings.Contains(out, "없음 —") {
			t.Errorf("%s: missing-result notice duplicated the failure line:\n%s", status, out)
		}
		if !strings.Contains(out, "타임아웃") {
			t.Errorf("%s: failure reason lost:\n%s", status, out)
		}
	}
}
