package process

import "testing"

func TestIsBlockedEnvKey(t *testing.T) {
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

func TestSanitizeNodeOptions(t *testing.T) {
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

func TestSanitizeEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/root",
		"LD_PRELOAD=/tmp/evil.so",
		"MAVEN_OPTS=-javaagent:evil.jar",
		"NODE_OPTIONS=--max-old-space-size=4096 --require=/tmp/evil.js",
		"TERM=xterm",
	}
	result := SanitizeEnv(env, nil)

	expected := map[string]bool{
		"PATH=/usr/bin":                         true,
		"HOME=/root":                            true,
		"NODE_OPTIONS=--max-old-space-size=4096": true,
		"TERM=xterm":                            true,
	}

	if len(result) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(result), result)
	}
	for _, entry := range result {
		if !expected[entry] {
			t.Errorf("unexpected entry: %q", entry)
		}
	}
}
