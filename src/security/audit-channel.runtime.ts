import {
  isNumericTelegramUserId,
  normalizeTelegramAllowFromEntry,
} from "deneb/plugin-sdk/telegram";
import { isDiscordMutableAllowEntry } from "./mutable-allowlist-detectors.js";

export const auditChannelRuntime = {
  isDiscordMutableAllowEntry,
  isNumericTelegramUserId,
  normalizeTelegramAllowFromEntry,
};
