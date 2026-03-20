// Outbound adapter stubs for removed extensions.
// Kept as lightweight stubs so test files that import them still compile.

import type { ChannelOutboundAdapter } from "../src/channels/plugins/types.js";

const stubOutbound = {} as unknown as ChannelOutboundAdapter;

export const discordOutbound: ChannelOutboundAdapter = stubOutbound;
export const imessageOutbound: ChannelOutboundAdapter = stubOutbound;
export const signalOutbound: ChannelOutboundAdapter = stubOutbound;
export const slackOutbound: ChannelOutboundAdapter = stubOutbound;
export { telegramOutbound } from "../extensions/telegram/src/outbound-adapter.js";
export const whatsappOutbound: ChannelOutboundAdapter = stubOutbound;
