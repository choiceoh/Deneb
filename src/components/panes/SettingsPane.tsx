import { useEffect, useState, type KeyboardEvent, type ReactNode } from "react";
import { getPrompt, listPrompts, resetPrompt, saveConfig, updatePrompt } from "@/gateway";
import { setString } from "@/storage";
import { moveItem } from "@/listReorder";
import { useGatewayStatus } from "@/hooks";
import { type LogLevel, getLogLevel, setLogLevel } from "@/log";
import { checkForUpdates } from "@/updater";
import { errText, fmtDate } from "@/format";
import { muted } from "@/theme";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import type { PromptDetailOut, PromptRow, View } from "@/types";
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

type TabKey = "connection" | "general" | "prompts" | "about";
const TABS: { key: TabKey; label: string }[] = [
  { key: "connection", label: "연결" },
  { key: "general", label: "일반" },
  { key: "prompts", label: "프롬프트" },
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
      const result = await checkForUpdates();
      switch (result.status) {
        case "unavailable":
          setUpdateMsg("업데이트는 데스크톱 앱에서만 지원됩니다.");
          break;
        case "up-to-date":
          setUpdateMsg(`최신 버전입니다 (v${result.currentVersion}).`);
          break;
        case "installed":
          setUpdateMsg(`v${result.version}으로 업데이트되어 재시작 중입니다.`);
          break;
        case "deferred":
          setUpdateMsg(`v${result.version} 설치 완료 — 다음 실행 시 적용됩니다.`);
          break;
      }
    } catch (e) {
      setUpdateMsg(`업데이트 확인 실패: ${errText(e)}`);
    }
  }

  const isError = status.startsWith("오류");

  // WAI-ARIA tabs: ←/→ wrap, Home/End jump; selection follows focus (roving
  // tabindex below keeps only the active tab in the page tab order).
  function onTabKey(e: KeyboardEvent, idx: number) {
    const last = TABS.length - 1;
    let next = idx;
    if (e.key === "ArrowRight") next = idx === last ? 0 : idx + 1;
    else if (e.key === "ArrowLeft") next = idx === 0 ? last : idx - 1;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = last;
    else return;
    e.preventDefault();
    const key = TABS[next].key;
    setTab(key);
    document.getElementById(`settings-tab-${key}`)?.focus();
  }

  // Reorderable rail list (non-settings panes); ▲▼ swap a pane with its neighbour.
  const railOrder = orderedViews(viewOrder);
  function moveView(key: View, dir: -1 | 1) {
    setViewOrder(moveItem(railOrder, key, dir));
  }

  return (
    <div>
      <h2 style={{ marginTop: 2 }}>설정</h2>

      <div className="settings-tabs" role="tablist" aria-label="설정 섹션">
        {TABS.map((t, i) => (
          <button
            key={t.key}
            type="button"
            role="tab"
            id={`settings-tab-${t.key}`}
            aria-selected={tab === t.key}
            aria-controls="settings-panel"
            tabIndex={tab === t.key ? 0 : -1}
            className={"settings-tab" + (tab === t.key ? " active" : "")}
            onClick={() => setTab(t.key)}
            onKeyDown={(e) => onTabKey(e, i)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* key={tab} remounts the panel so it re-runs the fade on each switch */}
      <div
        key={tab}
        id="settings-panel"
        role="tabpanel"
        aria-labelledby={`settings-tab-${tab}`}
        className="settings-panel"
      >
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
                    <div key={key} className="settings-rail-row">
                      <label>
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
                      type="button"
                      className={"opt" + (active ? " active" : "")}
                      onClick={() => applyLevel(l)}
                      aria-pressed={active}
                    >
                      {LOG_LABEL[l]}
                    </button>
                  );
                })}
              </div>
            </Section>
          </>
        )}

        {tab === "prompts" && <PromptSettings />}

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

function PromptSettings() {
  const { connected, cfg } = useWorkspace();
  const [prompts, setPrompts] = useState<PromptRow[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [detail, setDetail] = useState<PromptDetailOut | null>(null);
  const [draft, setDraft] = useState("");
  const [loadingList, setLoadingList] = useState(false);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");

  const originalText = detail?.text ?? "";
  const dirty = Boolean(detail) && draft !== originalText;
  const editable = detail?.editable !== false;

  useEffect(() => {
    setPrompts([]);
    setSelectedId("");
    setDetail(null);
    setDraft("");
    setStatus("");
    setError("");
    if (connected) void refreshPrompts(true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, cfg.url, cfg.token]);

  async function refreshPrompts(openFirst = false) {
    if (!connected) return;
    setLoadingList(true);
    setError("");
    try {
      const rows = await listPrompts(cfg);
      setPrompts(rows);
      if (openFirst) {
        const first = rows.find((p) => p.id)?.id;
        if (first) void openPrompt(first, true);
      }
    } catch (e) {
      setError(`프롬프트 목록을 불러오지 못했습니다: ${errText(e)}`);
    } finally {
      setLoadingList(false);
    }
  }

  async function openPrompt(id: string, force = false) {
    if (!force && dirty && !window.confirm("저장하지 않은 변경을 버리고 다른 프롬프트를 열까요?")) return;
    setSelectedId(id);
    setLoadingDetail(true);
    setStatus("");
    setError("");
    try {
      const next = await getPrompt(cfg, id);
      setDetail(next);
      setDraft(next.text ?? "");
    } catch (e) {
      setDetail(null);
      setDraft("");
      setError(`프롬프트를 불러오지 못했습니다: ${errText(e)}`);
    } finally {
      setLoadingDetail(false);
    }
  }

  async function savePrompt() {
    if (!detail?.id || !editable || !dirty) return;
    setSaving(true);
    setStatus("저장 중...");
    setError("");
    try {
      const saved = await updatePrompt(cfg, detail.id, draft);
      setDetail(saved);
      setDraft(saved.text ?? "");
      setStatus("저장됨");
      void refreshPrompts(false);
    } catch (e) {
      setStatus("");
      setError(`저장 실패: ${errText(e)}`);
    } finally {
      setSaving(false);
    }
  }

  async function restorePrompt() {
    if (!detail?.id) return;
    setSaving(true);
    setStatus("초기화 중...");
    setError("");
    try {
      const restored = await resetPrompt(cfg, detail.id);
      setDetail(restored);
      setDraft(restored.text ?? "");
      setStatus("기본값으로 초기화됨");
      void refreshPrompts(false);
    } catch (e) {
      setStatus("");
      setError(`초기화 실패: ${errText(e)}`);
    } finally {
      setSaving(false);
    }
  }

  if (!connected) {
    return (
      <Section title="프롬프트">
        <p style={{ ...muted, margin: 0 }}>게이트웨이에 연결하면 프롬프트를 볼 수 있습니다.</p>
      </Section>
    );
  }

  return (
    <Section title="프롬프트">
      <div className="prompt-settings">
        <aside className="prompt-list" aria-label="프롬프트 목록">
          <div className="prompt-list-head">
            <span>{loadingList ? "불러오는 중..." : `${prompts.length}개`}</span>
            <button className="row-btn" onClick={() => void refreshPrompts(false)} disabled={loadingList || saving}>
              새로고침
            </button>
          </div>
          {prompts.length === 0 && !loadingList ? (
            <p style={{ ...muted, margin: "8px 0" }}>편집할 프롬프트가 없습니다.</p>
          ) : (
            prompts.map((prompt) => {
              const id = prompt.id ?? "";
              const active = id && id === selectedId;
              return (
                <button
                  key={id || prompt.title}
                  type="button"
                  className={"prompt-row" + (active ? " active" : "")}
                  onClick={() => id && void openPrompt(id)}
                  disabled={!id || loadingDetail || saving}
                >
                  <span>{prompt.title || id || "(이름 없음)"}</span>
                  <small>
                    {[
                      prompt.category,
                      prompt.overridden ? "수정됨" : "기본",
                      prompt.editable === false ? "읽기 전용" : "",
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </small>
                </button>
              );
            })
          )}
        </aside>

        <div className="prompt-editor">
          {!detail ? (
            <p style={{ ...muted, margin: 0 }}>
              {loadingDetail ? "프롬프트를 불러오는 중..." : "프롬프트를 선택하세요."}
            </p>
          ) : (
            <>
              <div className="prompt-editor-head">
                <div>
                  <h4>{detail.title || detail.id}</h4>
                  <div className="prompt-meta">
                    {[detail.category, detail.overridden ? "수정됨" : "기본값", fmtDate(detail.updatedAtMs)]
                      .filter(Boolean)
                      .join(" · ")}
                  </div>
                </div>
                <div className="prompt-actions">
                  <button className="btn" onClick={() => void restorePrompt()} disabled={saving || !detail.overridden}>
                    초기화
                  </button>
                  <button
                    className="btn btn-accent"
                    onClick={() => void savePrompt()}
                    disabled={saving || !editable || !dirty}
                  >
                    저장
                  </button>
                </div>
              </div>
              {detail.description && <p className="prompt-description">{detail.description}</p>}
              <label style={fieldLabel}>
                프롬프트 본문
                <textarea
                  className="field prompt-textarea"
                  value={draft}
                  readOnly={!editable}
                  onChange={(e) => setDraft(e.target.value)}
                />
              </label>
              {!editable && <p style={{ ...muted, margin: 0 }}>이 프롬프트는 읽기 전용입니다.</p>}
              {(status || error) && <p className={error ? "pane-error" : "pane-status"}>{error || status}</p>}
            </>
          )}
        </div>
      </div>
    </Section>
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
