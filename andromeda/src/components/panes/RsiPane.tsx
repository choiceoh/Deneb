import { useEffect, useMemo, useState } from "react";
import type {
  RSIHealthView,
  RSILayerView,
  RSILoopStatusResponse,
  SelfCorrectionCandidate,
  SelfImprovementCodingListResponse,
  SkillLifecycleEvent,
  SkillsLifecycleResponse,
} from "@/types";
import { callRpc } from "@/gateway";
import { RSI_RPC } from "@/resources";
import { serializeList } from "@/aiText";
import { errText } from "@/format";
import { color, line, pane } from "@/theme";
import { Detail, Modal } from "@/components/Modal";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

// 재귀적 자가개선 (recursive self-improvement) loop status — the desktop window onto
// the four RSI layers the gateway computes (miniapp.rsi.status): L1 skill evolution,
// L2 meta-evolution, L3 verifier co-evolution, L4 source self-edit. Each layer shows
// an honest state, a one-line diagnosis, and key metrics. Complements the 스킬 pane's
// Propus feed (which is L1 lifecycle events) with the whole-system loop-health view.

// State → badge color so the loop taxonomy separates at a glance: LIVE = turning
// (online), DATA-GATED = waiting for data (accent), STARVED = input gap (danger),
// FROZEN = self-braked (text2), IDLE = dormant (muted). DATA-GATED ≠ STARVED is the
// whole point — one is normal, the other is actionable.
function stateColor(state: string | undefined): string {
  switch (state) {
    case "LIVE":
      return color.online;
    case "STARVED":
      return color.danger;
    case "DATA-GATED":
      return color.accent;
    case "FROZEN":
      return color.text2;
    default:
      return color.muted; // IDLE / unknown
  }
}

// State → Korean badge label (the app is Korean-first; the enum stays English for
// color logic).
function stateLabel(state: string | undefined): string {
  switch (state) {
    case "LIVE":
      return "가동 중";
    case "DATA-GATED":
      return "데이터 대기";
    case "STARVED":
      return "연료 부족";
    case "FROZEN":
      return "동결";
    default:
      return "휴면";
  }
}

function LayerCard({
  layer,
  lifecycle,
  candidates,
  health,
  onOpenCandidate,
}: {
  layer: RSILayerView;
  lifecycle: SkillLifecycleEvent[];
  candidates: SelfCorrectionCandidate[];
  health: RSIHealthView;
  onOpenCandidate: (c: SelfCorrectionCandidate) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const tint = stateColor(layer.state);
  const hasDetail = !!layer.detail?.trim();
  return (
    <div style={{ border: line, borderRadius: 8, padding: "12px 14px", marginBottom: 12 }}>
      <div
        onClick={hasDetail ? () => setExpanded((e) => !e) : undefined}
        style={{ display: "flex", alignItems: "center", gap: 8, cursor: hasDetail ? "pointer" : "default" }}
      >
        <span style={{ fontWeight: 600, fontSize: 14, color: color.text, flex: 1 }}>
          {layer.key ?? ""} · {layer.title ?? ""}
        </span>
        <span
          style={{
            fontSize: 11,
            fontWeight: 600,
            padding: "2px 8px",
            borderRadius: 5,
            border: `1px solid ${tint}`,
            color: tint,
            whiteSpace: "nowrap",
          }}
        >
          {stateLabel(layer.state)}
        </span>
        {hasDetail && <span style={{ fontSize: 11, color: color.muted }}>{expanded ? "⌃" : "⌄"}</span>}
      </div>
      {layer.diagnosis && (
        <div style={{ fontSize: 12.5, color: color.text2, marginTop: 6, lineHeight: 1.5 }}>{layer.diagnosis}</div>
      )}
      {layer.metrics && layer.metrics.length > 0 && (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "8px 20px", marginTop: 10 }}>
          {layer.metrics.map((m, i) => (
            <div key={i}>
              <div style={{ fontSize: 15, fontWeight: 600, color: color.text }}>{m.value || "—"}</div>
              <div style={{ fontSize: 11, color: color.muted }}>{m.label ?? ""}</div>
            </div>
          ))}
        </div>
      )}
      {expanded && hasDetail && (
        <div
          style={{ fontSize: 12.5, color: color.text, marginTop: 10, paddingTop: 10, borderTop: line, lineHeight: 1.6 }}
        >
          {layer.detail}
        </div>
      )}
      {expanded && layer.key === "L1" && (
        <RsiDrill
          header="최근 스킬 생애"
          emptyText="최근 스킬 진화 이벤트 없음"
          rows={lifecycle.slice(0, 6).map((e): [string, string] => [eventTypeLabel(e.type), eventText(e)])}
        />
      )}
      {expanded && layer.key === "L2" && (
        <RsiDrill
          header="메타 진화 현황"
          emptyText="메타 진화 데이터 없음"
          rows={[
            ["개정", `${health.metaRevisions7d ?? 0}건 (7일)`],
            ["자동채택", health.autoAdoptFrozen ? "동결 — 드리프트 자기 브레이크 작동" : "정상"],
          ]}
        />
      )}
      {expanded && layer.key === "L3" && (
        <RsiDrill
          header="판정자 공진화 현황"
          emptyText="판정 데이터 없음"
          rows={[
            [
              "오수용률",
              `${(health.resolvedEvolves7d ?? 0) > 0 ? pct(health.falseAcceptRate) : "—"} (표본 ${health.resolvedEvolves7d ?? 0})`,
            ],
            ["롤백", `${health.rolledBack7d ?? 0}건 (7일)`],
          ]}
        />
      )}
      {expanded && layer.key === "L4" && <RsiCandidateDrill candidates={candidates} onOpen={onOpenCandidate} />}
    </div>
  );
}

// L4 drill: the coding queue rendered as clickable rows — status + provenance +
// dispatch-track chips at a glance, click opens the full candidate detail.
function RsiCandidateDrill({
  candidates,
  onOpen,
}: {
  candidates: SelfCorrectionCandidate[];
  onOpen: (c: SelfCorrectionCandidate) => void;
}) {
  return (
    <div style={{ marginTop: 10, paddingTop: 10, borderTop: line }}>
      <div style={{ fontSize: 11, fontWeight: 600, color: color.muted, marginBottom: 6 }}>대기 중 코딩 후보</div>
      {candidates.length === 0 ? (
        <div style={{ fontSize: 12, color: color.muted }}>대기 중인 코딩 후보 없음</div>
      ) : (
        candidates.slice(0, 6).map((c, i) => {
          const src = sourceLabel(c.source);
          return (
            <div
              key={c.id ?? `cand-${i}`}
              role="button"
              tabIndex={0}
              onClick={() => onOpen(c)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onOpen(c);
                }
              }}
              style={{
                display: "flex",
                flexWrap: "wrap",
                gap: 6,
                alignItems: "baseline",
                padding: "4px 0",
                cursor: "pointer",
              }}
            >
              <span style={{ fontSize: 11, color: color.muted, minWidth: 34 }}>{candidateStatusLabel(c.status)}</span>
              <span
                style={{
                  fontSize: 12.5,
                  color: color.text,
                  flex: 1,
                  minWidth: 120,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {c.title || c.candidate || "—"}
              </span>
              {src && <Chip text={src} />}
              {c.scope === "code" && <Chip text={c.autoDispatch ? "자동수리" : "검토 대기"} accent={c.autoDispatch} />}
            </div>
          );
        })
      )}
    </div>
  );
}

// A compact "recent detail" list inside an expanded layer card — the drill-in
// that folds the Propus feed / coding queue into the hub.
function RsiDrill({ header, rows, emptyText }: { header: string; rows: Array<[string, string]>; emptyText: string }) {
  return (
    <div style={{ marginTop: 10, paddingTop: 10, borderTop: line }}>
      <div style={{ fontSize: 11, fontWeight: 600, color: color.muted, marginBottom: 6 }}>{header}</div>
      {rows.length === 0 ? (
        <div style={{ fontSize: 12, color: color.muted }}>{emptyText}</div>
      ) : (
        rows.map(([label, text], i) => (
          <div key={i} style={{ display: "flex", gap: 8, padding: "2px 0", alignItems: "baseline" }}>
            <span style={{ fontSize: 11, color: color.muted, minWidth: 34 }}>{label}</span>
            <span
              style={{
                fontSize: 12.5,
                color: color.text,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                flex: 1,
              }}
            >
              {text || "—"}
            </span>
          </div>
        ))
      )}
    </div>
  );
}

function eventTypeLabel(type?: string): string {
  switch (type) {
    case "evolved":
      return "진화";
    case "genesis":
      return "생성";
    case "evolve_rejected":
      return "기각";
    case "evolution_proposal":
      return "제안";
    case "confirmed":
      return "확정";
    case "rolled_back":
      return "롤백";
    default:
      return type ?? "";
  }
}

function eventText(e: SkillLifecycleEvent): string {
  const name = e.skillName?.trim() || "(전역)";
  return e.detail?.trim() ? `${name} · ${e.detail}` : name;
}

function candidateStatusLabel(status?: string): string {
  switch (status) {
    case "proposed":
      return "제안";
    case "accepted":
      return "채택";
    case "applied":
      return "적용";
    case "rejected":
      return "기각";
    case "superseded":
      return "대체";
    default:
      return status || "제안";
  }
}

// Maps a candidate `source` namespace to a short Korean label. Suffix-aware for
// tool-quality (:desc description / :latency perf). Keep in sync with the miner
// source prefixes (scripts/audit/*_miner.py, genesis L4 sources).
function sourceLabel(source?: string): string | null {
  const s = (source ?? "").trim();
  if (!s) return null;
  if (s.startsWith("runtime-error")) return "런타임 오류";
  if (s.startsWith("health-finding:runtime-")) return "런타임 건강";
  if (s.startsWith("health-finding")) return "코드 건강";
  if (s.startsWith("deadcode-finding")) return "죽은 코드";
  if (s.startsWith("tool-quality") && s.endsWith(":latency")) return "도구 지연";
  if (s.startsWith("tool-quality")) return "도구 설명";
  if (s.startsWith("evolve-tool-gap")) return "도구 갭";
  if (s.startsWith("self-harness")) return "하네스";
  if (s.startsWith("sop-mining")) return "SOP";
  return s.split(":")[0] || s;
}

function pct(v?: number): string {
  return `${Math.round((v ?? 0) * 100)}%`;
}

// Small pill: neutral (bordered) for provenance, or warm accent (soft fill) for
// the auto-dispatch track / self-brake flags — a paused or auto-acting loop must
// read at a glance.
function Chip({ text, accent }: { text: string; accent?: boolean }) {
  return (
    <span
      style={
        accent
          ? {
              fontSize: 11,
              fontWeight: 600,
              padding: "2px 8px",
              borderRadius: 5,
              background: color.active,
              color: color.accent,
              whiteSpace: "nowrap",
            }
          : {
              fontSize: 11,
              padding: "1px 7px",
              borderRadius: 5,
              border: line,
              color: color.muted,
              whiteSpace: "nowrap",
            }
      }
    >
      {text}
    </span>
  );
}

// Evolution-health scoreboard (7-day) from rsi.status.health: the numeric fields
// the layer diagnoses only render as prose. Hidden entirely when nothing has
// happened (the layer cards already say IDLE). Self-brake flags surface as warm
// accent chips.
function HealthCard({ health }: { health: RSIHealthView }) {
  // Outcome-only weeks matter too: an evolve confirmed / rolled back this window
  // (resolvedEvolves7d) is activity even when no new evolve/genesis started.
  const resolved = health.resolvedEvolves7d ?? 0;
  const active =
    (health.evolves7d ?? 0) > 0 ||
    (health.genesis7d ?? 0) > 0 ||
    (health.metaRevisions7d ?? 0) > 0 ||
    (health.confirmed7d ?? 0) > 0 ||
    (health.rolledBack7d ?? 0) > 0 ||
    (health.rejected7d ?? 0) > 0 ||
    resolved > 0 ||
    !!health.thrash ||
    !!health.autoAdoptFrozen;
  if (!active) return null;
  // Rates are undefined with no resolved sample — show "—", not a misleading 0%
  // that reads as a failed confirmation while activity is still pending.
  const rate = (v?: number) => (resolved > 0 ? pct(v) : "—");
  const stats: Array<[string, string, string?]> = [
    ["확정률", rate(health.confirmRate)],
    ["오수용률", rate(health.falseAcceptRate), `n=${resolved}`],
    ["진화", String(health.evolves7d ?? 0)],
    ["확정", String(health.confirmed7d ?? 0)],
    ["롤백", String(health.rolledBack7d ?? 0)],
    ["신규 스킬", String(health.genesis7d ?? 0)],
    ["메타 개정", String(health.metaRevisions7d ?? 0)],
  ];
  return (
    <div style={{ border: line, borderRadius: 8, padding: "12px 14px", marginBottom: 12 }}>
      <div style={{ fontSize: 11, fontWeight: 600, color: color.muted, marginBottom: 10 }}>진화 건강 (7일)</div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: "10px 22px" }}>
        {stats.map(([label, value, sub], i) => (
          <div key={i}>
            <div style={{ fontSize: 15, fontWeight: 600, color: color.text }}>{value}</div>
            <div style={{ fontSize: 11, color: color.muted }}>{label}</div>
            {sub && <div style={{ fontSize: 11, color: color.muted }}>{sub}</div>}
          </div>
        ))}
      </div>
      {(health.autoAdoptFrozen || health.thrash) && (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 10 }}>
          {health.autoAdoptFrozen && <Chip text="메타 자동채택 동결" accent />}
          {health.thrash && <Chip text="진화 쓰래싱" accent />}
        </div>
      )}
    </div>
  );
}

// Read-only detail for one coding candidate (the L4 drill row → this modal): the
// rich fields the drill row can't show — provenance, dispatch track, evidence,
// proposed change, risk, target files.
function CandidateModal({ candidate, onClose }: { candidate: SelfCorrectionCandidate; onClose: () => void }) {
  const src = sourceLabel(candidate.source);
  const track =
    candidate.scope === "code" ? (candidate.autoDispatch ? "자동수리 (졸업 소스)" : "검토 대기 (스테이지)") : null;
  return (
    <Modal title={candidate.title || candidate.candidate || "코딩 후보"} onClose={onClose} width={560}>
      <Detail label="상태" value={candidateStatusLabel(candidate.status)} />
      {src && <Detail label="출처" value={`${src}${candidate.source ? ` · ${candidate.source}` : ""}`} />}
      {track && <Detail label="처리 방식" value={track} />}
      {candidate.proposedChange && <Detail label="제안 변경" value={candidate.proposedChange} multiline />}
      {candidate.candidate && <Detail label="관찰" value={candidate.candidate} multiline />}
      {candidate.evidence && <Detail label="근거" value={candidate.evidence} multiline />}
      {candidate.risk && <Detail label="리스크" value={candidate.risk} multiline />}
      {candidate.targetFiles && candidate.targetFiles.length > 0 && (
        <Detail label="대상 파일" value={candidate.targetFiles.join(" · ")} multiline />
      )}
      {candidate.reviewNote && <Detail label="결과" value={candidate.reviewNote} multiline />}
    </Modal>
  );
}

export function RsiPane() {
  const { cfg, connected } = useWorkspace();
  const [data, setData] = useState<RSILoopStatusResponse | null>(null);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(true);
  // Drill-down detail folded into the hub: L1 → Propus lifecycle, L4 → coding queue.
  const [lifecycle, setLifecycle] = useState<SkillLifecycleEvent[]>([]);
  const [candidates, setCandidates] = useState<SelfCorrectionCandidate[]>([]);
  const [selected, setSelected] = useState<SelfCorrectionCandidate | null>(null);

  // Reset spinner/error on each (re)connect as a render-phase adjustment, so the
  // effect stays free of synchronous setState (mirrors SkillsPane's PropusFeed).
  const key = `${connected}|${cfg.url}|${cfg.token}`;
  const [prevKey, setPrevKey] = useState(key);
  if (prevKey !== key) {
    setPrevKey(key);
    if (connected) {
      setLoading(true);
      setErr("");
    }
  }

  useEffect(() => {
    if (!connected) return;
    void callRpc<RSILoopStatusResponse>(cfg, RSI_RPC.status, {})
      .then((d) => {
        setData(d);
        setErr("");
      })
      .catch((e) => setErr(errText(e)))
      .finally(() => setLoading(false));
    // Best-effort drill data — a failure here leaves the overview intact.
    void callRpc<SkillsLifecycleResponse>(cfg, RSI_RPC.lifecycle, { limit: 12 })
      .then((d) => setLifecycle(d.events ?? []))
      .catch(() => setLifecycle([]));
    void Promise.all([
      // translate: the review models answer in English (measured 2026-09-03: 78%
      // of live queue titles carried no Hangul). Opt-in so the dispatch selector
      // and the L4 miners, which feed a coding agent its instructions, keep the
      // untranslated text.
      callRpc<SelfImprovementCodingListResponse>(cfg, RSI_RPC.coding, {
        limit: 24,
        status: "proposed",
        translate: true,
      }),
      callRpc<SelfImprovementCodingListResponse>(cfg, RSI_RPC.coding, {
        limit: 24,
        status: "accepted",
        translate: true,
      }),
    ])
      .then(([proposed, accepted]) => {
        // Fetch statuses separately so applied/rejected churn cannot crowd the
        // server-side limit before we filter (bot review #3612).
        const pending = [...(proposed.candidates ?? []), ...(accepted.candidates ?? [])]
          .filter((c) => !c.scope || c.scope === "code")
          .sort((a, b) => (b.updatedAt || b.createdAt || 0) - (a.updatedAt || a.createdAt || 0));
        const seen = new Set<string>();
        const deduped = pending.filter((c) => {
          const id = c.id || "";
          if (!id || seen.has(id)) return false;
          seen.add(id);
          return true;
        });
        setCandidates(deduped.slice(0, 8));
      })
      .catch(() => setCandidates([]));
  }, [cfg, connected]);

  const aiText = useMemo(
    () =>
      serializeList(
        "재귀적 자가개선 루프",
        data?.layers ?? [],
        (l) => `${l.key ?? ""} ${l.title ?? ""}: ${l.state ?? ""} — ${l.diagnosis ?? ""}`,
      ),
    [data],
  );
  useRegisterPane(undefined, aiText);

  const layers = data?.layers ?? [];
  return (
    <div style={pane}>
      {!connected ? (
        <p style={{ color: color.muted }}>미연결</p>
      ) : loading && !data ? (
        <p style={{ color: color.muted }}>불러오는 중…</p>
      ) : err ? (
        <p className="pane-error">자가개선 상태를 불러오지 못했습니다: {err}</p>
      ) : layers.length === 0 ? (
        <p style={{ color: color.muted }}>표시할 자가개선 상태가 없습니다.</p>
      ) : (
        <>
          <div style={{ fontSize: 13, color: color.muted, marginBottom: 12 }}>
            {data?.turning ?? 0}/{layers.length}개 루프 가동 중
          </div>
          {data?.health && <HealthCard health={data.health} />}
          {layers.map((l) => (
            <LayerCard
              key={l.key}
              layer={l}
              lifecycle={lifecycle}
              candidates={candidates}
              health={data?.health ?? {}}
              onOpenCandidate={setSelected}
            />
          ))}
          {selected && <CandidateModal candidate={selected} onClose={() => setSelected(null)} />}
        </>
      )}
    </div>
  );
}
