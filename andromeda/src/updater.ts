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

// Check for a newer release and, if the user agrees, install + relaunch into it.
// No-op off-desktop (web build) or when already current. Throws on failure so the
// caller can decide whether to log silently (startup) or report it (settings UI).
export async function checkForUpdates(): Promise<UpdateResult> {
  if (!isTauri()) return { status: "unavailable" };
  const { check } = await import("@tauri-apps/plugin-updater");
  const update = await check();
  if (!update) {
    u.debug("already up to date");
    // No Update object to read currentVersion from; the build-time pkg version is
    // the closest source of truth for "what's running".
    return { status: "up-to-date", currentVersion: __APP_VERSION__ };
  }
  u.info("update available", update.version, "←", update.currentVersion);

  let downloaded = 0;
  await update.downloadAndInstall((event) => {
    if (event.event === "Progress") {
      downloaded += event.data.chunkLength;
      u.debug("downloading", downloaded);
    } else if (event.event === "Finished") {
      u.info("download finished, installing");
    }
  });

  // Don't yank the app out from under the user mid-task — ask first.
  const relaunchNow =
    typeof window === "undefined" || window.confirm(`새 버전 ${update.version} 설치 완료. 지금 재시작할까요?`);
  if (relaunchNow) {
    const { relaunch } = await import("@tauri-apps/plugin-process");
    await relaunch();
    return { status: "installed", version: update.version, currentVersion: update.currentVersion };
  }
  return { status: "deferred", version: update.version, currentVersion: update.currentVersion };
}
