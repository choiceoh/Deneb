package genesis

import (
	"path"
	"strings"
)

// Declared self-improvement editable surfaces (Self-Harness principle: a
// proposer may only claim DECLARED surfaces, and permission control lives
// outside the improvement loop). Deneb's only auto-apply surface today is the
// skill-body evolve path, which has the full gate stack (patch-first budget +
// judge + held-out replay + post-evolve rollback). Everything else is
// PROPOSE-ONLY: recorded, reviewed, and landed through normal PR gates.
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
	// Patterns match case-insensitively: "*.ext" by extension, otherwise by
	// base filename.
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
			Name: "skill-body", Tier: SurfaceTierAutoApply,
			Patterns: []string{"SKILL.md"},
			Note:     "evolver path: patch-first budget + judge + held-out replay + rollback watch",
		},
		{
			Name: "heartbeat-instructions", Tier: SurfaceTierProposeOnly,
			Patterns: []string{"HEARTBEAT.md"},
			Note:     "heartbeat turn contract; no replay gate yet",
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
	base := strings.ToLower(strings.TrimSpace(path.Base(strings.ReplaceAll(target, "\\", "/"))))
	if base == "" || base == "." {
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
			if base == p {
				return surface
			}
		}
	}
	return undeclaredSurface
}

// classifyProposalSurfaces summarizes a candidate's target files into one
// tier (the most restrictive wins for the summary; any forbidden target is
// returned separately so the caller can reject the record outright).
func classifyProposalSurfaces(targets []string) (tier string, forbidden []string) {
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
	return tier, forbidden
}
