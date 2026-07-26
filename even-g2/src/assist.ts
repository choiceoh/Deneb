// Live meeting assist — the one idea from the Even Hub AI plugins that Deneb
// can do and they cannot.
//
// saleseye-universal (AI #3) listens to a sales call and suggests what to say
// next. The gap is that it only knows the call. Deneb knows 거래처, 현장, 계약,
// 미수금 and every prior thread with the person on the other side of the table,
// so the useful surface is not coaching phrasing — it is putting the fact the
// operator would otherwise have to remember in front of them.
//
// The governing rule is SILENCE BY DEFAULT. Most windows of a conversation
// warrant nothing, and a HUD that speaks every cycle during a negotiation is
// worse than one that never does: it pulls the eye at exactly the moment
// attention belongs on the person opposite. So the model is asked for a verdict
// of "nothing" as a first-class answer, and anything unusable is treated as
// nothing rather than shown.

/** What the model is told to answer when a window carries nothing worth saying. */
export const ASSIST_SILENT = "NOTHING";

/**
 * ASSIST_PROMPT — grounded, and permitted to stay quiet.
 *
 * Explicitly forbids inference beyond the wiki: a plausible-sounding invented
 * figure ("미수금 3200만") read out during a live negotiation is worse than any
 * silence, and unlike a chat reply nobody will scroll up to check it.
 */
export const ASSIST_PROMPT = [
  "아래는 지금 진행 중인 대화의 최근 일부입니다.",
  "거래처·현장·인물·계약 등 업무 고유명사가 나오면 위키에서 확인해,",
  "지금 이 자리에서 알아야 할 사실 하나만 25자 이내 한 줄로 답하세요.",
  "",
  "규칙:",
  `- 알릴 것이 없거나 확신이 없으면 정확히 ${ASSIST_SILENT} 라고만 답하세요.`,
  "- 위키에서 확인한 사실만 쓰세요. 추측·일반론·조언은 금지입니다.",
  "- 인사말·설명·따옴표 없이 사실 한 줄만 출력하세요.",
].join("\n");

/** Longest line worth putting in front of someone mid-conversation. */
export const ASSIST_MAX_CHARS = 40;

/**
 * assistLine decides whether a model reply is worth showing at all.
 *
 * Everything ambiguous resolves to null. The asymmetry is deliberate: a missed
 * useful line costs the operator a moment of memory, while a wrong or noisy one
 * costs them the conversation.
 */
export function assistLine(reply: string | undefined): string | null {
  const s = (reply ?? "").trim();
  if (!s) return null;
  // The model sometimes wraps its verdict in punctuation or a sentence.
  if (s.toUpperCase().includes(ASSIST_SILENT)) return null;
  const first = s
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)[0];
  if (!first) return null;
  const cleaned = first.replace(/^["'«»\-•\s]+|["'«»\s]+$/g, "").trim();
  if (!cleaned) return null;
  if (cleaned.length > ASSIST_MAX_CHARS) return null;
  // A reply that is only punctuation or an ellipsis is the model hedging.
  if (!/[가-힣0-9A-Za-z]/.test(cleaned)) return null;
  return cleaned;
}

/**
 * shouldAssistNow paces the analysis.
 *
 * Windows overlap by design — a fact is worth surfacing while the subject is
 * still being discussed, not once the topic has moved on — but a chat turn takes
 * seconds and stacking them would queue the HUD behind a conversation that has
 * already moved. One in flight at a time, and never faster than the period.
 */
export function shouldAssistNow(
  nowMs: number,
  lastMs: number,
  inFlight: boolean,
  periodMs: number,
): boolean {
  if (inFlight) return false;
  if (lastMs <= 0) return true;
  return nowMs - lastMs >= periodMs;
}

/** How often a window is analysed. */
export const ASSIST_PERIOD_MS = 25_000;
