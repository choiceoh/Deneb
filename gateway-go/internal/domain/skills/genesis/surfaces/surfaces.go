package surfaces

import (
	"path"
	"strings"
)

// Declared self-improvement editable surfaces (Self-Harness principle: a
// proposer may only claim DECLARED surfaces, and permission control lives
// outside the improvement loop). Auto-apply surfaces today: the skill-body
// evolve path (patch-first budget + judge + held-out replay + post-evolve
// rollback) and, since the 2026-08-16 rollback drill, gateway source via the
// coding lane (dev-tree gates + PR/CI + deploy-watch rollback). Everything
// else is PROPOSE-ONLY: recorded, reviewed, and landed through normal PR
// gates.
// Widening a surface to auto-apply requires (1) a behavioral regression gate
// equivalent to the skill held-out replay and (2) explicit operator approval —
// never flip a tier as a drive-by.

const (
	// SurfaceTierAutoApply — the loop may promote edits itself, behind gates.
	SurfaceTierAutoApply = "auto-apply"
	// SurfaceTierProposeOnly — the loop may only record a proposal.
	SurfaceTierProposeOnly = "propose-only"
	// SurfaceTierForbidden — never self-editable; proposals are rejected at
	// record time so a misaimed candidate can't even enter the queue.
	SurfaceTierForbidden = "forbidden"
)

// EditableSurface is one declared surface class.
type EditableSurface struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
	// Patterns match case-insensitively: "*.ext" by extension; a pattern
	// containing "/" is a repo-relative path matched by basename, path
	// suffix, AND directory containment (a directory target enclosing the
	// path matches — a proposal aimed at a whole package cannot skirt a
	// forbidden file inside it); otherwise by base filename.
	Patterns []string `json:"patterns"`
	Note     string   `json:"note,omitempty"`
}

// DeclaredEditableSurfaces is the whitelist, ordered by precedence (first
// match wins — forbidden entries lead so they can never be shadowed).
func DeclaredEditableSurfaces() []EditableSurface {
	return []EditableSurface{
		{
			Name: "prompt-cache-path", Tier: SurfaceTierForbidden,
			Patterns: []string{"prompt_cache.go", "cache_breakpoints.go", "tier1_cache.go", "prompt_snapshot_persist.go"},
			Note:     "prompt-cache invariants (docs/agent-rules/prompt-cache.md) — never self-editable",
		},
		{
			Name: "security-owned", Tier: SurfaceTierForbidden,
			Patterns: []string{"dependabot.yml", "codeql.yml"},
			Note:     "security CODEOWNERS paths — operator-only",
		},
		{
			// The acceptance machinery must never be optimizable by the loop it
			// accepts for (unanimous across the 2026H1 sweep — an exploitable
			// gate poisons every downstream label). Human/agent PRs may still
			// change these through normal review; the SELF-improvement queue
			// rejects them at record time. Full repo-relative paths so a
			// directory-shaped target (the health miner's normal output) that
			// encloses one of these is ALSO rejected — basename-only matching
			// let ".../genesis" through (RSI code eval C2).
			Name: "acceptance-machinery", Tier: SurfaceTierForbidden,
			Patterns: []string{
				"gateway-go/internal/domain/skills/genesis/validation_engine.go",
				"gateway-go/internal/domain/skills/genesis/validation_replay.go",
				"gateway-go/internal/domain/skills/genesis/eprocess/eprocess.go",
				"gateway-go/internal/domain/skills/genesis/meta_judge_bench.go",
				"gateway-go/internal/domain/skills/genesis/meta_producer_bench.go",
				"gateway-go/internal/domain/skills/genesis/meta_genesis_bench.go",
				"gateway-go/internal/domain/skills/genesis/meta_evolution.go",
				"gateway-go/internal/domain/skills/genesis/judge_accuracy.go",
				"gateway-go/internal/domain/skills/genesis/surfaces/surfaces.go",
				"gateway-go/internal/domain/skills/genesis/tracker_usage.go",
				"gateway-go/internal/domain/skills/genesis/tracker_self_correction.go",
				"gateway-go/internal/domain/skills/genesis/tracker_eprocess_cutover.go",
				"gateway-go/internal/domain/skills/genesis/evolution_drift.go",
				// Graduation machinery (operator delegated unlock EXECUTION
				// 2026-07-14): the loop may exercise evidence-met unlocks, but
				// the thresholds and the executor are policy — a loop that
				// could edit them could grant itself autonomy without evidence.
				"gateway-go/internal/domain/skills/genesis/rsi_ladder.go",
				"gateway-go/internal/domain/skills/genesis/ladder_watch.go",
				"gateway-go/internal/domain/skills/genesis/graduation_state.go",
				"gateway-go/internal/domain/skills/genesis/lifecycle/rsi_identity.go",
				"gateway-go/internal/domain/skills/genesis/lifecycle/self_correction.go",
				"gateway-go/internal/domain/skills/genesis/tracker_self_correction_dispatch_selection.go",
			},
			Note: "deterministic accept/reject core (gates, benches, e-process, rollback watch, drift brake, graduation policy, record-time gate, this whitelist)",
		},
		{
			// The scripts-side half of the same acceptor: the dispatch
			// allowlist, the outcome decision table, the prompt composer, and
			// the landing tool all decide what self-improvement work runs and
			// how its results are judged — dispatchable-by-the-loop would let
			// it queue an edit to its own dispatcher (RSI code eval C2/F3).
			Name: "acceptance-scripts", Tier: SurfaceTierForbidden,
			Patterns: []string{
				"scripts/dev/coding-dispatch.sh",
				"scripts/dev/dispatch_prompt.py",
				"scripts/dev/dispatch_outcome.py",
				"scripts/dev/pr.sh",
				".github/workflows/ci.yml",
			},
			Note: "dispatch allowlist, outcome table, prompt composer, landing tool, CI gate — operator/PR review only",
		},
		{
			// Operator authorization 2026-07-12 ("게이트웨이 소스 자가편집도 괜찮다 —
			// dev 소스만 뜯어고치고 문제 없을 때만 핫스왑"): gateway source is a
			// DECLARED propose-only surface. The execution contract for the
			// coding lane: dev worktree only, full gates (make check + live-test
			// smoke) green, PR + CI green, land → auto-deploy hot-swap; prod
			// tree is never edited directly. Graduated to auto-apply
			// 2026-08-16: the deploy-level rollback watch exists and was
			// proven by a live drill (freeze → detect 55s → bak-prev restore
			// → healthy in 89s) with explicit operator approval — the
			// graduation ladder's final row.
			Name: "gateway-source", Tier: SurfaceTierAutoApply,
			Patterns: []string{"*.go"},
			Note:     "operator-authorized self-edit via dev-tree + gates + hot-swap (2026-07-12); auto-apply graduated 2026-08-16 on rollback-drill evidence; acceptance-machinery excluded above",
		},
		{
			Name: "skill-body", Tier: SurfaceTierAutoApply,
			Patterns: []string{"SKILL.md"},
			Note:     "evolver path: patch-first budget + judge + held-out replay + rollback watch",
		},
		{
			Name: "heartbeat-instructions", Tier: SurfaceTierProposeOnly,
			Patterns: []string{"HEARTBEAT.md"},
			Note:     "heartbeat turn contract; P2 auto-apply mechanism landed (heartbeat_auto_apply.go: shadow gate + backup + anomaly rollback watch) behind DENEB_HEARTBEAT_AUTO_APPLY=1, default off — the flag is the tier flip, operator-owned",
		},
		{
			Name: "workspace-context", Tier: SurfaceTierProposeOnly,
			Patterns: []string{"AGENTS.md", "SOUL.md", "TOOLS.md", "IDENTITY.md", "USER.md", "MEMORY.md"},
			Note:     "system-prompt context files — cache-prefix critical, operator review required",
		},
	}
}

// undeclaredSurface is the default for any path outside the whitelist: the
// loop may still PROPOSE (repo code fixes land as normal PRs) but never
// auto-apply.
var undeclaredSurface = EditableSurface{Name: "undeclared", Tier: SurfaceTierProposeOnly}

// ClassifySurface maps one proposal target path to its declared surface.
func ClassifySurface(target string) EditableSurface {
	norm := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(target, "\\", "/")))
	norm = strings.TrimSuffix(strings.TrimPrefix(norm, "./"), "/")
	base := path.Base(norm)
	if base == "" || base == "." || base == "/" {
		return undeclaredSurface
	}
	for _, surface := range DeclaredEditableSurfaces() {
		for _, pattern := range surface.Patterns {
			p := strings.ToLower(pattern)
			if strings.HasPrefix(p, "*.") {
				if strings.HasSuffix(base, p[1:]) {
					return surface
				}
				continue
			}
			if strings.Contains(p, "/") {
				if pathPatternMatches(p, norm, base) {
					return surface
				}
				continue
			}
			if base == p {
				return surface
			}
		}
	}
	return undeclaredSurface
}

// pathPatternMatches reports whether a repo-relative path pattern matches the
// normalized target: by basename (bare-filename targets keep working), by
// path suffix at a component boundary (absolute or repo-relative spellings),
// or by directory containment — the target names a directory that encloses
// the pattern, at any depth of path abbreviation. Containment is deliberately
// conservative: a proposal aimed at a whole directory cannot be lexically
// proven to exclude the forbidden file inside it.
func pathPatternMatches(pattern, norm, base string) bool {
	if base == path.Base(pattern) {
		return true
	}
	if norm == pattern || strings.HasSuffix(norm, "/"+pattern) {
		return true
	}
	// Directory containment: "gateway-go/.../genesis", ".../skills/genesis",
	// or "genesis" all enclose ".../genesis/<file>.go".
	if strings.HasPrefix(pattern, norm+"/") || strings.Contains(pattern, "/"+norm+"/") {
		return true
	}
	return false
}

// ClassifyProposalSurfaces summarizes target files into a tier and forbidden list.
// tier (the most restrictive wins for the summary; any forbidden target is
// returned separately so the caller can reject the record outright).
// An empty target list is still a proposal (runtime-health findings have no
// single file yet) — default to propose-only rather than leaving Surface blank.
func ClassifyProposalSurfaces(targets []string) (tier string, forbidden []string) {
	tier = ""
	for _, target := range targets {
		surface := ClassifySurface(target)
		if surface.Tier == SurfaceTierForbidden {
			forbidden = append(forbidden, target+" ("+surface.Name+")")
			continue
		}
		if tier == "" || (tier == SurfaceTierAutoApply && surface.Tier == SurfaceTierProposeOnly) {
			tier = surface.Tier
		}
	}
	if tier == "" && len(forbidden) == 0 {
		tier = undeclaredSurface.Tier
	}
	return tier, forbidden
}

// ForbiddenSurfaceMentions returns forbidden basenames mentioned as whole path
// components in untrusted proposal text. It is the single prose-side companion
// to ClassifyProposalSurfaces, so the dispatcher does not maintain a second
// acceptance-file list in Python.
func ForbiddenSurfaceMentions(values ...string) []string {
	blob := strings.ToLower(strings.Join(values, "\n"))
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, surface := range DeclaredEditableSurfaces() {
		if surface.Tier != SurfaceTierForbidden {
			continue
		}
		for _, pattern := range surface.Patterns {
			if strings.HasPrefix(pattern, "*.") {
				continue
			}
			base := strings.ToLower(path.Base(pattern))
			if seen[base] || !mentionsPathComponent(blob, base) {
				continue
			}
			seen[base] = true
			out = append(out, base)
		}
	}
	return out
}

func mentionsPathComponent(blob, component string) bool {
	for start := 0; ; {
		relative := strings.Index(blob[start:], component)
		if relative < 0 {
			return false
		}
		index := start + relative
		end := index + len(component)
		beforeOK := index == 0 || !isFilenameBody(blob[index-1])
		afterOK := end == len(blob) || !isFilenameTail(blob, end)
		if beforeOK && afterOK {
			return true
		}
		start = index + 1
	}
}

func isFilenameBody(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == '-'
}

func isFilenameTail(blob string, index int) bool {
	ch := blob[index]
	if ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' {
		return true
	}
	return ch == '.' && index+1 < len(blob) && ((blob[index+1] >= 'a' && blob[index+1] <= 'z') || (blob[index+1] >= '0' && blob[index+1] <= '9'))
}
