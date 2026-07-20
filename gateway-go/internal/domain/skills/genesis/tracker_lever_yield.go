package genesis

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/pkg/vectorutil"
)

// leverYield is the effectiveness of one evolution "lever" — a (target failure
// signature × edited surface) combination — aggregated over the lifecycle log
// (#2, HarnessX Appendix D). It answers "do Procedure edits on timeout
// signatures hold up more often than Pitfalls edits?", which per-skill tracking
// could not. Committed counts shipped evolves on the lever; Confirmed counts
// those that proved out over the post-evolve window; Partial counts
// confirmed-but-target-recurred; RolledBack counts reverts; ConfirmRate is
// Confirmed/Committed.
type leverYield struct {
	Signature   string  `json:"signature"`
	Surface     string  `json:"surface"`
	Committed   int     `json:"committed"`
	Confirmed   int     `json:"confirmed"`
	Partial     int     `json:"partial"`
	RolledBack  int     `json:"rolledBack"`
	ConfirmRate float64 `json:"confirmRate"`
}

type leverKey struct {
	sig     string
	surface string
}

func leverKeyFromAudit(audit *HarnessEditAudit) leverKey {
	if audit == nil {
		return leverKey{}
	}
	return leverKey{
		sig:     normalizedSelfHarnessSignature(audit.TargetSignature),
		surface: canonicalSkillSurface(strings.ToLower(strings.TrimSpace(audit.EditedSurface))),
	}
}

// leverYields aggregates the lifecycle log into per-lever effectiveness. It pairs
// each shipped evolve (an "evolved" entry, which carries the Self-Harness audit)
// with its later outcome ("evolve_confirmed" / "evolve_rolled_back") for the same
// skill — rollback entries carry no audit, so the lever is recovered from that
// skill's most recent shipped evolve. limit bounds how much of the log is read.
func (t *Tracker) leverYields(limit int) ([]leverYield, error) {
	entries, err := t.RecentLifecycleLog(limit)
	if err != nil {
		return nil, err
	}
	agg := map[leverKey]*leverYield{}
	lastLever := map[string]leverKey{} // skill -> lever of its last shipped evolve
	get := func(k leverKey) *leverYield {
		y := agg[k]
		if y == nil {
			y = &leverYield{Signature: k.sig, Surface: k.surface}
			agg[k] = y
		}
		return y
	}
	// RecentLifecycleLog returns newest-first; walk oldest-first so a skill's
	// shipped lever is known before its later confirm/rollback is attributed.
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		switch e.Type {
		case "evolved":
			k := leverKeyFromAudit(e.SelfHarnessAudit)
			lastLever[e.SkillName] = k
			get(k).Committed++
		case "evolve_confirmed":
			k := leverKeyFromAudit(e.SelfHarnessAudit)
			if k == (leverKey{}) {
				k = lastLever[e.SkillName]
			}
			y := get(k)
			y.Confirmed++
			if strings.HasPrefix(e.Reason, "partial") {
				y.Partial++
			}
		case "evolve_rolled_back":
			get(lastLever[e.SkillName]).RolledBack++
		}
	}
	out := make([]leverYield, 0, len(agg))
	for _, y := range agg {
		if y.Committed == 0 {
			// Orphaned confirm/rollback whose 'evolved' entry fell outside the
			// limit window — no shipped lever to attribute it to; skip the phantom
			// (empty-signature, Committed==0) bucket rather than emit it.
			continue
		}
		y.ConfirmRate = float64(y.Confirmed) / float64(y.Committed)
		out = append(out, *y)
	}
	// Stable, deterministic order: most-shipped levers first.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Committed != out[j].Committed {
			return out[i].Committed > out[j].Committed
		}
		if out[i].Signature != out[j].Signature {
			return out[i].Signature < out[j].Signature
		}
		return out[i].Surface < out[j].Surface
	})
	return out, nil
}

// lowYieldLevers returns levers the evolver should stop proposing: a
// (signature×surface) pair whose RESOLVED evolves confirm at or below
// maxConfirmRate. It feeds the evolve prompt's avoid-directions (HarnessX
// Appendix D).
//
// Two refinements over a raw Committed-denominator point estimate — the data
// regime is sparse, so the rule must use every signal yet still decide early:
//
//   - Resolved denominator (Confirmed+RolledBack): pending, not-yet-confirmed
//     evolves are excluded, so a lever with confirms-plus-pending isn't falsely
//     avoided (raw Confirmed/Committed counts a pending evolve like a failure).
//     Reverts — the strongest negative signal, ignored by Confirmed/Committed —
//     count directly.
//   - Laplace smoothing (Beta(1,1) posterior mean): (Confirmed+1)/(resolved+2)
//     shrinks a 0/3 toward 0.2 and a 1/3 toward 0.4 so one or two resolved
//     outcomes can't slam the rate to 0/1. This is the decisive point-estimate
//     use of the posterior; a credible-interval variant was prototyped and
//     rejected (at single-user volumes it needs ~60 resolved to flag a
//     borderline lever, so it would never fire — uncertainty paralysis).
//
// minResolved gates on resolved outcomes (confirm+revert), not bare ships.
func (t *Tracker) lowYieldLevers(limit, minResolved int, maxConfirmRate float64) ([]leverYield, error) {
	all, err := t.leverYields(limit)
	if err != nil {
		return nil, err
	}
	return filterLowYieldLevers(all, minResolved, maxConfirmRate), nil
}

// filterLowYieldLevers is the pure avoid-decision (resolved denominator + Laplace
// smoothing), split out for testability.
func filterLowYieldLevers(levers []leverYield, minResolved int, maxConfirmRate float64) []leverYield {
	var low []leverYield
	for _, y := range levers {
		resolved := y.Confirmed + y.RolledBack
		if resolved < minResolved {
			continue // not enough resolved evidence to avoid the direction yet
		}
		smoothed := float64(y.Confirmed+1) / float64(resolved+2)
		if smoothed <= maxConfirmRate {
			low = append(low, y)
		}
	}
	return low
}

// confirmedEvolveExemplar is one cross-skill "this edit actually held up"
// exhibit for the evolve prompt (RSI P1.5 ⑤, TPGO 2604.20714 / GRAO): the
// audit of a confirmed evolve whose target signature matches one of the
// failing skill's current failure signatures.
type confirmedEvolveExemplar struct {
	SkillName string           `json:"skillName"`
	Audit     HarnessEditAudit `json:"audit"`
	CreatedAt int64            `json:"createdAt"`
}

// confirmedEvolveExemplars returns confirmed evolves (newest first, at most
// limit) whose normalized target signature matches any of signatures,
// excluding excludeSkill (its own history already reaches the prompt via
// optimizer memory). This is the positive mirror of LowYieldLevers — the
// GRAO experience-retrieval mechanism: a memoryless improvement loop repeats
// dead ends AND forgets what worked (TPGO ablation 30.0→14.5%).
func (t *Tracker) confirmedEvolveExemplars(signatures []string, excludeSkill string, limit int) ([]confirmedEvolveExemplar, error) {
	// Compatibility path for callers without a request context. The only such
	// callers are tests/diagnostics; keep any optional semantic request bounded.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return t.confirmedEvolveExemplarsContext(ctx, signatures, excludeSkill, limit)
}

func (t *Tracker) confirmedEvolveExemplarsContext(ctx context.Context, signatures []string, excludeSkill string, limit int) ([]confirmedEvolveExemplar, error) {
	if limit <= 0 || len(signatures) == 0 {
		return nil, nil
	}
	wanted := make([]string, 0, len(signatures))
	for _, s := range signatures {
		if n := normalizedSelfHarnessSignature(s); n != "" {
			wanted = append(wanted, n)
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	entries, err := t.RecentLifecycleLog(skillLeverYieldScanLimit)
	if err != nil {
		return nil, err
	}
	out := confirmedExemplarsMatching(entries, wanted, excludeSkill, limit)
	// Mechanism-level fallback (ToE 2606.06960 / Experience Graphs 2606.29823
	// — RSI 2026H2 addendum #6): at organic volume an exact-signature repeat
	// is rare, so the precise pass often finds nothing. Retry on the
	// mechanism=… component — the same key the failure clusters group by — so
	// a confirmed fix for the same failure MECHANISM still reaches the prompt.
	// Precise matches always win; the fallback only fills an empty result.
	if len(out) == 0 {
		if mech := signatureMechanisms(wanted); len(mech) > 0 {
			out = confirmedExemplarsMatching(entries, mech, excludeSkill, limit)
		}
	}
	if len(out) < limit {
		out = t.fillConfirmedExemplarsSemantic(ctx, entries, wanted, excludeSkill, limit, out)
	}
	return out, nil
}

const (
	// Keep well below the sidecar's 256-text request cap. The lifecycle scan can
	// contain up to 300 confirmed entries, so a single request is not safe.
	confirmedExemplarEmbedBatch = 128
)

type scoredConfirmedExemplar struct {
	exemplar confirmedEvolveExemplar
	score    float64
}

// fillConfirmedExemplarsSemantic adds analogous confirmed successes only after
// deterministic exact/mechanism retrieval. This is prompt evidence, never a
// gate signal; every degradation path returns the deterministic base unchanged.
func (t *Tracker) fillConfirmedExemplarsSemantic(
	ctx context.Context,
	entries []LifecycleLogEntry,
	wanted []string,
	excludeSkill string,
	limit int,
	base []confirmedEvolveExemplar,
) []confirmedEvolveExemplar {
	embedder := t.exemplarEmbedderSnapshot()
	if embedder == nil || !embedder.IsHealthy() || ctx == nil || ctx.Err() != nil {
		return base
	}
	seen := make(map[string]struct{}, len(base))
	for _, exemplar := range base {
		seen[confirmedExemplarKey(exemplar)] = struct{}{}
	}
	candidates := make([]confirmedEvolveExemplar, 0, len(entries))
	texts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != "evolve_confirmed" || entry.SkillName == excludeSkill || entry.SelfHarnessAudit == nil {
			continue
		}
		exemplar := confirmedEvolveExemplar{
			SkillName: entry.SkillName,
			Audit:     withHarnessDimensions(*entry.SelfHarnessAudit),
			CreatedAt: entry.CreatedAt,
		}
		if strings.TrimSpace(exemplar.Audit.TargetSignature) == "" {
			continue
		}
		if _, exists := seen[confirmedExemplarKey(exemplar)]; exists {
			continue
		}
		candidates = append(candidates, exemplar)
		texts = append(texts, confirmedExemplarPassage(exemplar))
	}
	if len(candidates) == 0 {
		return base
	}
	passages, ok := embedConfirmedExemplarPassages(ctx, embedder, texts)
	if !ok {
		return base
	}
	query := "Current failure signatures:\n" + strings.Join(wanted, "\n")
	if dimensions := harnessDimensionsForSignatures(wanted); len(dimensions) > 0 {
		query += "\nCurrent harness dimensions:\n" + strings.Join(dimensions, "\n")
	}
	queries, err := embedindex.EmbedQueries(ctx, embedder, []string{query})
	if err != nil || len(queries) != 1 {
		return base
	}
	semanticFloor := embedindex.CalibrationFor(embedder, embedindex.SemanticSurfaceRSIExemplar).Floor
	scored := make([]scoredConfirmedExemplar, 0, len(candidates))
	for i, candidate := range candidates {
		score := vectorutil.Cosine(queries[0], passages[i])
		if score >= semanticFloor {
			scored = append(scored, scoredConfirmedExemplar{exemplar: candidate, score: score})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].exemplar.CreatedAt != scored[j].exemplar.CreatedAt {
			return scored[i].exemplar.CreatedAt > scored[j].exemplar.CreatedAt
		}
		if scored[i].exemplar.SkillName != scored[j].exemplar.SkillName {
			return scored[i].exemplar.SkillName < scored[j].exemplar.SkillName
		}
		return scored[i].exemplar.Audit.TargetSignature < scored[j].exemplar.Audit.TargetSignature
	})
	out := append([]confirmedEvolveExemplar(nil), base...)
	for _, candidate := range scored {
		out = append(out, candidate.exemplar)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func embedConfirmedExemplarPassages(ctx context.Context, embedder embedindex.Embedder, texts []string) ([][]float32, bool) {
	passages := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += confirmedExemplarEmbedBatch {
		if ctx.Err() != nil {
			return nil, false
		}
		end := min(start+confirmedExemplarEmbedBatch, len(texts))
		batch, err := embedder.Embed(ctx, texts[start:end])
		if err != nil || len(batch) != end-start {
			return nil, false
		}
		passages = append(passages, batch...)
	}
	return passages, true
}

func confirmedExemplarPassage(exemplar confirmedEvolveExemplar) string {
	audit := withHarnessDimensions(exemplar.Audit)
	return "Confirmed improvement\n" +
		"Failure: " + audit.TargetSignature + "\n" +
		"Harness dimension: " + formatHarnessDiagnosis(&HarnessDimensionDiagnosis{
		Primary:   audit.PrimaryDimension,
		Secondary: audit.SecondaryDimensions,
	}) + "\n" +
		"Edited surface: " + audit.EditedSurface + "\n" +
		"Behavior change: " + audit.ExpectedBehaviorChange + "\n" +
		"Regression risk: " + audit.RegressionRisk
}

func confirmedExemplarKey(exemplar confirmedEvolveExemplar) string {
	return exemplar.SkillName + "\x00" + exemplar.Audit.TargetSignature + "\x00" + time.UnixMilli(exemplar.CreatedAt).UTC().Format(time.RFC3339Nano)
}

// confirmedExemplarsMatching scans newest-first confirmed evolves whose target
// signature matches any of wanted (substring semantics, SignatureMatches).
func confirmedExemplarsMatching(entries []LifecycleLogEntry, wanted []string, excludeSkill string, limit int) []confirmedEvolveExemplar {
	var out []confirmedEvolveExemplar
	for _, e := range entries { // newest first
		if e.Type != "evolve_confirmed" || e.SkillName == excludeSkill || e.SelfHarnessAudit == nil {
			continue
		}
		sig := normalizedSelfHarnessSignature(e.SelfHarnessAudit.TargetSignature)
		if sig == "" {
			continue
		}
		matched := false
		for _, w := range wanted {
			if selfHarnessSignatureMatches(w, sig) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		out = append(out, confirmedEvolveExemplar{
			SkillName: e.SkillName,
			Audit:     withHarnessDimensions(*e.SelfHarnessAudit),
			CreatedAt: e.CreatedAt,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

// signatureMechanisms extracts the distinct mechanism=… components from
// normalized signatures ("terminal=x|mechanism=y" form); empty for signatures
// without one.
func signatureMechanisms(signatures []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, sig := range signatures {
		for _, part := range strings.Split(sig, "|") {
			if !strings.HasPrefix(part, "mechanism=") || part == "mechanism=" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			out = append(out, part)
		}
	}
	return out
}
