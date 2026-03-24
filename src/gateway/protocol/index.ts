import AjvPkg, { type ErrorObject, type ValidateFunction } from "ajv";
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
  AgentWaitParamsSchema,
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
  ChatAbortParamsSchema,
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
  DevicePairApproveParamsSchema,
  type DevicePairListParams,
  DevicePairListParamsSchema,
  type DevicePairRemoveParams,
  DevicePairRemoveParamsSchema,
  type DevicePairRejectParams,
  DevicePairRejectParamsSchema,
  type DeviceTokenRevokeParams,
  DeviceTokenRevokeParamsSchema,
  type DeviceTokenRotateParams,
  DeviceTokenRotateParamsSchema,
  type ExecApprovalsGetParams,
  ExecApprovalsGetParamsSchema,
  type ExecApprovalsNodeGetParams,
  ExecApprovalsNodeGetParamsSchema,
  type ExecApprovalsNodeSetParams,
  ExecApprovalsNodeSetParamsSchema,
  type ExecApprovalsSetParams,
  ExecApprovalsSetParamsSchema,
  type ExecApprovalsSnapshot,
  type ExecApprovalRequestParams,
  ExecApprovalRequestParamsSchema,
  type ExecApprovalResolveParams,
  ExecApprovalResolveParamsSchema,
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
  NodeDescribeParamsSchema,
  type NodeEventParams,
  NodeEventParamsSchema,
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
  NodeInvokeResultParamsSchema,
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
  NodeRenameParamsSchema,
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
  SecretsResolveParamsSchema,
  SecretsResolveResultSchema,
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
  SessionsMessagesSubscribeParamsSchema,
  type SessionsMessagesUnsubscribeParams,
  SessionsMessagesUnsubscribeParamsSchema,
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
  SkillsBinsParamsSchema,
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
  TalkModeParamsSchema,
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

// AJV's default export varies between ESM/CJS; cast to constructor signature for safe instantiation.
const ajv = new (AjvPkg as unknown as new (opts?: object) => import("ajv").default)({
  allErrors: true,
  strict: false,
  removeAdditional: false,
});

/**
 * Create a validator that delegates to native Rust validation when available,
 * falling back to the AJV-compiled validator otherwise.
 * Preserves the `validate(data) => boolean` + `.errors` contract.
 */
function makeNativeValidator<T>(
  method: string,
  fallback: ValidateFunction<T>,
): ValidateFunction<T> {
  const native = loadCoreRs();
  if (!native) {
    return fallback;
  }

  const validator = ((data: unknown) => {
    let result: NativeValidationResult;
    try {
      result = native.validateParams(method, JSON.stringify(data));
    } catch {
      // Fallback to AJV on native errors (e.g. unknown method).
      return fallback(data);
    }
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

export const validateConnectParams = ajv.compile<ConnectParams>(ConnectParamsSchema);

/**
 * Validate a raw JSON string as a gateway frame using the native Rust validator.
 * Returns the frame type ("req"/"res"/"event") on success, or null if invalid/unavailable.
 */
export function validateFrameNative(json: string): string | null {
  const native = loadCoreRs();
  if (!native) {
    return null;
  }
  try {
    return native.validateFrame(json);
  } catch {
    return null;
  }
}

function frameTypeOf(raw: string, parsed: unknown): string | null {
  const native = validateFrameNative(raw);
  if (native !== null) {
    return native;
  }
  // Lightweight fallback when native addon is unavailable.
  if (parsed && typeof parsed === "object" && "type" in parsed) {
    const t = (parsed as { type: unknown }).type;
    if (t === "req" || t === "res" || t === "event") {
      return t;
    }
  }
  return null;
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

export const validateSendParams = makeNativeValidator("agent.send", ajv.compile(SendParamsSchema));
export const validatePollParams = makeNativeValidator(
  "agent.poll",
  ajv.compile<PollParams>(PollParamsSchema),
);
export const validateAgentParams = makeNativeValidator("agent", ajv.compile(AgentParamsSchema));
export const validateAgentIdentityParams = makeNativeValidator(
  "agent.identity",
  ajv.compile<AgentIdentityParams>(AgentIdentityParamsSchema),
);
export const validateAgentWaitParams = makeNativeValidator(
  "agent.wait",
  ajv.compile<AgentWaitParams>(AgentWaitParamsSchema),
);
export const validateWakeParams = makeNativeValidator(
  "agent.wake",
  ajv.compile<WakeParams>(WakeParamsSchema),
);
export const validateAgentsListParams = makeNativeValidator(
  "agents.list",
  ajv.compile<AgentsListParams>(AgentsListParamsSchema),
);
export const validateAgentsCreateParams = makeNativeValidator(
  "agents.create",
  ajv.compile<AgentsCreateParams>(AgentsCreateParamsSchema),
);
export const validateAgentsUpdateParams = makeNativeValidator(
  "agents.update",
  ajv.compile<AgentsUpdateParams>(AgentsUpdateParamsSchema),
);
export const validateAgentsDeleteParams = makeNativeValidator(
  "agents.delete",
  ajv.compile<AgentsDeleteParams>(AgentsDeleteParamsSchema),
);
export const validateAgentsFilesListParams = makeNativeValidator(
  "agents.files.list",
  ajv.compile<AgentsFilesListParams>(AgentsFilesListParamsSchema),
);
export const validateAgentsFilesGetParams = makeNativeValidator(
  "agents.files.get",
  ajv.compile<AgentsFilesGetParams>(AgentsFilesGetParamsSchema),
);
export const validateAgentsFilesSetParams = makeNativeValidator(
  "agents.files.set",
  ajv.compile<AgentsFilesSetParams>(AgentsFilesSetParamsSchema),
);
export const validateNodePairRequestParams = makeNativeValidator(
  "node.pair.request",
  ajv.compile<NodePairRequestParams>(NodePairRequestParamsSchema),
);
export const validateNodePairListParams = makeNativeValidator(
  "node.pair.list",
  ajv.compile<NodePairListParams>(NodePairListParamsSchema),
);
export const validateNodePairApproveParams = makeNativeValidator(
  "node.pair.approve",
  ajv.compile<NodePairApproveParams>(NodePairApproveParamsSchema),
);
export const validateNodePairRejectParams = makeNativeValidator(
  "node.pair.reject",
  ajv.compile<NodePairRejectParams>(NodePairRejectParamsSchema),
);
export const validateNodePairVerifyParams = makeNativeValidator(
  "node.pair.verify",
  ajv.compile<NodePairVerifyParams>(NodePairVerifyParamsSchema),
);
export const validateNodeRenameParams = makeNativeValidator(
  "node.rename",
  ajv.compile<NodeRenameParams>(NodeRenameParamsSchema),
);
export const validateNodeListParams = makeNativeValidator(
  "node.list",
  ajv.compile<NodeListParams>(NodeListParamsSchema),
);
export const validateNodePendingAckParams = makeNativeValidator(
  "node.pending.ack",
  ajv.compile<NodePendingAckParams>(NodePendingAckParamsSchema),
);
export const validateNodeDescribeParams = makeNativeValidator(
  "node.describe",
  ajv.compile<NodeDescribeParams>(NodeDescribeParamsSchema),
);
export const validateNodeInvokeParams = makeNativeValidator(
  "node.invoke",
  ajv.compile<NodeInvokeParams>(NodeInvokeParamsSchema),
);
export const validateNodeInvokeResultParams = makeNativeValidator(
  "node.invoke.result",
  ajv.compile<NodeInvokeResultParams>(NodeInvokeResultParamsSchema),
);
export const validateNodeEventParams = makeNativeValidator(
  "node.event",
  ajv.compile<NodeEventParams>(NodeEventParamsSchema),
);
export const validateNodePendingDrainParams = makeNativeValidator(
  "node.pending.drain",
  ajv.compile<NodePendingDrainParams>(NodePendingDrainParamsSchema),
);
export const validateNodePendingEnqueueParams = makeNativeValidator(
  "node.pending.enqueue",
  ajv.compile<NodePendingEnqueueParams>(NodePendingEnqueueParamsSchema),
);
export const validateSecretsResolveParams = makeNativeValidator(
  "secrets.resolve",
  ajv.compile<SecretsResolveParams>(SecretsResolveParamsSchema),
);
export const validateSecretsResolveResult = ajv.compile<SecretsResolveResult>(
  SecretsResolveResultSchema,
);
export const validateSessionsListParams = makeNativeValidator(
  "sessions.list",
  ajv.compile<SessionsListParams>(SessionsListParamsSchema),
);
export const validateSessionsPreviewParams = makeNativeValidator(
  "sessions.preview",
  ajv.compile<SessionsPreviewParams>(SessionsPreviewParamsSchema),
);
export const validateSessionsResolveParams = makeNativeValidator(
  "sessions.resolve",
  ajv.compile<SessionsResolveParams>(SessionsResolveParamsSchema),
);
export const validateSessionsCreateParams = makeNativeValidator(
  "sessions.create",
  ajv.compile<SessionsCreateParams>(SessionsCreateParamsSchema),
);
export const validateSessionsSendParams = makeNativeValidator(
  "sessions.send",
  ajv.compile<SessionsSendParams>(SessionsSendParamsSchema),
);
export const validateSessionsMessagesSubscribeParams = makeNativeValidator(
  "sessions.messages.subscribe",
  ajv.compile<SessionsMessagesSubscribeParams>(SessionsMessagesSubscribeParamsSchema),
);
export const validateSessionsMessagesUnsubscribeParams = makeNativeValidator(
  "sessions.messages.unsubscribe",
  ajv.compile<SessionsMessagesUnsubscribeParams>(SessionsMessagesUnsubscribeParamsSchema),
);
export const validateSessionsAbortParams = makeNativeValidator(
  "sessions.abort",
  ajv.compile<SessionsAbortParams>(SessionsAbortParamsSchema),
);
export const validateSessionsPatchParams = makeNativeValidator(
  "sessions.patch",
  ajv.compile<SessionsPatchParams>(SessionsPatchParamsSchema),
);
export const validateSessionsResetParams = makeNativeValidator(
  "sessions.reset",
  ajv.compile<SessionsResetParams>(SessionsResetParamsSchema),
);
export const validateSessionsDeleteParams = makeNativeValidator(
  "sessions.delete",
  ajv.compile<SessionsDeleteParams>(SessionsDeleteParamsSchema),
);
export const validateSessionsCompactParams = makeNativeValidator(
  "sessions.compact",
  ajv.compile<SessionsCompactParams>(SessionsCompactParamsSchema),
);
export const validateSessionsUsageParams = makeNativeValidator(
  "sessions.usage",
  ajv.compile<SessionsUsageParams>(SessionsUsageParamsSchema),
);
export const validateConfigGetParams = makeNativeValidator(
  "config.get",
  ajv.compile<ConfigGetParams>(ConfigGetParamsSchema),
);
export const validateConfigSetParams = makeNativeValidator(
  "config.set",
  ajv.compile<ConfigSetParams>(ConfigSetParamsSchema),
);
export const validateConfigApplyParams = makeNativeValidator(
  "config.apply",
  ajv.compile<ConfigApplyParams>(ConfigApplyParamsSchema),
);
export const validateConfigPatchParams = makeNativeValidator(
  "config.patch",
  ajv.compile<ConfigPatchParams>(ConfigPatchParamsSchema),
);
export const validateConfigSchemaParams = makeNativeValidator(
  "config.schema",
  ajv.compile<ConfigSchemaParams>(ConfigSchemaParamsSchema),
);
export const validateConfigSchemaLookupParams = makeNativeValidator(
  "config.schema.lookup",
  ajv.compile<ConfigSchemaLookupParams>(ConfigSchemaLookupParamsSchema),
);
export const validateConfigSchemaLookupResult = ajv.compile<ConfigSchemaLookupResult>(
  ConfigSchemaLookupResultSchema,
);
export const validateWizardStartParams = makeNativeValidator(
  "wizard.start",
  ajv.compile<WizardStartParams>(WizardStartParamsSchema),
);
export const validateWizardNextParams = makeNativeValidator(
  "wizard.next",
  ajv.compile<WizardNextParams>(WizardNextParamsSchema),
);
export const validateWizardCancelParams = makeNativeValidator(
  "wizard.cancel",
  ajv.compile<WizardCancelParams>(WizardCancelParamsSchema),
);
export const validateWizardStatusParams = makeNativeValidator(
  "wizard.status",
  ajv.compile<WizardStatusParams>(WizardStatusParamsSchema),
);
export const validateTalkModeParams = makeNativeValidator(
  "talk.mode",
  ajv.compile<TalkModeParams>(TalkModeParamsSchema),
);
export const validateTalkConfigParams = makeNativeValidator(
  "talk.config",
  ajv.compile<TalkConfigParams>(TalkConfigParamsSchema),
);
export const validateTalkConfigResult = ajv.compile<TalkConfigResult>(TalkConfigResultSchema);
export const validateChannelsStatusParams = makeNativeValidator(
  "channels.status",
  ajv.compile<ChannelsStatusParams>(ChannelsStatusParamsSchema),
);
export const validateChannelsLogoutParams = makeNativeValidator(
  "channels.logout",
  ajv.compile<ChannelsLogoutParams>(ChannelsLogoutParamsSchema),
);
export const validateModelsListParams = makeNativeValidator(
  "models.list",
  ajv.compile<ModelsListParams>(ModelsListParamsSchema),
);
export const validateSkillsStatusParams = makeNativeValidator(
  "skills.status",
  ajv.compile<SkillsStatusParams>(SkillsStatusParamsSchema),
);
export const validateToolsCatalogParams = makeNativeValidator(
  "tools.catalog",
  ajv.compile<ToolsCatalogParams>(ToolsCatalogParamsSchema),
);
export const validateSkillsBinsParams = makeNativeValidator(
  "skills.bins",
  ajv.compile<SkillsBinsParams>(SkillsBinsParamsSchema),
);
export const validateSkillsInstallParams = makeNativeValidator(
  "skills.install",
  ajv.compile<SkillsInstallParams>(SkillsInstallParamsSchema),
);
export const validateSkillsUpdateParams = makeNativeValidator(
  "skills.update",
  ajv.compile<SkillsUpdateParams>(SkillsUpdateParamsSchema),
);
export const validateCronListParams = makeNativeValidator(
  "cron.list",
  ajv.compile<CronListParams>(CronListParamsSchema),
);
export const validateCronStatusParams = makeNativeValidator(
  "cron.status",
  ajv.compile<CronStatusParams>(CronStatusParamsSchema),
);
export const validateCronAddParams = makeNativeValidator(
  "cron.add",
  ajv.compile<CronAddParams>(CronAddParamsSchema),
);
export const validateCronUpdateParams = makeNativeValidator(
  "cron.update",
  ajv.compile<CronUpdateParams>(CronUpdateParamsSchema),
);
export const validateCronRemoveParams = makeNativeValidator(
  "cron.remove",
  ajv.compile<CronRemoveParams>(CronRemoveParamsSchema),
);
export const validateCronRunParams = makeNativeValidator(
  "cron.run",
  ajv.compile<CronRunParams>(CronRunParamsSchema),
);
export const validateCronRunsParams = makeNativeValidator(
  "cron.runs",
  ajv.compile<CronRunsParams>(CronRunsParamsSchema),
);
export const validateDevicePairListParams = makeNativeValidator(
  "device.pair.list",
  ajv.compile<DevicePairListParams>(DevicePairListParamsSchema),
);
export const validateDevicePairApproveParams = makeNativeValidator(
  "device.pair.approve",
  ajv.compile<DevicePairApproveParams>(DevicePairApproveParamsSchema),
);
export const validateDevicePairRejectParams = makeNativeValidator(
  "device.pair.reject",
  ajv.compile<DevicePairRejectParams>(DevicePairRejectParamsSchema),
);
export const validateDevicePairRemoveParams = makeNativeValidator(
  "device.pair.remove",
  ajv.compile<DevicePairRemoveParams>(DevicePairRemoveParamsSchema),
);
export const validateDeviceTokenRotateParams = makeNativeValidator(
  "device.token.rotate",
  ajv.compile<DeviceTokenRotateParams>(DeviceTokenRotateParamsSchema),
);
export const validateDeviceTokenRevokeParams = makeNativeValidator(
  "device.token.revoke",
  ajv.compile<DeviceTokenRevokeParams>(DeviceTokenRevokeParamsSchema),
);
export const validateExecApprovalsGetParams = makeNativeValidator(
  "exec.approvals.get",
  ajv.compile<ExecApprovalsGetParams>(ExecApprovalsGetParamsSchema),
);
export const validateExecApprovalsSetParams = makeNativeValidator(
  "exec.approvals.set",
  ajv.compile<ExecApprovalsSetParams>(ExecApprovalsSetParamsSchema),
);
export const validateExecApprovalRequestParams = makeNativeValidator(
  "exec.approval.request",
  ajv.compile<ExecApprovalRequestParams>(ExecApprovalRequestParamsSchema),
);
export const validateExecApprovalResolveParams = makeNativeValidator(
  "exec.approval.resolve",
  ajv.compile<ExecApprovalResolveParams>(ExecApprovalResolveParamsSchema),
);
export const validateExecApprovalsNodeGetParams = makeNativeValidator(
  "exec.approvals.node.get",
  ajv.compile<ExecApprovalsNodeGetParams>(ExecApprovalsNodeGetParamsSchema),
);
export const validateExecApprovalsNodeSetParams = makeNativeValidator(
  "exec.approvals.node.set",
  ajv.compile<ExecApprovalsNodeSetParams>(ExecApprovalsNodeSetParamsSchema),
);
export const validateLogsTailParams = makeNativeValidator(
  "logs.tail",
  ajv.compile<LogsTailParams>(LogsTailParamsSchema),
);
export const validateChatHistoryParams = makeNativeValidator(
  "chat.history",
  ajv.compile(ChatHistoryParamsSchema),
);
export const validateChatSendParams = makeNativeValidator(
  "chat.send",
  ajv.compile(ChatSendParamsSchema),
);
export const validateChatAbortParams = makeNativeValidator(
  "chat.abort",
  ajv.compile<ChatAbortParams>(ChatAbortParamsSchema),
);
export const validateChatInjectParams = makeNativeValidator(
  "chat.inject",
  ajv.compile<ChatInjectParams>(ChatInjectParamsSchema),
);
export const validateChatEvent = ajv.compile(ChatEventSchema);
export const validateUpdateRunParams = makeNativeValidator(
  "update.run",
  ajv.compile<UpdateRunParams>(UpdateRunParamsSchema),
);
export const validateWebLoginStartParams = makeNativeValidator(
  "weblogin.start",
  ajv.compile<WebLoginStartParams>(WebLoginStartParamsSchema),
);
export const validateWebLoginWaitParams = makeNativeValidator(
  "weblogin.wait",
  ajv.compile<WebLoginWaitParams>(WebLoginWaitParamsSchema),
);

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
    const fallback = ajv.errorsText(errors, { separator: "; " });
    return fallback || "unknown validation error";
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
