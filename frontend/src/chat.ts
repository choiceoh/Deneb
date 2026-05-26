// chat.ts — typed client for the miniapp.chat.send RPC.

import { call } from './rpc';

export interface ChatResult {
  sessionKey: string;
  response: string;
  model?: string;
  stopReason?: string;
  durationMs: number;
  inputTokens?: number;
  outputTokens?: number;
}

export interface ChatSendOptions {
  sessionKey?: string;
  model?: string;
}

export function sendChat(
  initData: string,
  message: string,
  opts: ChatSendOptions = {},
): Promise<ChatResult> {
  return call<ChatResult>(
    'miniapp.chat.send',
    { message, sessionKey: opts.sessionKey, model: opts.model },
    initData,
  );
}
