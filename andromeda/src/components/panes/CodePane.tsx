import { useEffect, useState } from "react";
import { codeDiscard, codePush, codeRepos, codeSessions, codeStart, codeUndo } from "@/gateway";
import { projectList } from "@/aiText";
import { errText } from "@/format";
import { line } from "@/theme";
import type { CodeSession } from "@/types";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

// CodePane (코드) — 코드 모드 우측 보조 패널: 새 워크트리 작업 생성, 세션 목록(클릭 → 가운데
// 채팅이 그 작업에 연결), 세션별 올리기(push)·되돌리기(undo)·삭제(discard). 검증은 게이트웨이가
// 턴마다 자동으로 돌려 상태 배지로 보여주므로 수동 버튼은 두지 않는다. Query-driven
// (miniapp.code.* 직접 호출). 레포는 GitHub picker(code.repos); gh 미인증이면 비어서 owner/repo
// 직접 입력으로 폴백.
const STATUS_LABEL: Record<string, string> = {
  working: "작업중",
  passed: "통과",
  failed: "실패",
  missing: "없음",
};

interface RepoOption {
  owner?: string;
  name?: string;
}

export function CodePane() {
  const { connected, cfg, bumpCodeSessions, openCodeChat, activeCodeKey } = useWorkspace();
  const [sessions, setSessions] = useState<CodeSession[]>([]);
  const [repos, setRepos] = useState<RepoOption[]>([]);
  const [status, setStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [owner, setOwner] = useState("");
  const [repo, setRepo] = useState("");
  const [taskId, setTaskId] = useState("");
  const [title, setTitle] = useState("");

  const aiText = projectList(
    `[코딩 세션 — ${sessions.length}개]`,
    sessions,
    (s) =>
      `- ${s.title || s.id} (${s.repo?.owner ?? "?"}/${s.repo?.name ?? "?"}) — ${STATUS_LABEL[s.status ?? ""] ?? s.status ?? ""}`,
  );
  useRegisterPane("code", aiText);

  async function refresh() {
    if (!connected) return;
    try {
      setSessions(await codeSessions(cfg));
      setStatus("");
    } catch (e) {
      setStatus(errText(e));
    }
    // Keep the Sidebar rail in sync after any change.
    bumpCodeSessions();
  }

  useEffect(() => {
    void refresh();
    if (connected)
      codeRepos(cfg)
        .then(setRepos)
        .catch(() => setRepos([]));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected]);

  async function start() {
    if (!connected || busy) return;
    if (!owner.trim() || !repo.trim() || !taskId.trim()) {
      setStatus("레포와 작업 ID를 입력하세요");
      return;
    }
    setBusy(true);
    try {
      const sess = await codeStart(cfg, owner.trim(), repo.trim(), taskId.trim(), title.trim() || undefined);
      setTaskId("");
      setTitle("");
      await refresh();
      // Open the new task's chat right away → start giving instructions in the center.
      openCodeChat(sess.chatSessionKey || "code:" + sess.id);
    } catch (e) {
      setStatus(errText(e));
    } finally {
      setBusy(false);
    }
  }

  // act runs a per-session action and refreshes the list, surfacing failures.
  async function act(label: string, run: () => Promise<unknown>) {
    if (!connected || busy) return;
    setBusy(true);
    try {
      await run();
      await refresh();
    } catch (e) {
      setStatus(`${label} 실패: ${errText(e)}`);
    } finally {
      setBusy(false);
    }
  }

  const hasRepos = repos.length > 0;

  return (
    <div className="code-pane">
      <p style={{ opacity: 0.7, fontSize: 12.5, margin: "0 0 10px", lineHeight: 1.5 }}>
        새 작업을 만들면 격리된 워크트리가 생기고, 가운데 채팅이 그 작업에 연결됩니다. 채팅으로 코드를 시키면 턴마다
        검증·체크포인트가 자동으로 남습니다.
      </p>

      {/* 새 작업 — 좁은 패널이라 세로로 쌓는다 */}
      <div className="code-new" style={{ display: "flex", flexDirection: "column", gap: 6, marginBottom: 14 }}>
        {hasRepos ? (
          <select
            className="field"
            disabled={!connected}
            value={owner && repo ? `${owner}/${repo}` : ""}
            onChange={(e) => {
              const [o, n] = e.target.value.split("/");
              setOwner(o ?? "");
              setRepo(n ?? "");
            }}
          >
            <option value="">레포 선택…</option>
            {repos.map((r) => {
              const v = `${r.owner ?? ""}/${r.name ?? ""}`;
              return (
                <option key={v} value={v}>
                  {v}
                </option>
              );
            })}
          </select>
        ) : (
          <div style={{ display: "flex", gap: 6 }}>
            <input
              className="field"
              style={{ flex: 1, minWidth: 0 }}
              placeholder="owner"
              value={owner}
              disabled={!connected}
              onChange={(e) => setOwner(e.target.value)}
            />
            <input
              className="field"
              style={{ flex: 1, minWidth: 0 }}
              placeholder="repo"
              value={repo}
              disabled={!connected}
              onChange={(e) => setRepo(e.target.value)}
            />
          </div>
        )}
        <input
          className="field"
          placeholder="작업 ID (예: fix-login)"
          value={taskId}
          disabled={!connected}
          onChange={(e) => setTaskId(e.target.value)}
        />
        <input
          className="field"
          placeholder="제목 (선택)"
          value={title}
          disabled={!connected}
          onChange={(e) => setTitle(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void start();
          }}
        />
        <button className="btn btn-accent" onClick={() => void start()} disabled={!connected || busy}>
          + 새 작업
        </button>
      </div>

      {status && <p className="pane-status">{status}</p>}

      <div className="code-sessions">
        {sessions.length === 0 && !status && (
          <p style={{ opacity: 0.6, fontSize: 13 }}>
            {connected ? "아직 작업이 없습니다." : "먼저 게이트웨이에 연결하세요."}
          </p>
        )}
        {sessions.map((s) => {
          // A worktree that vanished (reconciled to "missing") can only be deleted.
          const missing = s.status === "missing";
          const noCheckpoints = (s.checkpoints?.length ?? 0) === 0;
          const key = s.chatSessionKey || "code:" + s.id;
          const active = key === activeCodeKey;
          return (
            <div
              key={s.id}
              style={{
                borderTop: line,
                padding: 8,
                margin: "0 -8px",
                borderRadius: 8,
                background: active ? "var(--accent-soft)" : "transparent",
              }}
            >
              <button
                onClick={() => openCodeChat(key)}
                disabled={missing}
                title={missing ? "워크트리가 없어 대화할 수 없습니다" : "이 작업과 대화 — Deneb에게 코드를 시키세요"}
                style={{
                  width: "100%",
                  textAlign: "left",
                  background: "none",
                  border: "none",
                  padding: 0,
                  font: "inherit",
                  color: "inherit",
                  cursor: missing ? "default" : "pointer",
                }}
              >
                <div style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {s.title || s.id}
                </div>
                <div style={{ opacity: 0.7, fontSize: 12.5 }}>
                  {(s.repo?.owner ?? "?") + "/" + (s.repo?.name ?? "?")} ·{" "}
                  {STATUS_LABEL[s.status ?? ""] ?? s.status ?? ""}
                </div>
              </button>
              {/* 보조 동작 — 검증은 턴마다 자동(상태 배지로 표시)이라 뺐다. 올리기/되돌리기/삭제만. */}
              <div style={{ display: "flex", gap: 6, marginTop: 6 }}>
                <button
                  className="btn"
                  style={{ flex: 1 }}
                  onClick={() => void act("올리기", () => codePush(cfg, s.id))}
                  disabled={busy || missing || noCheckpoints}
                  title={noCheckpoints ? "저장된 변경이 없습니다" : "GitHub 브랜치에 올리기 (PR용)"}
                >
                  올리기
                </button>
                <button
                  className="btn"
                  style={{ flex: 1 }}
                  onClick={() => void act("되돌리기", () => codeUndo(cfg, s.id))}
                  disabled={busy || missing}
                  title="한 단계 되돌리기"
                >
                  되돌리기
                </button>
                <button
                  className="btn"
                  onClick={() => void act("삭제", () => codeDiscard(cfg, s.id))}
                  disabled={busy}
                  title="작업 삭제"
                >
                  삭제
                </button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
