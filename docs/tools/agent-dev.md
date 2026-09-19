---
title: "Agent Development Tools"
summary: "Discover, inspect and run Deneb development tools through one JSON entrypoint."
read_when:
  - Starting a coding task or finding an existing development tool
  - Diagnosing missing dependencies or maintaining tool adapters
---

# Agent Development Tools

Start a coding session with `./dev`. It implements the same version 1 JSON
interface as stkernel's agent tool entrypoint, with a Deneb-specific catalog.
Deneb does not need a stkernel checkout or an additional runtime package.
Discovery uses the Python standard library and never imports discovered tools,
evaluates Makefile recipes, installs dependencies, or starts a service.

## Discover and inspect

```bash
./dev
./dev search "RPC"
./dev search "검증"
./dev describe rpcmap
./dev status
./dev audit
```

`search` combines managed adapters with automatically discovered entrypoints in
`scripts/`, literal Makefile targets, and scripts in the Andromeda and Even G2
package manifests. It returns at most 20 items by default; use `--limit` (1–100)
and `--offset` with `next_offset` to page. Script modules without an entrypoint,
test files, hidden directories, and symlinks are excluded from script discovery.
An unmanaged result is inspectable through `describe`, but cannot be executed
by `run` until its adapter is registered.

`describe` returns an argument schema, examples, source help, working directory,
effects, prerequisites, and timeout. It does not call the underlying `--help`;
some scripts do work before parsing arguments. Search terms match tool IDs,
summaries, and curated Korean intent keywords.

## Invoke existing tools

```bash
./dev run rpcmap -- miniapp.people.list --json
./dev run --dry-run andromeda.verify
./dev run python.test
./dev run ci.fast
./dev run ci -- ARGS=--kotlin
./dev run --timeout 1800 andromeda.verify
./dev run pr.watch -- 123
```

Place wrapper options before the tool ID. Arguments after the ID and optional
`--` separator are forwarded literally, without shell interpolation. Relative
file arguments resolve from the declared working directory: the repository
root for most adapters, `andromeda/` for desktop tools. The entrypoint also works
when invoked by absolute path from another directory. `--dry-run` returns the
command and directory without checking readiness or executing the tool.

The adapters delegate to existing Makefile targets, `scripts/dev/rpcmap.py`,
`scripts/dev/live-test.sh`, and `scripts/dev/pr.sh`. Their rules still apply:
`ci.fast` is for an unambiguous single-lane diff; shared surfaces need `ci`;
desktop changes need `andromeda.verify`; gateway behavior changes need live
verification. `pr.land` uses the existing check, squash, landing verification,
and remote branch cleanup path. Only use it for a requested merge. See the
[Git rules](/agent-rules/git-pr) and [live testing rules](/agent-rules/live-testing).

Discovery and the wrapper work on macOS and Linux. The underlying full Python
lane includes existing operational shell fixtures that assume Linux/GNU
behavior; run that lane in Linux CI or a Linux container when working on macOS.
The wrapper does not silently skip incompatible tests or replace their assertions.

## Environment and evidence

Python selection is `DENEB_DEV_PYTHON`, then the checkout's `.venv/bin/python`,
then the interpreter running the entrypoint. An invalid explicit override is
an error. The selected interpreter's bin directory and the Go user bin directory
are prepended to child `PATH`, so nested `python3` commands use the same environment.

`status` reports executable paths, locked Python package versions, prerequisite
directories, and requirements read from `gateway-go/go.mod`,
`andromeda/package.json`, and `pyproject.toml`. It does not print environment
variables or read credentials. Executable versions, authentication, and live
readiness are not probed. Missing optional lane tools may yield `needs_attention`.
Recovery commands are suggestions and never run automatically. To prepare the
repository's existing locked Python dependencies:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install --require-hashes -r requirements-dev.lock
```

Every response except CLI help is JSON with `schema_version: 1`. `run` includes
`status`, `process_exit_code`, `validation`, bounded output, and truncation flags.
Complete JSON stdout is returned once as `data`, with `stdout: null`. The default
output limit is 12,000 bytes per stream; `--max-output` accepts 256–1,048,576 bytes.
Output is spooled into temporary files removed after the call. A timeout (exit
124) or cancellation (130) terminates the local process group. Partial files,
remote work, and already completed operations may remain; inspect before retrying.

`completed` means the process exited zero; `validation: not_assessed` means its
output still needs interpretation. It does not establish deployment, model
quality, skipped lane coverage, or live readiness. A prerequisite failure returns
`blocked` (2). The advisory environment doctor with missing requirements, and a
fast gate with nothing selected, return `incomplete` (3) while retaining the
underlying zero exit code. Shell lint blocks when ShellCheck is unavailable.
Other failures preserve the child exit code. `status` is an inventory, so its
`needs_attention` response is not a failing test result.

## Maintain the catalog

Add execution adapters in `scripts/dev/agent_dev_catalog.py`. Use the existing
command, source, working directory, effects, prerequisites, and a useful example.
Do not add another installer, queue, dispatcher, or merge implementation.
`audit` catches duplicate IDs, missing source/docs/directories, removed Makefile
targets, and removed package scripts. It reports the count of discovered but
unmanaged entrypoints so additions do not stay invisible.

The owner is `scripts/dev/agent_dev.py`; behavior contracts live in
`scripts/dev/test_agent_dev.py` and run automatically in the existing Python lane:

```bash
python3 -m unittest discover -s scripts/dev -p 'test_agent_dev.py' -v
make python-lint
make ci/fast
```
