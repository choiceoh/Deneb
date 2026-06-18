import { useMemo, useState } from "react";
import { Refine } from "@refinedev/core";
import { type GatewayConfig, loadConfig } from "./gateway";
import { denebDataProvider } from "./dataProvider";
import { denebAuthProvider } from "./authProvider";
import { refineResources } from "./resources";
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

  return (
    <ErrorBoundary>
      <Refine
        dataProvider={dataProvider}
        authProvider={authProvider}
        resources={refineResources}
        options={{ disableTelemetry: true }}
      >
        <WorkspaceProvider connected={connected} cfg={cfg}>
          <Workstation cfg={cfg} setCfg={setCfg} />
        </WorkspaceProvider>
      </Refine>
    </ErrorBoundary>
  );
}
