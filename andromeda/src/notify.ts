// OS-native notification for proactive nudges — only when the window isn't
// focused (in-app the ProactivePanel already shows them) and only on desktop
// (web build no-ops). Permission is requested lazily on first use.
import { isTauri } from "./tauri";
import { log } from "./log";

const n = log.child("notify");

export async function notifyDesktop(title: string, body: string): Promise<void> {
  if (!isTauri()) return;
  if (typeof document !== "undefined" && document.hasFocus()) return;
  try {
    const { isPermissionGranted, requestPermission, sendNotification } =
      await import("@tauri-apps/plugin-notification");
    let granted = await isPermissionGranted();
    if (!granted) granted = (await requestPermission()) === "granted";
    if (granted) sendNotification({ title, body });
  } catch (e) {
    n.debug("notification skipped", e);
  }
}
