package genesis

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Meta artifacts — P1 of the RSI roadmap
// (docs/research/recursive-self-improvement-roadmap.md): the generative half
// of the improvement pipeline (the system prompts driving genesis, evolve,
// and the judges) becomes a versioned artifact on disk that later phases can
// evolve, while the deterministic gates stay in Go. Rollout is
// behavior-neutral: every load falls back to the compiled-in constant, and
// materialized files start as byte-copies of those constants.

// Meta artifact file names under <managed genesis dir>/meta/.
const (
	MetaGenesisSystemPrompt      = "genesis-system-prompt.md"
	MetaGenesisJudgeSystemPrompt = "genesis-judge-system-prompt.md"
	MetaEvolveSystemPrompt       = "evolve-system-prompt.md"
	MetaSkillJudgeSystemPrompt   = "skill-judge-system-prompt.md"
)

// metaArtifactMinBytes is the deterministic safety floor: a file shorter than
// this (truncated write, botched future evolve) is treated as absent so the
// loop degrades to the compiled-in prompt instead of running on a stump. Every
// shipped prompt is well above this.
const metaArtifactMinBytes = 200

// MetaArtifacts resolves prompt artifacts from a directory with compiled-in
// fallbacks. A nil receiver or empty dir means pure-fallback mode, so callers
// never need nil checks and non-production instances read as if unwired.
// Reads happen per use (no cache): evolve/genesis runs are rare and this keeps
// operator edits and future slow-loop revisions hot without a restart.
type MetaArtifacts struct {
	dir    string
	logger *slog.Logger
}

// NewMetaArtifacts creates a resolver rooted at dir ("" → pure fallback).
func NewMetaArtifacts(dir string, logger *slog.Logger) *MetaArtifacts {
	if logger == nil {
		logger = slog.Default()
	}
	return &MetaArtifacts{dir: strings.TrimSpace(dir), logger: logger}
}

// Load returns the artifact's content, or fallback when unwired, absent,
// unreadable, or suspiciously short (metaArtifactMinBytes floor).
func (m *MetaArtifacts) Load(name, fallback string) string {
	if m == nil || m.dir == "" || name == "" {
		return fallback
	}
	raw, err := os.ReadFile(filepath.Join(m.dir, name))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			m.logger.Warn("meta artifact unreadable, using compiled fallback", "name", name, "error", err)
		}
		return fallback
	}
	content := strings.TrimSpace(string(raw))
	if len(content) < metaArtifactMinBytes {
		m.logger.Warn("meta artifact suspiciously short, using compiled fallback",
			"name", name, "bytes", len(content), "floor", metaArtifactMinBytes)
		return fallback
	}
	return content
}

// MaterializeDefaults writes each artifact that does not exist yet as a
// byte-copy of its compiled-in default, so the operator (and the future slow
// loop) has a concrete file to inspect and revise. Existing files are never
// touched — an evolved artifact survives deploys. Best-effort: failures are
// logged and skipped, never fatal (the Load fallback covers them).
func (m *MetaArtifacts) MaterializeDefaults(defaults map[string]string) {
	if m == nil || m.dir == "" {
		return
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		m.logger.Warn("meta artifact dir create failed", "dir", m.dir, "error", err)
		return
	}
	for name, content := range defaults {
		path := filepath.Join(m.dir, name)
		if _, err := os.Stat(path); err == nil {
			continue // present (possibly evolved) — never clobber
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			m.logger.Warn("meta artifact materialize failed", "name", name, "error", err)
			continue
		}
		m.logger.Info("meta artifact materialized from compiled default", "name", name)
	}
}

// DefaultMetaArtifacts maps every known artifact to its compiled-in default —
// the single registry MaterializeDefaults and tests share.
func DefaultMetaArtifacts() map[string]string {
	return map[string]string{
		MetaGenesisSystemPrompt:      genesisSystemPrompt,
		MetaGenesisJudgeSystemPrompt: genesisJudgeSystemPrompt,
		MetaEvolveSystemPrompt:       evolveSystemPrompt,
		MetaSkillJudgeSystemPrompt:   skillJudgeSystemPrompt,
	}
}

// Version returns a short content hash (sha256, 12 hex) of the artifact's
// EFFECTIVE content — the file when present and valid, the compiled fallback
// otherwise. This is the evaluator-version attribution the lifecycle ledger
// records (RSI P1.5): two decisions carry the same version iff the exact same
// prompt text produced them, whether it came from disk or the binary.
func (m *MetaArtifacts) Version(name, fallback string) string {
	sum := sha256.Sum256([]byte(m.Load(name, fallback)))
	return hex.EncodeToString(sum[:])[:12]
}

// ActiveVersions maps every artifact in defaults to its effective version.
func (m *MetaArtifacts) ActiveVersions(defaults map[string]string) map[string]string {
	out := make(map[string]string, len(defaults))
	for name, fallback := range defaults {
		out[name] = m.Version(name, fallback)
	}
	return out
}
