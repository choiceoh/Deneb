// Barrel export for protobuf-generated TypeScript types.
//
// Do not edit manually. Regenerate with: ./scripts/proto-gen.sh --ts
//
// These types are generated from proto/ definitions and mirror the canonical
// TypeScript types in src/gateway/protocol/schema/frames.ts.

export type {
  ErrorShape,
  RequestFrame,
  ResponseFrame,
  StateVersion,
  EventFrame,
  GatewayFrame,
  PresenceEntry,
  HelloOk,
  HelloOk_ServerInfo,
  HelloOk_Features,
  HelloOk_Policy,
  HelloOk_AuthInfo,
} from "./gateway";

export type {
  ChannelCapabilities,
  ChannelMeta,
  ChannelAccountSnapshot,
} from "./channel";

export {
  SessionRunStatus,
  SessionKind,
} from "./session";

export type {
  GatewaySessionRow,
  SessionPreviewItem,
  SessionsPreviewEntry,
} from "./session";
