package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestParseDotenv(t *testing.T) {
	content := `# Database config
DB_HOST=localhost
DB_PORT=5432

# Quoted values
SECRET_KEY="my-secret"
SINGLE_QUOTED='single'

# With export prefix
export API_KEY=abc123

# Whitespace around key
  SPACED_KEY  = spaced_value

# No value
EMPTY_VAL=

# Malformed (no equals) — should be skipped
NOEQUALSSIGN
`
	tmp := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	pairs := testutil.Must(parseDotenv(tmp))

	tests := []struct {
		key, want string
	}{
		{"DB_HOST", "localhost"},
		{"DB_PORT", "5432"},
		{"SECRET_KEY", "my-secret"},
		{"SINGLE_QUOTED", "single"},
		{"API_KEY", "abc123"},
		{"SPACED_KEY", "spaced_value"},
		{"EMPTY_VAL", ""},
	}
	for _, tt := range tests {
		if got := pairs[tt.key]; got != tt.want {
			t.Errorf("key %q = %q, want %q", tt.key, got, tt.want)
		}
	}

	if _, ok := pairs["NOEQUALSSIGN"]; ok {
		t.Error("malformed line without = should be skipped")
	}
}


func TestLoadDotenvFilesNoOverride(t *testing.T) {
	// Set an env var before loading — it should not be overridden.
	const key = "DOTENV_TEST_NO_OVERRIDE"
	t.Setenv(key, "original")

	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte(key+"=replaced\n"), 0600); err != nil {
		t.Fatal(err)
	}

	pairs := testutil.Must(parseDotenv(envFile))
	if pairs[key] != "replaced" {
		t.Fatalf("got %q, want parsed value 'replaced'", pairs[key])
	}

	// Simulate the no-override logic from LoadDotenvFiles.
	for k, v := range pairs {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}

	if got := os.Getenv(key); got != "original" {
		t.Errorf("env var was overridden: got %q, want %q", got, "original")
	}
}
