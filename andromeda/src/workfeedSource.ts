const WORKFEED_SOURCE_LABELS: Readonly<Record<string, string>> = {
  alert: "알림",
  deal_question: "질문",
  followup: "후속",
  proactive: "제안",
  "groupware-approval": "전자결재",
};

export function workfeedSourceLabel(source?: string): string {
  const key = source?.trim();
  if (!key) return "피드";
  return WORKFEED_SOURCE_LABELS[key] ?? key.replace(/[_-]+/g, " ");
}
