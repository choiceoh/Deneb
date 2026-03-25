import process from "node:process";
import { fileURLToPath } from "node:url";
import { normalizeEnv } from "../infra/env.js";
import { formatUncaughtError } from "../infra/errors.js";
import { isMainModule } from "../infra/is-main.js";
import { ensureDenebCliOnPath } from "../infra/path-env.js";
import { assertSupportedRuntime } from "../infra/runtime-guard.js";
import { getCommandPathWithRootOptions, hasHelpOrVersion } from "./argv.js";
import { applyCliProfileEnv, parseCliProfileArgs } from "./profile.js";

async function closeCliMemoryManagers(): Promise<void> {
  try {
    const { closeAllMemorySearchManagers } = await import("../memory/search-manager.js");
    await closeAllMemorySearchManagers();
  } catch {
    // Best-effort teardown for short-lived CLI processes.
  }
}

export function rewriteUpdateFlagArgv(argv: string[]): string[] {
  const index = argv.indexOf("--update");
  if (index === -1) {
    return argv;
  }

  const next = [...argv];
  next.splice(index, 1, "update");
  return next;
}

export function shouldEnsureCliPath(argv: string[]): boolean {
  if (hasHelpOrVersion(argv)) {
    return false;
  }
  const [primary, secondary] = getCommandPathWithRootOptions(argv, 2);
  if (!primary) {
    return true;
  }
  if (primary === "status" || primary === "health" || primary === "sessions") {
    return false;
  }
  if (primary === "config" && (secondary === "get" || secondary === "unset")) {
    return false;
  }
  if (primary === "models" && (secondary === "list" || secondary === "status")) {
    return false;
  }
  return true;
}

/**
 * CLI command tree has been removed. This stub preserves the runCli export
 * so that src/index.ts (legacy entry) continues to resolve, but all CLI
 * commands are unavailable. The gateway and agent runtime do not depend on
 * the CLI command tree — they use src/commands/ and src/gateway/ directly.
 */
export async function runCli(argv: string[] = process.argv) {
  const parsedProfile = parseCliProfileArgs(argv);
  if (!parsedProfile.ok) {
    throw new Error(parsedProfile.error);
  }
  if (parsedProfile.profile) {
    applyCliProfileEnv({ profile: parsedProfile.profile });
  }

  normalizeEnv();
  if (shouldEnsureCliPath(argv)) {
    ensureDenebCliOnPath();
  }

  assertSupportedRuntime();

  try {
    const { installUnhandledRejectionHandler } = await import("../infra/unhandled-rejections.js");
    installUnhandledRejectionHandler();

    process.on("uncaughtException", (error) => {
      console.error("[deneb] Uncaught exception:", formatUncaughtError(error));
      process.exit(1);
    });

    console.error(
      "[deneb] CLI command tree is not available. Use the gateway or agent runtime directly.",
    );
    process.exit(1);
  } finally {
    await closeCliMemoryManagers();
  }
}

export function isCliMainModule(): boolean {
  return isMainModule({ currentFile: fileURLToPath(import.meta.url) });
}
