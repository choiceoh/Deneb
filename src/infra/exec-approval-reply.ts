// Stub: exec approval reply removed for solo-dev simplification.
export function formatExecApprovalReply(): string {
  return "";
}
export function buildExecApprovalReplyPayload(): unknown {
  return null;
}
export function buildExecApprovalUnavailableReplyPayload(_params: {
  warningText?: string;
  reason?: string;
  channelLabel?: string;
  sentApproverDms?: boolean;
}): { text: string } {
  return { text: _params.warningText ?? "Exec approval unavailable." };
}
