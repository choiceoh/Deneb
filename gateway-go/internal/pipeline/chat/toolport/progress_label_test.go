package toolport

import "testing"

func TestChatProgressLabelCoversKnownPhasesAndFallsBack(t *testing.T) {
	t.Parallel()
	if got := ChatProgressLabel("working"); got != "도구로 필요한 내용을 확인하고 있습니다" {
		t.Errorf("working = %q", got)
	}
	if got := ChatProgressLabel("writing"); got != "답변을 작성하고 있습니다" {
		t.Errorf("writing = %q", got)
	}
	// A phase this client build has never heard of must still read as prose.
	if got := ChatProgressLabel("quantum_reticulating"); got != "응답을 준비하고 있습니다" {
		t.Errorf("unknown = %q", got)
	}
}
