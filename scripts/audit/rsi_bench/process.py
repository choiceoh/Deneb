"""Process domain — acceptor honesty and loop dynamics (paper-mapped, rubric 1.3)."""

from __future__ import annotations

from pathlib import Path
from typing import Any

from .cache import self_evolution_from_cache
from .model import (
    ABILITY_TRANSFER_PROXY_CAP,
    DOMAIN_WEIGHTS,
    MIN_RESOLVED_FOR_HARD,
    SOFT_RESOLVE_SCORE_CAP,
    SWAP_CONSISTENCY_PROXY_CAP,
    SWAP_CONSISTENCY_SATURATED_CAP,
    SWAP_RATE_TOLERANCE,
    UNMEASURED_RATE_FLOOR,
    Domain,
    Evidence,
    Finding,
    Metric,
    clamp,
    grade_rate_high_good,
    grade_rate_low_good,
    stable_id,
)
from .impact_first import ImpactFirstWindow, load_impact_first_window
from .ledgers import (
    GenesisWindow,
    JudgeWindow,
    MetaWindow,
    TransferWindow,
    WatchWindow,
    data_dir,
    load_genesis_window,
    load_judge_window,
    load_meta_window,
    load_transfer_window,
    load_watch_window,
)

BOOTSTRAP = {
    "acceptor-trust": 28.0,
    "confirm-honesty": 28.0,
    "judge-fuel": 22.0,
    "preference-collapse": 30.0,
    "swap-consistency": 30.0,
    "probe-coverage": 25.0,
    "timescale-turn": 18.0,
    "ability-transfer": 22.0,
    "anti-collapse": 40.0,
    "impact-first": 25.0,
}

# A thin sample cannot claim full discipline credit: four runs at 100% is a
# coincidence, not a habit. Below this many source-editing runs the rate is
# scored but ceilinged, mirroring the proxy-cap convention used elsewhere.
IMPACT_FIRST_THIN_SAMPLE = 8
IMPACT_FIRST_THIN_CAP = 70.0


def _merge_health(genesis: GenesisWindow, health: dict[str, Any]) -> GenesisWindow:
    if not health:
        return genesis
    mapping = {
        "evolves_7d": "evolves",
        "evolve_rejected_7d": "rejected",
        "evolve_confirmed_7d": "confirmed",
        "evolve_rolled_back_7d": "rolled_back",
        "genesis_7d": "genesis",
        "proposals_7d": "proposals",
    }
    for src, attr in mapping.items():
        if src in health and health[src] is not None:
            setattr(genesis, attr, int(health[src]))
    camel = {
        "evolves7d": "evolves",
        "rejected7d": "rejected",
        "confirmed7d": "confirmed",
        "rolledBack7d": "rolled_back",
        "genesis7d": "genesis",
    }
    for src, attr in camel.items():
        if src in health and health[src] is not None:
            setattr(genesis, attr, int(health[src]))
    top_skill = health.get("top_evolved_skill") or health.get("topEvolvedSkill")
    top_count = health.get("top_evolved_count") or health.get("topEvolvedCount")
    if top_skill and top_count:
        genesis.per_skill = {str(top_skill): int(top_count)}
    return genesis


def _resolution(
    genesis: GenesisWindow, health: dict[str, Any], watch: WatchWindow
) -> tuple[int, int, int, str]:
    """Return confirmed, rolled_back, resolved, mode (hard|soft|none)."""
    if health.get("resolved_evolves_7d") is not None and int(health.get("resolved_evolves_7d") or 0) > 0:
        confirmed = int(health.get("evolve_confirmed_7d") or health.get("confirmed7d") or genesis.confirmed)
        rolled = int(health.get("evolve_rolled_back_7d") or health.get("rolledBack7d") or genesis.rolled_back)
        return confirmed, rolled, confirmed + rolled, "hard"
    if genesis.resolved >= MIN_RESOLVED_FOR_HARD:
        return genesis.confirmed, genesis.rolled_back, genesis.resolved, "hard"
    if watch.soft_confirmed >= MIN_RESOLVED_FOR_HARD:
        # Soft: treat watch soft-confirms as confirmed, no soft rollbacks from watch file.
        return watch.soft_confirmed, 0, watch.soft_confirmed, "soft"
    return genesis.confirmed, genesis.rolled_back, genesis.resolved, "none"


def _score_acceptor(
    genesis: GenesisWindow, health: dict[str, Any], watch: WatchWindow
) -> tuple[float, Evidence, list[Finding]]:
    findings: list[Finding] = []
    confirmed, rolled, resolved, mode = _resolution(genesis, health, watch)
    if resolved < MIN_RESOLVED_FOR_HARD and mode == "none":
        return (
            UNMEASURED_RATE_FLOOR,
            Evidence(
                "process-acceptor-trust",
                "bootstrap",
                f"resolved={resolved} < {MIN_RESOLVED_FOR_HARD}; unmeasured (PACE)",
                required=False,
            ),
            findings,
        )
    rate = rolled / resolved if resolved else 0.0
    if mode == "hard" and health.get("false_accept_rate") is not None:
        rate = float(health["false_accept_rate"])
    score = grade_rate_low_good(rate, soft=0.10, hard=0.40)
    if mode == "soft":
        score = min(score, SOFT_RESOLVE_SCORE_CAP)
    if health.get("e_process_ready") or health.get("eProcessReady"):
        score = clamp(score + 5.0)
    if mode == "hard" and rate >= 0.25:
        findings.append(
            Finding(
                id=stable_id("acceptor-trust", f"{rate:.3f}"),
                domain="process",
                pillar="acceptor-trust",
                severity="high",
                path="skill_genesis_log.jsonl",
                evidence=f"falseAcceptRate={rate:.3f} resolved={resolved} mode={mode}",
                why="PACE: high false-accept rate means the acceptor is not trustworthy",
                remediation="Investigate rollbacks, tighten flip/e-process gates",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=90.0,
            )
        )
    return (
        score,
        Evidence(
            "process-acceptor-trust",
            "measured",
            f"falseAcceptRate={rate:.3f} resolved={resolved} mode={mode} watches={watch.watches}",
        ),
        findings,
    )


def _score_confirm(
    genesis: GenesisWindow, health: dict[str, Any], watch: WatchWindow
) -> tuple[float, Evidence, list[Finding]]:
    findings: list[Finding] = []
    confirmed, rolled, resolved, mode = _resolution(genesis, health, watch)
    if resolved < MIN_RESOLVED_FOR_HARD and mode == "none":
        return (
            UNMEASURED_RATE_FLOOR,
            Evidence(
                "process-confirm-honesty",
                "bootstrap",
                f"resolved={resolved} < {MIN_RESOLVED_FOR_HARD}; unmeasured (CoVerRL)",
                required=False,
            ),
            findings,
        )
    rate = confirmed / resolved if resolved else 0.0
    if mode == "hard" and health.get("confirm_rate") is not None:
        rate = float(health["confirm_rate"])
    score = grade_rate_high_good(rate, soft=0.55, hard=0.15)
    if mode == "soft":
        score = min(score, SOFT_RESOLVE_SCORE_CAP)
    if mode == "hard" and rate < 0.30:
        findings.append(
            Finding(
                id=stable_id("confirm-honesty", f"{rate:.3f}"),
                domain="process",
                pillar="confirm-honesty",
                severity="medium",
                path="skill_genesis_log.jsonl",
                evidence=f"confirmRate={rate:.3f} resolved={resolved}",
                why="CoVerRL: confirm rate below stall band",
                remediation="Reduce false accepts; raise post-evolve confirmation density",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=70.0,
            )
        )
    return (
        score,
        Evidence(
            "process-confirm-honesty",
            "measured",
            f"confirmRate={rate:.3f} resolved={resolved} mode={mode}",
        ),
        findings,
    )


def _score_judge(judge: JudgeWindow) -> tuple[float, Evidence, list[Finding]]:
    if judge.runs <= 0 or judge.accuracy is None:
        return (
            BOOTSTRAP["judge-fuel"],
            Evidence("process-judge-fuel", "bootstrap", "no judge_accuracy runs in window", required=False),
            [],
        )
    acc_score = 100.0 * judge.accuracy
    density = clamp(100.0 * judge.runs / 4.0)
    miss_penalty = clamp(2.0 * judge.misses)
    fr_bonus = min(15.0, 5.0 * judge.false_rejects)  # CoEvoSkills fuel present
    label_term = 40.0 if judge.operator_verdicts == 0 else min(100.0, 40.0 + 15.0 * judge.operator_verdicts)
    score = clamp(0.50 * acc_score + 0.25 * density + 0.15 * label_term + 0.10 * (40.0 + fr_bonus) - miss_penalty)
    if judge.operator_verdicts == 0:
        score = min(score, 50.0)
    if judge.operator_verdicts == 0 and judge.accuracy >= 0.99:
        score = min(score, 45.0)
    findings: list[Finding] = []
    chronic = judge.chronic_miss
    if judge.misses >= 3:
        chronic_note = (
            f"; chronic '{chronic[0]}' re-missed in {chronic[1]} runs" if chronic and chronic[1] > 1 else ""
        )
        findings.append(
            Finding(
                id=stable_id("judge-fuel", "misses", judge.misses),
                domain="process",
                pillar="judge-fuel",
                severity="medium",
                path="judge_accuracy_log.jsonl",
                evidence=(
                    f"distinct misses={judge.misses} (events={judge.miss_events}) "
                    f"accuracy={judge.accuracy:.3f}{chronic_note}"
                ),
                why="BabelJudge/CoEvoSkills: planted-defect misses are verifier co-evolution fuel — and a live weakness",
                remediation="Escalate probe curriculum; feed organic false-accept labels into evaluator epoch",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=68.0,
            )
        )
    chronic_detail = f" chronic={chronic[0]}×{chronic[1]}" if chronic and chronic[1] > 1 else ""
    return (
        score,
        Evidence(
            "process-judge-fuel",
            "measured",
            f"runs={judge.runs} accuracy={judge.accuracy:.3f} misses={judge.misses} "
            f"miss_events={judge.miss_events}{chronic_detail} "
            f"false_rejects={judge.false_rejects} operator_verdicts={judge.operator_verdicts}",
        ),
        findings,
    )


def _score_swap_consistency(judge: JudgeWindow) -> tuple[float, Evidence, list[Finding]]:
    """BabelJudge order/swap consistency proxy: run-to-run byClass rate stability."""
    runs = judge.class_rate_runs
    if len(runs) < 2:
        return (
            BOOTSTRAP["swap-consistency"],
            Evidence(
                "process-swap-consistency",
                "bootstrap",
                f"need ≥2 judge runs with byClass; have {len(runs)}",
                required=False,
            ),
            [],
        )
    agreements: list[float] = []
    for left, right in zip(runs, runs[1:]):
        common = set(left) & set(right)
        if not common:
            continue
        ok = sum(1 for key in common if abs(left[key] - right[key]) <= SWAP_RATE_TOLERANCE)
        agreements.append(ok / len(common))
    if not agreements:
        return (
            BOOTSTRAP["swap-consistency"],
            Evidence("process-swap-consistency", "bootstrap", "no overlapping byClass between runs"),
            [],
        )
    mean_agree = sum(agreements) / len(agreements)
    score = clamp(100.0 * mean_agree)
    # Proxy only until literal BabelJudge KO/EN order-swap corpus lands.
    score = min(score, SWAP_CONSISTENCY_PROXY_CAP)
    if mean_agree >= 0.97:
        score = min(score, SWAP_CONSISTENCY_SATURATED_CAP)
    findings: list[Finding] = []
    if mean_agree < 0.80:
        findings.append(
            Finding(
                id=stable_id("swap-consistency", f"{mean_agree:.3f}"),
                domain="process",
                pillar="swap-consistency",
                severity="medium",
                path="judge_accuracy_log.jsonl",
                evidence=f"run_to_run_agreement={mean_agree:.3f} pairs={len(agreements)}",
                why="BabelJudge: unstable byClass rates across runs imply order/framing sensitivity",
                remediation="Add held-out order-swap probes and calibrate judge on disagreements",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=66.0,
            )
        )
    return (
        score,
        Evidence(
            "process-swap-consistency",
            "measured",
            f"agreement={mean_agree:.3f} run_pairs={len(agreements)} tol={SWAP_RATE_TOLERANCE}",
        ),
        findings,
    )


def _score_preference_collapse(judge: JudgeWindow) -> tuple[float, Evidence, list[Finding]]:
    """BabelJudge preference-collapse: category accuracy spread."""
    rates: list[float] = []
    for _name, pair in judge.by_category.items():
        correct, total = pair[0], pair[1]
        if total >= 3:
            rates.append(correct / total)
    if len(rates) < 2:
        return (
            BOOTSTRAP["preference-collapse"],
            Evidence(
                "process-preference-collapse",
                "bootstrap",
                f"categories_with_n>=3: {len(rates)}",
                required=False,
            ),
            [],
        )
    mean = sum(rates) / len(rates)
    var = sum((r - mean) ** 2 for r in rates) / len(rates)
    # Perfect flat high accuracy → good; large spread → collapse risk.
    spread = var**0.5
    score = clamp(100.0 * (1.0 - spread / 0.25))
    if mean >= 0.98:
        # Saturated probe corpus across categories — honest mid score.
        score = min(score, 52.0)
    return (
        score,
        Evidence(
            "process-preference-collapse",
            "measured",
            f"categories={len(rates)} mean={mean:.3f} stdev={spread:.3f}",
        ),
        [],
    )


def _score_probe_coverage(judge: JudgeWindow) -> tuple[float, Evidence, list[Finding]]:
    """BabelJudge probe-class coverage proxy (stand-in until KO/EN swap corpus)."""
    n = len(judge.by_class)
    if n <= 0:
        return (
            BOOTSTRAP["probe-coverage"],
            Evidence("process-probe-coverage", "bootstrap", "no byClass data", required=False),
            [],
        )
    # Design target: ≥8 degradation classes exercised; saturated coverage
    # without operator labels is mid, not 100.
    score = clamp(100.0 * n / 8.0)
    if n >= 8:
        score = min(score, 60.0)
    return (
        score,
        Evidence("process-probe-coverage", "measured", f"byClass={n} classes={sorted(judge.by_class)}"),
        [],
    )


def _score_impact_first(window: ImpactFirstWindow) -> tuple[float, Evidence, list[Finding]]:
    """Did coding runs check the dependency graph BEFORE their first source edit?

    Scored, not advisory, and deliberately so. The failure this guards is a
    discipline that decays silently: an agent that reads whole files instead of
    the reached symbols still produces plausible diffs, so nothing in the loop
    notices until a change lands on a caller nobody looked at. Prompt text
    cannot hold a habit the scoreboard does not price.

    The rate is over runs that ACTUALLY edited source — a research turn that
    never touched code is not a missed impact check.
    """
    n = window.edit_runs
    if n < MIN_RESOLVED_FOR_HARD:
        return (
            BOOTSTRAP["impact-first"],
            Evidence(
                "process-impact-first",
                "bootstrap",
                f"editRuns={n} (<{MIN_RESOLVED_FOR_HARD}) lanes={sorted(window.lanes_seen) or 'none'}",
                required=False,
            ),
            [],
        )
    rate = window.rate or 0.0
    score = clamp(100.0 * rate)
    thin = n < IMPACT_FIRST_THIN_SAMPLE
    if thin:
        score = min(score, IMPACT_FIRST_THIN_CAP)
    detail = (
        f"impactFirst={window.impact_first}/{n} rate={rate:.2f} "
        f"late={window.impact_late} runtime={window.runtime_edit_runs} "
        f"dispatch={window.dispatch_edit_runs}" + (" thin-sample-cap" if thin else "")
    )
    findings: list[Finding] = []
    if rate < 0.5:
        findings.append(
            Finding(
                id=stable_id("impact-first", "below-half"),
                domain="process",
                pillar="impact-first",
                severity="medium" if rate > 0.2 else "high",
                path="scripts/dev/impact_brief.py",
                evidence=detail,
                why=(
                    "Source edits are landing without a blast-radius check; the "
                    "editing agent is working from whole-file reads, not from the "
                    "symbols the change reaches"
                ),
                remediation=(
                    "Verify the supply half before the discipline half: codegraph "
                    "tools in the implementer allow-list, a seeded .codegraph in the "
                    "dispatch worktree, then the contract's impact-first step"
                ),
                verify="python3 -c 'from rsi_bench.impact_first import load_impact_first_window as f; print(f().to_dict())'",
                priority=70.0,
            )
        )
    if window.runtime_edit_runs == 0 and window.dispatch_edit_runs > 0:
        findings.append(
            Finding(
                id=stable_id("impact-first", "runtime-lane-silent"),
                domain="process",
                pillar="impact-first",
                severity="low",
                path="gateway-go/internal/pipeline/toolpreset/preset.go",
                evidence=f"runtime editRuns=0, dispatch editRuns={window.dispatch_edit_runs}",
                why="Only the L4 dispatch lane is observable; runtime source edits leave no measured sample",
                remediation="Confirm implementer sub-agents are actually dispatched for code work",
                verify="grep -c 'turn.tool' ~/.deneb/agent-logs/*.jsonl",
                priority=35.0,
            )
        )
    return score, Evidence("process-impact-first", "measured", detail), findings


def _score_timescale(
    genesis: GenesisWindow, meta: MetaWindow, health: dict[str, Any]
) -> tuple[float, Evidence, list[Finding]]:
    evolves = int(health.get("evolves_7d", genesis.evolves) or genesis.evolves)
    genesis_n = int(health.get("genesis_7d", genesis.genesis) or genesis.genesis)
    meta_n = int(health.get("meta_revisions_7d", meta.revisions) or meta.revisions)
    l1 = evolves + genesis_n
    l2 = meta_n
    findings: list[Finding] = []
    if l1 <= 0 and l2 <= 0:
        score, status, detail = BOOTSTRAP["timescale-turn"], "bootstrap", "no L1/L2 activity in 7d"
    elif l1 > 0 and l2 > 0:
        volume = 12.0 * (min(l1, 5) / 5.0) + 10.0 * (min(l2, 3) / 3.0)
        score, status, detail = clamp(28.0 + volume), "measured", f"L1={l1} L2={l2}"
    else:
        score, status, detail = clamp(22.0 + 5.0 * min(l1 + l2, 4)), "measured", f"partial turn L1={l1} L2={l2}"
    if l1 <= 0:
        findings.append(
            Finding(
                id=stable_id("timescale-turn", "l1-starved"),
                domain="process",
                pillar="timescale-turn",
                severity="medium",
                path="skill_genesis_log.jsonl",
                evidence=detail,
                why="MetaSkill-Evolve: fast timescale starved",
                remediation="Raise demand (curriculum) and ensure evolve underperformers fires",
                verify="python3 scripts/audit/rsi-status.py",
                priority=72.0,
            )
        )
    return score, Evidence("process-timescale-turn", status, detail), findings


def _score_ability_transfer(
    transfer: TransferWindow, genesis: GenesisWindow, health: dict[str, Any]
) -> tuple[float, Evidence, list[Finding]]:
    """EvoAgentBench-style: evolved/genesis skills backed by validation cases + diversity."""
    findings: list[Finding] = []
    if transfer.validation_skills <= 0 and transfer.evolved_skills <= 0:
        return (
            BOOTSTRAP["ability-transfer"],
            Evidence("process-ability-transfer", "bootstrap", "no validation/evolve skill sets", required=False),
            findings,
        )
    evolve_cover = (
        transfer.evolved_with_cases / transfer.evolved_skills if transfer.evolved_skills else 0.0
    )
    genesis_cover = (
        transfer.genesis_with_cases / transfer.genesis_skills if transfer.genesis_skills else 0.0
    )
    evolves = int(health.get("evolves_7d", transfer.evolves7d) or transfer.evolves7d or genesis.evolves)
    distinct = int(
        health.get("distinct_skills_evolved_7d")
        or transfer.distinct_evolved7d
        or len(genesis.per_skill)
        or 0
    )
    diversity = (distinct / max(evolves, 1)) if evolves > 0 else 0.0
    opp_rate = (
        transfer.opportunity_actionable / transfer.opportunities if transfer.opportunities else 0.0
    )
    # Case coverage dominates; diversity and actionable opportunities are secondary.
    score = clamp(
        100.0
        * (0.45 * evolve_cover + 0.25 * genesis_cover + 0.20 * diversity + 0.10 * opp_rate)
    )
    # Proxy until EvoAgentBench ability-graph edges exist.
    score = min(score, ABILITY_TRANSFER_PROXY_CAP)
    if transfer.evolved_skills > 0 and evolve_cover < 0.5:
        findings.append(
            Finding(
                id=stable_id("ability-transfer", f"{evolve_cover:.2f}"),
                domain="process",
                pillar="ability-transfer",
                severity="medium",
                path="skill_validation_cases.jsonl",
                evidence=(
                    f"evolved_with_cases={transfer.evolved_with_cases}/{transfer.evolved_skills} "
                    f"genesis={transfer.genesis_with_cases}/{transfer.genesis_skills} "
                    f"diversity={diversity:.2f}"
                ),
                why="EvoAgentBench: evolves without held-out cases do not transfer verifiably",
                remediation="Author validation cases at evolve commit (reproduction oracle)",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=70.0,
            )
        )
    return (
        score,
        Evidence(
            "process-ability-transfer",
            "measured",
            f"evolve_cover={evolve_cover:.2f} genesis_cover={genesis_cover:.2f} "
            f"diversity={diversity:.2f} opp_actionable={transfer.opportunity_actionable}/"
            f"{transfer.opportunities} validation_skills={transfer.validation_skills}",
        ),
        findings,
    )


def _score_anti_collapse(
    genesis: GenesisWindow, meta: MetaWindow, health: dict[str, Any]
) -> tuple[float, Evidence, list[Finding]]:
    thrash = bool(health.get("thrash", genesis.thrash))
    score = 42.0 if not thrash else 12.0
    streak = meta.consecutive_parametric_adoptions
    if streak >= 3:
        score -= 15.0
    if meta.structural > 0:
        score += 6.0
    score = clamp(score)
    findings: list[Finding] = []
    if thrash:
        top, count = genesis.top_skill
        findings.append(
            Finding(
                id=stable_id("anti-collapse", "thrash", top),
                domain="process",
                pillar="anti-collapse",
                severity="high",
                path="skill_genesis_log.jsonl",
                evidence=f"thrash on {top} count={count}",
                why="Thrash collapses diversity — loop edits the same skill",
                remediation="Honor thrash cooldown; diversify underperformer selection",
                verify="python3 scripts/audit/rsi-bench.py --format json",
                priority=88.0,
            )
        )
    if streak >= 3:
        findings.append(
            Finding(
                id=stable_id("anti-collapse", "parametric-streak", streak),
                domain="process",
                pillar="anti-collapse",
                severity="medium",
                path="meta_evolution_log.jsonl",
                evidence=f"consecutive_parametric_adoptions={streak}",
                why="Bilevel Autoresearch: parametric streak is the L1.5 null regime",
                remediation="Nudge producer epoch toward structural candidates",
                verify="python3 scripts/audit/rsi-status.py",
                priority=60.0,
            )
        )
    return (
        score,
        Evidence(
            "process-anti-collapse",
            "measured",
            f"thrash={thrash} parametric_streak={streak} structural={meta.structural}",
        ),
        findings,
    )


def evaluate_process(
    root: Path,
    *,
    cache: dict[str, Any] | None = None,
    data: Path | None = None,
) -> Domain:
    data_path = data or data_dir()
    health = self_evolution_from_cache(cache)
    if cache and isinstance(cache.get("rsi_status"), dict):
        rsi_health = cache["rsi_status"].get("health")
        if isinstance(rsi_health, dict) and not health:
            health = rsi_health

    genesis = _merge_health(load_genesis_window(data_path), health)
    judge = load_judge_window(data_path)
    meta = load_meta_window(data_path)
    watch = load_watch_window(data_path)
    transfer = load_transfer_window(data_path)
    impact = load_impact_first_window(data=data_path)

    scores = [
        ("acceptor-trust", "Acceptor trust", 16, *_score_acceptor(genesis, health, watch),
         "PACE/SEA false-accept honesty (hard lifecycle or soft watch)"),
        ("confirm-honesty", "Confirm honesty", 10, *_score_confirm(genesis, health, watch),
         "CoVerRL confirmRate with resolved n"),
        ("judge-fuel", "Judge fuel", 12, *_score_judge(judge),
         "CoEvoSkills/BabelJudge accuracy + misses + falseRejects"),
        ("preference-collapse", "Preference collapse", 8, *_score_preference_collapse(judge),
         "BabelJudge byCategory accuracy spread"),
        ("swap-consistency", "Swap consistency", 7, *_score_swap_consistency(judge),
         "BabelJudge run-to-run byClass stability (order/swap proxy)"),
        ("probe-coverage", "Probe coverage", 4, *_score_probe_coverage(judge),
         "BabelJudge byClass coverage breadth"),
        ("timescale-turn", "Timescale turn", 12, *_score_timescale(genesis, meta, health),
         "MetaSkill-Evolve L1+L2 activity"),
        ("ability-transfer", "Ability transfer", 9, *_score_ability_transfer(transfer, genesis, health),
         "EvoAgentBench validation∩evolve coverage + diversity"),
        ("anti-collapse", "Anti-collapse", 14, *_score_anti_collapse(genesis, meta, health),
         "Thrash off + parametric streak penalty"),
        ("impact-first", "Impact first", 8, *_score_impact_first(impact),
         "Dependency-graph check before the first source edit (both coding lanes)"),
    ]
    # weights: 16+10+12+8+7+4+12+9+14+8 = 100. The 8 points for impact-first come
    # off the three proxy-ceilinged metrics (swap-consistency, probe-coverage,
    # ability-transfer): each is a stand-in until its dedicated corpus lands and
    # is capped below 60 anyway, while impact-first reads real per-run artifacts.
    metrics: list[Metric] = []
    evidence: list[Evidence] = []
    for mid, title, weight, score, ev, findings, intent in scores:
        metrics.append(Metric(mid, title, weight, score, intent, {}, findings))
        evidence.append(ev)

    return Domain(
        id="process",
        title="Process",
        weight=DOMAIN_WEIGHTS["process"],
        metrics=metrics,
        evidence=evidence,
        ratcheted=True,
    )
