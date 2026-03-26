package auth

import (
	"net/url"
	"path"
	"strings"
)

const maxPathDecodePasses = 32

// SecurityPathCanonicalization holds the result of path canonicalization.
type SecurityPathCanonicalization struct {
	CanonicalPath          string   // Final fully-decoded and normalized path
	Candidates             []string // All normalized intermediate paths (for security checks)
	DecodePasses           int      // Number of successful decode iterations
	DecodePassLimitReached bool     // True if more decoding was possible beyond the limit
	MalformedEncoding      bool     // True if decoding failed at some point
	RawNormalizedPath      string   // Path with separators/dots normalized but not decoded
}

// CanonicalizePathForSecurity performs multi-pass URL decode with normalization.
// Used for fail-closed security checks on request paths.
func CanonicalizePathForSecurity(pathname string) SecurityPathCanonicalization {
	result := SecurityPathCanonicalization{}

	// First: normalize without decoding
	result.RawNormalizedPath = normalizePath(pathname)

	// Collect candidates through iterative decoding
	current := pathname
	seen := make(map[string]bool)

	for i := 0; i < maxPathDecodePasses; i++ {
		normalized := normalizePath(current)
		if !seen[normalized] {
			seen[normalized] = true
			result.Candidates = append(result.Candidates, normalized)
		}

		decoded, err := url.PathUnescape(current)
		if err != nil {
			result.MalformedEncoding = true
			break
		}

		if decoded == current {
			// No more decoding possible
			break
		}

		current = decoded
		result.DecodePasses = i + 1

		if i == maxPathDecodePasses-1 {
			// Check if more decoding would be possible
			nextDecoded, _ := url.PathUnescape(current)
			if nextDecoded != current {
				result.DecodePassLimitReached = true
			}
		}
	}

	// Final normalization of the fully decoded path
	if len(result.Candidates) > 0 {
		result.CanonicalPath = result.Candidates[len(result.Candidates)-1]
	} else {
		result.CanonicalPath = normalizePath(pathname)
	}

	return result
}

// IsPathProtectedByPrefixes checks if any canonicalized version of the path
// matches any of the given protected prefixes. Fail-closed: returns true on
// edge cases (decode limit reached, malformed encoding after prefix detection).
func IsPathProtectedByPrefixes(pathname string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return false
	}

	canon := CanonicalizePathForSecurity(pathname)

	// Fail-closed: if decode pass limit reached, assume protected
	if canon.DecodePassLimitReached {
		return true
	}

	// Normalize prefixes
	normalizedPrefixes := make([]string, len(prefixes))
	for i, p := range prefixes {
		normalizedPrefixes[i] = normalizePath(p)
	}

	// Check all candidates against all prefixes
	for _, candidate := range canon.Candidates {
		for _, prefix := range normalizedPrefixes {
			if matchesPrefix(candidate, prefix) {
				return true
			}
		}
	}

	// Also check raw normalized path
	for _, prefix := range normalizedPrefixes {
		if matchesPrefix(canon.RawNormalizedPath, prefix) {
			return true
		}
	}

	return false
}

// IsProtectedPluginRoutePath checks if a path targets protected plugin routes.
// Protected prefixes: ["/api/channels"]
func IsProtectedPluginRoutePath(pathname string) bool {
	return IsPathProtectedByPrefixes(pathname, []string{"/api/channels"})
}

// normalizePath lowercases, collapses multiple slashes, removes trailing slash,
// and resolves dot segments.
func normalizePath(p string) string {
	p = strings.ToLower(p)
	// Collapse multiple slashes
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	// Remove trailing slash (except root)
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	// Resolve dot segments
	p = path.Clean(p)
	if p == "." {
		p = "/"
	}
	return p
}

// matchesPrefix checks if a path matches a prefix exactly or starts with prefix/.
func matchesPrefix(pathname, prefix string) bool {
	if pathname == prefix {
		return true
	}
	if strings.HasPrefix(pathname, prefix+"/") {
		return true
	}
	// Fail-closed: check for encoded separator after prefix
	if strings.HasPrefix(pathname, prefix+"%") {
		return true
	}
	return false
}
