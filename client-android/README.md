# Deneb Native Client

The native client for Deneb — a Kotlin Multiplatform (Compose) app that runs on
Android (Galaxy S26, the daily driver), desktop (Mac/JVM), and iOS from one
codebase. It renders the chat UI, the interactive `deneb-ui` blocks, and the
`miniapp.*` screens (mail, calendar, people, search, work feed). All agent work,
tools, memory, and always-on processing live on the DGX Spark gateway.

The app talks to the gateway over the `miniapp.*` RPC surface and authenticates
with a bearer secret in the `X-Deneb-Client-Token` header. Generate it once on
the gateway host:

```bash
go run ./gateway-go/cmd/deneb-client-token   # prints the token; paste into the app
```

## Layout

```
client-android/
  README.md   ← you are here
  NOTICE      ← Apache-2.0 attribution for the vendored upstream UI
  VENDOR.txt  ← the upstream commit this client was seeded from
  app/        ← the KMP app (composeApp shared code + androidApp/iosApp shells)
```

## Build & verify

- Compile (no device): `cd app && ANDROID_HOME=~/android-sdk ./gradlew :composeApp:compileKotlinDesktop`
- Static previews: `./gradlew :composeApp:renderPreviews` → `/tmp/deneb-render/*.png`
- Live app on the server (headless harness): `scripts/dev/native-app.sh start|shot|tap|stop` — see `.claude/rules/native-live-app.md`
- Publish an OTA build: `scripts/dev/publish-apk.sh "release notes"`

## Architecture

The seam between the UI and the gateway is `DataRepository`, a single Koin
binding: `DenebGatewayClient` implements it against the gateway's `miniapp.*`
RPC. The interactive `deneb-ui` renderer lives in
`app/composeApp/src/commonMain/kotlin/ai/deneb/ui/dynamicui/`; the agent emits
```` ```deneb-ui ```` JSON fences that it draws as native interactive screens,
and button presses round-trip back to the gateway as new turns.

## Provenance & license

The UI layer was seeded by vendoring the Compose UI of
[Kai](https://github.com/SimonSchubert/Kai) (Apache-2.0) and replacing its brain
with the Deneb gateway. The upstream attribution and license are retained in
`NOTICE`, `VENDOR.txt`, and the per-file headers; Deneb's own modifications are
likewise licensed under the Apache License, Version 2.0.
