package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// wormhole-owned secrets: upstream keys live in a mode-600 file NEXT TO the config
// (~/.wormhole/secrets.env), expanded into config ${VAR} refs — not pinned into the
// systemd unit's EnvironmentFiles (which only loads at service start). The win is
// LIVE ROTATION: edit secrets.env (or have the gateway write it on the operator's
// behalf) and the watch loop hot-reloads it with NO service restart; the key-health
// probe then validates the new key on its next cycle. Fully backwards-compatible —
// an absent secrets.env is a no-op, so env-only / EnvironmentFiles deploys are
// unchanged until the file is populated.

// secretsFileFor returns wormhole's secret file path, derived from the config path
// (sibling secrets.env). Empty configPath → "" (no secrets file, e.g. in tests).
func secretsFileFor(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), "secrets.env")
}

// loadSecretsEnv reads a dotenv-style file (KEY=value per line; blank lines and
// '#' comments skipped; surrounding single/double quotes trimmed) and sets each
// pair in the process environment so loadConfig's os.ExpandEnv resolves ${KEY}. A
// missing file is not an error (returns 0). path == "" is a no-op. Returns the
// count of keys set. Values are never logged.
func loadSecretsEnv(path string) (int, error) {
	if path == "" {
		return 0, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue // not a KEY=value line
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		_ = os.Setenv(key, val)
		n++
	}
	return n, sc.Err()
}

// secretsMtimeNanos returns the file's modtime in unix-nano, or 0 if it is absent
// or unreadable. Used by the watch loop to detect an edit and trigger a reload.
func secretsMtimeNanos(path string) int64 {
	if path == "" {
		return 0
	}
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.ModTime().UnixNano()
}
