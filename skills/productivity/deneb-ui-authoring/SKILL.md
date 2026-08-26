---
name: deneb-ui-authoring
version: "1.0.0"
category: productivity
description: "Author validated Deneb rich cards with labeled HTML, including structured data, exploration, quick actions, and decision controls. Use when: a dashboard, briefing, comparison, numeric/status summary, timeline, table-centered answer, or interactive choice is materially clearer than prose. NOT for: short answers, casual conversation, intermediate narration, or ordinary prose with a small incidental table."
metadata:
  {
    "deneb":
      {
        "emoji": "🪪",
        "tags": ["deneb-ui", "rich-card", "structured-output", "interactive", "dashboard", "briefing"],
        "triggers": ["카드로", "버튼으로", "대시보드로", "선택지로", "표로 보여", "접어서 보여"],
        "exercise_output": ["fence:deneb-ui"],
      },
  }
---

# Deneb UI Authoring

Use Deneb's labeled-HTML wire format only when structure materially improves the
answer. The grammar source of truth is `docs/research/deneb-ui-html.md`; this
skill owns the agent-facing selection, composition, and interaction procedure.

## Output Contract

- Emit at most one `deneb-ui` fenced block per response, containing one labeled-HTML tree.
- Use one root `<column>`. Put multiple topics in separate `<card>` children rather than separate fences.
- Keep short answers, casual conversation, and work-in-progress narration as prose.
- Inside a card, use `<table>` for tabular data and `<code>` for code. Do not nest Markdown tables or code fences.
- Interactive inputs (`input`, `textarea`, `checkbox`, `switch`, `select`, `radio-group`, `slider`, `chips`) require a non-empty `id`.
- Buttons that use selected/input values require `collect="id1,id2"`; otherwise only the event arrives.

## Node Selection

- Headline or hierarchy: `<text style="headline|title|body|caption">`.
- Two or three key figures: sibling `<stat value="381톤" label="주간 생산" description="+2.1%"/>` nodes in a `<row>`.
- Trend or distribution: `<chart type="bar|line" label="…"><point label="현장A" value="50"/>…</chart>` with 3-8 points.
- Completion: `<progress value="0.68" label="…"/>`.
- Table: `<table><tr><th>항목</th><th>상태</th></tr><tr><td>A</td><td>완료</td></tr></table>`.
- Attention: `<alert severity="info|success|warning|error" title="…">본문</alert>`.
- Compact status: `<badge color="success|warning|error">완료</badge>`.
- Quote/list/separator: `<blockquote source="…">`, `<ul><li>…</li></ul>`, `<hr/>`.
- Section cue: start a card with `<row><icon name="calendar" size="16"/><text style="caption">라벨</text></row>`.

The renderers recognize `10:00 — 제목` list items as a timeline and `키 — 내용`
items as key/value rows. Signed stat descriptions such as `+2.1%` or `-14톤`
render as trends. Use status colors only for real state; do not decorate every
node. Inline `**bold**`, `*italic*`, and `` `code` `` work inside text and list
items.

## Composition

1. Lead with the conclusion using a headline or the most important stats.
2. Follow with the supporting table, list, chart, or progress state.
3. End with a caption only when a source, caveat, or next action is useful.
4. Keep prose inside a card to one or two lead sentences; express the rest as structure.
5. Split unrelated topics into sibling cards under the root column.

## Interaction Patterns

Use exploration controls to reduce cognitive load without a server round trip:

- Long evidence or examples: 2-3 `<accordion title="…">` sections, closed by default.
- Alternate perspectives: `<tabs><tab label="이번 주">…</tab><tab label="이번 달">…</tab></tabs>`.
- Optional detail: `<button toggle="clause-1" variant="text">상세</button>` with `<box id="clause-1">…</box>`.

Use quick actions when they shorten a real next step:

- Copy: `<button copy="npm run dev">복사</button>`.
- Open a link: `<button href="https://…">열기</button>`.

When the answer ends in a decision, present controls instead of asking only in
prose. Put approval/alternative buttons in a row, make destructive labels
explicit, or use 2-5 chips plus a confirm button. Button callbacks return as
`Pressed: 이벤트`; collected values return as `Responded with: id: 값`. Execute
the chosen action when that next user message arrives.

## Examples

Static briefing:

```deneb-ui
<column><card><row><icon name="calendar" size="16"/><text style="caption">오늘 일정</text></row><ul><li>10:00 — 회의</li><li>14:00 — 현장 점검</li></ul></card></column>
```

Explorable result:

```deneb-ui
<column><card><text style="headline">Q3 매출 +14%</text><stat value="381억" label="매출" description="+14%"/><accordion title="현장별 상세"><chart type="bar"><point label="A" value="180"/><point label="B" value="120"/></chart></accordion><accordion title="근거·주석"><text>집계 기준과 예외</text></accordion></card></column>
```

Decision with collected input:

```deneb-ui
<column><card><text style="title">시간 선택</text><chips id="slot"><chip value="10:00">오전 10시</chip><chip value="14:00">오후 2시</chip></chips><row><button event="confirm_slot" collect="slot">이 시간으로 잡기</button><button event="cancel" variant="outlined">다음에</button></row></card></column>
```

## Pitfalls

- Do not emit legacy JSON; it is display-only compatibility for old transcripts.
- Do not invent tags or enum values. Use the nodes and values listed above.
- Do not emit multiple deneb-ui fences; use multiple cards under one column.
- Do not put a long prose report into one card or overuse badges, alerts, and colors.
- Do not add an interaction that has no meaningful user action.
- Escape literal backticks inside raw-text nodes as `&#96;` when needed.

## Verification

- The fence body starts with `<` and has one root column.
- `go run ./gateway-go/cmd/denebui-check` accepts the final block with no issues when working in the repository.
- Every interactive input has an id; every value-consuming callback has collect.
- The response contains no second deneb-ui fence.
- If the server rejects the card, it degrades the invalid block to readable plain text rather than sending raw markup.
