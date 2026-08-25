package subagent

import "testing"

// The queue is filled from the terminal status event, which fires before
// handleRunSuccess stores session.LastOutput. Without a re-read at flush time
// the parent is told "Synthesize the result below" with nothing below it
// (observed 2026-08-26).
func TestFormatBatchNotificationCarriesTheChildOutput(t *testing.T) {
	out := formatBatchNotification([]notifyItem{{
		childKey:   "client:sub:probe",
		label:      "probe",
		status:     "done",
		lastOutput: "CHILD-REPORT-XYZ",
	}})

	if !contains(out, "CHILD-REPORT-XYZ") {
		t.Fatalf("the parent must receive the child's report:\n%s", out)
	}
	if !contains(out, "Synthesize the result below") {
		t.Fatalf("instruction lost:\n%s", out)
	}
}

func TestEmptyOutputStillRendersTheEnvelope(t *testing.T) {
	out := formatBatchNotification([]notifyItem{{label: "probe", status: "failed", failureReason: "timeout"}})

	if !contains(out, "timeout") {
		t.Fatalf("a failure must still reach the parent:\n%s", out)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}()
}
