import {
  isNumericTelegramUserId,
  normalizeTelegramAllowFromEntry,
} from "deneb/plugin-sdk/telegram";
import {
  isDiscordMutableAllowEntry,
  isZalouserMutableGroupEntry,
} from "./mutable-allowlist-detectors.js";

export const auditChannelRuntime = {
  isDiscordMutableAllowEntry,
  isZalouserMutableGroupEntry,
  isNumericTelegramUserId,
  normalizeTelegramAllowFromEntry,
};
