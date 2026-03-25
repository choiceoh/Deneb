import type { SandboxContext } from "./types.js";

export function createSandboxTestContext(params?: {
  overrides?: Partial<SandboxContext>;
  dockerOverrides?: Partial<SandboxContext["docker"]>;
}): SandboxContext {
  const overrides = params?.overrides ?? {};
  const { docker: _unusedDockerOverrides, ...sandboxOverrides } = overrides;
  const docker = {
    image: "deneb-sandbox:bookworm-slim",
    containerPrefix: "deneb-sbx-",
    network: "none",
    user: "1000:1000",
    workdir: "/workspace",
    readOnlyRoot: false,
    tmpfs: [],
    capDrop: [],
    seccompProfile: "",
    apparmorProfile: "",
    setupCommand: "",
    binds: [],
    dns: [],
    extraHosts: [],
    pidsLimit: 0,
    ...overrides.docker,
    ...params?.dockerOverrides,
  };

  return {
    enabled: true,
    backendId: "docker",
    sessionKey: "sandbox:test",
    workspaceDir: "/tmp/workspace",
    agentWorkspaceDir: "/tmp/workspace",
    workspaceAccess: "rw",
    runtimeId: "deneb-sbx-test",
    runtimeLabel: "deneb-sbx-test",
    containerName: "deneb-sbx-test",
    containerWorkdir: "/workspace",
    tools: { allow: ["*"], deny: [] },
    ...sandboxOverrides,
    docker,
  };
}
