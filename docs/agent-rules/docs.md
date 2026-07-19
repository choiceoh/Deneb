---
description: "Mintlify 문서 작성 표준 및 규칙"
globs: ["docs/**"]
---

# Documentation Standards

## Docs Linking & Hosting

- Docs are hosted on Mintlify (docs.deneb.ai).
- Internal doc links in `docs/**/*.md`: root-relative, no `.md`/`.mdx` (example: `[Config](/configuration)`).
- Mintlify 구성의 단일 진실원은 `docs/docs.json` (내비게이션·테마). 문법/컴포넌트 규칙은 아래 "Docs Syntax Rules" 섹션을 따른다.
- For docs, UI copy, and picker lists, order services/providers alphabetically unless the section is explicitly describing runtime behavior (for example auto-detection or execution order).
- Section cross-references: use anchors on root-relative paths (example: `[Hooks](/configuration#hooks)`).
- Doc headings and anchors: avoid em dashes and apostrophes in headings because they break Mintlify anchor links.
- When the operator asks for links, reply with full `https://docs.deneb.ai/...` URLs (not root-relative).
- When you touch docs, end the reply with the `https://docs.deneb.ai/...` URLs you referenced.
- README (GitHub): keep absolute docs URLs (`https://docs.deneb.ai/...`) so links work on GitHub.
- Docs content must be generic: no personal device names/hostnames/paths; use placeholders like `user@gateway-host` and "gateway host".

## Docs Syntax Rules (Mintlify)

- Frontmatter (YAML) is required on every doc page wired into `docs/docs.json` navigation, with these fields (internal research notes under `docs/research/` that are not in the navigation are exempt):
  - `title` (required): matches the page H1 heading; 2-5 words.
  - `summary` (required): 1-2 sentences, max ~100 chars.
  - `read_when` (required): array of 2-3 user scenarios/intents describing when to read this page.
  - `sidebarTitle` (optional): shorter label for the sidebar.
- Heading structure: one H1 (`#`) per page matching frontmatter `title`; H2 (`##`) for major sections (3-5 per page typical); H3 (`###`) for subsections; H4 (`####`) rarely.
- Code blocks: always use language tags (`bash`, `json5`, `python`, `typescript`, `powershell`, `swift`, `mermaid`). Use `json5` (not `json`) for config examples (supports comments and trailing commas). Use inline code (single backticks) for file paths, commands, config keys, and JSON fields.
- Mintlify components are globally available (no imports needed):
  - `<Steps>` / `<Step title="...">`: numbered procedures, quick starts.
  - `<Tabs>` / `<Tab title="...">`: platform/OS variants, mutually exclusive content.
  - `<Info>`, `<Tip>`, `<Warning>`, `<Note>`, `<Check>`: callout boxes.
  - `<AccordionGroup>` / `<Accordion title="...">`: collapsible optional/advanced content.
  - `<CardGroup cols={N}>` / `<Card title="..." icon="..." href="...">`: feature grids, navigation.
  - `<Columns>` / `<Card>`: responsive card layouts (alternative to CardGroup).
  - `<Tooltip headline="..." tip="...">`: hover definitions.
  - `<Frame caption="...">`: image wrapper with caption.
  - Icons use the Lucide library (e.g. `icon="rocket"`, `icon="settings"`, `icon="message-square"`).
- Images: use root-relative paths (`/assets/...`). For light/dark mode, use paired `<img>` tags with `class="dark:hidden"` and `class="hidden dark:block"`.
- Tables: standard Markdown tables for feature matrices, mode mappings, option lists.
- File conventions: all doc files are `.md` (Mintlify processes MDX syntax transparently). File naming: lowercase, hyphenated (`getting-started.md`, `voice-wake.md`).
- Local preview: run `npx mintlify dev` from `docs/` (the repo has no pnpm docs scripts).

## Documentation Commands

| Command | Description |
|---|---|
| `npx mintlify dev` (from `docs/`) | Run Mintlify local preview |

## 문서 참조 정합 (doc-ref-lint)

에이전트 문서(CLAUDE.md·docs/agent-rules 등)에 박힌 코드 참조는 `make
doc-ref-lint`(CI 게이트, validate-or-freeze — arXiv:2607.13285에서 채택)가
현재 레포와 대조한다. 소스 파일 경로가 사라졌거나 `file.go:라인` 앵커가 파일
길이를 넘으면 BROKEN으로 게이트가 레드다 — 코드를 옮겼으면 문서도 같이 고쳐라.
`file.go:심볼` 앵커와 심볼 참조는 CodeGraph 인덱스로 검증되는 advisory warn.

의도적으로 존재하지 않는 참조(가상 예시 파일, "만들지 마라" 반례, 레포 밖
호스트 파일)는 해당 줄 끝에 `<!-- docref:ignore -->`, 블록 단위는
`<!-- docref:off -->`/`<!-- docref:on -->`으로 감싼다. 런타임 데이터 파일
(`deneb.json` 류 bare 파일명)과 개념/외부 참조는 자동으로 warn 이하로
분류되므로 마킹이 필요 없다.

부가 명령: `--fix`(broken 라인 앵커를 심볼 힌트→심볼 시작 라인, 힌트 없으면
라인 드롭으로 결정적 수리), `--unmentioned <디렉토리> <문서>`(모듈 문서가
큐레이션하지 않은 소스 나열 — advisory), `make memory-ref-audit`(레포 밖
메모리 파일의 코드 참조 감사 — 회상 메모리의 file:line 검증 규칙 일괄 실행).

드리프트 검출: `file.go:N` 앵커는 같은 줄 백틱 심볼의 CodeGraph 범위와
대조된다(하나라도 품으면 무고, 전부 밖이면 warn-drift → `--fix`가 심볼
시작 줄로 스냅). 주간 자가감사 크론 `weekly-ref-audit`(토 06:30)이
doc-ref-lint·memory-ref-audit를 돌려 메모리 rot은 자가 수리하고 레포 문서
rot은 작업 피드 제안 카드로 올린다.
