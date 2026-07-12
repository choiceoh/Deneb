# Skills domain change map

This package owns the filesystem skill catalog and the deterministic rules that
turn `SKILL.md` files into entries, prompts, commands, and eligibility decisions.
Runtime lifecycle orchestration and Genesis evolution consume these contracts;
they must not be pulled into this domain package.

## Entry points and ownership

- `catalog.go`: `Catalog`, `NewCatalog`, and `SkillEntry` own the concurrent
  in-memory view and copy-on-read boundary.
- `discovery.go`: `DiscoverWorkspaceSkills` and `LoadSkillEntry` own directory
  precedence, size limits, nested categories, and file loading.
- `frontmatter.go`: metadata parsing and invocation-policy normalization. Do not
  reproduce frontmatter defaults in RPC or runtime callers.
- `eligibility.go`: `ShouldIncludeSkill` and `FilterEligibleSkills` are the
  single inclusion policy for environment, tool, platform, and allowlist gates.
- `prompt.go`: `BuildSkillsPrompt` and `BuildSkillsIndex` own bounded prompt and
  index rendering.
- `commands.go`: `BuildSkillCommandSpecs` owns stable slash-command naming and
  collision resolution.
- `executor.go`: `ExecuteLocalSkill` is the process boundary for explicitly
  configured local skills; system skill handlers remain runtime-owned.

## Dependency direction and invariants

- `domain/skills` may depend on core and infrastructure utilities, but never on
  `runtime`, RPC handlers, or `pipeline/chat`. Higher layers inject environment
  snapshots and consume typed catalog results.
- Discovery precedence and `ResolveSkillKey` must stay deterministic. The same
  filesystem snapshot must produce the same keys and ordering on every run.
- `Catalog` must retain deep copies: a caller may not mutate registered state
  through `Get`, `List`, or `Snapshot` results.
- Frontmatter is untrusted input. Size limits, safe package/download
  normalization, and command timeout checks must run before execution.
- Prompt limits are hard output bounds. Adding metadata must not bypass the
  existing per-entry and total-budget truncation paths.

## Focused verification

Start with the source-matched tests: `catalog_test.go` for `Catalog`,
`discovery_test.go` for `DiscoverWorkspaceSkills`, `frontmatter_test.go` for
metadata, `eligibility_test.go` for `ShouldIncludeSkill`, and `prompt_test.go`
for `BuildSkillsPrompt`.

`cd gateway-go && go test -count=1 ./internal/domain/skills`

Changes to Genesis belong in `genesis/` and require its separate package test;
do not use that much larger suite as a substitute for this package's contract.
