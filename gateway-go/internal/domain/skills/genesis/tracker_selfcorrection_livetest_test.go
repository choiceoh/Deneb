package genesis

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromoteFailureClusterCandidates_LiveProdData runs the deterministic cluster
// promotion against a COPY of the production failure-signal sidecars, so it uses
// real prod data but writes candidates only to a temp dir (prod is never mutated).
// It answers "does prod actually have promotable clusters, and does the promoter
// mint sane candidates from them?" before merge.
//
//	DENEB_SELFCODING_LIVETEST=1 go test -run TestPromoteFailureClusterCandidates_LiveProdData -v ./internal/domain/skills/genesis/
func TestPromoteFailureClusterCandidates_LiveProdData(t *testing.T) {
	if os.Getenv("DENEB_SELFCODING_LIVETEST") == "" {
		t.Skip("set DENEB_SELFCODING_LIVETEST=1 to run against copied prod sidecars")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}
	prodDir := filepath.Join(home, ".deneb", "data")
	dir := t.TempDir()
	// Copy the read-side signal sidecars into the temp tracker. Writes (new
	// candidates) land in dir, never in prodDir.
	for _, name := range []string{"skill_usage.jsonl", "skill_genesis_log.jsonl", "skill_rejected_edits.jsonl", "self_correction_candidates.jsonl"} {
		if b, rerr := os.ReadFile(filepath.Join(prodDir, name)); rerr == nil {
			if werr := os.WriteFile(filepath.Join(dir, name), b, 0o644); werr != nil {
				t.Fatalf("copy %s: %v", name, werr)
			}
		}
	}
	tr := &Tracker{
		logger:              slog.Default(),
		usagePath:           filepath.Join(dir, "skill_usage.jsonl"),
		logPath:             filepath.Join(dir, "skill_genesis_log.jsonl"),
		curatorPath:         filepath.Join(dir, "skill_curator_state.json"),
		livenessPath:        filepath.Join(dir, "skill_liveness.json"),
		rejectedPath:        filepath.Join(dir, "skill_rejected_edits.jsonl"),
		opportunityPath:     filepath.Join(dir, "skill_opportunities.jsonl"),
		optimizerMemoryPath: filepath.Join(dir, "skill_optimizer_memory.json"),
		validationPath:      filepath.Join(dir, "skill_validation_cases.jsonl"),
		selfCorrectionPath:  filepath.Join(dir, "self_correction_candidates.jsonl"),
		stats:               make(map[string]*usageAgg),
		recentErrors:        make(map[string][]string),
		recentFailureTraces: make(map[string][]UsageFailureTrace),
		postEvolve:          make(map[string]*evolveWatch),
	}

	clusters := tr.FailureEvidenceClusters(20)
	t.Logf("prod failure clusters in the 7d window: %d", len(clusters))
	for i, c := range clusters {
		if i >= 10 {
			break
		}
		t.Logf("  support=%d kind=%s skill=%q sig=%q", c.Support, c.Kind, c.Skill, c.Signature)
	}

	promoted, err := tr.PromoteFailureClusterCandidates()
	if err != nil {
		t.Fatalf("PromoteFailureClusterCandidates: %v", err)
	}
	t.Logf("PROMOTED %d cluster candidate(s) from prod data (temp queue)", promoted)

	cands, err := tr.RecentSelfCorrectionCandidates("", SelfCorrectionStatusProposed, 20)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates: %v", err)
	}
	for _, c := range cands {
		if strings.HasPrefix(c.Source, failureClusterSource+":") {
			t.Logf("  candidate: scope=%s skill=%q title=%q", c.Scope, c.SkillName, c.Title)
		}
	}
}
