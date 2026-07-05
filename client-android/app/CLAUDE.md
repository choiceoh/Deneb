# Native client (Kotlin Multiplatform, mobile)

This module is the **mobile** user surface (Android daily driver + iOS) of the
Deneb gateway. The desktop product UI was retired — the desktop surface is the
separate `andromeda/` workstation; the Compose Desktop target here survives
only as a build target for the headless verification harness below.

## Verifying changes (live app, no device)

Compile (`./gradlew :composeApp:compileKotlinDesktop`, run from this directory)
and mock previews (`./gradlew :composeApp:renderPreviews`) catch types and
static layout. To see the **real app live** — production data, navigation,
input, state flow — run the headless harness on the server (paths are
**repo-root relative**):

```bash
scripts/dev/native-app.sh start          # boot the real Compose Desktop app (phone, prod-connected)
scripts/dev/native-app.sh shot home      # screenshot → Read it
scripts/dev/native-app.sh tap 245 37     # drive it (coords = screenshot pixels)
scripts/dev/native-app.sh type "..."     # tap a field first, then type
scripts/dev/native-app.sh view           # noVNC, so a human can watch too
scripts/dev/native-app.sh stop
```

Full guide, command reference, and troubleshooting: repo root
`docs/agent-rules/native-live-app.md`. Design-system boundaries (controls =
Material, presentation = Deneb typography): repo root
`docs/agent-rules/native-design-system.md`. System gestures (edge swipes, etc.)
still need a real device.

## Gates before pushing

Run `make ci ARGS=--kotlin` from the repo root — spotless, detekt, desktop
smoke test, android compile (use full `make ci` only when the diff also spans
other lanes; CI re-verifies everything anyway). APK publishing goes through
`scripts/dev/publish-apk.sh` only (see repo root
`docs/agent-rules/release-and-deploy.md`).

