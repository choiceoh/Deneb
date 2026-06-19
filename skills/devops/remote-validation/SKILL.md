---
name: remote-validation
version: "1.0.0"
category: devops
description: "Run and report real remote or CI-parity validation for Deneb work. Use when: broad tests, deploy proof, remote listener checks, APK/build artifact proof, srv1/DGX validation, or CI-like gates are needed. NOT for: tight local edit loops or speculative remote work."
metadata:
  {
    "deneb":
      {
        "emoji": "🧪",
        "requires": { "anyBins": ["ssh", "gh"] },
        "tags": ["remote", "validation", "CI", "deploy", "srv1", "DGX", "proof"],
        "related_skills": ["healthcheck", "tmux", "review-closeout"],
        "install":
          [
            {
              "id": "brew-gh",
              "kind": "brew",
              "formula": "gh",
              "bins": ["gh"],
              "label": "Install GitHub CLI (brew)",
            },
            {
              "id": "apt-gh",
              "kind": "apt",
              "package": "gh",
              "bins": ["gh"],
              "label": "Install GitHub CLI (apt)",
            },
          ],
      },
  }
---

# Remote Validation

Use this when local proof is not enough: broad suites, CI parity, remote deploy,
native artifact, production listener, or host-specific behavior.

## First Decision

Choose exactly one validation surface:

- **local focused**: smallest test that proves an edit loop.
- **CI/GitHub**: authoritative checks for a PR.
- **srv1/DGX remote**: host-specific services, listeners, GPUs, sidecars, or
  deploy checkout.
- **native artifact**: APK/build output and the served/downloadable artifact.

Do not mix surfaces in the report. Run more than one only when each proves a
different risk.

## Procedure

1. State the risk being proven.
2. Confirm the target: host, branch/ref, PR/check run, service, route, artifact,
   or listener.
3. Run the smallest command that proves that risk.
4. Capture the exact proof: command intent, exit status, commit/ref, URL/port,
   artifact path/hash/size, check run, or listener response.
5. If proof fails, report the first actionable failure. Do not downgrade to a
   weaker surface without saying so.
6. Clean up temporary remote sessions, leases, or background processes unless
   the user asked to keep them alive.

## Reporting

Always report:

- surface: local, CI, srv1, DGX, native artifact, or other named host.
- ref: branch, commit, PR, or deploy checkout.
- proof: tests/checks/listener/artifact, with the exact result.
- limitations: what this proof did not cover.

Good:

```text
surface=srv1, ref=origin/main@abc123
proof=:10024 accepted 20/20 LMTP connects after restart; systemd socket active
limitation=native APK not rebuilt in this run
```

Bad:

```text
Looks deployed.
```

## Safety

- Never print secrets. Use configured secret references or host-side env.
- Do not change firewall, SSH, systemd, or exposed routes without explicit user
  approval unless the user already asked for that state-changing operation.
- If `ssh srv1` is available, prefer it for Deneb remote checks. If not, say the
  access path is blocked instead of inventing proof.
