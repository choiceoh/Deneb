package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func TestWritePairingOutputKeepsSecretOffStderr(t *testing.T) {
	const token = "private-token"
	var stdout, stderr bytes.Buffer
	writePairingOutput(&stdout, &stderr, token)

	if stdout.String() != token+"\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stderr.String(), token) {
		t.Fatal("pairing secret leaked into guidance stream")
	}
	if !strings.Contains(stderr.String(), clientauth.Header) || !strings.Contains(stderr.String(), "0600") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
