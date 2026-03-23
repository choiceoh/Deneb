// Barrel re-export: actual implementations live in src/utils/*.ts
export { ensureDir, pathExists } from "./utils/fs.js";
export { clamp, clampInt, clampNumber } from "./utils/number.js";
export { escapeRegExp, sliceUtf16Safe, truncateUtf16Safe } from "./utils/string.js";
export { isPlainObject, isRecord, safeParseJson } from "./utils/type-guards.js";
export { sleep, waitFor, waitForEvent } from "./utils/async.js";
export {
  CONFIG_DIR,
  displayPath,
  displayString,
  resolveConfigDir,
  resolveHomeDir,
  resolveUserPath,
  shortenHomeInString,
  shortenHomePath,
} from "./utils/paths.js";
export {
  assertWebChannel,
  isSelfChatMode,
  jidToE164,
  normalizeE164,
  resolveJidToE164,
  toWhatsappJid,
} from "./utils/whatsapp.js";
export type { JidToE164Options, WebChannel } from "./utils/whatsapp.js";
export { formatTerminalLink } from "./utils/terminal-link.js";
