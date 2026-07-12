import { useEffect, useMemo, useState } from "react";
import type { RSILayerView, RSILoopStatusResponse } from "@/types";
import { callRpc } from "@/gateway";
import { RSI_RPC } from "@/resources";
import { serializeList } from "@/aiText";
import { errText } from "@/format";
import { color, line, pane } from "@/theme";
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

function LayerCard({ layer }: { layer: RSILayerView }) {
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
    </div>
  );
}

export function RsiPane() {
  const { cfg, connected } = useWorkspace();
  const [data, setData] = useState<RSILoopStatusResponse | null>(null);
  const [err, setErr] = useState("");
  const [loading, setLoading] = useState(true);

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
          {layers.map((l) => (
            <LayerCard key={l.key} layer={l} />
          ))}
        </>
      )}
    </div>
  );
}
