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

# Feature Docs

Feature specs live in `docs/features/`. Each describes a feature from a product/behavior perspective — no Kotlin code blocks, no class/function names in prose.

When you modify logic in a feature area that has a corresponding doc in `docs/features/`:
- Update the doc to reflect the new behavior
- Update the "Last verified" date in the doc header
- Keep the Key Files table accurate (add/remove files as needed)
