import { spawnSync } from "node:child_process";
import os from "node:os";

export type OsSummary = {
  platform: NodeJS.Platform;
  arch: string;
  release: string;
  label: string;
};

const PLATFORM_LABELS: Partial<Record<NodeJS.Platform, string>> = {
  darwin: "macos",
  win32: "windows",
};

function resolveDarwinVersion(): string | null {
  try {
    const result = spawnSync("sw_vers", ["-productVersion"], {
      encoding: "utf8",
      timeout: 5000,
      stdio: ["ignore", "pipe", "pipe"],
    });
    const version = result.stdout?.trim();
    return version && version.length > 0 ? version : null;
  } catch {
    return null;
  }
}

export function resolveOsSummary(): OsSummary {
  const platform = os.platform();
  const release = os.release();
  const arch = os.arch();
  const platformLabel = PLATFORM_LABELS[platform] ?? platform;
  const version = platform === "darwin" ? (resolveDarwinVersion() ?? release) : release;
  const label = `${platformLabel} ${version} (${arch})`;
  return { platform, arch, release, label };
}
