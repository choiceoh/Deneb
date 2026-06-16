# changelog.d — native client patch-note fragments

User-facing release notes for the native client live here as **one file per change**,
instead of a single shared list. This is what kills the merge conflicts: every PR adds
a *new* file, so two PRs never touch the same lines.

## When to add a fragment

Add one only when a change is **user-visible** (a feature, a fix they would notice, a
visible behavior change). Internal refactors, build/CI, and test-only changes get none.

## How

Create a file named `YYYY-MM-DD-<slug>.md` (today's date + a short kebab-case slug):

```
client-android/app/changelog.d/2026-06-16-voice-row-group.md
```

- Each **non-blank** line is one highlight bullet (a change can have several).
- Lines starting with `#` are comments and are ignored.
- Write the polished Korean note the user will read — same voice as before.

Example (`2026-06-16-voice-row-group.md`):

```md
더보기 화면의 '음성 입력'이 다른 항목들과 같은 카드 안에 정리됩니다 — 예전엔 혼자 떨어진 별도 박스로 보이던 것을 합쳤습니다
```

## How it reaches the app

At **build time**, `composeApp/build.gradle.kts` reads every `YYYY-MM-DD-*.md` here,
sorts them newest-first by filename, and generates
`build/generated/.../DenebChangelogGenerated.kt` (`GENERATED_CHANGELOG_FRAGMENTS`). The
generated file is **not committed** (it lives under `build/`), so it can never become a
shared file that every PR edits — that was the whole point.

`DenebPatchNotes.kt` exposes `DENEB_PATCH_NOTES = GENERATED_CHANGELOG_FRAGMENTS + history`
(fragments are the newest entries; the frozen historical list follows). The settings
"버전" card and `DenebPatchNotesTest` read that combined symbol unchanged.

> Do **not** add new entries to the frozen `DENEB_PATCH_NOTES` history in
> `DenebPatchNotes.kt` — add a fragment here instead.
