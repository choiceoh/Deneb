// Barrel exports for the web channel pieces. Splitting the original 900+ line
// module keeps responsibilities small and testable.
// NOTE: WhatsApp-specific exports were removed along with the WhatsApp channel code.

export { HEARTBEAT_PROMPT } from "./auto-reply/heartbeat.js";
export { HEARTBEAT_TOKEN } from "./auto-reply/tokens.js";
export { loadWebMedia, optimizeImageToJpeg } from "./media/web-media.js";
