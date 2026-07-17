// Desktop auto-update. On launch the app asks the GitHub Releases endpoint
// (configured in src-tauri/tauri.conf.json → plugins.updater) whether a newer
// SIGNED build exists; if so it downloads, installs, and offers to relaunch into
// it. The plugin imports are dynamic so the web bundle never pulls them — same
// guard pattern as tauri.ts.
//
// checkForUpdates() returns a discriminated result so the caller (settings UI)
// can show what actually happened, instead of a guess. Errors are NOT swallowed
// here — the startup caller (App.tsx) catches and logs them itself so an update
// hiccup still never blocks startup, but the interactive caller gets to surface
// the real reason.
import { isTauri } from "./tauri";
import { log } from "./log";

const u = log.child("updater");

// What checkForUpdates() did. The settings UI maps each case to a message.
//   - "unavailable"   : off-desktop (web build) — auto-update isn't supported.
//   - "up-to-date"    : ran the check, no newer signed build was published.
//   - "installed"     : downloaded + installed a newer build (version set).
//   - "deferred"      : a newer build was installed but the user declined the
//                       relaunch prompt, so it'll apply on the next manual start.
export type UpdateResult =
  | { status: "unavailable" }
  | { status: "up-to-date"; currentVersion: string }
  | { status: "installed"; version: string; currentVersion: string }
  | { status: "deferred"; version: string; currentVersion: string };

// A newer signed build the user hasn't consented to yet. `install` downloads +
// installs on demand — the startup path surfaces this as a nudge instead of
// silently spending bandwidth/disk on an update nobody asked for.
export interface AvailableUpdate {
  version: string;
  currentVersion: string;
  /** Download + install; reports 0–100 progress (null while size unknown). */
  install: (onProgress?: (pct: number | null) => void) => Promise<void>;
}

// Check-only: returns the available update (with a deferred installer) or null.
// No-op off-desktop. Throws on check failure so callers decide how to surface it.
export async function checkForUpdate(): Promise<AvailableUpdate | null> {
  if (!isTauri()) return null;
  const { check } = await import("@tauri-apps/plugin-updater");
  const update = await check();
  if (!update) {
    u.debug("already up to date");
    return null;
  }
  u.info("update available", update.version, "←", update.currentVersion);
  return {
    version: update.version,
    currentVersion: update.currentVersion,
    install: async (onProgress) => {
      let downloaded = 0;
      let totalBytes: number | null = null;
      await update.downloadAndInstall((event) => {
        if (event.event === "Started") {
          totalBytes = event.data.contentLength ?? null;
        } else if (event.event === "Progress") {
          downloaded += event.data.chunkLength;
          onProgress?.(totalBytes ? Math.min(100, Math.round((downloaded / totalBytes) * 100)) : null);
          u.debug("downloading", downloaded);
        } else if (event.event === "Finished") {
          onProgress?.(100);
          u.info("download finished, installing");
        }
      });
    },
  };
}

export async function relaunchApp(): Promise<void> {
  const { relaunch } = await import("@tauri-apps/plugin-process");
  await relaunch();
}

// Check for a newer release and, if the user agrees, install + relaunch into it.
// User-initiated path (settings 업데이트 확인 button) — clicking IS the consent,
// so download+install proceeds directly. Startup uses checkForUpdate + the nudge.
export async function checkForUpdates(): Promise<UpdateResult> {
  if (!isTauri()) return { status: "unavailable" };
  const update = await checkForUpdate();
  if (!update) {
    // No Update object to read currentVersion from; the build-time pkg version is
    // the closest source of truth for "what's running".
    return { status: "up-to-date", currentVersion: __APP_VERSION__ };
  }
  await update.install();

  // Don't yank the app out from under the user mid-task — ask first.
  const relaunchNow =
    typeof window === "undefined" || window.confirm(`새 버전 ${update.version} 설치 완료. 지금 재시작할까요?`);
  if (relaunchNow) {
    await relaunchApp();
    return { status: "installed", version: update.version, currentVersion: update.currentVersion };
  }
  return { status: "deferred", version: update.version, currentVersion: update.currentVersion };
}
