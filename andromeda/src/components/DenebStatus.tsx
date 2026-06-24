import { useEffect, useState } from "react";

import { DenebStar } from "./DenebStar";

// The rotating waiting words, mirroring the native PulsingStatusIndicator
// (waiting_thinking / working / brewing → 생각 중 / 작업 중 / 준비 중).
const WAITING = ["생각 중…", "작업 중…", "준비 중…"];

// Deneb's "응답 중" row: the sparkle + a slowly cycling waiting word, plus an
// optional live summary (the gateway's thinking preview / current step). Ported
// from the native client's status indicator — a transparent inline row, no slab.
export function DenebStatus({ summary }: { summary?: string }) {
  const [i, setI] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setI((x) => (x + 1) % WAITING.length), 3000);
    return () => clearInterval(t);
  }, []);

  return (
    <div className="deneb-status">
      <DenebStar />
      {/* key={i} remounts the word so it fades in on each change. */}
      <span key={i} className="deneb-status-text">
        {WAITING[i]}
      </span>
      {summary ? <span className="deneb-status-summary">· {summary}</span> : null}
    </div>
  );
}
