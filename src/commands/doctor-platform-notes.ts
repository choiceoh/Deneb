import os from "node:os";
import { note } from "../terminal/note.js";

export function noteDeprecatedLegacyEnvVars(
  env: NodeJS.ProcessEnv = process.env,
  deps?: { noteFn?: typeof note },
) {
  const entries = Object.entries(env)
    .filter(([key, value]) => key.startsWith("CLAWDBOT_") && value?.trim())
    .map(([key]) => key);
  if (entries.length === 0) {
    return;
  }

  const lines = [
    "- Deprecated legacy environment variables detected (ignored).",
    "- Use DENEB_* equivalents instead:",
    ...entries.map((key) => {
      const suffix = key.slice(key.indexOf("_") + 1);
      return `  ${key} -> DENEB_${suffix}`;
    }),
  ];
  (deps?.noteFn ?? note)(lines.join("\n"), "Environment");
}

function isTruthyEnvValue(value: string | undefined): boolean {
  return typeof value === "string" && value.trim().length > 0;
}

function isTmpCompileCachePath(cachePath: string): boolean {
  const normalized = cachePath.trim().replace(/\/+$/, "");
  return (
    normalized === "/tmp" ||
    normalized.startsWith("/tmp/") ||
    normalized === "/private/tmp" ||
    normalized.startsWith("/private/tmp/")
  );
}

export function noteStartupOptimizationHints(
  env: NodeJS.ProcessEnv = process.env,
  deps?: {
    platform?: NodeJS.Platform;
    arch?: string;
    totalMemBytes?: number;
    noteFn?: typeof note;
  },
) {
  const arch = deps?.arch ?? os.arch();
  const totalMemBytes = deps?.totalMemBytes ?? os.totalmem();
  const isArmHost = arch === "arm64";
  const isLowMemoryLinux = totalMemBytes > 0 && totalMemBytes <= 8 * 1024 ** 3;
  const isStartupTuneTarget = isArmHost || isLowMemoryLinux;
  if (!isStartupTuneTarget) {
    return;
  }

  const noteFn = deps?.noteFn ?? note;
  const compileCache = env.NODE_COMPILE_CACHE?.trim() ?? "";
  const disableCompileCache = env.NODE_DISABLE_COMPILE_CACHE?.trim() ?? "";
  const noRespawn = env.DENEB_NO_RESPAWN?.trim() ?? "";
  const lines: string[] = [];

  if (!compileCache) {
    lines.push(
      "- NODE_COMPILE_CACHE is not set; repeated CLI runs can be slower on small hosts (Pi/VM).",
    );
  } else if (isTmpCompileCachePath(compileCache)) {
    lines.push(
      "- NODE_COMPILE_CACHE points to /tmp; use /var/tmp so cache survives reboots and warms startup reliably.",
    );
  }

  if (isTruthyEnvValue(disableCompileCache)) {
    lines.push("- NODE_DISABLE_COMPILE_CACHE is set; startup compile cache is disabled.");
  }

  if (noRespawn !== "1") {
    lines.push(
      "- DENEB_NO_RESPAWN is not set to 1; set it to avoid extra startup overhead from self-respawn.",
    );
  }

  if (lines.length === 0) {
    return;
  }

  const suggestions = [
    "- Suggested env for low-power hosts:",
    "  export NODE_COMPILE_CACHE=/var/tmp/deneb-compile-cache",
    "  mkdir -p /var/tmp/deneb-compile-cache",
    "  export DENEB_NO_RESPAWN=1",
    isTruthyEnvValue(disableCompileCache) ? "  unset NODE_DISABLE_COMPILE_CACHE" : undefined,
  ].filter((line): line is string => Boolean(line));

  noteFn([...lines, ...suggestions].join("\n"), "Startup optimization");
}
