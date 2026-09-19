---
title: "Development Environment Sync"
summary: "Keep six development machines on the same isolated Deneb toolchain."
read_when:
  - Preparing or repairing a Deneb development host
  - Updating the shared development toolchain across machines
---

# Development Environment Sync

The [agent entrypoint](/tools/agent-dev) brings stkernel's synchronization workflow
to Deneb: a committed manifest, checked archives, independent host results,
repeatable installs, and an optional daily origin/main bootstrap. The fleet is
four Linux ARM servers, one optional Linux x64 WSL workstation, and the local
Apple Silicon Mac. SSH aliases and optional hosts live in
`scripts/devenv/manifest.json` and can be overridden per run.

## Check and synchronize

```bash
./dev run env.check
./dev run env.check -- --local
./dev run env.setup -- --verify
./dev run env.sync -- --verify
./dev run env.sync -- --hosts build-host --verify
```

Run `env.sync` on the Mac to cover all six machines. A Linux coordinator covers
the five Linux hosts, calling itself locally. The Mac handles scheduled self-sync
locally; it needs no inbound SSH service. Only unreachable hosts explicitly
listed as optional are skipped. A required server failure, failed installer,
unknown timeout outcome, or reachable but outdated host fails the run.
An all-skipped run is `incomplete`, never a healthy fleet.

SSH uses the operator's existing configuration. Per-host JSON and diagnostics
are stored under `~/.local/state/deneb-devenv-sync/`. A timeout may leave remote
work running; inspect before retrying. A host lock prevents concurrent installers.
Each host needs Git, Python 3.12 or newer, HTTPS access to release publishers,
and configured SSH access for remote peers.

## Toolchain and isolation

The manifest pins Go, Node, Python, uv, golangci-lint, pnpm, and CodeGraph.
Linux ARM64, Linux AMD64, and macOS ARM64 release archives have committed
SHA-256 digests checked before extraction. Go follows the repository's 1.25 CI
lane; Node and uv match stkernel's existing pins. The npm lock pins pnpm and
CodeGraph platform packages; lifecycle scripts are disabled. Python uses both
existing hash-locked requirement files. No GPU packages or services are started.
Project dependencies such as `andromeda/node_modules`, Android SDKs, and native
desktop build libraries keep their existing setup workflows. Use `./dev status`
and the relevant adapter's prerequisites to inspect those requirements.

Tools live in generations under `~/.local/share/deneb-dev/`. Only the `current`
symlink moves after versions, locked packages, and requested CPU smoke checks
pass. Failed replacements leave the old generation available; old and incomplete
generations stay for inspection. Host SDKs, shell startup files, git identity,
credential helpers, serving checkouts, and stkernel environments are preserved.

`./dev` selects an explicit `DENEB_DEV_PYTHON`, a checkout `.venv`, the managed
interpreter, then its current interpreter. Child commands use the managed binary
directory. Make adapters pass PATH as a command-line variable so the repository's
Go bin prefix cannot silently replace the pinned linter in nested gates.
Managed child commands also use `GOTOOLCHAIN=local` and clear inherited `GOROOT`.
`env.check` compares actual versions and package metadata. `--verify` also runs
a small Go program and a Node assertion; it is toolchain evidence, not live
application, deployment, GPU, or model-quality evidence.

Linux hosts clone a missing development checkout at `~/deneb-dev`. Existing
feature branches and checkouts with tracked changes are reported as `held` and
left untouched. A clean `main` can only fast-forward. The production checkout at
`~/deneb`, including symlinks into it, is rejected. Interactive Mac setup uses
the caller's repository; scheduled Mac self-sync updates tools only.

## Optional daily synchronization

Ordinary synchronization never creates a recurring job. After a successful setup,
an operator can explicitly run this on the Linux coordinator and on the Mac:

```bash
./dev run env.timer
```

It installs the namespaced `deneb-devenv-sync` bootstrap and a systemd user timer
or macOS launchd job. The Linux job handles configured Linux peers; the Mac job
uses `--local`. Both run around 05:20 local time. Linux adds up to ten minutes
of randomized delay and catches missed runs.

The bootstrap fetches into a private bare mirror, freezes an exact `origin/main`
commit, and archives only synchronization code and lockfiles. It never executes
a dirty working tree or fetches into a serving checkout. The source commit is
reported with each run. Disable Linux scheduling with
`systemctl --user disable --now deneb-devenv-sync.timer`. Unload the Mac job with
`launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/ai.deneb.devenv-sync.plist`.

## Maintain and validate

Update release versions with publisher digests in `scripts/devenv/manifest.json`.
Update the npm manifest and lock together; pnpm must match
`andromeda/package.json`. Python requirement locks remain the package source of
truth. Keep all supported platform assets together. `DENEB_DEV_HOME` and
`DENEB_DEV_LOGS` select isolated installation and report locations for tests.

```bash
python3 -m unittest discover -s scripts/dev -p 'test_devenv*.py' -v
python3 -m unittest discover -s scripts/dev -p 'test_agent_dev.py' -v
make python-lint
make ci/fast
```

Operational shell tests run on Linux, as described in the agent tool guide.
Tests need no SSH access, credentials, running gateway, or GPU.
