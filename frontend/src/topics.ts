// topics.ts — typed client for the miniapp.topics.* RPC surface.
//
// Telegram owns the topic data — deneb keeps no topic store. createTopic
// is just a thin wrapper that asks the gateway to call Bot API
// createForumTopic on our behalf, injecting the active home's chat ID
// so the Mini App doesn't have to track which supergroup is in play.

import { call } from './rpc';

export interface CreateTopicResult {
  messageThreadId: number;
  name: string;
  iconColor?: number;
  chatId: number;
}

/**
 * createTopic asks the gateway to create a new forum topic in the chat
 * /use-forum bound. The Mini App passes just the user-supplied name (and
 * optionally an icon color); the gateway resolves the chat from the
 * persisted active-home setting and surfaces upstream errors verbatim.
 *
 * Errors the caller should distinguish:
 *  - VALIDATION_FAILED ("active home not configured") — user hasn't
 *    run /use-forum yet; show them the migration hint.
 *  - DEPENDENCY_FAILED ("not enough rights to manage topics") — bot is
 *    admin but lost Manage Topics; show the Telegram-side fix.
 */
export function createTopic(
  initData: string,
  name: string,
  iconColor?: number,
): Promise<CreateTopicResult> {
  const params: Record<string, unknown> = { name };
  if (iconColor) params.iconColor = iconColor;
  return call<CreateTopicResult>('miniapp.topics.create', params, initData);
}
