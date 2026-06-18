package tools

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPhoneSSHTransportFailureClassifier(t *testing.T) {
	if !isPhoneSSHTransportFailure("ssh: connect to host 100.93.163.49 port 8022: Connection refused", errors.New("exit status 255")) {
		t.Fatal("connection refused should be classified as an ssh transport failure")
	}
	if isPhoneSSHTransportFailure("termux-location: permission denied by Android", errors.New("exit status 1")) {
		t.Fatal("remote termux command failure should not be cached as an ssh transport failure")
	}
}

func TestPhoneSSHFailureBackoff(t *testing.T) {
	target := []string{"phone"}
	clearPhoneSSHFailure(target)
	t.Cleanup(func() { clearPhoneSSHFailure(target) })

	recordPhoneSSHFailure(target, errors.New("phone ssh failed: connection refused"))
	err := activePhoneSSHFailure(target)
	if err == nil {
		t.Fatal("expected active phone ssh failure")
	}
	if !strings.Contains(err.Error(), "retry after") || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("unexpected backoff error: %v", err)
	}

	phoneSSHFailures.Lock()
	phoneSSHFailures.until = time.Now().Add(-time.Second)
	phoneSSHFailures.Unlock()
	if err := activePhoneSSHFailure(target); err != nil {
		t.Fatalf("expired phone ssh failure should not block: %v", err)
	}
}
