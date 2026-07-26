package process

import "testing"

func TestIsBlockedEnvKeyRejectsDangerousAndAllowsSafeKeys(t *testing.T) {
	blocked := []string{
		"LD_PRELOAD", "LD_LIBRARY_PATH", "BASH_ENV", "MAVEN_OPTS",
		"SBT_OPTS", "GRADLE_OPTS", "_JAVA_OPTIONS", "JAVA_TOOL_OPTIONS",
		"PYTHONSTARTUP", "PERL5OPT", "RUBYOPT", "DOTNET_STARTUP_HOOKS",
		"GLIBC_TUNABLES", "DYLD_INSERT_LIBRARIES", "DYLD_FRAMEWORK_PATH",
		"LD_AUDIT_MOD",
	}
	for _, k := range blocked {
		if !isBlockedEnvKey(k) {
			t.Errorf("expected %q to be blocked", k)
		}
	}

	allowed := []string{
		"PATH", "HOME", "USER", "SHELL", "TERM", "LANG",
		"DENEB_CLI", "NODE_OPTIONS", // NODE_OPTIONS handled separately
	}
	for _, k := range allowed {
		if isBlockedEnvKey(k) {
			t.Errorf("expected %q to be allowed", k)
		}
	}
}

func TestSanitizeNodeOptions_PreservesSafeFlagsStripsDangerous(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"--max-old-space-size=4096", "--max-old-space-size=4096"},
		{"--require=/tmp/evil.js", ""},
		{"--max-old-space-size=4096 --require=/tmp/evil.js", "--max-old-space-size=4096"},
		{"--require evil.js --max-old-space-size=4096", "--max-old-space-size=4096"},
		{"--import=evil.mjs", ""},
		{"--loader=evil.mjs --max-old-space-size=2048", "--max-old-space-size=2048"},
	}
	for _, tc := range tests {
		got := sanitizeNodeOptions(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizeNodeOptions(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsSecretEnvKeyWithholdsCredentialsKeepsTooling(t *testing.T) {
	secret := []string{
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN",
		"OPENAI_API_KEY", "KAGI_API_KEY", "ZAI_API_KEY", "GEMINI_API_KEY",
		"GOOGLE_APPLICATION_CREDENTIALS", "OPENROUTER_API_KEY",
		// suffix rules catch unknown/future providers and generic secrets
		"SOMENEWPROVIDER_API_KEY", "STRIPE_SECRET", "DB_PASSWORD",
		"FOO_ACCESS_TOKEN", "SVC_AUTH_TOKEN", "TLS_PRIVATE_KEY", "X_CREDENTIALS",
	}
	for _, k := range secret {
		if !isSecretEnvKey(k) {
			t.Errorf("expected %q to be withheld as a secret", k)
		}
	}

	// Operational vars the model-driven shell legitimately needs must survive.
	kept := []string{
		"PATH", "HOME", "USER", "SHELL", "LANG", "TERM",
		"GH_TOKEN", "GITHUB_TOKEN", // gh/git for the PR/land flow
		"WORMHOLE_TOKEN",                          // bare _TOKEN, local 127.0.0.1 — not scrubbed
		"DENEB_GATEWAY_URL", "DENEB_CLIENT_TOKEN", // DENEB_* runtime config/token
		"SSH_AUTH_SOCK", "GOPATH", "NODE_OPTIONS",
	}
	for _, k := range kept {
		if isSecretEnvKey(k) {
			t.Errorf("expected %q to be kept, but it was withheld", k)
		}
	}
}

func TestSanitizeEnvWithholdsProviderSecrets(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"GH_TOKEN=ghp_operational",
		"ANTHROPIC_API_KEY=sk-ant-secret",
		"KAGI_API_KEY=kagi-secret",
		"DENEB_GATEWAY_URL=http://localhost:8080",
	}
	result := SanitizeEnv(env, nil)

	got := map[string]struct{}{}
	for _, e := range result {
		got[e] = struct{}{}
	}
	for _, want := range []string{"PATH=/usr/bin", "GH_TOKEN=ghp_operational", "DENEB_GATEWAY_URL=http://localhost:8080"} {
		if _, ok := got[want]; !ok {
			t.Errorf("expected %q to survive", want)
		}
	}
	for _, gone := range []string{"ANTHROPIC_API_KEY=sk-ant-secret", "KAGI_API_KEY=kagi-secret"} {
		if _, ok := got[gone]; ok {
			t.Errorf("expected secret %q to be withheld", gone)
		}
	}
}

func TestSanitizeEnv_PreservesSafeVarsRemovesDangerous(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/root",
		"LD_PRELOAD=/tmp/evil.so",
		"MAVEN_OPTS=-javaagent:evil.jar",
		"NODE_OPTIONS=--max-old-space-size=4096 --require=/tmp/evil.js",
		"TERM=xterm",
	}
	result := SanitizeEnv(env, nil)

	expected := map[string]struct{}{
		"PATH=/usr/bin":                          {},
		"HOME=/root":                             {},
		"NODE_OPTIONS=--max-old-space-size=4096": {},
		"TERM=xterm":                             {},
	}

	if len(result) != len(expected) {
		t.Fatalf("got %d: %v, want %d entries", len(result), result, len(expected))
	}
	for _, entry := range result {
		if _, ok := expected[entry]; !ok {
			t.Errorf("unexpected entry: %q", entry)
		}
	}
}
