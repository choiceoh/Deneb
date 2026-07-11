package testutil

import (
	"errors"
	"fmt"
	"testing"
)

func TestMustReturnsValue(t *testing.T) {
	if got := Must("value", nil); got != "value" {
		t.Fatalf("Must() = %q, want value", got)
	}
}

func TestMustPanicsWithCause(t *testing.T) {
	defer func() {
		got := fmt.Sprint(recover())
		if got != "testutil.Must: broken" {
			t.Fatalf("panic = %q", got)
		}
	}()
	Must(0, errors.New("broken"))
}

func TestNoErrorAcceptsNil(t *testing.T) {
	NoError(t, nil)
}
