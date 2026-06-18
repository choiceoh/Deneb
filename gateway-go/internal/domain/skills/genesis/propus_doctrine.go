package genesis

import "strings"

// PropusDoctrineSpec is the product contract that keeps the research-backed
// self-improvement rules in one place. It deliberately stores the source-paper
// mapping next to the operational rule so future Propus surfaces preserve the
// original ideas instead of re-interpreting raw lifecycle logs ad hoc.
type PropusDoctrineSpec struct {
	Name           string
	Codename       string
	Version        string
	Lifecycle      []string
	Papers         []PropusDoctrinePaper
	FilteredPapers []PropusDoctrinePaper
	Invariants     []string
	QualityGates   []string
}

type PropusDoctrinePaper struct {
	ID                string
	Title             string
	OriginalPrinciple string
	PropusRule        string
	EvidenceGrade     string
	FilterReason      string
}

func PropusDoctrine() PropusDoctrineSpec {
	return PropusDoctrineSpec{
		Name:     "Propus",
		Codename: "propus",
		Version:  "2026-06-filtered-source-doctrine",
		Lifecycle: []string{
			"observe",
			"propose",
			"validate",
			"genesis_or_evolve",
			"watch",
			"rollback_or_backlog",
		},
		Papers: []PropusDoctrinePaper{
			{
				ID:                "arxiv:2602.20867",
				Title:             "SoK: Agentic Skills -- Beyond Tool Use in LLM Agents",
				OriginalPrinciple: "Skills are lifecycle-managed procedural modules with evaluation, governance, and supply-chain risk; self-generated skills can hurt without deterministic validation.",
				PropusRule:        "Treat generated/evolved skills as untrusted until validated, scored, and curator-visible.",
			},
			{
				ID:                "arxiv:2510.16079",
				Title:             "EvolveR: Self-Evolving LLM Agents through an Experience-Driven Lifecycle",
				OriginalPrinciple: "Experience must close the loop: collect trajectories, distill reusable principles, deduplicate, score, retrieve, and update from outcomes.",
				PropusRule:        "Every Propus item must preserve an evidence path from observed work to reusable rule to outcome watch.",
			},
			{
				ID:                "arxiv:2507.02778",
				Title:             "Self-Correction Bench: Uncovering and Addressing the Self-Correction Blind Spot in Large Language Models",
				OriginalPrinciple: "Models can fail to correct their own output while correcting identical external errors; correction needs activation and controlled error evidence.",
				PropusRule:        "Do not accept same-turn self-critique alone; externalize failed traces into validation cases or queued corrections.",
			},
			{
				ID:                "arxiv:2606.05976",
				Title:             "The Self-Correction Illusion: LLMs Correct Others but Not Themselves",
				OriginalPrinciple: "Correction success depends on chat-template role labels; byte-identical claims are corrected more when presented as external user/tool/memory content.",
				PropusRule:        "Present candidate failures to judges as external evidence, not as the model's own thought.",
			},
			{
				ID:                "arxiv:2606.09498",
				Title:             "Self-Harness: Harnesses That Improve Themselves",
				OriginalPrinciple: "Harness improvement should mine repeated verifier-grounded failure mechanisms, propose bounded edits to concrete harness surfaces, and promote only regression-tested candidates while keeping the base model fixed.",
				PropusRule:        "Every evolve candidate must name one supported failure signature, edited surface, expected behavior change, and regression risk before validation.",
			},
			{
				ID:                "arxiv:2606.11459",
				Title:             "APEX: Automated Prompt Engineering eXpert with Dynamic Data Selection",
				OriginalPrinciple: "Prompt optimization is data-selection limited; Easy/Hard/Mixed tiers from lineage identify Mixed frontier cases for mutation and ranking while Easy anchors protect mastered behavior.",
				PropusRule:        "Only claim APEX-style frontier coverage when validation cases carry Mixed frontier and Easy anchor tiers; otherwise treat the signal as an unproven corpus gap.",
				EvidenceGrade:     "core",
			},
			{
				ID:                "arxiv:2605.21240",
				Title:             "APEX: Autonomous Policy Exploration for Self-Evolving LLM Agents",
				OriginalPrinciple: "Self-evolving agents can collapse into familiar high-reward routines unless they maintain an explicit strategy map of tried and unexplored directions.",
				PropusRule:        "Keep opportunity backlog as an exploration map: record tried routes, expose unexplored forks, and balance exploitation against frontier discovery.",
				EvidenceGrade:     "supporting-transfer",
			},
		},
		FilteredPapers: []PropusDoctrinePaper{
			{
				ID:                "arxiv:2606.15363",
				Title:             "APEX: Adaptive Principle EXtraction A Three-Layer Self-Evolution Framework for Production AI Agents",
				OriginalPrinciple: "Self-evolution is multi-dimensional: harness patches, behavioral principle distillation, and workflow topology selection should co-evolve on the same production trace pool.",
				PropusRule:        "Do not promote the three-axis completion claim into a Propus quality gate until it has broader validation than a single production-agent case study.",
				EvidenceGrade:     "filtered",
				FilterReason:      "Single-agent production case study with a custom composite score; reported ablation stores L2 principles but does not inject them, so it is useful background rather than a canonical Propus source.",
			},
		},
		Invariants: []string{
			"generated_or_evolved_skill_requires_validation_evidence",
			"ambiguous_or_non_actionable_principle_is_rejected",
			"same_failure_candidate_must_not_repeat_without_new_evidence",
			"judgement_uses_externalized_evidence_not_same_turn_introspection",
			"evolve_candidate_declares_failure_signature_surface_behavior_and_risk",
			"validation_selection_records_mixed_frontier_and_easy_anchor_tiers",
			"propus_change_axis_is_diagnostic_metadata_not_completion_proof",
			"opportunity_backlog_tracks_tried_routes_and_unexplored_forks",
			"status_and_summary_are_the_single_state_model",
		},
		QualityGates: []string{
			"validation_corpus_or_replay_trace",
			"specific_trigger_procedure_pitfalls_verification",
			"deduped_and_curator_visible",
			"post_evolve_watch_or_rollback",
			"failure_signature_surface_behavior_risk_audit",
			"mixed_frontier_with_easy_anchor_tiers_recorded",
			"change_axis_recorded_for_diagnostics_not_completion_claim",
			"exploration_map_updates_tried_and_frontier_routes",
		},
	}
}

func (d PropusDoctrineSpec) LifecycleText() string {
	return strings.Join(d.Lifecycle, " -> ")
}

func (d PropusDoctrineSpec) SourceIDs() []string {
	out := make([]string, 0, len(d.Papers))
	for _, paper := range d.Papers {
		out = append(out, paper.ID)
	}
	return out
}

func (d PropusDoctrineSpec) FilteredSourceIDs() []string {
	out := make([]string, 0, len(d.FilteredPapers))
	for _, paper := range d.FilteredPapers {
		out = append(out, paper.ID)
	}
	return out
}

func (d PropusDoctrineSpec) ProductRules() []string {
	out := make([]string, 0, len(d.Papers))
	for _, paper := range d.Papers {
		if rule := strings.TrimSpace(paper.PropusRule); rule != "" {
			out = append(out, rule)
		}
	}
	return out
}
