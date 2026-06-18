// skills.go — miniapp.skills.* RPC handlers.
//
// Exposes the workspace skill catalog to the native client settings
// (DenebConfigScreen Skills tab), a per-skill detail
// (miniapp.skills.detail: the same enriched row plus the SKILL.md body for
// the tap-through detail screen), write RPCs for mutable local skills, plus
// the Propus lifecycle feed (miniapp.skills.lifecycle) so the operator can
// watch the proposal → validation → genesis/evolve → rollback/backlog loop. The skills.*
// RPC surface (skill/ handler) still covers the full snapshot/install/configure
// flow for richer consumers; this miniapp projection is intentionally narrow.
//
// The skills are pre-filtered by the caller (chat.EligibleWorkspaceSkills)
// through the same archived + eligibility passes the system prompt applies,
// so the tab advertises only skills the agent can actually use — not the raw
// discovery result, which would include archived or ineligible skills.
//
// The list does not render a runnable slash command per skill: the live slash
// dispatcher (slash_commands.go) matches strings.ToLower(skill.Name) — not a
// sanitized command name — and only for local/system skills, so reproducing the
// exact runnable string here is fragile and would risk advertising a command
// that doesn't route. Name + description + category + source is enough for a
// "what can this agent do" catalog.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Skill origins for SkillRow.Origin: loop-generated vs pre-existing.
const (
	skillOriginGenesis = "genesis"
	skillOriginInitial = "initial"
)

// lifecycleScanLimit bounds how many recent lifecycle entries are folded into
// the per-skill evolve counters on list calls. The log is a small JSONL that
// is fully loaded by the tracker anyway; this only caps the fold.
const lifecycleScanLimit = 500

// skillBodyMaxRunes caps the SKILL.md body returned by miniapp.skills.detail.
// Typical skills are a few KB; this only guards against a pathological doc
// flooding the detail screen.
const skillBodyMaxRunes = 60_000

// SkillRow is one entry in the Settings skills list. A slim projection of
// skills.SkillEntry — only the fields the native list/detail screens render.
//
//deneb:wire
type SkillRow struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	// Source is the discovery origin: managed | workspace |
	// agents-skills-personal | agents-skills-project | bundled | plugin | extra.
	Source  string `json:"source,omitempty"`
	Version string `json:"version,omitempty"`
	// Origin separates Propus-authored output from pre-existing skills:
	// "genesis" (the loop created it) | "initial" (installed or hand-authored).
	Origin string `json:"origin,omitempty"`
	// CreatedAt is the genesis creation time (unix millis); 0 for initial skills.
	CreatedAt int64 `json:"createdAt,omitempty"`
	// EvolveCount / LastEvolvedAt summarize committed evolve rewrites from the
	// lifecycle log — covers generated and initial skills alike.
	EvolveCount   int   `json:"evolveCount,omitempty"`
	LastEvolvedAt int64 `json:"lastEvolvedAt,omitempty"`
	// TotalUses / LastUsedAt are tracker usage aggregates.
	TotalUses  int   `json:"totalUses,omitempty"`
	LastUsedAt int64 `json:"lastUsedAt,omitempty"`
	// CuratorState is active | stale | archived for curator-managed
	// (agent-created) skills; empty for initial skills.
	CuratorState string `json:"curatorState,omitempty"`
	// Editable / Deletable are true only for local mutable skill sources.
	// Bundled and plugin skills are visible but protected from native writes.
	Editable  bool `json:"editable,omitempty"`
	Deletable bool `json:"deletable,omitempty"`
}

// SkillsListResponse is the miniapp.skills.list payload.
//
//deneb:wire
type SkillsListResponse struct {
	Skills []SkillRow `json:"skills"`
	Count  int        `json:"count"`
}

// SkillLifecycleEvent is one entry in the Propus timeline:
// a skill creation, a committed evolve, a rejected/rolled-back evolve, or a
// review decision (the per-session routing verdict that precedes them).
//
//deneb:wire
type SkillLifecycleEvent struct {
	// Type: genesis | evolved | evolve_rejected | evolve_rolled_back | review.
	Type      string `json:"type"`
	SkillName string `json:"skillName,omitempty"`
	At        int64  `json:"at,omitempty"` // unix millis
	// Version is the new version of a committed evolve.
	Version string `json:"version,omitempty"`
	// Detail is the human summary (description or reason). The timeline row
	// clamps it visually and reveals the full text when expanded.
	Detail string `json:"detail,omitempty"`
	// Route is the review decision for type=review: no-op | evolve | create | genesis.
	Route string `json:"route,omitempty"`
	// Evidence is the session observation a review verdict was based on —
	// only set when it isn't already serving as Detail.
	Evidence string `json:"evidence,omitempty"`
	// Self-Harness audit fields keep the target failure mechanism and
	// regression risk queryable for evolved/rejected events.
	TargetSignature        string `json:"targetSignature,omitempty"`
	EditedSurface          string `json:"editedSurface,omitempty"`
	ExpectedBehaviorChange string `json:"expectedBehaviorChange,omitempty"`
	RegressionRisk         string `json:"regressionRisk,omitempty"`
}

// PropusLifecycleSummary is the server-owned summary for the native Propus log.
// Keep this in the payload instead of recomputing it in the client: Propus has
// one state model, and the UI should render that model rather than drifting into
// a second interpretation of the same event feed.
//
//deneb:wire
type PropusLifecycleSummary struct {
	System          string   `json:"system"`
	State           string   `json:"state"`
	Total           int      `json:"total"`
	Genesis         int      `json:"genesis"`
	Evolved         int      `json:"evolved"`
	Review          int      `json:"review"`
	Rejected        int      `json:"rejected"`
	RolledBack      int      `json:"rolledBack"`
	Attention       int      `json:"attention"`
	LatestAt        int64    `json:"latestAt,omitempty"`
	LatestType      string   `json:"latestType,omitempty"`
	LatestSkill     string   `json:"latestSkill,omitempty"`
	DoctrineVersion string   `json:"doctrineVersion,omitempty"`
	Doctrine        string   `json:"doctrine,omitempty"`
	SourcePapers    []string `json:"sourcePapers,omitempty"`
	FilteredSources []string `json:"filteredSources,omitempty"`
	Principles      []string `json:"principles,omitempty"`
	QualityGates    []string `json:"qualityGates,omitempty"`
	NextActions     []string `json:"nextActions,omitempty"`
	CoverageState   string   `json:"coverageState,omitempty"`
	CoverageGaps    []string `json:"coverageGaps,omitempty"`
	NextCue         string   `json:"nextCue,omitempty"`
	QualityGate     string   `json:"qualityGate,omitempty"`
	AttentionCue    string   `json:"attentionCue,omitempty"`
}

// SkillsLifecycleResponse is the miniapp.skills.lifecycle payload,
// newest first.
//
//deneb:wire
type SkillsLifecycleResponse struct {
	Events  []SkillLifecycleEvent  `json:"events"`
	Count   int                    `json:"count"`
	Summary PropusLifecycleSummary `json:"summary"`
}

// SkillDetailResponse is the miniapp.skills.detail payload: the same enriched
// row the list renders plus the SKILL.md document itself, so the detail screen
// can show what the skill actually instructs the agent to do.
//
//deneb:wire
type SkillDetailResponse struct {
	Skill SkillRow `json:"skill"`
	// Body is the raw SKILL.md markdown (frontmatter included). Empty when the
	// file is unreadable — the detail still renders from the row meta.
	Body string `json:"body,omitempty"`
	// BodyTruncated marks a Body capped at skillBodyMaxRunes.
	BodyTruncated bool `json:"bodyTruncated,omitempty"`
	// Path is the SKILL.md location on the gateway host (operator reference).
	Path string `json:"path,omitempty"`
}

// SkillsDeps provides the already-filtered workspace skills plus optional
// tracker projections. List returns the skills after the archived +
// eligibility passes (see chat.EligibleWorkspaceSkills), so read rows and
// guarded writes target the same catalog the agent actually sees. A nil List
// disables the domain so method_registry can register conditionally. The
// tracker providers are nil-safe: without them rows stay un-enriched and the
// lifecycle feed is empty (the gateway can boot without a genesis tracker).
type SkillsDeps struct {
	List                  func() []skills.SkillEntry
	CuratorRecords        func() ([]genesis.SkillCuratorRecord, error)
	UsageStats            func() ([]genesis.UsageStats, error)
	RecentLifecycle       func(limit int) ([]genesis.LifecycleLogEntry, error)
	ValidationSummary     func(skillName string) (genesis.SkillValidationCaseSummary, error)
	RecentOpportunities   func(skillName string, limit int) ([]genesis.SkillOpportunityRecord, error)
	RecentSelfCorrections func(skillName string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error)
	SelfHarnessSignals    func() genesis.SelfHarnessSignalSummary
	InvalidateSkills      func()
}

// SkillsMethods returns the miniapp.skills.* handler map, or nil when no
// skills provider is wired.
func SkillsMethods(deps SkillsDeps) map[string]rpcutil.HandlerFunc {
	if deps.List == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.skills.list":      skillsList(deps),
		"miniapp.skills.detail":    skillsDetail(deps),
		"miniapp.skills.update":    skillsUpdate(deps),
		"miniapp.skills.delete":    skillsDelete(deps),
		"miniapp.skills.lifecycle": skillsLifecycle(deps),
	}
}

func skillsList(deps SkillsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}

		entries := deps.List()
		curator := curatorBySkill(deps)
		usage := usageBySkill(deps)
		evolves := evolveAggBySkill(deps)

		// entries arrive sorted by name from discovery; the front-end can
		// re-group by category/source without losing a stable secondary order.
		rows := make([]SkillRow, 0, len(entries))
		for _, e := range entries {
			rows = append(rows, buildSkillRow(e, curator, usage, evolves))
		}

		return rpcutil.RespondOK(req.ID, SkillsListResponse{Skills: rows, Count: len(rows)})
	}
}

// buildSkillRow projects one catalog entry into the enriched wire row —
// shared by the list and detail handlers so both render identical meta.
func buildSkillRow(
	e skills.SkillEntry,
	curator map[string]genesis.SkillCuratorRecord,
	usage map[string]genesis.UsageStats,
	evolves map[string]evolveAgg,
) SkillRow {
	row := SkillRow{
		Name:        e.Skill.Name,
		Description: e.Skill.Description,
		Category:    e.Skill.Category,
		Source:      string(e.Skill.Source),
		Version:     e.Skill.Version,
		Origin:      skillOriginInitial,
	}
	row.Editable = skillEntryMutable(e)
	row.Deletable = row.Editable
	rec, isManaged := curator[e.Skill.Name]
	agentCreated := isManaged && rec.CreatedBy == genesis.SkillCuratorCreatedByAgent
	// Two origin signals, belt and suspenders: the curator marker is
	// written on LogGenesis, while the genesis output dir catches
	// generated skills that predate the marker.
	if agentCreated || underGenesisDir(e.Skill.FilePath) {
		row.Origin = skillOriginGenesis
	}
	if agentCreated {
		row.CreatedAt = rec.CreatedAt
		row.CuratorState = rec.State
	}
	if st, ok := usage[e.Skill.Name]; ok {
		row.TotalUses = st.TotalUses
		row.LastUsedAt = st.LastUsed
	}
	if agg, ok := evolves[e.Skill.Name]; ok {
		row.EvolveCount = agg.count
		row.LastEvolvedAt = agg.lastAt
	}
	return row
}

func skillsDetail(deps SkillsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}

		var p struct {
			Name string `json:"name"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if strings.TrimSpace(p.Name) == "" {
			return rpcerr.MissingParam("name").Response(req.ID)
		}

		entry, ok := skillEntryByName(deps, p.Name)
		if !ok {
			return rpcerr.NotFound("skill").Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, skillDetailResponse(deps, entry))
	}
}

func skillsUpdate(deps SkillsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}

		var p struct {
			Name string `json:"name"`
			Body string `json:"body"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			return rpcerr.MissingParam("name").Response(req.ID)
		}
		if strings.TrimSpace(p.Body) == "" {
			return rpcerr.MissingParam("body").Response(req.ID)
		}

		entry, ok := skillEntryByName(deps, p.Name)
		if !ok {
			return rpcerr.NotFound("skill").Response(req.ID)
		}
		if !skillEntryMutable(entry) {
			return rpcerr.InvalidRequest("skill is not editable from the native app").Response(req.ID)
		}
		if err := validateSkillUpdateBody(p.Name, p.Body); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if err := atomicfile.WriteFile(entry.Skill.FilePath, []byte(p.Body), nil); err != nil {
			return rpcerr.WrapUnavailable("failed to write SKILL.md", err).Response(req.ID)
		}
		invalidateSkills(deps)

		if refreshed, ok := skillEntryByName(deps, p.Name); ok {
			return rpcutil.RespondOK(req.ID, skillDetailResponse(deps, refreshed))
		}
		return rpcutil.RespondOK(req.ID, skillDetailResponse(deps, entry))
	}
}

func skillsDelete(deps SkillsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}

		var p struct {
			Name string `json:"name"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			return rpcerr.MissingParam("name").Response(req.ID)
		}

		entry, ok := skillEntryByName(deps, p.Name)
		if !ok {
			return rpcerr.NotFound("skill").Response(req.ID)
		}
		if !skillEntryMutable(entry) {
			return rpcerr.InvalidRequest("skill is not deletable from the native app").Response(req.ID)
		}
		if err := os.RemoveAll(filepath.Dir(entry.Skill.FilePath)); err != nil {
			return rpcerr.WrapUnavailable("failed to delete skill directory", err).Response(req.ID)
		}
		invalidateSkills(deps)

		return rpcutil.RespondOK(req.ID, map[string]any{"name": p.Name, "deleted": true})
	}
}

func skillEntryByName(deps SkillsDeps, name string) (skills.SkillEntry, bool) {
	name = strings.TrimSpace(name)
	if name == "" || deps.List == nil {
		return skills.SkillEntry{}, false
	}
	for _, e := range deps.List() {
		if e.Skill.Name == name {
			return e, true
		}
	}
	return skills.SkillEntry{}, false
}

func skillDetailResponse(deps SkillsDeps, entry skills.SkillEntry) SkillDetailResponse {
	row := buildSkillRow(entry, curatorBySkill(deps), usageBySkill(deps), evolveAggBySkill(deps))
	resp := SkillDetailResponse{Skill: row, Path: entry.Skill.FilePath}
	// Body read is best-effort: catalog entries always carry a FilePath from
	// discovery, but the file may have been removed since the last scan.
	if data, err := os.ReadFile(entry.Skill.FilePath); err == nil {
		resp.Body = string(data)
		if runes := []rune(resp.Body); len(runes) > skillBodyMaxRunes {
			resp.Body = string(runes[:skillBodyMaxRunes])
			resp.BodyTruncated = true
		}
	}
	return resp
}

func skillEntryMutable(entry skills.SkillEntry) bool {
	if strings.TrimSpace(entry.Skill.FilePath) == "" || filepath.Base(entry.Skill.FilePath) != "SKILL.md" {
		return false
	}
	switch entry.Skill.Source {
	case skills.SourceManaged, skills.SourceWorkspace, skills.SourceExtra, skills.SourcePersonal, skills.SourceProject:
		return true
	default:
		return false
	}
}

func validateSkillUpdateBody(skillName, body string) error {
	header, _ := skills.ExtractFrontmatterBlock(body)
	if header == "" {
		return fmt.Errorf("body must include SKILL.md frontmatter (---\\nname: ...\\n---)")
	}
	fm := skills.ParseFrontmatter(body)
	name := strings.TrimSpace(fm["name"])
	if name == "" {
		return fmt.Errorf("frontmatter must include name")
	}
	if name != skillName {
		return fmt.Errorf("frontmatter name %q must match skill %q", name, skillName)
	}
	return nil
}

func invalidateSkills(deps SkillsDeps) {
	if deps.InvalidateSkills != nil {
		deps.InvalidateSkills()
	}
}

func skillsLifecycle(deps SkillsDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}

		var p struct {
			Limit     int    `json:"limit"`
			SkillName string `json:"skillName"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		if p.Limit <= 0 || p.Limit > lifecycleScanLimit {
			p.Limit = 60
		}

		events := make([]SkillLifecycleEvent, 0, p.Limit)
		lifecycleEntries := make([]genesis.LifecycleLogEntry, 0, p.Limit)
		if deps.RecentLifecycle != nil {
			// Over-fetch when filtering by skill so the filter doesn't starve
			// the requested window.
			fetch := p.Limit
			if p.SkillName != "" {
				fetch = lifecycleScanLimit
			}
			entries, err := deps.RecentLifecycle(fetch)
			if err != nil {
				return rpcerr.WrapUnavailable("lifecycle log unavailable", err).Response(req.ID)
			}
			for _, entry := range entries {
				if p.SkillName != "" && entry.SkillName != p.SkillName {
					continue
				}
				lifecycleEntries = append(lifecycleEntries, entry)
				events = append(events, lifecycleEvent(entry))
				if len(events) >= p.Limit {
					break
				}
			}
		}
		summary := propusLifecycleSummary(deps, lifecycleEntries, strings.TrimSpace(p.SkillName), p.Limit)

		return rpcutil.RespondOK(req.ID, SkillsLifecycleResponse{
			Events:  events,
			Count:   len(events),
			Summary: summary,
		})
	}
}

// lifecycleTextMaxRunes caps Detail/Evidence on lifecycle events. The native
// timeline clamps collapsed rows to a few lines and reveals the full text on
// tap, so this is a transport guard against a pathological log line, not a
// display cap (review reasons run 300-500 runes in practice).
const lifecycleTextMaxRunes = 2000

// lifecycleEvent projects a tracker log entry into the slim wire event.
func lifecycleEvent(e genesis.LifecycleLogEntry) SkillLifecycleEvent {
	ev := SkillLifecycleEvent{SkillName: e.SkillName, At: e.CreatedAt}
	if e.SelfHarnessAudit != nil {
		ev.TargetSignature = e.SelfHarnessAudit.TargetSignature
		ev.EditedSurface = e.SelfHarnessAudit.EditedSurface
		ev.ExpectedBehaviorChange = e.SelfHarnessAudit.ExpectedBehaviorChange
		ev.RegressionRisk = e.SelfHarnessAudit.RegressionRisk
	}
	switch e.Type {
	case "genesis":
		ev.Type = "genesis"
		ev.Detail = e.Description
	case "evolved":
		ev.Type = "evolved"
		ev.Version = e.NewVersion
		ev.Detail = e.Description
	case "evolve_rejected":
		ev.Type = "evolve_rejected"
		ev.Detail = e.Reason
	case "evolve_rolled_back":
		ev.Type = "evolve_rolled_back"
		ev.Detail = firstNonBlank(e.Reason, e.Description, "post-evolve rollback fired")
	default:
		// evolution_proposal (and any future type) renders as a review verdict.
		ev.Type = "review"
		ev.Route = e.Route
		ev.Detail = e.Reason
		if ev.Detail == "" {
			ev.Detail = e.Evidence
		} else {
			ev.Evidence = e.Evidence
		}
	}
	ev.Detail = truncateDetail(ev.Detail, lifecycleTextMaxRunes)
	ev.Evidence = truncateDetail(ev.Evidence, lifecycleTextMaxRunes)
	return ev
}

func propusLifecycleSummary(deps SkillsDeps, entries []genesis.LifecycleLogEntry, skillName string, limit int) PropusLifecycleSummary {
	scope := genesis.PropusScopeGlobal
	if strings.TrimSpace(skillName) != "" {
		scope = genesis.PropusScopeSkill
	}
	stats, _ := skillsDepsUsageStats(deps)
	curator, _ := skillsDepsCuratorRecords(deps)
	validationSummary, _ := skillsDepsValidationSummary(deps, skillName)
	opportunities, _ := skillsDepsOpportunities(deps, skillName, limit)
	selfCorrections, _ := skillsDepsSelfCorrections(deps, skillName, limit)
	selfHarnessSignals := genesis.SelfHarnessSignalSummary{}
	if deps.SelfHarnessSignals != nil && strings.TrimSpace(skillName) == "" {
		selfHarnessSignals = deps.SelfHarnessSignals()
	}
	shared := genesis.BuildPropusLifecycleSummary(genesis.PropusLifecycleSummaryInput{
		Scope:              scope,
		SkillName:          skillName,
		Recent:             entries,
		Stats:              stats,
		Curator:            curator,
		ValidationSummary:  validationSummary,
		Opportunities:      opportunities,
		SelfCorrections:    selfCorrections,
		SelfHarnessSignals: selfHarnessSignals,
	})
	return PropusLifecycleSummary{
		System:          shared.System,
		State:           shared.State,
		Total:           shared.Total,
		Genesis:         shared.Genesis,
		Evolved:         shared.Evolved,
		Review:          shared.Review,
		Rejected:        shared.Rejected,
		RolledBack:      shared.RolledBack,
		Attention:       shared.Attention,
		LatestAt:        shared.LatestAt,
		LatestType:      shared.LatestType,
		LatestSkill:     shared.LatestSkill,
		DoctrineVersion: shared.DoctrineVersion,
		Doctrine:        shared.Doctrine,
		SourcePapers:    shared.SourcePapers,
		FilteredSources: shared.FilteredSources,
		Principles:      shared.Principles,
		QualityGates:    shared.QualityGates,
		NextActions:     shared.NextActions,
		CoverageState:   shared.DoctrineCoverage.State,
		CoverageGaps:    shared.DoctrineCoverage.Gaps,
		NextCue:         shared.NextCue,
		QualityGate:     shared.QualityGate,
		AttentionCue:    shared.AttentionCue,
	}
}

func skillsDepsUsageStats(deps SkillsDeps) ([]genesis.UsageStats, error) {
	if deps.UsageStats == nil {
		return nil, nil
	}
	return deps.UsageStats()
}

func skillsDepsCuratorRecords(deps SkillsDeps) ([]genesis.SkillCuratorRecord, error) {
	if deps.CuratorRecords == nil {
		return nil, nil
	}
	return deps.CuratorRecords()
}

func skillsDepsValidationSummary(deps SkillsDeps, skillName string) (genesis.SkillValidationCaseSummary, error) {
	if deps.ValidationSummary == nil {
		return genesis.SkillValidationCaseSummary{SkillName: strings.TrimSpace(skillName)}, nil
	}
	return deps.ValidationSummary(strings.TrimSpace(skillName))
}

func skillsDepsOpportunities(deps SkillsDeps, skillName string, limit int) ([]genesis.SkillOpportunityRecord, error) {
	if deps.RecentOpportunities == nil {
		return nil, nil
	}
	return deps.RecentOpportunities(strings.TrimSpace(skillName), limit)
}

func skillsDepsSelfCorrections(deps SkillsDeps, skillName string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
	if deps.RecentSelfCorrections == nil {
		return nil, nil
	}
	return deps.RecentSelfCorrections(strings.TrimSpace(skillName), limit)
}

// evolveAgg folds committed-evolve lifecycle entries per skill.
type evolveAgg struct {
	count  int
	lastAt int64
}

func curatorBySkill(deps SkillsDeps) map[string]genesis.SkillCuratorRecord {
	out := map[string]genesis.SkillCuratorRecord{}
	if deps.CuratorRecords == nil {
		return out
	}
	recs, err := deps.CuratorRecords()
	if err != nil {
		return out
	}
	for _, r := range recs {
		out[r.SkillName] = r
	}
	return out
}

func usageBySkill(deps SkillsDeps) map[string]genesis.UsageStats {
	out := map[string]genesis.UsageStats{}
	if deps.UsageStats == nil {
		return out
	}
	stats, err := deps.UsageStats()
	if err != nil {
		return out
	}
	for _, s := range stats {
		out[s.SkillName] = s
	}
	return out
}

func evolveAggBySkill(deps SkillsDeps) map[string]evolveAgg {
	out := map[string]evolveAgg{}
	if deps.RecentLifecycle == nil {
		return out
	}
	entries, err := deps.RecentLifecycle(lifecycleScanLimit)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.Type != "evolved" || e.SkillName == "" {
			continue
		}
		agg := out[e.SkillName]
		agg.count++
		if e.CreatedAt > agg.lastAt {
			agg.lastAt = e.CreatedAt
		}
		out[e.SkillName] = agg
	}
	return out
}

// underGenesisDir reports whether a skill file lives under the genesis output
// dir (…/skills/genesis/…) — the on-disk signal for loop-generated skills.
func underGenesisDir(filePath string) bool {
	return strings.Contains(filepath.ToSlash(filePath), "/skills/genesis/")
}

// truncateDetail caps a detail line by rune count (CJK-safe).
func truncateDetail(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
