import path from "node:path";
import { CHANNEL_IDS } from "../../channels/ids.js";
import { STATE_DIR } from "../../config/paths.js";

export const DEFAULT_SANDBOX_WORKSPACE_ROOT = path.join(STATE_DIR, "sandboxes");

export const DEFAULT_SANDBOX_IMAGE = "deneb-sandbox:bookworm-slim";
export const DEFAULT_SANDBOX_CONTAINER_PREFIX = "deneb-sbx-";
export const DEFAULT_SANDBOX_WORKDIR = "/workspace";
export const DEFAULT_SANDBOX_IDLE_HOURS = 24;
export const DEFAULT_SANDBOX_MAX_AGE_DAYS = 7;

export const DEFAULT_TOOL_ALLOW = [
  "exec",
  "process",
  "read",
  "write",
  "edit",
  "apply_patch",
  "image",
  "sessions_list",
  "sessions_history",
  "sessions_send",
  "sessions_spawn",
  "sessions_yield",
  "subagents",
  "session_status",
] as const;

// Provider docking: keep sandbox policy aligned with provider tool names.
export const DEFAULT_TOOL_DENY = ["canvas", "nodes", "cron", "gateway", ...CHANNEL_IDS] as const;

export const DEFAULT_SANDBOX_COMMON_IMAGE = "deneb-sandbox-common:bookworm-slim";

export const SANDBOX_AGENT_WORKSPACE_MOUNT = "/agent";

export const SANDBOX_STATE_DIR = path.join(STATE_DIR, "sandbox");
export const SANDBOX_REGISTRY_PATH = path.join(SANDBOX_STATE_DIR, "containers.json");
