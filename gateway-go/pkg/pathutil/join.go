// Package pathutil confines filesystem joins under a trusted root.
//
// CodeQL's go/path-injection query treats filepath.Rel + ".." rejection as a
// path sanitizer; prefer JoinUnder over bare filepath.Join whenever a name
// may carry session keys, query params, or other externally influenced bytes.
package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// SafeFileName turns an arbitrary label (session key, etc.) into a single
// path element that cannot introduce separators or climb ("..").
func SafeFileName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "\x00", "")
	name = filepath.Base(name)
	if name == "" || name == "." || name == ".." || !hasAlnum(name) {
		return "_invalid_"
	}
	return name
}

func hasAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// JoinUnder joins elem under root and rejects any result that escapes root.
// elem may contain multiple relative components (e.g. "a/b.json"); absolute
// elem values are rejected.
func JoinUnder(root, elem string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("pathutil: empty root")
	}
	if strings.TrimSpace(elem) == "" {
		return "", fmt.Errorf("pathutil: empty path element")
	}
	if filepath.IsAbs(elem) {
		return "", fmt.Errorf("pathutil: absolute path element")
	}
	root = filepath.Clean(root)
	clean := filepath.Clean(elem)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("pathutil: path escapes root")
	}
	full := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("pathutil: path escapes root")
	}
	return full, nil
}

// MustJoinUnder is JoinUnder that falls back to root/_invalid_ on error so
// legacy string-returning path helpers keep a confined result.
func MustJoinUnder(root, elem string) string {
	full, err := JoinUnder(root, elem)
	if err == nil {
		return full
	}
	fallback, ferr := JoinUnder(root, "_invalid_")
	if ferr != nil {
		return filepath.Join(filepath.Clean(root), "_invalid_")
	}
	return fallback
}
