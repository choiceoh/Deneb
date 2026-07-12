package briefcase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGradeArtifactContextPreservesOutcomeContract(t *testing.T) {
	root := t.TempDir()
	content := []byte("actual")
	writeArtifactTestFile(t, filepath.Join(root, "actual.txt"), content)
	writeArtifactTestFile(t, filepath.Join(root, "parent.txt"), []byte("not a directory"))
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(t.TempDir(), "root.txt")
	writeArtifactTestFile(t, rootFile, []byte("not a root"))

	actualSum := sha256.Sum256(content)
	actualDigest := hex.EncodeToString(actualSum[:])
	mismatchSum := sha256.Sum256([]byte("expected"))
	mismatchDigest := hex.EncodeToString(mismatchSum[:])
	validCheck := func(path, digest string) Check {
		return Check{
			ID:             "artifact",
			Type:           CheckArtifact,
			Weight:         1,
			ArtifactPath:   path,
			ExpectedSHA256: digest,
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	type outcomeCase struct {
		name     string
		ctx      context.Context
		check    Check
		evidence Evidence
		status   Status
		detail   string
		err      error
	}
	tests := []outcomeCase{
		{
			name: "canceled before validation",
			ctx:  canceled, check: validCheck("actual.txt", actualDigest), evidence: Evidence{ArtifactRoot: root},
			status: StatusInvalid, detail: "artifact grading was canceled", err: context.Canceled,
		},
		{
			name:   "missing root contract",
			check:  validCheck("actual.txt", actualDigest),
			status: StatusInvalid, detail: "artifact root is required",
		},
		{
			name:  "unsafe relative path",
			check: validCheck("../actual.txt", actualDigest), evidence: Evidence{ArtifactRoot: root},
			status: StatusInvalid, detail: "artifact path must be a safe relative file path",
		},
		{
			name:  "invalid digest",
			check: validCheck("actual.txt", "abcd"), evidence: Evidence{ArtifactRoot: root},
			status: StatusInvalid, detail: "expected artifact sha256 must be a full hexadecimal digest",
		},
		{
			name:  "missing root directory",
			check: validCheck("actual.txt", actualDigest), evidence: Evidence{ArtifactRoot: filepath.Join(root, "missing-root")},
			status: StatusFail, detail: "artifact root does not exist",
		},
		{
			name:  "root is a file",
			check: validCheck("actual.txt", actualDigest), evidence: Evidence{ArtifactRoot: rootFile},
			status: StatusInvalid, detail: "artifact root is not a directory",
		},
		{
			name:  "missing artifact",
			check: validCheck("missing.txt", actualDigest), evidence: Evidence{ArtifactRoot: root},
			status: StatusFail, detail: "artifact does not exist",
		},
		{
			name:  "parent is not a directory",
			check: validCheck(filepath.Join("parent.txt", "child.txt"), actualDigest), evidence: Evidence{ArtifactRoot: root},
			status: StatusFail, detail: "artifact path parent is not a directory",
		},
		{
			name:  "target is not a regular file",
			check: validCheck("folder", actualDigest), evidence: Evidence{ArtifactRoot: root},
			status: StatusFail, detail: "artifact is not a regular file",
		},
		{
			name:     "signed size limit exceeded",
			check:    validCheck("actual.txt", actualDigest),
			evidence: Evidence{ArtifactRoot: root, ArtifactMaxBytes: map[string]int64{"actual.txt": int64(len(content) - 1)}},
			status:   StatusInvalid, detail: "artifact exceeds its signed size limit",
		},
		{
			name:  "digest mismatch",
			check: validCheck("actual.txt", mismatchDigest), evidence: Evidence{ArtifactRoot: root},
			status: StatusFail, detail: "artifact sha256 did not match",
		},
		{
			name:     "uppercase digest and exact signed size pass",
			check:    validCheck("actual.txt", strings.ToUpper(actualDigest)),
			evidence: Evidence{ArtifactRoot: root, ArtifactMaxBytes: map[string]int64{"actual.txt": int64(len(content))}},
			status:   StatusPass, detail: "artifact exists and sha256 matched",
		},
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	writeArtifactTestFile(t, outside, content)
	if err := os.Symlink(outside, filepath.Join(root, "link.txt")); err == nil {
		tests = append(tests, outcomeCase{
			name:  "symlink component",
			check: validCheck("link.txt", actualDigest), evidence: Evidence{ArtifactRoot: root},
			status: StatusInvalid, detail: "artifact path contains a symlink",
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := test.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			status, detail, err := gradeArtifactContext(ctx, test.check, test.evidence)
			if status != test.status || detail != test.detail || !errors.Is(err, test.err) {
				t.Fatalf("outcome = (%s, %q, %v), want (%s, %q, %v)", status, detail, err, test.status, test.detail, test.err)
			}
		})
	}
}

func writeArtifactTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
