---
description: "Mintlify 문서 작성 표준 및 규칙"
globs: ["docs/**"]
---

# Documentation Standards

## Docs Linking & Hosting

- Docs are hosted on Mintlify (docs.deneb.ai).
- Internal doc links in `docs/**/*.md`: root-relative, no `.md`/`.mdx` (example: `[Config](/configuration)`).
- When working with documentation, read the mintlify skill.
- For docs, UI copy, and picker lists, order services/providers alphabetically unless the section is explicitly describing runtime behavior (for example auto-detection or execution order).
- Section cross-references: use anchors on root-relative paths (example: `[Hooks](/configuration#hooks)`).
- Doc headings and anchors: avoid em dashes and apostrophes in headings because they break Mintlify anchor links.
- When Peter asks for links, reply with full `https://docs.deneb.ai/...` URLs (not root-relative).
- When you touch docs, end the reply with the `https://docs.deneb.ai/...` URLs you referenced.
- README (GitHub): keep absolute docs URLs (`https://docs.deneb.ai/...`) so links work on GitHub.
- Docs content must be generic: no personal device names/hostnames/paths; use placeholders like `user@gateway-host` and "gateway host".

## Docs Syntax Rules (Mintlify)

- Frontmatter (YAML) is required on every doc file with these fields:
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
- Validation scripts: `pnpm docs:dev` (local preview), `pnpm docs:spellcheck` (spell check; `pnpm docs:spellcheck:fix` to auto-fix).

## Documentation Commands

| Command | Description |
|---|---|
| `pnpm docs:dev` | Run Mintlify local preview |
| `pnpm docs:spellcheck` | Spell check docs |
| `pnpm docs:spellcheck:fix` | Auto-fix doc spelling |
