import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { resolveGatewaySystemdServiceName } from "../../daemon/constants.js";

/**
 * Shell-escape a string for embedding in single-quoted shell arguments.
 * Replaces every `'` with `'\''` (end quote, escaped quote, resume quote).
 */
function shellEscape(value: string): string {
  return value.replace(/'/g, "'\\''");
}

function resolveSystemdUnit(env: NodeJS.ProcessEnv): string {
  const override = env.DENEB_SYSTEMD_UNIT?.trim();
  if (override) {
    return override.endsWith(".service") ? override : `${override}.service`;
  }
  return `${resolveGatewaySystemdServiceName(env.DENEB_PROFILE)}.service`;
}

/**
 * Prepares a standalone script to restart the gateway service.
 * This script is written to a temporary directory and does not depend on
 * the installed package files, ensuring restart capability even if the
 * update process temporarily removes or corrupts installation files.
 */
export async function prepareRestartScript(
  env: NodeJS.ProcessEnv = process.env,
): Promise<string | null> {
  const tmpDir = os.tmpdir();
  const timestamp = Date.now();
  const platform = process.platform;

  let scriptContent = "";
  let filename = "";

  try {
    if (platform === "linux") {
      const unitName = resolveSystemdUnit(env);
      const escaped = shellEscape(unitName);
      filename = `deneb-restart-${timestamp}.sh`;
      scriptContent = `#!/bin/sh
# Standalone restart script — survives parent process termination.
# Wait briefly to ensure file locks are released after update.
sleep 1
systemctl --user restart '${escaped}'
# Self-cleanup
rm -f "$0"
`;
    } else {
      return null;
    }

    const scriptPath = path.join(tmpDir, filename);
    await fs.writeFile(scriptPath, scriptContent, { mode: 0o755 });
    return scriptPath;
  } catch {
    // If we can't write the script, we'll fall back to the standard restart method
    return null;
  }
}

/**
 * Executes the prepared restart script as a **detached** process.
 *
 * The script must outlive the CLI process because the CLI itself is part
 * of the service being restarted — `systemctl restart` / `launchctl
 * kickstart -k` will terminate the current process tree.  Using
 * `spawn({ detached: true })` + `unref()` ensures the script survives
 * the parent's exit.
 *
 * Resolves immediately after spawning; the script runs independently.
 */
export async function runRestartScript(scriptPath: string): Promise<void> {
  const child = spawn("/bin/sh", [scriptPath], {
    detached: true,
    stdio: "ignore",
  });
  child.unref();
}
