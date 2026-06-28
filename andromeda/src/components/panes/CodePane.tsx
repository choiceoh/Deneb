import { useEffect, useState } from "react";
import { codeDiscard, codePush, codeRepos, codeSessions, codeStart, codeUndo, codeVerify } from "@/gateway";
import { projectList } from "@/aiText";
import { errText } from "@/format";
import { line } from "@/theme";
import type { CodeSession } from "@/types";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";

// CodePane (코드) — the coding-mode work area: start a worktree session, then per
// session verify (build/test), push the branch, step back ("되돌리기"), or discard.
// Query-driven (calls miniapp.code.* directly). Repo comes from a GitHub picker
// (code.repos); when gh is unauthenticated the picker is empty and the raw
// owner/repo inputs take over so the loop still works.

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
  const { connected, cfg, bumpCodeSessions, openCodeChat } = useWorkspace();
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
    // Keep the Sidebar rail in sync with this work area after any change.
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
      // Jump straight into the new task's chat: the next thing the user does is tell
      // Deneb what to build, and that's where the work actually happens.
      openCodeChat(sess.chatSessionKey || "code:" + sess.id);
    } catch (e) {
      setStatus(errText(e));
    } finally {
      setBusy(false);
    }
  }

  // verify keeps the result so an unknown toolchain or a failing step gives the
  // vibe coder a readable message (the row status alone can't say "couldn't build").
  async function verify(id: string) {
    if (!connected || busy) return;
    setBusy(true);
    try {
      const { result } = await codeVerify(cfg, id);
      await refresh();
      if (result?.kind === "unknown") {
        setStatus("검증할 수 있는 빌드 설정을 찾지 못했습니다");
      } else if (result?.passed === false) {
        const failed = result.steps?.find((s) => s.ok === false);
        setStatus(`검증 실패${failed?.label ? ` — ${failed.label}` : ""}`);
      } else {
        setStatus("검증 통과");
      }
    } catch (e) {
      setStatus(`검증 실패: ${errText(e)}`);
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
      <p style={{ opacity: 0.7, fontSize: 13, margin: "0 0 12px", lineHeight: 1.5 }}>
        레포와 작업 ID로 <b>새 작업</b>을 만들면 격리된 git 워크트리가 생깁니다. 작업을 클릭하면 오른쪽 Deneb 채팅이 그
        작업에 연결되고, 거기서 코드를 시키면 됩니다. 턴마다 빌드·테스트 검증 + 되돌리기 지점이 자동으로 남고, 끝나면{" "}
        <b>올리기</b>로 GitHub 브랜치에 푸시하세요.
      </p>
      <div
        className="code-new"
        style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginBottom: 12 }}
      >
        {hasRepos ? (
          <select
            className="field"
            style={{ width: 220 }}
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
          <>
            <input
              className="field"
              style={{ width: 110 }}
              placeholder="owner"
              value={owner}
              disabled={!connected}
              onChange={(e) => setOwner(e.target.value)}
            />
            <input
              className="field"
              style={{ width: 120 }}
              placeholder="repo"
              value={repo}
              disabled={!connected}
              onChange={(e) => setRepo(e.target.value)}
            />
          </>
        )}
        <input
          className="field"
          style={{ width: 150 }}
          placeholder="작업 ID (예: fix-login)"
          value={taskId}
          disabled={!connected}
          onChange={(e) => setTaskId(e.target.value)}
        />
        <input
          className="field"
          style={{ flex: 1, minWidth: 120 }}
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
          <p style={{ opacity: 0.6 }}>{connected ? "아직 작업이 없습니다." : "먼저 게이트웨이에 연결하세요."}</p>
        )}
        {sessions.map((s) => {
          // A worktree that vanished (reconciled to "missing") can only be deleted —
          // verify/push/undo would run git in a gone directory.
          const missing = s.status === "missing";
          const noCheckpoints = (s.checkpoints?.length ?? 0) === 0;
          return (
            <div
              key={s.id}
              style={{ display: "flex", alignItems: "center", gap: 8, borderTop: line, padding: "8px 0" }}
            >
              <button
                onClick={() => openCodeChat(s.chatSessionKey || "code:" + s.id)}
                disabled={missing}
                title={missing ? "워크트리가 없어 대화할 수 없습니다" : "이 작업과 대화 — Deneb에게 코드를 시키세요"}
                style={{
                  flex: 1,
                  minWidth: 0,
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
                <div style={{ opacity: 0.7, fontSize: 13 }}>
                  {(s.repo?.owner ?? "?") + "/" + (s.repo?.name ?? "?")} ·{" "}
                  {STATUS_LABEL[s.status ?? ""] ?? s.status ?? ""}
                </div>
              </button>
              <button className="btn" onClick={() => void verify(s.id)} disabled={busy || missing}>
                검증
              </button>
              <button
                className="btn"
                onClick={() => void act("올리기", () => codePush(cfg, s.id))}
                disabled={busy || missing || noCheckpoints}
                title={noCheckpoints ? "저장된 변경이 없습니다" : "GitHub에 올리기"}
              >
                올리기
              </button>
              <button
                className="btn"
                onClick={() => void act("되돌리기", () => codeUndo(cfg, s.id))}
                disabled={busy || missing}
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
          );
        })}
      </div>
    </div>
  );
}
