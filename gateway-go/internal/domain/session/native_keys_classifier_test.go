package session

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Three rounds of the same defect (#4353, #4355, #4360) had one shape: the
// session key says who a run belongs to, a dozen call sites each re-answered
// that with their own inline prefix test, and two of them disagreed — so the
// same conversation was a chat here and delegated work there. The predicates in
// native_keys.go are now the single source of truth; this test is what keeps a
// new inline test from quietly reopening the class.
//
// Adding an entry to the allowlist is a deliberate act: it says "this is a
// specific job/namespace lookup, not a classification". If you are answering
// "what KIND of session is this", add or reuse a predicate instead.

// classifierPattern matches an inline prefix test on something key-shaped
// against one of the namespaces the classifier owns.
var classifierPattern = regexp.MustCompile(
	`HasPrefix\([^,]*(?i:key)[^,]*,\s*"(client|chat|cron|system|submain|glasses|boot|phone-event):`,
)

// allowed lists the inline tests that are NOT classification, keyed by file
// suffix → the literal that makes them specific.
var allowedInlineKeyTests = map[string][]string{
	// A named job / fork, not a session kind.
	"pipeline/chat/run_agent_config.go":        {`"system:skill-review:"`},
	"runtime/cronrunner/cron_agent_adapter.go": {`"cron:morning-letter:"`},
	// The puppet seat is a test harness carve-out inside client:, one level
	// below the kind question.
	"runtime/server/heartbeat_idle_review.go": {`"client:puppet"`},
	// The prefix constant this package owns is defined here.
	"runtime/server/native_sessions.go": {`nativeClientSessionPrefix`},
}

func TestSessionKeyClassificationStaysInThisPackage(t *testing.T) {
	t.Parallel()
	// The test runs with CWD = this package dir; walk the whole internal tree.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve internal root: %v", err)
	}
	selfDir := filepath.Join(root, "domain", "session")

	var offenders []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// This package IS the classifier.
		if strings.HasPrefix(path, selfDir) {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		for i, line := range strings.Split(string(body), "\n") {
			if !classifierPattern.MatchString(line) {
				continue
			}
			if isAllowedInlineKeyTest(rel, line) {
				continue
			}
			offenders = append(offenders, filepath.ToSlash(rel)+":"+itoa(i+1)+": "+strings.TrimSpace(line))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(offenders) > 0 {
		t.Errorf("session-key classification outside domain/session (use IsClientSession / IsSystemSession /"+
			" IsCronSession / IsSpawnedChildKey / RestorableTranscriptChannel, or allowlist it as a specific"+
			" job lookup in this test):\n  %s", strings.Join(offenders, "\n  "))
	}
}

func isAllowedInlineKeyTest(relPath, line string) bool {
	slash := filepath.ToSlash(relPath)
	for suffix, literals := range allowedInlineKeyTests {
		if !strings.HasSuffix(slash, suffix) {
			continue
		}
		for _, lit := range literals {
			if strings.Contains(line, lit) {
				return true
			}
		}
	}
	return false
}

// itoa avoids pulling strconv in for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
