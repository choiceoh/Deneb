import { loadConfig } from "../../config/config.js";
import { getSandboxBackendManager } from "./backend.js";
import { readRegistry, removeRegistryEntry, type SandboxRegistryEntry } from "./registry.js";
import { resolveSandboxAgentId } from "./shared.js";

export type SandboxContainerInfo = SandboxRegistryEntry & {
  running: boolean;
  imageMatch: boolean;
};

export async function listSandboxContainers(): Promise<SandboxContainerInfo[]> {
  const config = loadConfig();
  const registry = await readRegistry();
  const results: SandboxContainerInfo[] = [];

  for (const entry of registry.entries) {
    const backendId = entry.backendId ?? "docker";
    const manager = getSandboxBackendManager(backendId);
    if (!manager) {
      results.push({
        ...entry,
        running: false,
        imageMatch: true,
      });
      continue;
    }
    const agentId = resolveSandboxAgentId(entry.sessionKey);
    const runtime = await manager.describeRuntime({
      entry,
      config,
      agentId,
    });
    results.push({
      ...entry,
      image: runtime.actualConfigLabel ?? entry.image,
      running: runtime.running,
      imageMatch: runtime.configLabelMatch,
    });
  }

  return results;
}

export async function removeSandboxContainer(containerName: string): Promise<void> {
  const config = loadConfig();
  const registry = await readRegistry();
  const entry = registry.entries.find((item) => item.containerName === containerName);
  if (entry) {
    const manager = getSandboxBackendManager(entry.backendId ?? "docker");
    await manager?.removeRuntime({
      entry,
      config,
      agentId: resolveSandboxAgentId(entry.sessionKey),
    });
  }
  await removeRegistryEntry(containerName);
}
