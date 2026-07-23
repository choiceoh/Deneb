package generation

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
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
	// MetaDispatchContractPrompt is consumed OUTSIDE the gateway process by
	// scripts/dev/dispatch_prompt.py (the L4 coding-dispatch lane) — the
	// gateway only materializes/refreshes it. The name must stay in sync with
	// that script's ARTIFACT_NAME (a scripts-side test asserts the parity).
	MetaDispatchContractPrompt = "dispatch-contract-prompt.md"
)

// MetaArtifactMinBytes is the deterministic safety floor: a file shorter than
// this (truncated write, botched future evolve) is treated as absent so the
// loop degrades to the compiled-in prompt instead of running on a stump. Every
// shipped prompt is well above this.
const MetaArtifactMinBytes = 200

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
// unreadable, or suspiciously short (MetaArtifactMinBytes floor).
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
	if len(content) < MetaArtifactMinBytes {
		m.logger.Warn("meta artifact suspiciously short, using compiled fallback",
			"name", name, "bytes", len(content), "floor", MetaArtifactMinBytes)
		return fallback
	}
	return content
}

// metaSidecarSuffix marks the provenance sidecar written next to each
// materialized artifact: the sha256 of the artifact content AS MATERIALIZED.
// It is how a later deploy distinguishes "file is still the pristine default
// I wrote" (safe to refresh when the compiled default moves) from "the slow
// loop or the operator revised this" (never touch).
const metaSidecarSuffix = ".default-sha256"

// MaterializeDefaults writes each absent artifact as a byte-copy of its
// compiled-in default, and REFRESHES an existing file when two things hold:
// the compiled default changed since materialization AND the file is still
// byte-identical to what was materialized (per the provenance sidecar). A
// revised artifact — slow-loop evolve or operator edit — is never touched; a
// pre-sidecar file of unknown provenance is preserved with a warning.
// Best-effort: failures are logged and skipped, never fatal (the Load
// fallback covers them).
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
		sidecarPath := path + metaSidecarSuffix
		defaultSum := ContentSHA256(content)
		existing, err := os.ReadFile(path)
		switch {
		case err == nil:
			if ContentSHA256(string(existing)) == defaultSum {
				m.writeSidecarIfAbsent(name, sidecarPath, defaultSum)
				continue // already the current default
			}
			sidecar, sErr := os.ReadFile(sidecarPath)
			if sErr != nil {
				m.logger.Warn("meta artifact diverged from compiled default without provenance sidecar — preserving as-is",
					"name", name)
				continue
			}
			if strings.TrimSpace(string(sidecar)) != ContentSHA256(string(existing)) {
				continue // revised since materialization (evolved/operator) — never clobber
			}
			// Pristine default from an older binary: refresh to the new default.
			if wErr := os.WriteFile(path, []byte(content), 0o644); wErr != nil {
				m.logger.Warn("meta artifact refresh failed", "name", name, "error", wErr)
				continue
			}
			if wErr := os.WriteFile(sidecarPath, []byte(defaultSum), 0o644); wErr != nil {
				m.logger.Warn("meta artifact sidecar write failed", "name", name, "error", wErr)
			}
			m.logger.Info("meta artifact refreshed to new compiled default (was pristine)", "name", name)
		case errors.Is(err, fs.ErrNotExist):
			if wErr := os.WriteFile(path, []byte(content), 0o644); wErr != nil {
				m.logger.Warn("meta artifact materialize failed", "name", name, "error", wErr)
				continue
			}
			if wErr := os.WriteFile(sidecarPath, []byte(defaultSum), 0o644); wErr != nil {
				m.logger.Warn("meta artifact sidecar write failed", "name", name, "error", wErr)
			}
			m.logger.Info("meta artifact materialized from compiled default", "name", name)
		default:
			m.logger.Warn("meta artifact unreadable during materialize", "name", name, "error", err)
		}
	}
}

// ContentSHA256 returns the stable content identity used by materialization
// sidecars and meta-evolution proposal records.
func ContentSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// ShortContentVersion is the 12-hex artifact-version form (as Version emits) but
// from content already in hand — so a caller that loaded a prompt for an LLM
// call can pin the EXACT text it used, instead of re-reading the file later and
// risking an intervening revision.
func ShortContentVersion(content string) string {
	return ContentSHA256(content)[:12]
}

// writeSidecarIfAbsent backfills provenance for a file that matches the current
// compiled default but predates the sidecar scheme.
func (m *MetaArtifacts) writeSidecarIfAbsent(name, sidecarPath, sum string) {
	if _, err := os.Stat(sidecarPath); err == nil {
		return
	}
	if err := os.WriteFile(sidecarPath, []byte(sum), 0o644); err != nil {
		m.logger.Warn("meta artifact sidecar backfill failed", "name", name, "error", err)
	}
}

// WriteProposal writes a slow-loop revision proposal NEXT TO the live
// artifact (<name>.proposed) without touching the live file — adoption is a
// separate decision (operator move, or a future bench-gated promotion).
func (m *MetaArtifacts) WriteProposal(name, content string) (string, error) {
	if m == nil || m.dir == "" {
		return "", errors.New("meta artifacts unwired")
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(m.dir, name+".proposed")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// AdoptProposal promotes <name>.proposed to the live artifact: atomic-ish
// write of the proposal content over the live file, then the .proposed is
// removed. The provenance sidecar is deliberately left untouched — the live
// content no longer matches it, which is exactly the "revised since
// materialization" state MaterializeDefaults never clobbers. Returns the
// adopted content's version (sha12).
func (m *MetaArtifacts) AdoptProposal(name string) (string, error) {
	if m == nil || m.dir == "" {
		return "", errors.New("meta artifacts unwired")
	}
	proposalPath := filepath.Join(m.dir, name+".proposed")
	raw, err := os.ReadFile(proposalPath)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(raw))
	if len(content) < MetaArtifactMinBytes {
		return "", errors.New("proposal below the artifact size floor")
	}
	// Back up the EFFECTIVE incumbent (live file, else compiled default) so a
	// regressing adoption can be reverted — the meta-level rollback watch and
	// the operator's 되돌리기 both restore from this.
	incumbent := m.Load(name, DefaultMetaArtifacts()[name])
	if incumbent != "" {
		if err := os.WriteFile(filepath.Join(m.dir, name+metaRollbackSuffix), []byte(incumbent), 0o644); err != nil {
			m.logger.Warn("adoption rollback backup failed", "name", name, "error", err)
		}
	}
	if err := os.WriteFile(filepath.Join(m.dir, name), []byte(content), 0o644); err != nil {
		return "", err
	}
	if err := os.Remove(proposalPath); err != nil {
		m.logger.Warn("adopted proposal file remove failed", "name", name, "error", err)
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:12], nil
}

// metaRollbackSuffix marks the pre-adoption backup an adoption writes.
const metaRollbackSuffix = ".rollback"

// RevertAdoption restores the pre-adoption backup over the live artifact and
// consumes the backup. Returns the restored content's version (sha12).
func (m *MetaArtifacts) RevertAdoption(name string) (string, error) {
	if m == nil || m.dir == "" {
		return "", errors.New("meta artifacts unwired")
	}
	backupPath := filepath.Join(m.dir, name+metaRollbackSuffix)
	raw, err := os.ReadFile(backupPath)
	if err != nil {
		return "", err
	}
	content := strings.TrimSpace(string(raw))
	if len(content) < MetaArtifactMinBytes {
		return "", errors.New("rollback backup below the artifact size floor")
	}
	if err := os.WriteFile(filepath.Join(m.dir, name), []byte(content), 0o644); err != nil {
		return "", err
	}
	if err := os.Remove(backupPath); err != nil {
		m.logger.Warn("consumed rollback backup remove failed", "name", name, "error", err)
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:12], nil
}

// RejectProposal discards <name>.proposed without touching the live artifact.
func (m *MetaArtifacts) RejectProposal(name string) error {
	if m == nil || m.dir == "" {
		return errors.New("meta artifacts unwired")
	}
	return os.Remove(filepath.Join(m.dir, name+".proposed"))
}

// DefaultMetaArtifacts maps every known artifact to its compiled-in default —
// the single registry MaterializeDefaults and tests share.
func DefaultMetaArtifacts() map[string]string {
	return map[string]string{
		MetaGenesisSystemPrompt:      genesisSystemPrompt,
		MetaGenesisJudgeSystemPrompt: genesisJudgeSystemPrompt,
		MetaEvolveSystemPrompt:       evolveSystemPrompt,
		MetaSkillJudgeSystemPrompt:   skillJudgeSystemPrompt,
		MetaDispatchContractPrompt:   dispatchContractPrompt,
	}
}

// Version returns a short content hash (sha256, 12 hex) of the artifact's
// EFFECTIVE content — the file when present and valid, the compiled fallback
// otherwise. This is the evaluator-version attribution the lifecycle ledger
// records (RSI P1.5): two decisions carry the same version iff the exact same
// prompt text produced them, whether it came from disk or the binary.
func (m *MetaArtifacts) Version(name, fallback string) string {
	return ShortContentVersion(m.Load(name, fallback))
}

// ActiveVersions maps every artifact in defaults to its effective version.
func (m *MetaArtifacts) ActiveVersions(defaults map[string]string) map[string]string {
	out := make(map[string]string, len(defaults))
	for name, fallback := range defaults {
		out[name] = m.Version(name, fallback)
	}
	return out
}

// ProcedureRef returns one content-addressed token — "proc-<12hex>" — folding
// together the versions of exactly the prompt artifacts NAMED by the caller:
// the ones that govern the decision being recorded. The hash is over the sorted
// "name=version" lines, so two decisions carry the same ProcedureRef iff every
// governing prompt was byte-identical. It is the composite analogue of the
// per-artifact Version (which pins one prompt each): a downstream outcome can be
// attributed to the exact procedure state that produced it.
//
// It is deliberately LANE-SPECIFIC rather than folding in DefaultMetaArtifacts
// wholesale — an evolve ref must not shift when an unrelated prompt (e.g. the
// L4 dispatch-contract prompt, consumed out-of-process) is revised, or the
// credit-assignment grouping would fragment. Callers pass the governing set
// (e.g. evolve + skill-judge for an L1 evolve decision).
//
// Model role is deliberately NOT folded in — which model executed the procedure
// is a separate axis, carried alongside on the record (EvolveModel/JudgeModel)
// so "the procedure text changed" and "the executor changed" stay
// distinguishable. Deterministic and nil-safe (all-fallback composite when
// unwired), so it is safe to mint on every decision.
func (m *MetaArtifacts) ProcedureRef(governing ...string) string {
	defaults := DefaultMetaArtifacts()
	versions := make(map[string]string, len(governing))
	for _, name := range governing {
		versions[name] = m.Version(name, defaults[name])
	}
	return ProcedureRefFromVersions(versions)
}

// ProcedureRefFromVersions builds the composite "proc-<hex>" from an explicit
// {artifact name → version} map — the point-of-use form. A caller that captured
// each governing prompt's version at the moment of its LLM call assembles the
// ref from those captured versions, so the ref reflects the procedure that
// ACTUALLY produced/judged the decision, not whatever is on disk at log time.
// Same hashing as ProcedureRef (sorted "name=version" lines), so the two agree
// when fed identical versions.
func ProcedureRefFromVersions(versions map[string]string) string {
	names := make([]string, 0, len(versions))
	for name := range versions {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		sb.WriteString(name)
		sb.WriteByte('=')
		sb.WriteString(versions[name])
		sb.WriteByte('\n')
	}
	return "proc-" + ContentSHA256(sb.String())[:12]
}
