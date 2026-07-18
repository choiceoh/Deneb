import { useCallback, useEffect, useMemo, useState } from "react";
import { callRpc } from "@/gateway";
import { OBSERVE_RPC } from "@/resources";
import { errText, fmtDate } from "@/format";
import { color, line, pane } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

// Observation plane dashboard — Andromeda mirror of the native Settings「관찰」
// tab (ConfigObserveTab). Read-only operator view over miniapp.observe.*:
// cross-session behavior aggregate + recent warn/error logs. A flat 1일/7일
// switcher scopes both queries. Complements RsiPane (loop health) with
// runtime/tool telemetry.

interface ObserveToolStat {
  name?: string;
  calls?: number;
  errors?: number;
  avgMs?: number;
}

interface ObserveBehavior {
  runs?: number;
  proactiveRuns?: number;
  compactedRuns?: number;
  tools?: ObserveToolStat[];
}

interface ObserveLogLine {
  level?: string;
  msg?: string;
  runId?: string;
}

interface ObserveLogsPayload {
  lines?: ObserveLogLine[];
  count?: number;
}

// 워크스테이션 조종 도구의 사용 원장 — "화면 조종이 실제로 쓰이는가"의 접지 숫자.
interface WorkstationUsage {
  total?: number;
  byAction?: Record<string, number>;
  lastAt?: string;
}

const PERIODS: Array<{ label: string; days: number }> = [
  { label: "1일", days: 1 },
  { label: "7일", days: 7 },
];

function PeriodSwitcher({ days, onSelect }: { days: number; onSelect: (d: number) => void }) {
  return (
    <div style={{ display: "flex", gap: 18, borderBottom: line, marginBottom: 12 }}>
      {PERIODS.map((p) => (
        <button
          key={p.days}
          type="button"
          role="tab"
          aria-selected={days === p.days}
          onClick={() => {
            if (days !== p.days) onSelect(p.days);
          }}
          style={{
            background: "none",
            border: "none",
            cursor: "pointer",
            padding: "4px 0",
            fontSize: 14,
            fontWeight: days === p.days ? 600 : 400,
            color: days === p.days ? color.accent : color.muted,
          }}
        >
          {p.label}
        </button>
      ))}
    </div>
  );
}

function SectionHeader({ text }: { text: string }) {
  return (
    <div
      style={{
        fontSize: 11,
        fontWeight: 600,
        color: color.muted,
        letterSpacing: "0.04em",
        marginTop: 16,
        marginBottom: 6,
      }}
    >
      {text.toUpperCase()}
    </div>
  );
}

export function ObservePane() {
  const { cfg, connected } = useWorkspace();
  const [days, setDays] = useState(7);
  const [behavior, setBehavior] = useState<ObserveBehavior | null>(null);
  const [logs, setLogs] = useState<ObserveLogLine[]>([]);
  const [wsUsage, setWsUsage] = useState<WorkstationUsage | null>(null);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState("");
  const [reloadKey, setReloadKey] = useState(0);

  // Reset spinner/error on reconnect or period change as a render-phase
  // adjustment so the effect stays free of synchronous setState.
  const key = `${connected}|${cfg.url}|${cfg.token}|${days}|${reloadKey}`;
  const [prevKey, setPrevKey] = useState(key);
  if (prevKey !== key) {
    setPrevKey(key);
    if (connected) {
      setLoading(true);
      setErr("");
    }
  }

  const load = useCallback(() => {
    setReloadKey((k) => k + 1);
  }, []);

  useEffect(() => {
    if (!connected) return;
    let cancelled = false;
    void (async () => {
      const [bSettled, lSettled, uSettled] = await Promise.allSettled([
        callRpc<ObserveBehavior>(cfg, OBSERVE_RPC.behavior, { days }),
        callRpc<ObserveLogsPayload>(cfg, OBSERVE_RPC.logs, { level: "warn", limit: 40, days }),
        callRpc<WorkstationUsage>(cfg, OBSERVE_RPC.workstationUsage, {}),
      ]);
      if (cancelled) return;
      const bOk = bSettled.status === "fulfilled";
      const lOk = lSettled.status === "fulfilled";
      setBehavior(bOk ? bSettled.value : null);
      setLogs(lOk ? (lSettled.value.lines ?? []) : []);
      setWsUsage(uSettled.status === "fulfilled" ? uSettled.value : null);
      if (!bOk && !lOk) {
        const reason = bSettled.status === "rejected" ? errText(bSettled.reason) : errText(lSettled.reason);
        setErr(reason);
      } else {
        setErr("");
      }
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, [cfg, connected, days, reloadKey]);

  const tools = useMemo(() => behavior?.tools ?? [], [behavior]);
  const runs = behavior?.runs ?? 0;
  const empty = runs === 0 && logs.length === 0 && !err;

  const aiText = useMemo(() => {
    if (!connected) return "[관찰] 미연결";
    if (err) return `[관찰] 오류: ${err}`;
    if (loading && !behavior) return "[관찰] 불러오는 중…";
    const lines: string[] = [
      `[관찰] 최근 ${days}일 · 실행 ${runs}회 · 능동 ${behavior?.proactiveRuns ?? 0} · 압축 ${behavior?.compactedRuns ?? 0}`,
    ];
    for (const t of tools.slice(0, 8)) {
      const name = t.name?.trim() || "(도구)";
      const errN = t.errors ?? 0;
      lines.push(
        errN > 0
          ? `- ${name}: ${t.calls ?? 0}회 · ${errN} 오류 · 평균 ${t.avgMs ?? 0}ms`
          : `- ${name}: ${t.calls ?? 0}회 · 평균 ${t.avgMs ?? 0}ms`,
      );
    }
    for (const l of logs.slice(0, 5)) {
      lines.push(`- [${l.level ?? "?"}] ${l.msg ?? ""}`);
    }
    return lines.join("\n");
  }, [connected, err, loading, behavior, days, runs, tools, logs]);

  useRegisterPane(undefined, aiText);

  return (
    <div style={pane}>
      {!connected ? (
        <p style={{ color: color.muted }}>미연결</p>
      ) : (
        <>
          <PeriodSwitcher days={days} onSelect={setDays} />
          {loading && !behavior && !err ? (
            <p style={{ color: color.muted }}>불러오는 중…</p>
          ) : err ? (
            <div>
              <p className="pane-error">관찰 데이터를 불러오지 못했습니다: {err}</p>
              <button
                type="button"
                onClick={load}
                style={{
                  marginTop: 8,
                  background: "none",
                  border: line,
                  borderRadius: 6,
                  padding: "6px 12px",
                  cursor: "pointer",
                  color: color.accent,
                  fontSize: 13,
                }}
              >
                다시 시도
              </button>
            </div>
          ) : empty ? (
            <p style={{ color: color.muted }}>아직 관찰된 동작이 없습니다.</p>
          ) : (
            <>
              {behavior && (
                <div style={{ marginBottom: 4 }}>
                  <div style={{ fontSize: 14, fontWeight: 600, color: color.text }}>최근 {days}일 동작</div>
                  <div style={{ fontSize: 12.5, color: color.muted, marginTop: 4 }}>
                    실행 {runs}회 · 능동 {behavior.proactiveRuns ?? 0} · 압축 {behavior.compactedRuns ?? 0}
                  </div>
                </div>
              )}
              {wsUsage && (wsUsage.total ?? 0) > 0 && (
                <>
                  <SectionHeader text="워크스테이션 조종 (효용 접지)" />
                  <div style={{ padding: "10px 0", borderBottom: line }}>
                    <div style={{ fontSize: 13.5, color: color.text }}>
                      누적 {wsUsage.total}회{wsUsage.lastAt ? ` · 마지막 ${fmtDate(wsUsage.lastAt)}` : ""}
                    </div>
                    <div style={{ fontSize: 12, marginTop: 2, color: color.muted }}>
                      {Object.entries(wsUsage.byAction ?? {})
                        .sort((a, b) => b[1] - a[1])
                        .map(([k, v]) => `${k} ${v}`)
                        .join(" · ")}
                    </div>
                  </div>
                </>
              )}
              {tools.length > 0 && (
                <>
                  <SectionHeader text="도구 사용" />
                  {tools.map((t, i) => {
                    const hasErr = (t.errors ?? 0) > 0;
                    return (
                      <div
                        key={t.name ?? `tool-${i}`}
                        style={{
                          padding: "10px 0",
                          borderBottom: line,
                        }}
                      >
                        <div style={{ fontSize: 13.5, color: color.text }}>{t.name ?? ""}</div>
                        <div
                          style={{
                            fontSize: 12,
                            marginTop: 2,
                            color: hasErr ? color.danger : color.muted,
                          }}
                        >
                          {hasErr
                            ? `${t.calls ?? 0}회 · ${t.errors} 오류 · 평균 ${t.avgMs ?? 0}ms`
                            : `${t.calls ?? 0}회 · 평균 ${t.avgMs ?? 0}ms`}
                        </div>
                      </div>
                    );
                  })}
                </>
              )}
              {logs.length > 0 && (
                <>
                  <SectionHeader text="최근 경고 / 오류" />
                  {logs.map((l, i) => (
                    <div key={`${l.runId ?? ""}-${i}`} style={{ padding: "8px 0", borderBottom: line }}>
                      <div
                        style={{
                          fontSize: 11,
                          fontWeight: 600,
                          color: l.level === "ERROR" ? color.danger : color.muted,
                          marginBottom: 2,
                        }}
                      >
                        {l.level ?? ""}
                      </div>
                      <div
                        style={{
                          fontSize: 13,
                          color: color.text,
                          display: "-webkit-box",
                          WebkitLineClamp: 3,
                          WebkitBoxOrient: "vertical",
                          overflow: "hidden",
                        }}
                      >
                        {l.msg ?? ""}
                      </div>
                    </div>
                  ))}
                </>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}
