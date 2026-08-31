import { useEffect, useMemo, useRef, useState } from "react";
import { DENEB_HTML_MIN_HEIGHT, buildSrcdoc, denebHtmlFrameHeight, parseDenebHtmlMessage } from "./denebHtmlSandbox";

// deneb-html — a webpage-style HTML answer the agent authors as a complete
// self-contained document (```deneb-html fence). It renders sandboxed INLINE
// in the transcript: an iframe with sandbox="allow-scripts" (unique origin, no
// same-origin power) plus an injected CSP that blocks every network fetch, so
// inline CSS/JS run but nothing loads from or leaks to the outside.
//
// Two-way bridge (the page side is injected below, contract in the gateway
// system prompt): window.deneb.send(text) posts the text back into the chat
// as a user message; the page also reports its scrollHeight so the frame
// grows to fit instead of double-scrolling (clamping: denebHtmlFrameHeight).

export function DenebHtmlAnswer({
  body,
  onSubmit,
  busy,
  // false on non-last assistant turns: the page still renders and its local
  // scripts run, but deneb.send round-trips are ignored (stale-card gating,
  // same rule as deneb-ui callbacks).
  interactive = true,
}: {
  body: string;
  onSubmit: (msg: string) => void;
  busy?: boolean;
  interactive?: boolean;
}) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [height, setHeight] = useState(DENEB_HTML_MIN_HEIGHT);
  const srcdoc = useMemo(() => buildSrcdoc(body), [body]);

  // Live props for the stable message listener (busy flips per stream tick).
  // Updated post-commit, never during render (react-hooks ref rule).
  const live = useRef({ onSubmit, busy, interactive });
  useEffect(() => {
    live.current = { onSubmit, busy, interactive };
  });

  useEffect(() => {
    function onMessage(e: MessageEvent) {
      if (!frameRef.current || e.source !== frameRef.current.contentWindow) return;
      const msg = parseDenebHtmlMessage(e.data);
      if (!msg) return;
      if (msg.type === "height") {
        setHeight((h) => denebHtmlFrameHeight(h, msg.h));
        return;
      }
      const { onSubmit: submit, busy: b, interactive: it } = live.current;
      if (!b && it) submit(msg.text);
    }
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, []);

  return (
    <iframe
      ref={frameRef}
      className="deneb-html-frame"
      sandbox="allow-scripts"
      srcDoc={srcdoc}
      style={{ height }}
      title="웹 응답"
    />
  );
}
