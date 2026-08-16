import { useEffect, useState } from "react";

import { DenebStar } from "./DenebStar";

// The rotating waiting words, mirroring the native PulsingStatusIndicator
// (waiting_thinking / working / brewing → 생각 중 / 작업 중 / 준비 중).
const WAITING = ["생각 중…", "작업 중…", "준비 중…"];

// Deneb's "응답 중" row: the sparkle + a slowly cycling waiting word, plus an
// optional live summary (normally the gateway-owned current step). Ported
// from the native client's status indicator — a transparent inline row, no slab.
function elapsedTurnLabel(startedAt: number, now: number): string | undefined {
  const elapsed = Math.max(0, Math.floor((now - startedAt) / 1000));
  if (elapsed < 10) return undefined;
  if (elapsed < 60) return `${elapsed}초 경과`;
  return `${Math.floor(elapsed / 60)}분 ${elapsed % 60}초 경과`;
}

export function DenebStatus({ summary, startedAt }: { summary?: string; startedAt?: number }) {
  const [i, setI] = useState(0);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setI((x) => (x + 1) % WAITING.length), 4000);
    return () => clearInterval(t);
  }, []);
  useEffect(() => {
    if (startedAt === undefined) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [startedAt]);
  const elapsed = startedAt === undefined ? undefined : elapsedTurnLabel(startedAt, now);

  return (
    <div className="deneb-status">
      <DenebStar />
      {/* key={i} remounts the word so it fades in on each change. */}
      <span key={i} className="deneb-status-text">
        {WAITING[i]}
      </span>
      {summary ? <span className="deneb-status-summary">· {summary}</span> : null}
      {elapsed ? <span className="deneb-status-elapsed">· {elapsed}</span> : null}
    </div>
  );
}
