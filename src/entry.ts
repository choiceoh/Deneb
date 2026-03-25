#!/usr/bin/env node
import { spawn } from "node:child_process";
import { enableCompileCache } from "node:module";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { isRootHelpInvocation, isRootVersionInvocation } from "./cli/argv.js";
import { applyCliProfileEnv, parseCliProfileArgs } from "./cli/profile.js";
import { shouldSkipRespawnForArgv } from "./cli/respawn-policy.js";
import { ensureDenebExecMarkerOnProcess } from "./infra/deneb-exec-env.js";
import { isTruthyEnvValue, normalizeEnv } from "./infra/env.js";
import { isMainModule } from "./infra/is-main.js";
import { installProcessWarningFilter } from "./infra/warning-filter.js";
import { attachChildProcessBridge } from "./process/child-process-bridge.js";

const ENTRY_WRAPPER_PAIRS = [
  { wrapperBasename: "deneb.mjs", entryBasename: "entry.js" },
  { wrapperBasename: "deneb.js", entryBasename: "entry.js" },
] as const;

function shouldForceReadOnlyAuthStore(argv: string[]): boolean {
  const tokens = argv.slice(2).filter((token) => token.length > 0 && !token.startsWith("-"));
  for (let index = 0; index < tokens.length - 1; index += 1) {
    if (tokens[index] === "secrets" && tokens[index + 1] === "audit") {
      return true;
    }
  }
  return false;
}

// Guard: only run entry-point logic when this file is the main module.
// The bundler may import entry.js as a shared dependency when dist/index.js
// is the actual entry point; without this guard the top-level code below
// would call runCli a second time, starting a duplicate gateway that fails
// on the lock / port and crashes the process.
if (
  !isMainModule({
    currentFile: fileURLToPath(import.meta.url),
    wrapperEntryPairs: [...ENTRY_WRAPPER_PAIRS],
  })
) {
  // Imported as a dependency — skip all entry-point side effects.
} else {
  // DGX SPARK: set UV_THREADPOOL_SIZE before any async I/O
  const { applyDgxSparkEnvTuning } = await import("./infra/dgx-spark-init.js");
  applyDgxSparkEnvTuning();

  const { installGaxiosFetchCompat } = await import("./infra/gaxios-fetch-compat.js");

  await installGaxiosFetchCompat();
  process.title = "deneb";
  ensureDenebExecMarkerOnProcess();
  installProcessWarningFilter();
  normalizeEnv();
  if (!isTruthyEnvValue(process.env.NODE_DISABLE_COMPILE_CACHE)) {
    try {
      enableCompileCache();
    } catch {
      // Best-effort only; never block startup.
    }
  }

  if (shouldForceReadOnlyAuthStore(process.argv)) {
    process.env.DENEB_AUTH_STORE_READONLY = "1";
  }

  if (process.argv.includes("--no-color")) {
    process.env.NO_COLOR = "1";
    process.env.FORCE_COLOR = "0";
  }

  const EXPERIMENTAL_WARNING_FLAG = "--disable-warning=ExperimentalWarning";

  function hasExperimentalWarningSuppressed(): boolean {
    const nodeOptions = process.env.NODE_OPTIONS ?? "";
    if (nodeOptions.includes(EXPERIMENTAL_WARNING_FLAG) || nodeOptions.includes("--no-warnings")) {
      return true;
    }
    for (const arg of process.execArgv) {
      if (arg === EXPERIMENTAL_WARNING_FLAG || arg === "--no-warnings") {
        return true;
      }
    }
    return false;
  }

  function ensureExperimentalWarningSuppressed(): boolean {
    if (shouldSkipRespawnForArgv(process.argv)) {
      return false;
    }
    if (isTruthyEnvValue(process.env.DENEB_NO_RESPAWN)) {
      return false;
    }
    if (isTruthyEnvValue(process.env.DENEB_NODE_OPTIONS_READY)) {
      return false;
    }
    if (hasExperimentalWarningSuppressed()) {
      return false;
    }

    // Respawn guard (and keep recursion bounded if something goes wrong).
    process.env.DENEB_NODE_OPTIONS_READY = "1";
    // Pass flag as a Node CLI option, not via NODE_OPTIONS (--disable-warning is disallowed in NODE_OPTIONS).
    const child = spawn(
      process.execPath,
      [EXPERIMENTAL_WARNING_FLAG, ...process.execArgv, ...process.argv.slice(1)],
      {
        stdio: "inherit",
        env: process.env,
      },
    );

    attachChildProcessBridge(child);

    child.once("exit", (code, signal) => {
      if (signal) {
        process.exitCode = 1;
        return;
      }
      process.exit(code ?? 1);
    });

    child.once("error", (error) => {
      console.error(
        "[deneb] Failed to respawn CLI:",
        error instanceof Error ? (error.stack ?? error.message) : error,
      );
      process.exit(1);
    });

    // Parent must not continue running the CLI.
    return true;
  }

  function tryHandleRootVersionFastPath(argv: string[]): boolean {
    if (!isRootVersionInvocation(argv)) {
      return false;
    }
    Promise.all([import("./version.js"), import("./infra/git-commit.js")])
      .then(([{ VERSION }, { resolveCommitHash }]) => {
        const commit = resolveCommitHash({ moduleUrl: import.meta.url });
        console.log(commit ? `Deneb ${VERSION} (${commit})` : `Deneb ${VERSION}`);
        process.exit(0);
      })
      .catch((error) => {
        console.error(
          "[deneb] Failed to resolve version:",
          error instanceof Error ? (error.stack ?? error.message) : error,
        );
        process.exit(1);
      });
    return true;
  }

  if (!ensureExperimentalWarningSuppressed()) {
    const parsed = parseCliProfileArgs(process.argv);
    if (!parsed.ok) {
      // Keep it simple; Commander will handle rich help/errors after we strip flags.
      console.error(`[deneb] ${parsed.error}`);
      process.exit(2);
    }

    if (parsed.profile) {
      applyCliProfileEnv({ profile: parsed.profile });
      // Keep Commander and ad-hoc argv checks consistent.
      process.argv = parsed.argv;
    }

    if (!tryHandleRootVersionFastPath(process.argv)) {
      runMainOrRootHelp(process.argv);
    }
  }
}

export function tryHandleRootHelpFastPath(
  argv: string[],
  deps: {
    outputRootHelp?: () => void;
    onError?: (error: unknown) => void;
  } = {},
): boolean {
  if (!isRootHelpInvocation(argv)) {
    return false;
  }
  const handleError =
    deps.onError ??
    ((error: unknown) => {
      console.error(
        "[deneb] Failed to display help:",
        error instanceof Error ? (error.stack ?? error.message) : error,
      );
      process.exitCode = 1;
    });
  if (deps.outputRootHelp) {
    try {
      deps.outputRootHelp();
    } catch (error) {
      handleError(error);
    }
    return true;
  }
  // CLI command tree removed; show a minimal help message.
  console.log(
    "deneb — CLI command tree is not available. Use the gateway or agent runtime directly.",
  );
  process.exitCode = 0;
  return true;
}

function runMainOrRootHelp(argv: string[]): void {
  if (tryHandleRootHelpFastPath(argv)) {
    return;
  }
  import("./cli/run-main.js")
    .then(({ runCli }) => runCli(argv))
    .catch((error) => {
      console.error(
        "[deneb] Failed to start CLI:",
        error instanceof Error ? (error.stack ?? error.message) : error,
      );
      process.exitCode = 1;
    });
}
