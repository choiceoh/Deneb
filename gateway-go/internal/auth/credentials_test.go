package auth

import (
	"os"
	"testing"
)

func TestResolveCredentials_TokenFromEnv(t *testing.T) {
	os.Setenv("DENEB_GATEWAY_TOKEN", "env-token-123")
	defer os.Unsetenv("DENEB_GATEWAY_TOKEN")

	plan := ResolveCredentials(CredentialConfig{AuthMode: "token"})
	if plan.Mode != AuthModeToken {
		t.Errorf("mode = %q, want %q", plan.Mode, AuthModeToken)
	}
	if plan.Token != "env-token-123" {
		t.Errorf("token = %q, want %q", plan.Token, "env-token-123")
	}
	if plan.Source != CredentialSourceEnv {
		t.Errorf("source = %q, want %q", plan.Source, CredentialSourceEnv)
	}
}

func TestResolveCredentials_TokenFromConfig(t *testing.T) {
	os.Unsetenv("DENEB_GATEWAY_TOKEN")

	plan := ResolveCredentials(CredentialConfig{
		AuthMode: "token",
		Token:    "config-token-456",
	})
	if plan.Token != "config-token-456" {
		t.Errorf("token = %q, want %q", plan.Token, "config-token-456")
	}
	if plan.Source != CredentialSourceConfig {
		t.Errorf("source = %q, want %q", plan.Source, CredentialSourceConfig)
	}
}

func TestResolveCredentials_EnvPrecedenceOverConfig(t *testing.T) {
	os.Setenv("DENEB_GATEWAY_TOKEN", "env-wins")
	defer os.Unsetenv("DENEB_GATEWAY_TOKEN")

	plan := ResolveCredentials(CredentialConfig{
		AuthMode: "token",
		Token:    "config-loses",
	})
	if plan.Token != "env-wins" {
		t.Errorf("env should win: got %q", plan.Token)
	}
	if plan.Source != CredentialSourceEnv {
		t.Errorf("source = %q, want %q", plan.Source, CredentialSourceEnv)
	}
}

func TestResolveCredentials_PasswordMode(t *testing.T) {
	os.Setenv("DENEB_GATEWAY_PASSWORD", "pw-123")
	defer os.Unsetenv("DENEB_GATEWAY_PASSWORD")

	plan := ResolveCredentials(CredentialConfig{AuthMode: "password"})
	if plan.Mode != AuthModePassword {
		t.Errorf("mode = %q, want %q", plan.Mode, AuthModePassword)
	}
	if plan.Password != "pw-123" {
		t.Errorf("password = %q, want %q", plan.Password, "pw-123")
	}
}

func TestResolveCredentials_NoneMode(t *testing.T) {
	plan := ResolveCredentials(CredentialConfig{AuthMode: "none"})
	if plan.Mode != AuthModeNone {
		t.Errorf("mode = %q, want %q", plan.Mode, AuthModeNone)
	}
	if plan.Source != CredentialSourceNone {
		t.Errorf("source = %q, want %q", plan.Source, CredentialSourceNone)
	}
}

func TestResolveCredentials_RejectsSecretRef(t *testing.T) {
	os.Setenv("DENEB_GATEWAY_TOKEN", "${SOME_VAR}")
	defer os.Unsetenv("DENEB_GATEWAY_TOKEN")

	plan := ResolveCredentials(CredentialConfig{AuthMode: "token"})
	if plan.Token != "" {
		t.Errorf("should reject secret ref, got %q", plan.Token)
	}
}

func TestResolveCredentials_DefaultsToToken(t *testing.T) {
	os.Unsetenv("DENEB_GATEWAY_TOKEN")
	plan := ResolveCredentials(CredentialConfig{})
	if plan.Mode != AuthModeToken {
		t.Errorf("default mode = %q, want %q", plan.Mode, AuthModeToken)
	}
}

func TestResolveProbeAuth(t *testing.T) {
	os.Setenv("DENEB_GATEWAY_TOKEN", "probe-tok")
	defer os.Unsetenv("DENEB_GATEWAY_TOKEN")

	plan := ResolveProbeAuth(CredentialConfig{AuthMode: "token"})
	if plan.Token != "probe-tok" {
		t.Errorf("probe token = %q, want %q", plan.Token, "probe-tok")
	}
}
