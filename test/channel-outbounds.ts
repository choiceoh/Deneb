// Outbound adapter stubs for removed extensions.
// Kept as lightweight stubs so test files that import them still compile.

import type { ChannelOutboundAdapter } from "../src/channels/plugins/types.js";

// eslint-disable-next-line @typescript-eslint/no-explicit-any
const stubOutbound: ChannelOutboundAdapter = {} as any;

export const discordOutbound: ChannelOutboundAdapter = stubOutbound;
export const imessageOutbound: ChannelOutboundAdapter = stubOutbound;
export const signalOutbound: ChannelOutboundAdapter = stubOutbound;
export const slackOutbound: ChannelOutboundAdapter = stubOutbound;
export { telegramOutbound } from "../extensions/telegram/src/outbound-adapter.js";
export const whatsappOutbound: ChannelOutboundAdapter = stubOutbound;
