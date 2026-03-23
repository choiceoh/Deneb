/**
 * Channel IDs are managed by the dynamic registry.
 * ChatChannelId is widened to string so adding new channels requires no core changes.
 *
 * Backward-compatible: existing code referencing ChatChannelId still works.
 */

// No longer a fixed list — determined at runtime by the dynamic registry.
// Use getChatChannelOrder() to get the currently registered channel list.
export type ChatChannelId = string;

export const CHANNEL_IDS: string[] = []; // populated at runtime
