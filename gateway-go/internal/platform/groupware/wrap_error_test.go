package groupware

import (
	"errors"
	"strings"
	"testing"
)

// wrapGroupwareRunError must keep the reader's stdout/stderr diagnostic on the
// error so radar logs surface WHY an approval read failed instead of a bare
// "exit status 1", while preserving the underlying cause for errors.Is.
func TestWrapGroupwareRunError(t *testing.T) {
	base := errors.New("exit status 1")

	// Non-empty diagnostic is prepended AND the cause is still unwrappable.
	got := wrapGroupwareRunError("그룹웨어 읽기 실패 (approval/read): 세션이 만료되었습니다", base)
	if got == nil || !strings.Contains(got.Error(), "세션이 만료되었습니다") {
		t.Fatalf("diagnostic dropped: %v", got)
	}
	if !errors.Is(got, base) {
		t.Fatalf("wrapped error must keep the underlying cause for errors.Is")
	}

	// Empty diagnostic → the bare error, unchanged (no useless wrapping).
	if got := wrapGroupwareRunError("   ", base); got != base {
		t.Fatalf("blank out should return the bare error, got %v", got)
	}

	// nil error → nil.
	if got := wrapGroupwareRunError("x", nil); got != nil {
		t.Fatalf("nil error should stay nil, got %v", got)
	}
}
