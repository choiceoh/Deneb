/**
 * Skill plugin system — barrel re-export.
 *
 * Core skill loading, filtering, and prompt building has a parallel Go
 * implementation in gateway-go/internal/skills/. The TypeScript
 * implementation is retained for backward compatibility but will be
 * replaced once all consumers migrate to the Go RPC endpoints.
 */
import type { DenebConfig } from "../config/config.js";
import type { SkillsInstallPreferences } from "./skills/types.js";

// --- RPC client functions (Go gateway) ---
export {
  discoverSkills,
  getSkillStatus,
  installSkillViaRpc,
  loadSkillCommands,
  loadSkillEntries,
  loadSkillSnapshot,
  setSkillRpcCaller,
} from "./skills/rpc-client.js";

// --- Legacy TS implementation (retained for consumers not yet on RPC) ---
export {
  hasBinary,
  isBundledSkillAllowed,
  isConfigPathTruthy,
  resolveBundledAllowlist,
  resolveConfigPath,
  resolveRuntimePlatform,
  resolveSkillConfig,
} from "./skills/config.js";
export {
  applySkillEnvOverrides,
  applySkillEnvOverridesFromSnapshot,
} from "./skills/env-overrides.js";
export {
  buildWorkspaceSkillSnapshot,
  buildWorkspaceSkillsPrompt,
  buildWorkspaceSkillCommandSpecs,
  filterWorkspaceSkillEntries,
  loadWorkspaceSkillEntries,
  resolveSkillsPromptForRun,
  syncSkillsToWorkspace,
} from "./skills/workspace.js";

// --- Types ---
export type {
  DenebSkillMetadata,
  Skill,
  SkillCommandSpec,
  SkillEligibilityContext,
  SkillEntry,
  SkillInstallSpec,
  SkillInvocationPolicy,
  SkillSnapshot,
  SkillsInstallPreferences,
  ParsedSkillFrontmatter,
} from "./skills/types.js";

// --- Utility ---
export function resolveSkillsInstallPreferences(config?: DenebConfig): SkillsInstallPreferences {
  const raw = config?.skills?.install;
  const preferBrew = raw?.preferBrew ?? true;
  const managerRaw = typeof raw?.nodeManager === "string" ? raw.nodeManager.trim() : "";
  const manager = managerRaw.toLowerCase();
  const nodeManager: SkillsInstallPreferences["nodeManager"] =
    manager === "pnpm" || manager === "yarn" || manager === "bun" || manager === "npm"
      ? manager
      : "npm";
  return { preferBrew, nodeManager };
}
