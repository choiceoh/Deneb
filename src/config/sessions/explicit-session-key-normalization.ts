import type { MsgContext } from "../../auto-reply/templating.js";

// Extension-specific normalizers (discord, etc.) removed — extensions no longer bundled.
// Session keys are returned as-is (already lowercased).

export function normalizeExplicitSessionKey(sessionKey: string, _ctx: MsgContext): string {
  return sessionKey.trim().toLowerCase();
}
