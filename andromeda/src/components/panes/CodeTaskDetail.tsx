import { useEffect, useState } from "react";

import { type CodeVerifyResult, type GatewayConfig, codePr, codeVerify } from "@/gateway";
import { codeStatusColor, codeStatusLabel } from "@/codeStatus";
import { errText, fmtMailDate } from "@/format";
import type { CodeSession } from "@/types";

// CodeTaskDetail — 코드 모드 우측 패널에서 *선택된 작업*의 상세. 새 작업 폼 대신 이게 뜬다.
// 보여주는 것: 상태 헤더 + 진행 기록(체크포인트 타임라인, Deneb가 한 일) + 검증(빌드·테스트).
// 전부 이미 있는 데이터(session.checkpoints, miniapp.code.verify)만 쓴다 — 백엔드 변경 없음.
// 조작(편집·커밋·PR)은 여전히 가운데 채팅이 자동 처리; 여기서 검증만 사용자가 다시 돌릴 수 있다.
export function CodeTaskDetail({
  session,
  cfg,
  onChange,
}: {
  session: CodeSession;
  cfg: GatewayConfig;
  onChange: () => void;
}) {
  const [verifying, setVerifying] = useState(false);
  const [result, setResult] = useState<CodeVerifyResult | null>(null);
  const [err, setErr] = useState("");
  const [prUrl, setPrUrl] = useState("");

  // Look up the branch's PR link (empty until the autonomous flow opens one, or
  // when gh is unauthenticated). Re-runs when the session updates (a turn may have
  // just pushed + opened the PR).
  useEffect(() => {
    let alive = true;
    codePr(cfg, session.id)
      .then((url) => alive && setPrUrl(url))
      .catch(() => alive && setPrUrl(""));
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.id, session.updatedAt, cfg.url, cfg.token]);

  async function runVerify() {
    setVerifying(true);
    setErr("");
    try {
      const r = await codeVerify(cfg, session.id);
      setResult(r.result);
      // Verify flips the session status (passed/failed) → refresh list + rail dot.
      onChange();
    } catch (e) {
      setErr(errText(e));
    } finally {
      setVerifying(false);
    }
  }

  const checkpoints = session.checkpoints ?? [];
  const repoLabel = `${session.repo?.owner ?? "?"}/${session.repo?.name ?? "?"}`;
  const unknownKind = result !== null && (result.kind === "unknown" || !result.steps?.length);

  return (
    <div className="code-detail">
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span
          style={{
            width: 9,
            height: 9,
            borderRadius: "50%",
            background: codeStatusColor(session.status),
            flex: "0 0 auto",
          }}
        />
        <span style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {session.title || session.id}
        </span>
      </div>
      <p style={{ margin: "4px 0 14px", fontSize: 12.5, color: "var(--muted)" }}>
        {codeStatusLabel(session.status)} · {repoLabel}
        {session.branch ? ` · ${session.branch}` : ""}
      </p>

      {prUrl && (
        <a
          href={prUrl}
          target="_blank"
          rel="noreferrer"
          className="btn btn-accent"
          style={{ display: "inline-flex", textDecoration: "none", marginBottom: 16 }}
        >
          GitHub에서 결과 보기 ↗
        </a>
      )}

      <div style={{ fontSize: 11.5, letterSpacing: "0.04em", color: "var(--muted-2)", marginBottom: 8 }}>진행 기록</div>
      {checkpoints.length === 0 ? (
        <p style={{ opacity: 0.6, fontSize: 13, margin: "0 0 18px", lineHeight: 1.5 }}>
          아직 진행 기록이 없습니다. 가운데 채팅으로 작업을 시키면 단계마다 여기에 기록됩니다.
        </p>
      ) : (
        <ol
          style={{
            listStyle: "none",
            margin: "0 0 18px",
            padding: 0,
            display: "flex",
            flexDirection: "column",
            gap: 10,
          }}
        >
          {checkpoints
            .slice()
            .reverse()
            .map((cp, i) => (
              <li key={cp.sha || i} style={{ display: "flex", flexDirection: "column", gap: 1 }}>
                <span style={{ fontSize: 13, lineHeight: 1.4 }}>{cp.summary || "변경 저장"}</span>
                <span style={{ fontSize: 11.5, color: "var(--muted-2)" }}>{fmtMailDate(cp.at)}</span>
              </li>
            ))}
        </ol>
      )}

      <div style={{ fontSize: 11.5, letterSpacing: "0.04em", color: "var(--muted-2)", marginBottom: 8 }}>검증</div>
      <button className="btn" onClick={() => void runVerify()} disabled={verifying} style={{ marginBottom: 8 }}>
        {verifying ? "확인 중…" : "빌드·테스트 다시 확인"}
      </button>
      {err && <p className="pane-status">{err}</p>}
      {result !== null &&
        (unknownKind ? (
          <p style={{ fontSize: 13, color: "var(--muted)" }}>이 프로젝트는 자동 검증 대상이 아닙니다.</p>
        ) : (
          <ul style={{ listStyle: "none", margin: 0, padding: 0, display: "flex", flexDirection: "column", gap: 6 }}>
            {result.steps?.map((st, i) => (
              <li key={i} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13 }}>
                <span style={{ color: st.ok ? "var(--online)" : "var(--danger)", fontWeight: 600, flex: "0 0 auto" }}>
                  {st.ok ? "✓" : "✗"}
                </span>
                <span>{st.label || st.cmd}</span>
              </li>
            ))}
          </ul>
        ))}
    </div>
  );
}
