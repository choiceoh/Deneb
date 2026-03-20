/**
 * channel-bootstrap.ts — Deneb: 데네브가 지원하는 채널만 등록
 *
 * 업스트림에는 채널이 많지만 데네브는 Telegram만 지원.
 * 필요한 채널은 여기서만 추가하면 됨.
 */

import { registerChannel } from "./dynamic-registry.js";

// ── Telegram (데네브 유일 채널) ──
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

// ── 별칭 ──
// 필요한 별칭만 등록 (데네브에서 사용하는 것만)
