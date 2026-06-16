#!/usr/bin/env bash
# PR gate: a user-facing native feature must ship a 패치노트 entry.
#
# Why this exists: the native client's changelog (DenebPatchNotes.kt) is a
# hand-curated Korean list compiled into the app and surfaced by the settings
# "패치노트" sheet. versionCode-only (#2099) removed the old head==build test
# that used to force an update, and PR CI never runs the native commonTest — so
# user-facing feat(...) PRs kept merging with no note and the sheet went stale
# (the recurring bug behind #2061 and #2082). This gate restores the forcing
# function at PR time, where the author knows what is user-facing.
#
# Rule: if the PR title is a user-facing native feature —
#   feat(native|miniapp|calendar|markdown|chat): ...
# — and the PR touches client-android/, it must also ship a 패치노트 entry: a new
# changelog.d/YYYY-MM-DD-*.md fragment (preferred — one file per change, so PRs
# never conflict; see client-android/app/changelog.d/README.md) OR an edit to the
# frozen DenebPatchNotes.kt history.
#
# Escape hatch: add the "skip-patch-notes" label (e.g. a desktop-only or
# internal feat the operator judges not worth a phone-facing note).
#
# Fail-open by design: any uncertainty (missing base, empty diff) passes. This
# gate nudges; in a multi-agent repo it must never wedge unrelated PRs.

set -uo pipefail

NOTES_PATH="client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/DenebPatchNotes.kt"

# 1. Escape hatch — operator opted this PR out.
case ",${LABELS:-}," in
  *,skip-patch-notes,*)
    echo "skip-patch-notes label present — gate bypassed."
    exit 0
    ;;
esac

# 2. Only gate user-facing native features. Squash merge uses the PR title as
#    the commit subject on main, so the title is the reliable signal.
if ! printf '%s' "${PR_TITLE:-}" | grep -Eq '^feat\((native|miniapp|calendar|markdown|chat)\)'; then
  echo "PR title is not a user-facing native feat — gate not applicable."
  exit 0
fi

# 3. Diff the PR against its base. Fail-open if the base isn't reachable.
if [ -z "${BASE_SHA:-}" ] || [[ "$BASE_SHA" =~ ^0+$ ]] \
  || ! git rev-parse --verify "$BASE_SHA^{commit}" >/dev/null 2>&1; then
  echo "::warning::patch-notes gate: base commit unavailable; skipping (fail-open)."
  exit 0
fi

changed="$(git diff --name-only "$BASE_SHA" HEAD || true)"
if [ -z "$changed" ]; then
  echo "::warning::patch-notes gate: empty diff; skipping (fail-open)."
  exit 0
fi

# 4. A native feat must touch client-android/ AND ship a 패치노트 entry — either a
#    changelog.d fragment (preferred; one file per change, no merge conflicts) or
#    a direct edit to the frozen DenebPatchNotes.kt history.
touches_native="$(printf '%s\n' "$changed" | grep -c '^client-android/' || true)"
touches_notes="$(printf '%s\n' "$changed" | grep -cxF "$NOTES_PATH" || true)"
touches_fragment="$(printf '%s\n' "$changed" | grep -cE '^client-android/app/changelog\.d/[0-9]{4}-[0-9]{2}-[0-9]{2}-.+\.md$' || true)"

if [ "$touches_native" -eq 0 ]; then
  echo "feat title but no client-android/ changes — gate not applicable."
  exit 0
fi

if [ "$touches_fragment" -gt 0 ]; then
  echo "✓ Patch note present: changelog.d fragment added."
  exit 0
fi

if [ "$touches_notes" -gt 0 ]; then
  echo "✓ Patch note present: $NOTES_PATH updated."
  exit 0
fi

cat <<EOF
::error::This PR is a user-facing native feature but adds no 패치노트 entry.
Add a changelog.d fragment (preferred):
  client-android/app/changelog.d/$(date +%Y-%m-%d)-<slug>.md
with the Korean highlight line(s) the user will read — one bullet per non-blank
line (see client-android/app/changelog.d/README.md). It feeds the in-app
"패치노트" changelog; skipping it leaves it stale (#2061/#2082).
If this feat genuinely needs no phone-facing note (desktop-only or internal),
add the "skip-patch-notes" label to the PR.
EOF
exit 1
