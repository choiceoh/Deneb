import { useState, type ReactNode } from "react";
import { saveConfig } from "@/gateway";
import { setString } from "@/storage";
import { useGatewayStatus } from "@/hooks";
import { type LogLevel, getLogLevel, setLogLevel } from "@/log";
import { isTauri } from "@/tauri";
import { checkForUpdates } from "@/updater";
import { errText } from "@/format";
import { muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import type { View } from "@/types";
import { LiveDot } from "@/components/LiveDot";
import { PANES, orderedViews } from "@/components/panes";

// 설정 — a full-screen settings section (not a modal), split into tabs:
// 연결(gateway) · 일반(좌측 탭 + 로그 레벨) · 정보(version/update). Edits the
// app-owned config via useWorkspace().setCfg (App persists + rebuilds providers).
const LOG_LEVELS: LogLevel[] = ["debug", "info", "warn", "error", "silent"];
const LOG_LABEL: Record<LogLevel, string> = {
  debug: "디버그",
  info: "정보",
  warn: "경고",
  error: "오류",
  silent: "끄기",
};

type TabKey = "connection" | "general" | "about";
const TABS: { key: TabKey; label: string }[] = [
  { key: "connection", label: "연결" },
  { key: "general", label: "일반" },
  { key: "about", label: "정보" },
];

const fieldLabel = {
  display: "grid",
  gap: 4,
  fontSize: 12,
  color: "var(--muted)",
  marginBottom: 10,
} as const;

export function SettingsPane() {
  const { connected, cfg, setCfg, hiddenViews, toggleViewHidden, viewOrder, setViewOrder } = useWorkspace();
  const { status, check } = useGatewayStatus(cfg);
  const [level, setLevel] = useState<LogLevel>(getLogLevel());
  const [updateMsg, setUpdateMsg] = useState("");
  const [tab, setTab] = useState<TabKey>("connection");

  useRegisterPane(
    undefined,
    `[설정] 게이트웨이 ${connected ? "연결됨" : "미연결"} · 로그 ${LOG_LABEL[level]} · v${__APP_VERSION__}`,
  );

  function applyLevel(next: LogLevel) {
    setLevel(next);
    setLogLevel(next);
    setString("andromeda.logLevel", next);
  }

  async function runUpdateCheck() {
    setUpdateMsg("확인 중…");
    try {
      await checkForUpdates();
      setUpdateMsg(isTauri() ? "최신 버전이거나 업데이트를 시작했습니다." : "업데이트는 데스크톱 앱에서만 지원됩니다.");
    } catch (e) {
      setUpdateMsg(`업데이트 확인 실패: ${errText(e)}`);
    }
  }

  const isError = status.startsWith("오류");

  // Reorderable rail list (non-settings panes); ▲▼ swap a pane with its neighbour.
  const railOrder = orderedViews(viewOrder);
  function moveView(key: View, dir: -1 | 1) {
    const i = railOrder.indexOf(key);
    const j = i + dir;
    if (j < 0 || j >= railOrder.length) return;
    const next = [...railOrder];
    [next[i], next[j]] = [next[j], next[i]];
    setViewOrder(next);
  }

  return (
    <div>
      <h2 style={{ marginTop: 2 }}>설정</h2>

      <div className="settings-tabs" role="tablist" aria-label="설정 섹션">
        {TABS.map((t) => (
          <button
            key={t.key}
            type="button"
            role="tab"
            id={`settings-tab-${t.key}`}
            aria-selected={tab === t.key}
            aria-controls="settings-panel"
            className={"settings-tab" + (tab === t.key ? " active" : "")}
            onClick={() => setTab(t.key)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* key={tab} remounts the panel so it re-runs the fade on each switch */}
      <div key={tab} id="settings-panel" role="tabpanel" aria-labelledby={`settings-tab-${tab}`} className="fade-up">
        {tab === "connection" && (
          <Section title="게이트웨이 연결">
            <div className="settings-fields">
              <label style={fieldLabel}>
                URL
                <input
                  className="field"
                  placeholder="https://gateway.example"
                  value={cfg.url}
                  onChange={(e) => setCfg({ ...cfg, url: e.target.value })}
                />
              </label>
              <label style={fieldLabel}>
                클라이언트 토큰
                <input
                  className="field"
                  type="password"
                  placeholder="토큰"
                  value={cfg.token}
                  onChange={(e) => setCfg({ ...cfg, token: e.target.value })}
                />
              </label>
            </div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 2 }}>
              <button
                className="btn btn-accent"
                onClick={() => {
                  saveConfig(cfg);
                  void check();
                }}
              >
                연결 / 저장
              </button>
              <span
                style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--muted)" }}
              >
                <LiveDot connected={connected} />
                {status || (connected ? "연결됨" : "미연결")}
              </span>
            </div>
            {isError && <p style={{ fontSize: 12, color: "var(--due)", margin: "2px 0 0" }}>{status}</p>}
          </Section>
        )}

        {tab === "general" && (
          <>
            <Section title="좌측 탭">
              <div style={{ display: "grid", gap: 1 }}>
                {railOrder.map((key, idx) => {
                  const p = PANES.find((x) => x.key === key);
                  if (!p) return null;
                  return (
                    <div
                      key={key}
                      style={{ display: "flex", alignItems: "center", gap: 9, padding: "3px 4px", fontSize: 13 }}
                    >
                      <label
                        style={{
                          display: "flex",
                          alignItems: "center",
                          gap: 9,
                          flex: 1,
                          cursor: "pointer",
                          color: "var(--ink-2)",
                        }}
                      >
                        <input
                          type="checkbox"
                          checked={!hiddenViews.includes(key)}
                          onChange={() => toggleViewHidden(key)}
                        />
                        {p.label}
                      </label>
                      <button
                        className="row-btn"
                        onClick={() => moveView(key, -1)}
                        disabled={idx === 0}
                        title="위로"
                        aria-label={`${p.label} 위로`}
                      >
                        ↑
                      </button>
                      <button
                        className="row-btn"
                        onClick={() => moveView(key, 1)}
                        disabled={idx === railOrder.length - 1}
                        title="아래로"
                        aria-label={`${p.label} 아래로`}
                      >
                        ↓
                      </button>
                    </div>
                  );
                })}
              </div>
            </Section>

            <Section title="로그 레벨">
              <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                {LOG_LEVELS.map((l) => {
                  const active = l === level;
                  return (
                    <button
                      key={l}
                      onClick={() => applyLevel(l)}
                      aria-pressed={active}
                      style={{
                        padding: "6px 12px",
                        fontSize: 13,
                        borderRadius: "var(--radius-ctl)",
                        cursor: "pointer",
                        border: active ? "1px solid var(--accent)" : "1px solid var(--line-2)",
                        background: active ? "var(--accent-soft)" : "var(--panel)",
                        color: active ? "var(--accent-deep)" : "var(--ink-2)",
                      }}
                    >
                      {LOG_LABEL[l]}
                    </button>
                  );
                })}
              </div>
            </Section>
          </>
        )}

        {tab === "about" && (
          <Section title="정보">
            <div style={{ fontSize: 13, color: "var(--ink-2)" }}>
              Andromeda 버전 <b style={{ fontWeight: 600 }}>v{__APP_VERSION__}</b>
            </div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginTop: 10 }}>
              <button className="btn" onClick={() => void runUpdateCheck()}>
                업데이트 확인
              </button>
              {updateMsg && <span style={{ fontSize: 12, ...muted }}>{updateMsg}</span>}
            </div>
          </Section>
        )}
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="settings-section">
      <h3 style={{ marginBottom: 12 }}>{title}</h3>
      {children}
    </section>
  );
}
