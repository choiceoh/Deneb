import { useState, type ReactNode } from "react";
import { saveConfig } from "@/gateway";
import { setString } from "@/storage";
import { useGatewayStatus } from "@/hooks";
import { type LogLevel, getLogLevel, setLogLevel } from "@/log";
import { isTauri } from "@/tauri";
import { checkForUpdates } from "@/updater";
import { errText } from "@/format";
import { line, muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { LiveDot } from "@/components/LiveDot";
import { PANES } from "@/components/panes";

// 설정 — a full-screen settings section (not a modal). Promotes the gateway
// connection form out of the sidebar popover and adds log-level + about. Edits the
// app-owned config via useWorkspace().setCfg (App persists + rebuilds providers).
const LOG_LEVELS: LogLevel[] = ["debug", "info", "warn", "error", "silent"];
const LOG_LABEL: Record<LogLevel, string> = {
  debug: "디버그",
  info: "정보",
  warn: "경고",
  error: "오류",
  silent: "끄기",
};

const fieldLabel = {
  display: "grid",
  gap: 4,
  fontSize: 12,
  color: "var(--muted)",
  marginBottom: 10,
} as const;

export function SettingsPane() {
  const { connected, cfg, setCfg, hiddenViews, toggleViewHidden } = useWorkspace();
  const { status, check } = useGatewayStatus(cfg);
  const [level, setLevel] = useState<LogLevel>(getLogLevel());
  const [updateMsg, setUpdateMsg] = useState("");

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

  return (
    <div style={{ maxWidth: 480 }}>
      <h2 style={{ marginTop: 2 }}>설정</h2>

      <Section
        title="게이트웨이 연결"
        desc="Deneb 게이트웨이 주소와 클라이언트 토큰. 실제 호스트에서는 키체인에서 자동 연결됩니다."
      >
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
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12, color: "var(--muted)" }}>
            <LiveDot connected={connected} />
            {status || (connected ? "연결됨" : "미연결")}
          </span>
        </div>
        {isError && <p style={{ fontSize: 12, color: "var(--due)", margin: "2px 0 0" }}>{status}</p>}
      </Section>

      <Section title="로그 레벨" desc="">
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

      <Section title="좌측 탭" desc="좌측 레일에 표시할 화면을 고르세요. 설정은 항상 표시됩니다.">
        <div style={{ display: "grid", gap: 1 }}>
          {PANES.filter((p) => p.key !== "settings").map((p) => (
            <label
              key={p.key}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 9,
                padding: "6px 4px",
                fontSize: 13,
                color: "var(--ink-2)",
                cursor: "pointer",
              }}
            >
              <input type="checkbox" checked={!hiddenViews.includes(p.key)} onChange={() => toggleViewHidden(p.key)} />
              {p.label}
            </label>
          ))}
        </div>
      </Section>

      <Section title="정보" desc="">
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
    </div>
  );
}

function Section({ title, desc, children }: { title: string; desc: string; children: ReactNode }) {
  return (
    <section style={{ borderTop: line, paddingTop: 16, marginTop: 18 }}>
      <h3 style={{ marginBottom: desc ? 4 : 12 }}>{title}</h3>
      {desc && <p style={{ fontSize: 12, ...muted, margin: "0 0 12px", lineHeight: 1.5 }}>{desc}</p>}
      {children}
    </section>
  );
}
