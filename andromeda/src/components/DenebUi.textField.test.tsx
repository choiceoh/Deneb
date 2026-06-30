import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { AssistantText } from "./DenebUi";

// Regression: the morning letter (and other agent-drawn cards) sometimes emit text
// nodes with a "text" field and lists with plain-string items, instead of the
// spec's {type:"text","value":…}. The renderer must tolerate both, or the card
// shows empty rows / empty bullets (the broken-on-desktop morning letter).
describe("DenebUi — text-field tolerance", () => {
  it("renders text/badge `text` field, plain-string list items, and spec `value`", () => {
    const spec = {
      type: "column",
      children: [
        { type: "text", text: "날씨 조회 실패", style: "caption" }, // `text` not `value`
        { type: "badge", text: "D-day", tone: "warn" }, // badge via `text`
        { type: "list", items: ["첫째 항목", "둘째 항목"] }, // string items, not nodes
        { type: "text", value: "스펙대로 value" }, // spec-compliant `value` still works
      ],
    };
    const body = "머리말\n\n```deneb-ui\n" + JSON.stringify(spec) + "\n```\n";
    const html = renderToStaticMarkup(<AssistantText text={body} onUiSubmit={() => {}} busy={false} />);
    for (const s of ["날씨 조회 실패", "D-day", "첫째 항목", "둘째 항목", "스펙대로 value"]) {
      expect(html).toContain(s);
    }
  });
});
