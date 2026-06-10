---
description: Publish a Deneb native-client build (versionCode-only — no semantic version bump). Args: optional Korean release note.
---
Publish a Deneb native-client build. Optional release note argument: $ARGUMENTS.

> The old Kai flow (semantic `appVersion` bump, fastlane Play Store changelog,
> `v{X.Y.Z}` tags pushed to main) is retired. Deneb builds are identified by
> **versionCode only** (#2089/#2099): there is no version file to bump, no tag,
> and agents never push to main directly.

Follow these steps exactly:

## 1. Decide whether a patch note is needed

`DenebPatchNotes.kt` (composeApp/src/commonMain/.../deneb/DenebPatchNotes.kt) is
the compiled-in, user-facing Korean changelog. Prepend a new entry at the head
**only when this build carries user-visible changes**; internal refactors need
no entry. The patch-notes CI gate enforces this for `feat(...)` PRs touching
client-android.

## 2. Publish

Releases happen one of two ways — never by hand-copying APKs:

- **Automatic (normal path):** merging a PR that touches `client-android/**`
  into main triggers `.github/workflows/publish-apk.yml` on the gx10
  self-hosted runner. Merging the PR *is* the release approval.
- **Manual (operator, on the deploy machine):**

  ```bash
  DENEB_APK_BASE_URL=http://<gateway-host>:19010 \
    scripts/dev/publish-apk.sh "인앱 업데이트에 표시될 릴리스 노트"
  ```

`publish-apk.sh` assigns the next versionCode itself (flock-serialized across
worktrees), runs the native-app smoke gate, builds, and writes `version.json`.
Details: `.claude/rules/release-and-deploy.md`.

## Important

- Do NOT bump `android-versionCode` in `gradle/libs.versions.toml` — it is only
  a floor/IDE default; the publish script assigns real codes.
- Do NOT create tags or push to main for a client release.
- If the smoke gate fails, fix the regression (see `smoke-*.png`) instead of
  bypassing with `DENEB_SKIP_SMOKE=1`.
