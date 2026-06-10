# Verifying changes (live app, no device)

Compile (`./gradlew :composeApp:compileKotlinDesktop`) and mock previews
(`./gradlew :composeApp:renderPreviews`) catch types and static layout. To see
the **real app live** — production data, navigation, input, state flow — run the
headless harness on the server:

```bash
scripts/dev/native-app.sh start          # boot the real Compose Desktop app (phone, prod-connected)
scripts/dev/native-app.sh shot home      # screenshot → Read it
scripts/dev/native-app.sh tap 245 37     # drive it (coords = screenshot pixels)
scripts/dev/native-app.sh type "..."     # tap a field first, then type
scripts/dev/native-app.sh view           # noVNC, so a human can watch too
scripts/dev/native-app.sh stop
```

Full guide, command reference, and troubleshooting: `.claude/rules/native-live-app.md`.
System gestures (edge swipes, etc.) still need a real device.

