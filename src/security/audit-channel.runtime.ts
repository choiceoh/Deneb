import {
  isNumericTelegramUserId,
  normalizeTelegramAllowFromEntry,
} from "deneb/plugin-sdk/telegram";
// Stub: pairing store removed.
async function readChannelAllowFromStore(): Promise<string[]> {
  return [];
}
import {
  isDiscordMutableAllowEntry,
  isZalouserMutableGroupEntry,
} from "./mutable-allowlist-detectors.js";

export const auditChannelRuntime = {
  readChannelAllowFromStore,
  isDiscordMutableAllowEntry,
  isZalouserMutableGroupEntry,
  isNumericTelegramUserId,
  normalizeTelegramAllowFromEntry,
};
