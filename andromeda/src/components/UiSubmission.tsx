import { type UiSubmission } from "@/markdown/denebUiParse";
import { Icon } from "./Icon";

// A deneb-ui card answer in the transcript. The wire user message is machine
// text ("Pressed: e" / "Responded with: k: v") — present it as a compact
// card-reply chip row instead. The native hides the raw bubble behind the
// paired frozen card; the desktop keeps the bubble but humanizes it, which
// also survives transcript reloads (only the raw text is stored server-side).
export function UiSubmissionBubble({ sub }: { sub: UiSubmission }) {
  return (
    <span className="ui-submission" role="group" aria-label="카드 응답">
      <span className="ui-submission-label">
        <Icon name="check" size={11} /> 카드 응답
      </span>
      {sub.values.length > 0 ? (
        sub.values.map(([k, v], i) => (
          <span key={i} className="ui-submission-chip">
            <span className="ui-submission-k">{k}</span>
            <b className="ui-submission-v">{v}</b>
          </span>
        ))
      ) : (
        <span className="ui-submission-chip">
          <b className="ui-submission-v">{sub.event}</b>
        </span>
      )}
    </span>
  );
}
