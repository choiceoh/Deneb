import type { ErrorObject, ValidateFunction } from "ajv";
import { loadCoreRs, type NativeValidationResult } from "../../bindings/core-rs.js";
import type { SessionsPatchResult } from "../session/session-utils.types.js";
import {
  type AgentEvent,
  AgentEventSchema,
  type AgentIdentityParams,
  AgentIdentityParamsSchema,
  type AgentIdentityResult,
  AgentIdentityResultSchema,
  AgentParamsSchema,
  type AgentSummary,
  AgentSummarySchema,
  type AgentsFileEntry,
  AgentsFileEntrySchema,
  type AgentsCreateParams,
  AgentsCreateParamsSchema,
  type AgentsCreateResult,
  AgentsCreateResultSchema,
  type AgentsUpdateParams,
  AgentsUpdateParamsSchema,
  type AgentsUpdateResult,
  AgentsUpdateResultSchema,
  type AgentsDeleteParams,
  AgentsDeleteParamsSchema,
  type AgentsDeleteResult,
  AgentsDeleteResultSchema,
  type AgentsFilesGetParams,
  AgentsFilesGetParamsSchema,
  type AgentsFilesGetResult,
  AgentsFilesGetResultSchema,
  type AgentsFilesListParams,
  AgentsFilesListParamsSchema,
  type AgentsFilesListResult,
  AgentsFilesListResultSchema,
  type AgentsFilesSetParams,
  AgentsFilesSetParamsSchema,
  type AgentsFilesSetResult,
  AgentsFilesSetResultSchema,
  type AgentsListParams,
  AgentsListParamsSchema,
  type AgentsListResult,
  AgentsListResultSchema,
  type AgentWaitParams,
  type ChannelsLogoutParams,
  ChannelsLogoutParamsSchema,
  type TalkConfigParams,
  TalkConfigParamsSchema,
  type TalkConfigResult,
  TalkConfigResultSchema,
  type ChannelsStatusParams,
  ChannelsStatusParamsSchema,
  type ChannelsStatusResult,
  ChannelsStatusResultSchema,
  type ChatAbortParams,
  type ChatEvent,
  ChatEventSchema,
  ChatHistoryParamsSchema,
  type ChatInjectParams,
  ChatInjectParamsSchema,
  ChatSendParamsSchema,
  type ConfigApplyParams,
  ConfigApplyParamsSchema,
  type ConfigGetParams,
  ConfigGetParamsSchema,
  type ConfigPatchParams,
  ConfigPatchParamsSchema,
  type ConfigSchemaLookupParams,
  ConfigSchemaLookupParamsSchema,
  type ConfigSchemaLookupResult,
  ConfigSchemaLookupResultSchema,
  type ConfigSchemaParams,
  ConfigSchemaParamsSchema,
  type ConfigSchemaResponse,
  ConfigSchemaResponseSchema,
  type ConfigSetParams,
  ConfigSetParamsSchema,
  type ConnectParams,
  ConnectParamsSchema,
  type CronAddParams,
  CronAddParamsSchema,
  type CronJob,
  CronJobSchema,
  type CronListParams,
  CronListParamsSchema,
  type CronRemoveParams,
  CronRemoveParamsSchema,
  type CronRunLogEntry,
  type CronRunParams,
  CronRunParamsSchema,
  type CronRunsParams,
  CronRunsParamsSchema,
  type CronStatusParams,
  CronStatusParamsSchema,
  type CronUpdateParams,
  CronUpdateParamsSchema,
  type DevicePairApproveParams,
  type DevicePairListParams,
  type DevicePairRemoveParams,
  type DevicePairRejectParams,
  type DeviceTokenRevokeParams,
  type DeviceTokenRotateParams,
  type ExecApprovalsGetParams,
  type ExecApprovalsNodeGetParams,
  type ExecApprovalsNodeSetParams,
  type ExecApprovalsSetParams,
  type ExecApprovalsSnapshot,
  type ExecApprovalRequestParams,
  type ExecApprovalResolveParams,
  ErrorCodes,
  type ErrorShape,
  ErrorShapeSchema,
  type EventFrame,
  EventFrameSchema,
  errorShape,
  type GatewayFrame,
  GatewayFrameSchema,
  type HelloOk,
  HelloOkSchema,
  type LogsTailParams,
  LogsTailParamsSchema,
  type LogsTailResult,
  LogsTailResultSchema,
  type ModelsListParams,
  ModelsListParamsSchema,
  type NodeDescribeParams,
  type NodeEventParams,
  type NodePendingDrainParams,
  NodePendingDrainParamsSchema,
  type NodePendingDrainResult,
  NodePendingDrainResultSchema,
  type NodePendingEnqueueParams,
  NodePendingEnqueueParamsSchema,
  type NodePendingEnqueueResult,
  NodePendingEnqueueResultSchema,
  type NodeInvokeParams,
  NodeInvokeParamsSchema,
  type NodeInvokeResultParams,
  type NodeListParams,
  NodeListParamsSchema,
  type NodePendingAckParams,
  NodePendingAckParamsSchema,
  type NodePairApproveParams,
  NodePairApproveParamsSchema,
  type NodePairListParams,
  NodePairListParamsSchema,
  type NodePairRejectParams,
  NodePairRejectParamsSchema,
  type NodePairRequestParams,
  NodePairRequestParamsSchema,
  type NodePairVerifyParams,
  NodePairVerifyParamsSchema,
  type NodeRenameParams,
  type PollParams,
  PollParamsSchema,
  PROTOCOL_VERSION,
  type PresenceEntry,
  PresenceEntrySchema,
  ProtocolSchemas,
  type RequestFrame,
  RequestFrameSchema,
  type ResponseFrame,
  ResponseFrameSchema,
  SendParamsSchema,
  type SecretsResolveParams,
  type SecretsResolveResult,
  type SessionsAbortParams,
  SessionsAbortParamsSchema,
  type SessionsCompactParams,
  SessionsCompactParamsSchema,
  type SessionsCreateParams,
  SessionsCreateParamsSchema,
  type SessionsDeleteParams,
  SessionsDeleteParamsSchema,
  type SessionsListParams,
  SessionsListParamsSchema,
  type SessionsMessagesSubscribeParams,
  type SessionsMessagesUnsubscribeParams,
  type SessionsPatchParams,
  SessionsPatchParamsSchema,
  type SessionsPreviewParams,
  SessionsPreviewParamsSchema,
  type SessionsResetParams,
  SessionsResetParamsSchema,
  type SessionsResolveParams,
  SessionsResolveParamsSchema,
  type SessionsSendParams,
  SessionsSendParamsSchema,
  type SessionsUsageParams,
  SessionsUsageParamsSchema,
  type ShutdownEvent,
  ShutdownEventSchema,
  type SkillsBinsParams,
  type SkillsBinsResult,
  type SkillsInstallParams,
  SkillsInstallParamsSchema,
  type SkillsStatusParams,
  SkillsStatusParamsSchema,
  type SkillsUpdateParams,
  SkillsUpdateParamsSchema,
  type ToolsCatalogParams,
  ToolsCatalogParamsSchema,
  type ToolsCatalogResult,
  type Snapshot,
  SnapshotSchema,
  type StateVersion,
  StateVersionSchema,
  type TalkModeParams,
  type TickEvent,
  TickEventSchema,
  type UpdateRunParams,
  UpdateRunParamsSchema,
  type WakeParams,
  WakeParamsSchema,
  type WebLoginStartParams,
  WebLoginStartParamsSchema,
  type WebLoginWaitParams,
  WebLoginWaitParamsSchema,
  type WizardCancelParams,
  WizardCancelParamsSchema,
  type WizardNextParams,
  WizardNextParamsSchema,
  type WizardNextResult,
  WizardNextResultSchema,
  type WizardStartParams,
  WizardStartParamsSchema,
  type WizardStartResult,
  WizardStartResultSchema,
  type WizardStatusParams,
  WizardStatusParamsSchema,
  type WizardStatusResult,
  WizardStatusResultSchema,
  type WizardStep,
  WizardStepSchema,
} from "./schema.js";

/**
 * Create a validator that delegates to native Rust validation.
 * Preserves the `validate(data) => boolean` + `.errors` contract.
 */
function makeNativeValidator<T>(method: string): ValidateFunction<T> {
  const native = loadCoreRs();

  const validator = ((data: unknown) => {
    const result: NativeValidationResult = native.validateParams(method, JSON.stringify(data));
    if (result.valid) {
      validator.errors = null;
      return true;
    }
    validator.errors =
      result.errors?.map((e) => ({
        keyword: e.keyword,
        instancePath: e.path,
        message: e.message,
        params: {},
        schemaPath: "",
      })) ?? null;
    return false;
  }) as ValidateFunction<T>;
  validator.errors = null;
  return validator;
}

export const validateConnectParams = makeNativeValidator<ConnectParams>("connect");

/**
 * Validate a raw JSON string as a gateway frame using the native Rust validator.
 * Returns the frame type ("req"/"res"/"event") on success, or null if invalid.
 */
export function validateFrameNative(json: string): string | null {
  try {
    return loadCoreRs().validateFrame(json);
  } catch {
    return null;
  }
}

function frameTypeOf(raw: string, _parsed: unknown): string | null {
  return validateFrameNative(raw);
}

/** Type guard: validates a raw JSON string as a RequestFrame via Rust FFI (falls back to type check). */
export function isRequestFrame(raw: string, parsed: unknown): parsed is RequestFrame {
  return frameTypeOf(raw, parsed) === "req";
}

/** Type guard: validates a raw JSON string as a ResponseFrame via Rust FFI (falls back to type check). */
export function isResponseFrame(raw: string, parsed: unknown): parsed is ResponseFrame {
  return frameTypeOf(raw, parsed) === "res";
}

/** Type guard: validates a raw JSON string as an EventFrame via Rust FFI (falls back to type check). */
export function isEventFrame(raw: string, parsed: unknown): parsed is EventFrame {
  return frameTypeOf(raw, parsed) === "event";
}

export const validateSendParams = makeNativeValidator("agent.send");
export const validatePollParams = makeNativeValidator<PollParams>("agent.poll");
export const validateAgentParams = makeNativeValidator("agent");
export const validateAgentIdentityParams =
  makeNativeValidator<AgentIdentityParams>("agent.identity");
export const validateAgentWaitParams = makeNativeValidator<AgentWaitParams>("agent.wait");
export const validateWakeParams = makeNativeValidator<WakeParams>("agent.wake");
export const validateAgentsListParams = makeNativeValidator<AgentsListParams>("agents.list");
export const validateAgentsCreateParams = makeNativeValidator<AgentsCreateParams>("agents.create");
export const validateAgentsUpdateParams = makeNativeValidator<AgentsUpdateParams>("agents.update");
export const validateAgentsDeleteParams = makeNativeValidator<AgentsDeleteParams>("agents.delete");
export const validateAgentsFilesListParams =
  makeNativeValidator<AgentsFilesListParams>("agents.files.list");
export const validateAgentsFilesGetParams =
  makeNativeValidator<AgentsFilesGetParams>("agents.files.get");
export const validateAgentsFilesSetParams =
  makeNativeValidator<AgentsFilesSetParams>("agents.files.set");
export const validateNodePairRequestParams =
  makeNativeValidator<NodePairRequestParams>("node.pair.request");
export const validateNodePairListParams = makeNativeValidator<NodePairListParams>("node.pair.list");
export const validateNodePairApproveParams =
  makeNativeValidator<NodePairApproveParams>("node.pair.approve");
export const validateNodePairRejectParams =
  makeNativeValidator<NodePairRejectParams>("node.pair.reject");
export const validateNodePairVerifyParams =
  makeNativeValidator<NodePairVerifyParams>("node.pair.verify");
export const validateNodeRenameParams = makeNativeValidator<NodeRenameParams>("node.rename");
export const validateNodeListParams = makeNativeValidator<NodeListParams>("node.list");
export const validateNodePendingAckParams =
  makeNativeValidator<NodePendingAckParams>("node.pending.ack");
export const validateNodeDescribeParams = makeNativeValidator<NodeDescribeParams>("node.describe");
export const validateNodeInvokeParams = makeNativeValidator<NodeInvokeParams>("node.invoke");
export const validateNodeInvokeResultParams =
  makeNativeValidator<NodeInvokeResultParams>("node.invoke.result");
export const validateNodeEventParams = makeNativeValidator<NodeEventParams>("node.event");
export const validateNodePendingDrainParams =
  makeNativeValidator<NodePendingDrainParams>("node.pending.drain");
export const validateNodePendingEnqueueParams =
  makeNativeValidator<NodePendingEnqueueParams>("node.pending.enqueue");
export const validateSecretsResolveParams =
  makeNativeValidator<SecretsResolveParams>("secrets.resolve");
export const validateSecretsResolveResult =
  makeNativeValidator<SecretsResolveResult>("secrets.resolve.result");
export const validateSessionsListParams = makeNativeValidator<SessionsListParams>("sessions.list");
export const validateSessionsPreviewParams =
  makeNativeValidator<SessionsPreviewParams>("sessions.preview");
export const validateSessionsResolveParams =
  makeNativeValidator<SessionsResolveParams>("sessions.resolve");
export const validateSessionsCreateParams =
  makeNativeValidator<SessionsCreateParams>("sessions.create");
export const validateSessionsSendParams = makeNativeValidator<SessionsSendParams>("sessions.send");
export const validateSessionsMessagesSubscribeParams =
  makeNativeValidator<SessionsMessagesSubscribeParams>("sessions.messages.subscribe");
export const validateSessionsMessagesUnsubscribeParams =
  makeNativeValidator<SessionsMessagesUnsubscribeParams>("sessions.messages.unsubscribe");
export const validateSessionsAbortParams =
  makeNativeValidator<SessionsAbortParams>("sessions.abort");
export const validateSessionsPatchParams =
  makeNativeValidator<SessionsPatchParams>("sessions.patch");
export const validateSessionsResetParams =
  makeNativeValidator<SessionsResetParams>("sessions.reset");
export const validateSessionsDeleteParams =
  makeNativeValidator<SessionsDeleteParams>("sessions.delete");
export const validateSessionsCompactParams =
  makeNativeValidator<SessionsCompactParams>("sessions.compact");
export const validateSessionsUsageParams =
  makeNativeValidator<SessionsUsageParams>("sessions.usage");
export const validateConfigGetParams = makeNativeValidator<ConfigGetParams>("config.get");
export const validateConfigSetParams = makeNativeValidator<ConfigSetParams>("config.set");
export const validateConfigApplyParams = makeNativeValidator<ConfigApplyParams>("config.apply");
export const validateConfigPatchParams = makeNativeValidator<ConfigPatchParams>("config.patch");
export const validateConfigSchemaParams = makeNativeValidator<ConfigSchemaParams>("config.schema");
export const validateConfigSchemaLookupParams =
  makeNativeValidator<ConfigSchemaLookupParams>("config.schema.lookup");
export const validateConfigSchemaLookupResult = makeNativeValidator<ConfigSchemaLookupResult>(
  "config.schema.lookup.result",
);
export const validateWizardStartParams = makeNativeValidator<WizardStartParams>("wizard.start");
export const validateWizardNextParams = makeNativeValidator<WizardNextParams>("wizard.next");
export const validateWizardCancelParams = makeNativeValidator<WizardCancelParams>("wizard.cancel");
export const validateWizardStatusParams = makeNativeValidator<WizardStatusParams>("wizard.status");
export const validateTalkModeParams = makeNativeValidator<TalkModeParams>("talk.mode");
export const validateTalkConfigParams = makeNativeValidator<TalkConfigParams>("talk.config");
export const validateTalkConfigResult = makeNativeValidator<TalkConfigResult>("talk.config.result");
export const validateChannelsStatusParams =
  makeNativeValidator<ChannelsStatusParams>("channels.status");
export const validateChannelsLogoutParams =
  makeNativeValidator<ChannelsLogoutParams>("channels.logout");
export const validateModelsListParams = makeNativeValidator<ModelsListParams>("models.list");
export const validateSkillsStatusParams = makeNativeValidator<SkillsStatusParams>("skills.status");
export const validateToolsCatalogParams = makeNativeValidator<ToolsCatalogParams>("tools.catalog");
export const validateSkillsBinsParams = makeNativeValidator<SkillsBinsParams>("skills.bins");
export const validateSkillsInstallParams =
  makeNativeValidator<SkillsInstallParams>("skills.install");
export const validateSkillsUpdateParams = makeNativeValidator<SkillsUpdateParams>("skills.update");
export const validateCronListParams = makeNativeValidator<CronListParams>("cron.list");
export const validateCronStatusParams = makeNativeValidator<CronStatusParams>("cron.status");
export const validateCronAddParams = makeNativeValidator<CronAddParams>("cron.add");
export const validateCronUpdateParams = makeNativeValidator<CronUpdateParams>("cron.update");
export const validateCronRemoveParams = makeNativeValidator<CronRemoveParams>("cron.remove");
export const validateCronRunParams = makeNativeValidator<CronRunParams>("cron.run");
export const validateCronRunsParams = makeNativeValidator<CronRunsParams>("cron.runs");
export const validateDevicePairListParams =
  makeNativeValidator<DevicePairListParams>("device.pair.list");
export const validateDevicePairApproveParams =
  makeNativeValidator<DevicePairApproveParams>("device.pair.approve");
export const validateDevicePairRejectParams =
  makeNativeValidator<DevicePairRejectParams>("device.pair.reject");
export const validateDevicePairRemoveParams =
  makeNativeValidator<DevicePairRemoveParams>("device.pair.remove");
export const validateDeviceTokenRotateParams =
  makeNativeValidator<DeviceTokenRotateParams>("device.token.rotate");
export const validateDeviceTokenRevokeParams =
  makeNativeValidator<DeviceTokenRevokeParams>("device.token.revoke");
export const validateExecApprovalsGetParams =
  makeNativeValidator<ExecApprovalsGetParams>("exec.approvals.get");
export const validateExecApprovalsSetParams =
  makeNativeValidator<ExecApprovalsSetParams>("exec.approvals.set");
export const validateExecApprovalRequestParams =
  makeNativeValidator<ExecApprovalRequestParams>("exec.approval.request");
export const validateExecApprovalResolveParams =
  makeNativeValidator<ExecApprovalResolveParams>("exec.approval.resolve");
export const validateExecApprovalsNodeGetParams =
  makeNativeValidator<ExecApprovalsNodeGetParams>("exec.approvals.node.get");
export const validateExecApprovalsNodeSetParams =
  makeNativeValidator<ExecApprovalsNodeSetParams>("exec.approvals.node.set");
export const validateLogsTailParams = makeNativeValidator<LogsTailParams>("logs.tail");
export const validateChatHistoryParams = makeNativeValidator("chat.history");
export const validateChatSendParams = makeNativeValidator("chat.send");
export const validateChatAbortParams = makeNativeValidator<ChatAbortParams>("chat.abort");
export const validateChatInjectParams = makeNativeValidator<ChatInjectParams>("chat.inject");
export const validateChatEvent = makeNativeValidator("chat.event");
export const validateUpdateRunParams = makeNativeValidator<UpdateRunParams>("update.run");
export const validateWebLoginStartParams =
  makeNativeValidator<WebLoginStartParams>("weblogin.start");
export const validateWebLoginWaitParams = makeNativeValidator<WebLoginWaitParams>("weblogin.wait");

export function formatValidationErrors(errors: ErrorObject[] | null | undefined) {
  if (!errors?.length) {
    return "unknown validation error";
  }

  const parts: string[] = [];

  for (const err of errors) {
    const keyword = typeof err?.keyword === "string" ? err.keyword : "";
    const instancePath = typeof err?.instancePath === "string" ? err.instancePath : "";

    if (keyword === "additionalProperties") {
      const params = err?.params as { additionalProperty?: unknown } | undefined;
      const additionalProperty = params?.additionalProperty;
      if (typeof additionalProperty === "string" && additionalProperty.trim()) {
        const where = instancePath ? `at ${instancePath}` : "at root";
        parts.push(`${where}: unexpected property '${additionalProperty}'`);
        continue;
      }
    }

    const message =
      typeof err?.message === "string" && err.message.trim() ? err.message : "validation error";
    const where = instancePath ? `at ${instancePath}: ` : "";
    parts.push(`${where}${message}`);
  }

  // De-dupe while preserving order.
  const unique = Array.from(new Set(parts.filter((part) => part.trim())));
  if (!unique.length) {
    return "unknown validation error";
  }
  return unique.join("; ");
}

export {
  ConnectParamsSchema,
  HelloOkSchema,
  RequestFrameSchema,
  ResponseFrameSchema,
  EventFrameSchema,
  GatewayFrameSchema,
  PresenceEntrySchema,
  SnapshotSchema,
  ErrorShapeSchema,
  StateVersionSchema,
  AgentEventSchema,
  ChatEventSchema,
  SendParamsSchema,
  PollParamsSchema,
  AgentParamsSchema,
  AgentIdentityParamsSchema,
  AgentIdentityResultSchema,
  WakeParamsSchema,
  NodePairRequestParamsSchema,
  NodePairListParamsSchema,
  NodePairApproveParamsSchema,
  NodePairRejectParamsSchema,
  NodePairVerifyParamsSchema,
  NodeListParamsSchema,
  NodePendingAckParamsSchema,
  NodeInvokeParamsSchema,
  NodePendingDrainParamsSchema,
  NodePendingDrainResultSchema,
  NodePendingEnqueueParamsSchema,
  NodePendingEnqueueResultSchema,
  SessionsListParamsSchema,
  SessionsPreviewParamsSchema,
  SessionsResolveParamsSchema,
  SessionsCreateParamsSchema,
  SessionsSendParamsSchema,
  SessionsAbortParamsSchema,
  SessionsPatchParamsSchema,
  SessionsResetParamsSchema,
  SessionsDeleteParamsSchema,
  SessionsCompactParamsSchema,
  SessionsUsageParamsSchema,
  ConfigGetParamsSchema,
  ConfigSetParamsSchema,
  ConfigApplyParamsSchema,
  ConfigPatchParamsSchema,
  ConfigSchemaParamsSchema,
  ConfigSchemaLookupParamsSchema,
  ConfigSchemaResponseSchema,
  ConfigSchemaLookupResultSchema,
  WizardStartParamsSchema,
  WizardNextParamsSchema,
  WizardCancelParamsSchema,
  WizardStatusParamsSchema,
  WizardStepSchema,
  WizardNextResultSchema,
  WizardStartResultSchema,
  WizardStatusResultSchema,
  TalkConfigParamsSchema,
  TalkConfigResultSchema,
  ChannelsStatusParamsSchema,
  ChannelsStatusResultSchema,
  ChannelsLogoutParamsSchema,
  WebLoginStartParamsSchema,
  WebLoginWaitParamsSchema,
  AgentSummarySchema,
  AgentsFileEntrySchema,
  AgentsCreateParamsSchema,
  AgentsCreateResultSchema,
  AgentsUpdateParamsSchema,
  AgentsUpdateResultSchema,
  AgentsDeleteParamsSchema,
  AgentsDeleteResultSchema,
  AgentsFilesListParamsSchema,
  AgentsFilesListResultSchema,
  AgentsFilesGetParamsSchema,
  AgentsFilesGetResultSchema,
  AgentsFilesSetParamsSchema,
  AgentsFilesSetResultSchema,
  AgentsListParamsSchema,
  AgentsListResultSchema,
  ModelsListParamsSchema,
  SkillsStatusParamsSchema,
  ToolsCatalogParamsSchema,
  SkillsInstallParamsSchema,
  SkillsUpdateParamsSchema,
  CronJobSchema,
  CronListParamsSchema,
  CronStatusParamsSchema,
  CronAddParamsSchema,
  CronUpdateParamsSchema,
  CronRemoveParamsSchema,
  CronRunParamsSchema,
  CronRunsParamsSchema,
  LogsTailParamsSchema,
  LogsTailResultSchema,
  ChatHistoryParamsSchema,
  ChatSendParamsSchema,
  ChatInjectParamsSchema,
  UpdateRunParamsSchema,
  TickEventSchema,
  ShutdownEventSchema,
  ProtocolSchemas,
  PROTOCOL_VERSION,
  ErrorCodes,
  errorShape,
};

export type {
  GatewayFrame,
  ConnectParams,
  HelloOk,
  RequestFrame,
  ResponseFrame,
  EventFrame,
  PresenceEntry,
  Snapshot,
  ErrorShape,
  StateVersion,
  AgentEvent,
  AgentIdentityParams,
  AgentIdentityResult,
  AgentWaitParams,
  ChatEvent,
  TickEvent,
  ShutdownEvent,
  WakeParams,
  NodePairRequestParams,
  NodePairListParams,
  NodePairApproveParams,
  DevicePairListParams,
  DevicePairApproveParams,
  DevicePairRejectParams,
  ConfigGetParams,
  ConfigSetParams,
  ConfigApplyParams,
  ConfigPatchParams,
  ConfigSchemaParams,
  ConfigSchemaResponse,
  WizardStartParams,
  WizardNextParams,
  WizardCancelParams,
  WizardStatusParams,
  WizardStep,
  WizardNextResult,
  WizardStartResult,
  WizardStatusResult,
  TalkConfigParams,
  TalkConfigResult,
  TalkModeParams,
  ChannelsStatusParams,
  ChannelsStatusResult,
  ChannelsLogoutParams,
  WebLoginStartParams,
  WebLoginWaitParams,
  AgentSummary,
  AgentsFileEntry,
  AgentsCreateParams,
  AgentsCreateResult,
  AgentsUpdateParams,
  AgentsUpdateResult,
  AgentsDeleteParams,
  AgentsDeleteResult,
  AgentsFilesListParams,
  AgentsFilesListResult,
  AgentsFilesGetParams,
  AgentsFilesGetResult,
  AgentsFilesSetParams,
  AgentsFilesSetResult,
  AgentsListParams,
  AgentsListResult,
  SkillsStatusParams,
  ToolsCatalogParams,
  ToolsCatalogResult,
  SkillsBinsParams,
  SkillsBinsResult,
  SkillsInstallParams,
  SkillsUpdateParams,
  NodePairRejectParams,
  NodePairVerifyParams,
  NodeListParams,
  NodeInvokeParams,
  NodeInvokeResultParams,
  NodeEventParams,
  NodePendingDrainParams,
  NodePendingDrainResult,
  NodePendingEnqueueParams,
  NodePendingEnqueueResult,
  SessionsListParams,
  SessionsPreviewParams,
  SessionsResolveParams,
  SessionsPatchParams,
  SessionsPatchResult,
  SessionsResetParams,
  SessionsDeleteParams,
  SessionsCompactParams,
  SessionsUsageParams,
  CronJob,
  CronListParams,
  CronStatusParams,
  CronAddParams,
  CronUpdateParams,
  CronRemoveParams,
  CronRunParams,
  CronRunsParams,
  CronRunLogEntry,
  ExecApprovalsGetParams,
  ExecApprovalsSetParams,
  ExecApprovalsSnapshot,
  LogsTailParams,
  LogsTailResult,
  PollParams,
  UpdateRunParams,
  ChatInjectParams,
};
