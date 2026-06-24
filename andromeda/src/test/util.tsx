// Shared test helpers: render the real workstation tree with an injectable data
// provider so panes can be exercised against fixtures (no gateway/network).
import type { ReactElement } from "react";
import { render } from "@testing-library/react";
import { Refine, type DataProvider } from "@refinedev/core";
import type { GatewayConfig } from "@/gateway";
import { denebAuthProvider } from "@/authProvider";
import { refineResources } from "@/resources";
import { WorkspaceProvider } from "@/workspaceContext";

// A DataProvider backed by in-memory fixtures keyed by resource name.
export function fakeProvider(fixtures: Record<string, any[]> = {}): DataProvider {
  return {
    getApiUrl: () => "http://test",
    getList: async ({ resource }) => {
      const data = fixtures[resource] ?? [];
      return { data, total: data.length };
    },
    getOne: async ({ resource, id }) => ({
      data: (fixtures[resource] ?? []).find((r) => String(r.id) === String(id)) ?? { id },
    }),
    create: async ({ variables }) => ({ data: { id: "new", ...(variables as object) } as any }),
    update: async ({ id, variables }) => ({ data: { id, ...(variables as object) } as any }),
    deleteOne: async ({ id }) => ({ data: { id } as any }),
  };
}

export function renderWithProviders(
  ui: ReactElement,
  opts: {
    connected?: boolean;
    dataProvider?: DataProvider;
    cfg?: GatewayConfig;
    setCfg?: (c: GatewayConfig) => void;
  } = {},
) {
  const connected = opts.connected ?? false;
  const cfg: GatewayConfig = opts.cfg ?? (connected ? { url: "http://test", token: "tok" } : { url: "", token: "" });
  return render(
    <Refine
      dataProvider={opts.dataProvider ?? fakeProvider()}
      authProvider={denebAuthProvider(cfg)}
      resources={refineResources}
      options={{ disableTelemetry: true }}
    >
      <WorkspaceProvider connected={connected} cfg={cfg} setCfg={opts.setCfg ?? (() => {})}>
        {ui}
      </WorkspaceProvider>
    </Refine>,
  );
}
