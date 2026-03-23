/**
 * channel-bootstrap.ts — Register Deneb-supported channels
 *
 * Upstream supports many channels, but Deneb only supports Telegram.
 * Add new channels here as needed.
 */

import { registerChannel } from "./dynamic-registry.js";

// ── Telegram (only channel in Deneb) ──
registerChannel({
  id: "telegram",
  label: "Telegram",
  selectionLabel: "Telegram (Bot API)",
  detailLabel: "Telegram Bot",
  docsPath: "/channels/telegram",
  docsLabel: "telegram",
  blurb: "simplest way to get started — register a bot with @BotFather and get going.",
  systemImage: "paperplane",
  selectionDocsPrefix: "",
  selectionDocsOmitLabel: true,
});

// ── Aliases ──
// Register only aliases used in Deneb (none currently).
