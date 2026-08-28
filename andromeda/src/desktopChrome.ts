import { useEffect } from "react";

import { isTauri } from "./tauri";
import { initZoom } from "./zoom";

// Desktop webview chrome shared by every window (workstation · Cygnus):
// persistent Ctrl+휠/± zoom and no default context menu outside editable fields
// (custom row menus call preventDefault first, so this only swallows the
// meaningless browser menu on static chrome). Extracted from App so a second
// window can't silently drift from the main one.
export function useDesktopChrome(): void {
  useEffect(() => {
    const cleanupZoom = initZoom();
    function onContextMenu(e: MouseEvent) {
      if (!isTauri() || e.defaultPrevented) return;
      const t = e.target as HTMLElement | null;
      if (t?.closest("input, textarea, [contenteditable='true']")) return;
      e.preventDefault();
    }
    document.addEventListener("contextmenu", onContextMenu);
    return () => {
      cleanupZoom();
      document.removeEventListener("contextmenu", onContextMenu);
    };
  }, []);
}
