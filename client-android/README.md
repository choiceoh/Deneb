# Deneb Native Android Client

A native Android client for Deneb, built by **vendoring the UI of [Kai](https://github.com/SimonSchubert/Kai)** (Apache-2.0) and replacing its brain with calls to the Deneb gateway. The phone gets Kai's rich chat UI + interactive `kai-ui` renderer; the agent, tools, memory, and always-on work stay on the DGX Spark gateway.

> **Why vendor instead of fork?** Single git, no second repo to track. We keep ~half of Kai (the UI) and gut the other half (its own LLM providers, memory, tools, sandbox), so upstream merges aren't useful anyway.

This directory is the **kit**; `app/` is created when you run the vendor script.

```
client-android/
  README.md       ← you are here (the build plan)
  NOTICE          ← Apache-2.0 attribution for the vendored code
  vendor-kai.sh   ← clones Kai into app/
  app/            ← vendored Kai project (after you run the script)
```

## Server side is already done

Two committed gateway PRs make the phone able to attach:

- **kai-ui emission** — the agent emits ```` ```kai-ui ```` JSON fences that Kai's renderer (`ui/dynamicui/`) draws unchanged. Gated per-client; Telegram unaffected.
- **client auth** — a standalone app authenticates with a bearer secret in the `X-Deneb-Client-Token` header. Generate it once on the gateway host:

  ```bash
  go run ./gateway-go/cmd/deneb-client-token   # prints the token; paste into the app
  ```

## Prerequisites

- JDK 21, Android SDK, and a device/emulator (target: Galaxy S25).
- `git` for the vendor step.

## Step 1 — Vendor Kai

```bash
cd client-android
KAI_REF=<pin-a-sha> ./vendor-kai.sh    # records the SHA in VENDOR.txt
```

Pin a specific commit for reproducibility (omit `KAI_REF` to take `main`).

## Step 2 (PR-C) — Make it Android-only and build the UI shell

The Android UI compiles independently of the brain via one interface (`DataRepository`, Koin DI), so this step gets a running shell before any rewrite.

1. **Drop non-Android targets** in `app/composeApp/build.gradle.kts`: remove the `iosArm64`, `iosSimulatorArm64`, `jvm("desktop")`, and `wasmJs` target blocks.
2. **Delete their source sets**: `app/composeApp/src/{iosMain,desktopMain,wasmJsMain,jvmShared}`.
3. **Drop the iOS app**: remove `include(":iosApp")` from `app/settings.gradle.kts` and delete `app/iosApp/`. (Also droppable: `fastlane/`, `aur/`, non-Android CI.)
4. **Build**: `./gradlew :composeApp:assembleDebug` and fix any leftover `expect/actual` gaps for the removed targets.

At this point the app builds and runs with Kai's own brain still inside.

## Step 3 (PR-D) — Swap the brain for the Deneb gateway

The seam is `DataRepository` (interface in `app/composeApp/src/commonMain/kotlin/com/inspiredandroid/kai/data/DataRepository.kt`). One Koin binding swaps the whole brain.

1. **Implement `DenebGatewayClient : DataRepository`**:
   - Auth: send `X-Deneb-Client-Token: <token>` on every request.
   - Drive a turn and feed the `chatHistory: StateFlow<List<History>>` from the reply. v1 is whole-message (Kai is already whole-message; live token streaming is a later enhancement).
   - **Server dependency (gateway-side, Go):** the existing `POST /api/v1/miniapp/rpc` confines callers to the `miniapp.*` namespace, so `chat.send` is not reachable from it. Add a thin `miniapp.chat.send` (and `miniapp.chat.history`) bridge method on the gateway that forwards to the chat handler's `SendSync`. This is a small, verifiable Go change — do it before/with this step.
2. **Rewire DI**: in `app/composeApp/src/commonMain/kotlin/com/inspiredandroid/kai/di/AppModule.kt`, replace `single<DataRepository> { get<RemoteDataRepository>() }` with `single<DataRepository> { DenebGatewayClient(...) }`.
3. **Gut the brain** (delete; none are load-bearing for UI rendering): `data/RemoteDataRepository.kt` and the memory/heartbeat/scheduler files, `network/` + DTOs, `mcp/`, `inference/`, `tools/`, `email/`, `sms/`, `splinterlands/`, plus the `ui/settings/` and `ui/sandbox/` screens that reference them. Narrow `DataRepository` to the surviving members (chat + conversation ops) and stub the rest.

Keep `notifications/KaiNotificationListenerService.kt` if you want phone-notification ingestion later (repoint it to POST a Deneb ingest endpoint) — otherwise drop it too.

## Step 4 (PR-E) — Light up kai-ui end to end

The renderer is already vendored (`ui/dynamicui/`). The agent already emits `kai-ui` fences (server PR-A). Verify:

1. A reply containing a ```` ```kai-ui ```` fence renders as an interactive screen (Kai's `ui/markdown/BlockScanner.kt` detects the fence — make sure the native delivery path does **not** Telegram-normalize it; it should arrive as raw assistant text).
2. Wire `onCallback(event, data)` (from `KaiUiRenderer`) to send a follow-up turn via the bridge from Step 3 — e.g. a message carrying the event + collected input values. `collectFrom` on a button gathers the listed input-node ids.

## Step 5 (PR-F) — Chat polish parity

Markdown/math rendering (already in `ui/markdown/`), map server tool events to `TOOL_EXECUTING` chips, error states, conversation list.

## License

The vendored code under `app/` is Apache-2.0; retain its `LICENSE` and header notices. See `NOTICE`. Deneb modifications are Apache-2.0 as well.
