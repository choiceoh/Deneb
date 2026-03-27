package platform

import (
	"testing"
)

func TestWizardMethods_nilEngine(t *testing.T) {
	m := WizardMethods(WizardDeps{})
	if m != nil {
		t.Fatal("expected nil for nil Engine")
	}
}

func TestTalkMethods_nilTalk(t *testing.T) {
	m := TalkMethods(TalkDeps{})
	if m != nil {
		t.Fatal("expected nil for nil Talk")
	}
}

func TestSecretMethods_nilResolver(t *testing.T) {
	m := SecretMethods(SecretDeps{})
	if m != nil {
		t.Fatal("expected nil for nil Resolver")
	}
}
