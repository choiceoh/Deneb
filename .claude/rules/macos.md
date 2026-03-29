---
description: "macOS 플랫폼 빌드/디버깅/런타임 규칙"
globs: ["**/macos/**", "**/darwin/**", "**/*.swift", "**/*.xcodeproj/**"]
---

# macOS Platform

- Vocabulary: "makeup" = "mac app".
- Rebrand/migration issues or legacy config/service warnings: run `deneb doctor` (see `docs/gateway/doctor.md`).
- Skill notes go in `tools.md` or `CLAUDE.md`.
- SwiftUI state management (iOS/macOS): prefer the `Observation` framework (`@Observable`, `@Bindable`) over `ObservableObject`/`@StateObject`; don't introduce new `ObservableObject` unless required for compatibility, and migrate existing usages when touching related code.
- Version locations: `package.json`, `core-rs/core/Cargo.toml`.
- Do not rebuild the macOS app over SSH; rebuilds must be run directly on the Mac.

## macOS Gateway & Logging

- Gateway currently runs only as the menubar app; there is no separate LaunchAgent/helper label installed. Restart via the Deneb Mac app or `scripts/restart-mac.sh`; to verify/kill use `launchctl print gui/$UID | grep deneb` rather than assuming a fixed label.
- **When debugging on macOS, start/stop the gateway via the app, not ad-hoc tmux sessions; kill any temporary tunnels before handoff.**
- macOS logs: use `./scripts/clawlog.sh` to query unified logs for the Deneb subsystem; it supports follow/tail/category filters and expects passwordless sudo for `/usr/bin/log`.

## Session Files & Remote Access

- When asked to open a "session" file, open the Pi session logs under `~/.deneb/agents/<agentId>/sessions/*.jsonl` (use the `agent=<id>` value in the Runtime line of the system prompt; newest unless a specific ID is given), not the default `sessions.json`.
- If logs are needed from another machine, SSH via Tailscale and read the same path there.

## Platform-Specific Workflows

- Use `$deneb-parallels-smoke` at `.agents/skills/deneb-parallels-smoke/SKILL.md` for Parallels smoke, rerun, upgrade, debug, and result-interpretation workflows across macOS, Windows, and Linux guests.
- For the macOS Discord roundtrip deep dive, use the narrower `.agents/skills/parallels-discord-roundtrip/SKILL.md` companion skill.
- Signal: "update fly" => `fly ssh console -a flawd-bot -C "bash -lc 'cd /data/clawd/deneb && git pull --rebase origin main'"` then `fly machines restart e825232f34d058 -a flawd-bot`.
- Status output: `status --all` = read-only/pasteable, `status --deep` = probes.
- Connection providers: when adding a new connection, update every UI surface and docs (macOS app, web UI, mobile if applicable, onboarding/overview docs) and add matching status + configuration forms so provider lists and settings stay in sync.

## Git & .agents Directories

- If you need local-only `.agents` ignores, use `.git/info/exclude` instead of repo `.gitignore`.
