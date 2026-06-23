import { useEffect, useMemo, useState } from "react";
import { Refine } from "@refinedev/core";
import { type GatewayConfig, loadConfig, saveConfig } from "./gateway";
import { denebDataProvider } from "./dataProvider";
import { denebAuthProvider } from "./authProvider";
import { refineResources } from "./resources";
import { readDesktopToken } from "./tauri";
import { checkForUpdates } from "./updater";
import { WorkspaceProvider } from "./workspaceContext";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { Workstation } from "./components/Workstation";

// App owns the gateway config and the Refine providers (data + auth, both derived
// from it). The workstation UI lives under <Refine> + <WorkspaceProvider>, where
// data hooks and shared workstation state are available.
export function App() {
  const [cfg, setCfg] = useState<GatewayConfig>(loadConfig());
  const dataProvider = useMemo(() => denebDataProvider(cfg), [cfg]);
  const authProvider = useMemo(() => denebAuthProvider(cfg), [cfg]);
  const connected = Boolean(cfg.url && cfg.token);

  // Desktop auto-update: check GitHub Releases for a newer signed build on launch.
  // No-op on the web build; failures are swallowed so they never block startup.
  useEffect(() => {
    void checkForUpdates();
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
      <Refine
        dataProvider={dataProvider}
        authProvider={authProvider}
        resources={refineResources}
        options={{ disableTelemetry: true }}
      >
        <WorkspaceProvider connected={connected} cfg={cfg} setCfg={setCfg}>
          <Workstation cfg={cfg} setCfg={setCfg} />
        </WorkspaceProvider>
      </Refine>
    </ErrorBoundary>
  );
}
