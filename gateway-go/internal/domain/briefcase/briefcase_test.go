package briefcase

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDirValidCasepack(t *testing.T) {
	dir, manifest := writeValidCase(t)

	pack, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if pack.Digest != manifest.ManifestDigest {
		t.Fatalf("Digest = %q, want %q", pack.Digest, manifest.ManifestDigest)
	}
	if pack.Manifest.CaseID != "case-alpha" {
		t.Fatalf("CaseID = %q", pack.Manifest.CaseID)
	}

	got, err := pack.ReadFile("snapshot/wiki.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "approved budget: 100\n" {
		t.Fatalf("ReadFile = %q", got)
	}
	if _, err := pack.ReadFile(ManifestFile); err == nil {
		t.Fatal("ReadFile(manifest): expected undeclared-asset error")
	}
	if _, err := pack.ReadFile("not-present"); err == nil {
		t.Fatal("ReadFile(not-present): expected error")
	}

	if err := os.WriteFile(filepath.Join(dir, "snapshot", "wiki.md"), []byte("changed after load\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := pack.ReadFile("snapshot/wiki.md"); err == nil || !strings.Contains(err.Error(), "changed after validation") {
		t.Fatalf("ReadFile(changed asset) error = %v, want integrity error", err)
	}
}

func TestCanonicalDigestChangesWithManifest(t *testing.T) {
	_, manifest := writeValidCase(t)
	want := manifest.ManifestDigest

	manifest.ManifestDigest = strings.Repeat("f", 64)
	got, err := CanonicalDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("digest depends on its own field: got %q, want %q", got, want)
	}

	manifest.ManifestDigest = ""
	manifest.Episodes[0].ExpectedArtifactIDs = append(manifest.Episodes[0].ExpectedArtifactIDs, "another")
	changed, err := CanonicalDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if changed == want {
		t.Fatal("digest did not change after semantic manifest change")
	}
}

func TestMemoryMarkerRoundTripsAndChangesCanonicalDigest(t *testing.T) {
	dir, manifest := writeValidCase(t)
	withoutMemory := manifest.ManifestDigest
	manifest.Sources[0].Memory = true
	writeManifest(t, dir, &manifest)
	if manifest.ManifestDigest == withoutMemory {
		t.Fatal("Source.Memory did not change the signed manifest digest")
	}
	pack, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !pack.Manifest.Sources[0].Memory {
		t.Fatal("Source.Memory was lost during manifest load")
	}
}

func TestLoadDirRejectsUnknownFieldAndTrailingJSON(t *testing.T) {
	t.Run("duplicate field", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		path := filepath.Join(dir, ManifestFile)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.Replace(string(data), `"schemaVersion":`, `"schemaVersion":"deneb.briefcase/v1","schemaVersion":`, 1))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, dir, "duplicate JSON object key")
	})

	t.Run("unknown field", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		path := filepath.Join(dir, ManifestFile)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = []byte(strings.Replace(string(data), `"schemaVersion":`, `"unknown":true,"schemaVersion":`, 1))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, dir, "unknown field")
	})

	t.Run("trailing JSON", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		path := filepath.Join(dir, ManifestFile)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("\n{}\n"); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, dir, "multiple JSON values")
	})
}

func TestLoadDirRejectsFilesystemEscapes(t *testing.T) {
	t.Run("root symlink", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		link := filepath.Join(t.TempDir(), "case-link")
		if err := os.Symlink(dir, link); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, link, "root must be a real directory")
	})

	t.Run("asset symlink", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		outside := filepath.Join(t.TempDir(), "outside")
		if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "snapshot", "leak")); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, dir, "symlink is forbidden")
	})

	t.Run("path traversal", func(t *testing.T) {
		dir, manifest := writeValidCase(t)
		manifest.Sources[0].Path = "../outside"
		writeManifest(t, dir, &manifest)
		assertLoadError(t, dir, "traversing path is forbidden")
	})

	t.Run("unreferenced file", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		if err := os.WriteFile(filepath.Join(dir, "snapshot", "accidental-secret.txt"), []byte("secret"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, dir, "unreferenced file is forbidden")
	})

	t.Run("oversized asset", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		path := filepath.Join(dir, "snapshot", "oversized.bin")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(maxAssetBytes + 1); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, dir, "asset exceeds")
	})
}

func TestLoadDirRejectsIdentityAndIntegrityViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{
			name: "schema version",
			mutate: func(m *Manifest) {
				m.SchemaVersion = "deneb.briefcase/v2"
			},
			want: "schemaVersion",
		},
		{
			name: "duplicate source id",
			mutate: func(m *Manifest) {
				duplicate := m.Sources[0]
				duplicate.Path = m.Sources[2].Path
				duplicate.SHA256 = m.Sources[2].SHA256
				m.Sources = append(m.Sources, duplicate)
			},
			want: "duplicate source id",
		},
		{
			name: "duplicate episode id",
			mutate: func(m *Manifest) {
				m.Episodes = append(m.Episodes, m.Episodes[0])
			},
			want: "duplicate episode id",
		},
		{
			name: "heartbeat without input",
			mutate: func(m *Manifest) {
				m.Episodes[0].Kind = EpisodeHeartbeat
				m.Episodes[0].Input = nil
			},
			want: "executable episode",
		},
		{
			name: "duplicate artifact id",
			mutate: func(m *Manifest) {
				m.Artifacts = append(m.Artifacts, m.Artifacts[0])
			},
			want: "duplicate artifact id",
		},
		{
			name: "duplicate tool rule",
			mutate: func(m *Manifest) {
				m.ToolPolicy.Rules = append(m.ToolPolicy.Rules, m.ToolPolicy.Rules[0])
			},
			want: "duplicate tool rule",
		},
		{
			name: "supersession cycle",
			mutate: func(m *Manifest) {
				m.Sources[0].Supersedes = []string{m.Sources[1].ID}
				m.Sources[1].Supersedes = []string{m.Sources[0].ID}
			},
			want: "supersession graph contains a cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, manifest := writeValidCase(t)
			tt.mutate(&manifest)
			writeManifest(t, dir, &manifest)
			assertLoadError(t, dir, tt.want)
		})
	}

	t.Run("manifest digest mismatch", func(t *testing.T) {
		dir, manifest := writeValidCase(t)
		manifest.ManifestDigest = strings.Repeat("0", 64)
		writeManifestWithoutDigestUpdate(t, dir, manifest)
		assertLoadError(t, dir, "manifestDigest mismatch")
	})

	t.Run("source hash mismatch", func(t *testing.T) {
		dir, _ := writeValidCase(t)
		if err := os.WriteFile(filepath.Join(dir, "snapshot", "wiki.md"), []byte("tampered\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertLoadError(t, dir, "hash mismatch")
	})
}

func TestLoadDirRejectsSealedAndFutureVisibility(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{
			name: "sealed source marked as memory",
			mutate: func(m *Manifest) {
				m.Sources[2].Memory = true
			},
			want: "cannot mark sealed grader evidence as memory",
		},
		{
			name: "future snapshot source",
			mutate: func(m *Manifest) {
				m.Sources[0].AvailableAt = m.CutoffAt.Add(time.Minute)
				m.Sources[0].CapturedAt = m.CutoffAt.Add(2 * time.Minute)
			},
			want: "future data exposed in snapshot",
		},
		{
			name: "timeline source before availability",
			mutate: func(m *Manifest) {
				m.Episodes[0].At = m.Sources[1].AvailableAt.Add(-time.Minute)
			},
			want: "exposes future source",
		},
		{
			name: "sealed source release",
			mutate: func(m *Manifest) {
				m.Episodes[0].ReleaseSourceIDs = append(m.Episodes[0].ReleaseSourceIDs, m.Sources[2].ID)
			},
			want: "exposes sealed source",
		},
		{
			name: "future episode before frozen clock",
			mutate: func(m *Manifest) {
				m.Episodes[0].At = m.FrozenNow.Add(-time.Minute)
			},
			want: "before frozenNow",
		},
		{
			name: "unreleased timeline source",
			mutate: func(m *Manifest) {
				m.Episodes[0].ReleaseSourceIDs = nil
			},
			want: "is never released",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, manifest := writeValidCase(t)
			tt.mutate(&manifest)
			writeManifest(t, dir, &manifest)
			assertLoadError(t, dir, tt.want)
		})
	}
}

func TestPolicyValidationRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{
			name: "zero seed",
			mutate: func(m *Manifest) {
				m.Seed = 0
			},
			want: "seed must be positive",
		},
		{
			name: "zero max turns",
			mutate: func(m *Manifest) {
				m.RunPolicy.MaxTurns = 0
			},
			want: "runPolicy.maxTurns must be positive",
		},
		{
			name: "max turns above v1 cap",
			mutate: func(m *Manifest) {
				m.RunPolicy.MaxTurns = MaxTurnsV1 + 1
			},
			want: "exceeds schema v1 hard cap",
		},
		{
			name: "zero timeout",
			mutate: func(m *Manifest) {
				m.RunPolicy.TimeoutSeconds = 0
			},
			want: "runPolicy.timeoutSeconds must be positive",
		},
		{
			name: "global timeout above v1 cap",
			mutate: func(m *Manifest) {
				m.RunPolicy.TimeoutSeconds = MaxTimeoutSecondsV1 + 1
			},
			want: "runPolicy.timeoutSeconds exceeds schema v1 hard cap",
		},
		{
			name: "zero token budget",
			mutate: func(m *Manifest) {
				m.RunPolicy.MaxTokens = 0
			},
			want: "runPolicy.maxTokens must be positive",
		},
		{
			name: "token budget above v1 cap",
			mutate: func(m *Manifest) {
				m.RunPolicy.MaxTokens = MaxTokensV1 + 1
			},
			want: "runPolicy.maxTokens exceeds schema v1 hard cap",
		},
		{
			name: "follow-up budget above v1 cap",
			mutate: func(m *Manifest) {
				m.RunPolicy.MaxFollowUps = MaxFollowUpsV1 + 1
			},
			want: "runPolicy.maxFollowUps must be between",
		},
		{
			name: "negative per-turn timeout",
			mutate: func(m *Manifest) {
				m.RunPolicy.PerTurnTimeoutSeconds = -1
			},
			want: "runPolicy.perTurnTimeoutSeconds must not be negative",
		},
		{
			name: "per-turn timeout above v1 cap",
			mutate: func(m *Manifest) {
				m.RunPolicy.TimeoutSeconds = MaxTimeoutSecondsV1
				m.RunPolicy.PerTurnTimeoutSeconds = MaxTimeoutSecondsV1 + 1
			},
			want: "runPolicy.perTurnTimeoutSeconds exceeds schema v1 hard cap",
		},
		{
			name: "per-turn timeout above global timeout",
			mutate: func(m *Manifest) {
				m.RunPolicy.PerTurnTimeoutSeconds = m.RunPolicy.TimeoutSeconds + 1
			},
			want: "runPolicy.perTurnTimeoutSeconds must not exceed timeoutSeconds",
		},
		{
			name: "tool default allow",
			mutate: func(m *Manifest) {
				m.ToolPolicy.Default = ToolAllow
			},
			want: "toolPolicy.default must be",
		},
		{
			name: "tool-call budget above v1 cap",
			mutate: func(m *Manifest) {
				m.ToolPolicy.MaxCalls = MaxToolCallsV1 + 1
			},
			want: "toolPolicy.maxCalls exceeds schema v1 hard cap",
		},
		{
			name: "network deny with hosts",
			mutate: func(m *Manifest) {
				m.NetworkPolicy.AllowedHosts = []string{"example.com"}
			},
			want: "deny policy must not contain",
		},
		{
			name: "network allowlist unsupported in v1",
			mutate: func(m *Manifest) {
				m.NetworkPolicy.Mode = NetworkMode("allowlist")
				m.NetworkPolicy.AllowedHosts = []string{"example.com"}
			},
			want: "must be \"deny\" in schema v1",
		},
		{
			name: "output traversal",
			mutate: func(m *Manifest) {
				m.Artifacts[0].Path = "../report.pdf"
			},
			want: "traversing path is forbidden",
		},
		{
			name: "duplicate output path",
			mutate: func(m *Manifest) {
				m.Artifacts = append(m.Artifacts, Artifact{ID: "report-copy", Path: m.Artifacts[0].Path, MIME: "application/pdf"})
			},
			want: "output path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, manifest := writeValidCase(t)
			tt.mutate(&manifest)
			writeManifest(t, dir, &manifest)
			assertLoadError(t, dir, tt.want)
		})
	}
}

func writeValidCase(t *testing.T) (string, Manifest) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"snapshot/wiki.md":     "approved budget: 100\n",
		"timeline/mail.eml":    "Subject: revised budget\n\napproved budget: 120\n",
		"timeline/prompt-1.md": "Update the report with the approved budget.\n",
		"sealed/contract.txt":  "signed budget: 120\n",
	}
	for relative, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cutoff := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	mailAt := cutoff.Add(time.Hour)
	manifest := Manifest{
		SchemaVersion: SchemaVersionV1,
		CaseID:        "case-alpha",
		FamilyID:      "family-budget",
		Split:         SplitDev,
		PrivacyMode:   PrivacyPortable,
		Seed:          42001,
		CutoffAt:      cutoff,
		FrozenNow:     cutoff,
		Timezone:      "Asia/Seoul",
		Locale:        "ko-KR",
		Sources: []Source{
			{
				ID: "wiki-old", Kind: SourceWiki, Origin: SourceOriginHuman, Access: SourceAccessSnapshot,
				Path: "snapshot/wiki.md", SHA256: DigestBytes([]byte(files["snapshot/wiki.md"])),
				EventAt: cutoff.Add(-24 * time.Hour), AvailableAt: cutoff.Add(-24 * time.Hour), CapturedAt: cutoff,
				ProjectRefs: []string{"project-alpha"},
			},
			{
				ID: "mail-new", Kind: SourceMail, Origin: SourceOriginExternal, Access: SourceAccessTimeline,
				Path: "timeline/mail.eml", SHA256: DigestBytes([]byte(files["timeline/mail.eml"])),
				EventAt: mailAt, AvailableAt: mailAt, CapturedAt: mailAt,
				ProjectRefs: []string{"project-alpha"}, Supersedes: []string{"wiki-old"},
			},
			{
				ID: "gold-contract", Kind: SourceFile, Origin: SourceOriginHuman, Access: SourceAccessSealed,
				Path: "sealed/contract.txt", SHA256: DigestBytes([]byte(files["sealed/contract.txt"])),
				EventAt: mailAt, AvailableAt: mailAt, CapturedAt: mailAt,
				ProjectRefs: []string{"project-alpha"}, Sensitivity: "confidential",
			},
		},
		Episodes: []Episode{
			{
				ID: "episode-1", Kind: EpisodeUserTurn, At: mailAt,
				Input:               &FileRef{Path: "timeline/prompt-1.md", SHA256: DigestBytes([]byte(files["timeline/prompt-1.md"]))},
				ReleaseSourceIDs:    []string{"mail-new"},
				ExpectedArtifactIDs: []string{"report"},
			},
		},
		Artifacts: []Artifact{
			{ID: "report", Path: "output/report.pdf", MIME: "application/pdf", Required: true, MaxBytes: 10 << 20},
		},
		RunPolicy: RunPolicy{MaxTurns: 50, TimeoutSeconds: 3600, MaxTokens: 200_000},
		ToolPolicy: ToolPolicy{
			Default:  ToolDeny,
			MaxCalls: 20,
			Rules: []ToolRule{
				{Name: "mail_archive", Decision: ToolAllow, MaxCalls: 5},
				{Name: "phone_write", Decision: ToolAllow, MaxCalls: 1},
			},
		},
		NetworkPolicy: NetworkPolicy{Mode: NetworkDeny},
	}
	writeManifest(t, dir, &manifest)
	return dir, manifest
}

func writeManifest(t *testing.T, dir string, manifest *Manifest) {
	t.Helper()
	digest, err := CanonicalDigest(*manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestDigest = digest
	writeManifestWithoutDigestUpdate(t, dir, *manifest)
}

func writeManifestWithoutDigestUpdate(t *testing.T, dir string, manifest Manifest) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertLoadError(t *testing.T, dir, contains string) {
	t.Helper()
	_, err := LoadDir(dir)
	if err == nil {
		t.Fatalf("LoadDir: expected error containing %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("LoadDir error = %q, want substring %q", err, contains)
	}
	var validationErr *ValidationError
	if strings.Contains(err.Error(), "invalid casepack") && !errors.As(err, &validationErr) {
		t.Fatalf("validation error does not preserve ValidationError type: %T", err)
	}
}
