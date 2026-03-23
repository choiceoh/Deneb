import type { DenebConfig } from "../../config/config.js";
import type { ConversationRef } from "../../infra/outbound/session-binding-service.js";
import type {
  ConfiguredBindingRecordResolution,
  ConfiguredBindingResolution,
} from "./binding-types.js";
import {
  countCompiledBindingRegistry,
  primeCompiledBindingRegistry,
  resolveCompiledBindingRegistry,
} from "./configured-binding-compiler.js";
import {
  materializeConfiguredBindingRecord,
  resolveMatchingConfiguredBinding,
  toConfiguredBindingConversationRef,
} from "./configured-binding-match.js";
import { resolveConfiguredBindingRecordBySessionKeyFromRegistry } from "./configured-binding-session-lookup.js";

export function primeConfiguredBindingRegistry(params: { cfg: DenebConfig }): {
  bindingCount: number;
  channelCount: number;
} {
  return countCompiledBindingRegistry(primeCompiledBindingRegistry(params.cfg));
}

export function resolveConfiguredBindingRecord(params: {
  cfg: DenebConfig;
  channel: string;
  accountId: string;
  conversationId: string;
  parentConversationId?: string;
}): ConfiguredBindingRecordResolution | null {
  const conversation = toConfiguredBindingConversationRef({
    channel: params.channel,
    accountId: params.accountId,
    conversationId: params.conversationId,
    parentConversationId: params.parentConversationId,
  });
  if (!conversation) {
    return null;
  }
  return resolveConfiguredBindingRecordForConversation({
    cfg: params.cfg,
    conversation,
  });
}

/** Shared lookup: normalize conversation, resolve registry, find matching binding. */
function findMatchingBinding(cfg: DenebConfig, conversationRef: ConversationRef) {
  const conversation = toConfiguredBindingConversationRef(conversationRef);
  if (!conversation) {
    return null;
  }
  const registry = resolveCompiledBindingRegistry(cfg);
  const rules = registry.rulesByChannel.get(conversation.channel);
  if (!rules || rules.length === 0) {
    return null;
  }
  const resolved = resolveMatchingConfiguredBinding({ rules, conversation });
  if (!resolved) {
    return null;
  }
  return { conversation, resolved };
}

export function resolveConfiguredBindingRecordForConversation(params: {
  cfg: DenebConfig;
  conversation: ConversationRef;
}): ConfiguredBindingRecordResolution | null {
  const match = findMatchingBinding(params.cfg, params.conversation);
  if (!match) {
    return null;
  }
  return materializeConfiguredBindingRecord({
    rule: match.resolved.rule,
    accountId: match.conversation.accountId,
    conversation: match.resolved.match,
  });
}

export function resolveConfiguredBinding(params: {
  cfg: DenebConfig;
  conversation: ConversationRef;
}): ConfiguredBindingResolution | null {
  const match = findMatchingBinding(params.cfg, params.conversation);
  if (!match) {
    return null;
  }
  return {
    conversation: match.conversation,
    compiledBinding: match.resolved.rule,
    match: match.resolved.match,
    ...materializeConfiguredBindingRecord({
      rule: match.resolved.rule,
      accountId: match.conversation.accountId,
      conversation: match.resolved.match,
    }),
  };
}

export function resolveConfiguredBindingRecordBySessionKey(params: {
  cfg: DenebConfig;
  sessionKey: string;
}): ConfiguredBindingRecordResolution | null {
  return resolveConfiguredBindingRecordBySessionKeyFromRegistry({
    registry: resolveCompiledBindingRegistry(params.cfg),
    sessionKey: params.sessionKey,
  });
}
