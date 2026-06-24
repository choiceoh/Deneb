import { useState } from "react";

import { log } from "@/log";
import { isTauri } from "@/tauri";

import { Icon } from "./Icon";

// Frameless-window controls. The native title bar is turned off
// (`decorations: false` in tauri.conf.json), so on the desktop we draw our own:
// a compact minimize/maximize/close cluster that tucks into the top of the nav
// rail (rendered as the rail's first child). There's no titlebar band — the rail
// itself is the drag handle (data-tauri-drag-region on <nav>), so the panels rise
// to the window's top edge. Styled quiet in the warm Zen tone.
//
// Renders nothing on the web build — there's no OS window to drive there, and it
// keeps @tauri-apps/api out of the web bundle (the window API is imported lazily,
// only when a control is actually clicked inside Tauri).
const tlog = log.child("titlebar");

type WinAction = "minimize" | "toggleMaximize" | "close";

async function runWindow(action: WinAction): Promise<void> {
  try {
    const { getCurrentWindow } = await import("@tauri-apps/api/window");
    const w = getCurrentWindow();
    if (action === "minimize") await w.minimize();
    else if (action === "toggleMaximize") await w.toggleMaximize();
    else await w.close();
  } catch (e) {
    tlog.error(action, e);
  }
}

export function WindowControls() {
  // isTauri is stable for the session; compute once so the web build short-circuits.
  const [tauri] = useState(isTauri);
  if (!tauri) return null;

  return (
    <div className="win-ctls">
      <button className="win-ctl" onClick={() => void runWindow("minimize")} title="최소화" aria-label="최소화">
        <Icon name="win-min" size={14} />
      </button>
      <button className="win-ctl" onClick={() => void runWindow("toggleMaximize")} title="최대화" aria-label="최대화">
        <Icon name="win-max" size={13} />
      </button>
      <button className="win-ctl close" onClick={() => void runWindow("close")} title="닫기" aria-label="닫기">
        <Icon name="close" size={14} />
      </button>
    </div>
  );
}
