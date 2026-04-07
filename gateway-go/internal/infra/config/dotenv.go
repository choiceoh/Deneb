// Package config — dotenv loading.
//
// LoadDotenvFiles loads .env files into process environment variables.
// Precedence (highest → lowest): process env > ./.env > ~/.deneb/.env.
// Existing non-empty env vars are never overridden.
package config

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotenvFiles loads .env files into os environment, respecting precedence.
// Files are loaded in order: CWD ./.env first, then state dir ~/.deneb/.env.
// Keys already present in the process environment are never overridden.
func LoadDotenvFiles(logger *slog.Logger) {
	candidates := []string{
		".env",
		filepath.Join(ResolveStateDir(), ".env"),
	}

	for _, path := range candidates {
		pairs, err := parseDotenv(path)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Warn("failed to read .env file", "path", path, "error", err)
			}
			continue
		}
		applied := 0
		for key, val := range pairs {
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
				applied++
			}
		}
		logger.Debug("loaded .env file", "path", path, "keys", len(pairs), "applied", applied)
	}
}

// parseDotenv reads a .env file and returns key-value pairs.
// Supports: blank lines, # comments, KEY=VALUE, KEY="VALUE", KEY='VALUE',
// optional `export ` prefix. Does not support multi-line values or interpolation.
func parseDotenv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	pairs := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		idx := strings.IndexByte(line, '=')
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])

		// Strip matching outer quotes.
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') ||
				(val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}

		pairs[key] = val
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pairs, nil
}
