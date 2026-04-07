package platform

import (
	"testing"
)

func TestSecretMethods_nilResolver(t *testing.T) {
	m := SecretMethods(SecretDeps{})
	if m != nil {
		t.Fatal("expected nil for nil Resolver")
	}
}
