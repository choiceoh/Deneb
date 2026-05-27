// topics.ts — typed client for miniapp.topics.* RPCs.
//
// "Topics" are Telegram forum topics in the supergroup the operator
// migrated into via /use-forum. The Mini App can create new ones from
// the topics list without dropping back into Telegram's three-dot menu.

import { call } from './rpc';

export interface CreatedTopic {
  threadId: number;
  name: string;
  iconColor?: number;
}

export function createTopic(
  initData: string,
  input: { name: string; iconColor?: number },
): Promise<CreatedTopic> {
  return call<CreatedTopic>('miniapp.topics.create', input, initData);
}
