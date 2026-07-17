// Consented auto-update: a quiet bottom-right pill announcing a newer signed
// build. Nothing downloads until the user clicks 설치 (frontier desktop apps
// never spend bandwidth/disk silently); progress renders inline and the final
// relaunch is a button, not a blocking dialog.
import { useState } from "react";
import { type AvailableUpdate, relaunchApp } from "@/updater";
import { errText } from "@/format";

export function UpdateNudge({ update, onDismiss }: { update: AvailableUpdate; onDismiss: () => void }) {
  const [phase, setPhase] = useState<"idle" | "installing" | "ready" | "error">("idle");
  const [pct, setPct] = useState<number | null>(null);
  const [err, setErr] = useState("");

  async function install() {
    setPhase("installing");
    try {
      await update.install(setPct);
      setPhase("ready");
    } catch (e) {
      setErr(errText(e));
      setPhase("error");
    }
  }

  return (
    <div className="panel update-nudge" role="status" aria-live="polite">
      {phase === "idle" && (
        <>
          <span>새 버전 v{update.version}</span>
          <button className="btn btn-accent" onClick={() => void install()}>
            설치
          </button>
          <button className="btn" onClick={onDismiss}>
            나중에
          </button>
        </>
      )}
      {phase === "installing" && <span>다운로드 중{pct != null ? ` ${pct}%` : "…"}</span>}
      {phase === "ready" && (
        <>
          <span>v{update.version} 설치 완료</span>
          <button className="btn btn-accent" onClick={() => void relaunchApp()}>
            재시작
          </button>
          <button className="btn" onClick={onDismiss} title="다음 실행 시 적용됩니다">
            나중에
          </button>
        </>
      )}
      {phase === "error" && (
        <>
          <span>업데이트 실패: {err}</span>
          <button className="btn" onClick={onDismiss}>
            닫기
          </button>
        </>
      )}
    </div>
  );
}
