import { useEffect, useState } from "react";

import { codeSessions } from "@/gateway";
import { projectList } from "@/aiText";
import { errText } from "@/format";
import type { CodeSession } from "@/types";
import { useRegisterPane, useWorkspace } from "@/workspaceContext";
import { CodeTaskDetail } from "./CodeTaskDetail";

// CodePane (코드) — 코드 모드 우측 보조 패널. 작업을 고르면 그 작업의 *상세*(상태·진행 기록·검증·
// PR 링크)를 보여준다. 새 작업 *생성*은 왼쪽 레일 "새 작업" 버튼이 띄우는 모달(CodeNewTaskModal)이
// 담당하므로 여기엔 생성 폼이 없다. 세션 목록은 왼쪽 레일이 보여주고, 여기선 AI 컨텍스트용
// projectList 에만 남긴다.
const STATUS_LABEL: Record<string, string> = {
  working: "작업중",
  passed: "통과",
  failed: "실패",
  missing: "없음",
};

export function CodePane() {
  const { connected, cfg, codeSessionsRev, bumpCodeSessions, activeCodeKey } = useWorkspace();
  const [sessions, setSessions] = useState<CodeSession[]>([]);
  const [status, setStatus] = useState("");

  const aiText = projectList(
    `[코딩 세션 — ${sessions.length}개]`,
    sessions,
    (s) =>
      `- ${s.title || s.id} (${s.repo?.owner ?? "?"}/${s.repo?.name ?? "?"}) — ${STATUS_LABEL[s.status ?? ""] ?? s.status ?? ""}`,
  );
  useRegisterPane("code", aiText);

  // Reload on connect or whenever the session set changes (new task from the modal,
  // a verify status flip). bumpCodeSessions() drives codeSessionsRev — and this
  // effect only reads (never bumps), so the rail and panel stay in sync with no loop.
  useEffect(() => {
    if (!connected) return;
    let alive = true;
    codeSessions(cfg)
      .then((s) => {
        if (!alive) return;
        setSessions(s);
        setStatus("");
      })
      .catch((e) => alive && setStatus(errText(e)));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connected, codeSessionsRev]);

  const activeSession = activeCodeKey
    ? sessions.find((s) => (s.chatSessionKey || "code:" + s.id) === activeCodeKey)
    : undefined;

  return (
    <div className="code-pane">
      {activeSession ? (
        <CodeTaskDetail session={activeSession} cfg={cfg} onChange={bumpCodeSessions} />
      ) : (
        <p style={{ opacity: 0.6, fontSize: 13, lineHeight: 1.6, margin: "8px 4px" }}>
          {status
            ? status
            : connected
              ? "왼쪽에서 작업을 고르면 진행 기록·검증·결과가 여기에 표시됩니다. 새로 시작하려면 왼쪽 “새 작업”을 누르세요."
              : "먼저 게이트웨이에 연결하세요."}
        </p>
      )}
    </div>
  );
}
