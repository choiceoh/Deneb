import { useEffect, useMemo, useState } from "react";
import { DataProviderScope } from "@/crud";
import { type GatewayConfig, loadConfig, saveConfig } from "./gateway";
import { denebDataProvider } from "./dataProvider";
import { isTauri, readDesktopToken } from "./tauri";
import { initZoom } from "./zoom";
import { type AvailableUpdate, checkForUpdate } from "./updater";
import { errText } from "./format";
import { log } from "./log";
import { WorkspaceProvider } from "./WorkspaceProvider";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { Workstation } from "./components/Workstation";
import { Toasts } from "./components/Toasts";
import { UpdateNudge } from "./components/UpdateNudge";

// App owns the gateway config and the DIY data provider derived from it.
// The workstation UI lives under <DataProviderScope> + <WorkspaceProvider>.
export function App() {
  const [cfg, setCfg] = useState<GatewayConfig>(loadConfig());
  const dataProvider = useMemo(() => denebDataProvider(cfg), [cfg]);
  const connected = Boolean(cfg.url && cfg.token);

  // Desktop auto-update: check GitHub Releases for a newer signed build on launch.
  // Check ONLY — installing waits for consent via the UpdateNudge pill (no silent
  // bandwidth/disk spend). No-op on the web build; failures just log so a
  // transient update hiccup never blocks startup.
  const [update, setUpdate] = useState<AvailableUpdate | null>(null);
  useEffect(() => {
    checkForUpdate()
      .then((u) => setUpdate(u))
      .catch((e) => log.child("updater").warn("startup update check failed", errText(e)));
  }, []);

  // Desktop chrome: persistent zoom (Ctrl+휠/±) and no default webview context
  // menu outside editable fields — custom row menus call preventDefault first,
  // so this only swallows the meaningless browser menu on static chrome.
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

  // Desktop auto-connect: if we have no token yet, pull it from the OS keychain /
  // ~/.deneb/client_token so the live gateway connects without manual entry.
  useEffect(() => {
    if (cfg.token) return;
    let cancelled = false;
    void readDesktopToken().then((token) => {
      if (cancelled || !token) return;
      setCfg((c) => {
        if (c.token) return c;
        const next = { ...c, token };
        saveConfig(next);
        return next;
      });
    });
    return () => {
      cancelled = true;
    };
  }, [cfg.token]);

  return (
    <ErrorBoundary>
      <DataProviderScope dataProvider={dataProvider}>
        <WorkspaceProvider connected={connected} cfg={cfg} setCfg={setCfg}>
          <Workstation cfg={cfg} />
          {update && <UpdateNudge update={update} onDismiss={() => setUpdate(null)} />}
          <Toasts />
        </WorkspaceProvider>
      </DataProviderScope>
    </ErrorBoundary>
  );
}
