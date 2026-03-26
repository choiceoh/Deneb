# Contributing to Deneb

Welcome to Deneb!

## Quick Links

- **GitHub:** https://github.com/choiceoh/Deneb
- **Vision:** [`VISION.md`](VISION.md)

## How to Contribute

1. **Bugs & small fixes** → Open a PR!
2. **New features / architecture** → Start a [GitHub Discussion](https://github.com/choiceoh/Deneb/discussions) or ask in Discord first
3. **Test/CI-only PRs for known `main` failures** → Don't open a PR, the Maintainer team is already tracking it and such PRs will be closed automatically. If you've spotted a _new_ regression not yet shown in main CI, report it as an issue first.
4. **Questions** → Open a [GitHub Discussion](https://github.com/choiceoh/Deneb/discussions) or file an issue

## Before You PR

- Test locally with your Deneb instance
- Run tests: `make check && make test`
- If you have access to Codex, run `codex review --base origin/main` locally before opening or updating your PR. Treat this as the current highest standard of AI review, even if GitHub Codex review also runs.
- Do not submit test or CI-config fixes for failures already red on `main` CI. If a failure is already visible in the [main branch CI runs](https://github.com/choiceoh/Deneb/actions), it's a known issue the Maintainer team is tracking, and a PR that only addresses those failures will be closed automatically. If you spot a _new_ regression not yet shown in main CI, report it as an issue first.
- Ensure CI checks pass
- Keep PRs focused (one thing per PR; do not mix unrelated concerns)
- Describe what & why
- Reply to or resolve bot review conversations you addressed before asking for review again
- **Include screenshots** — one showing the problem/before, one showing the fix/after (for UI or visual changes)
- Use American English spelling and grammar in code, comments, docs, and UI strings
- Do not edit files covered by `CODEOWNERS` security ownership unless a listed owner explicitly asked for the change or is already reviewing it with you. Treat those paths as restricted review surfaces, not opportunistic cleanup targets.

## Review Conversations Are Author-Owned

If a review bot leaves review conversations on your PR, you are expected to handle the follow-through:

- Resolve the conversation yourself once the code or explanation fully addresses the bot's concern
- Reply and leave it open only when you need maintainer or reviewer judgment
- Do not leave "fixed" bot review conversations for maintainers to clean up for you
- If Codex leaves comments, address every relevant one or resolve it with a short explanation when it is not applicable to your change
- If GitHub Codex review does not trigger for some reason, run `codex review --base origin/main` locally anyway and treat that output as required review work

This applies to both human-authored and AI-assisted PRs.

## AI/Vibe-Coded PRs Welcome! 🤖

Built with Codex, Claude, or other AI tools? **Awesome - just mark it!**

Please include in your PR:

- [ ] Mark as AI-assisted in the PR title or description
- [ ] Note the degree of testing (untested / lightly tested / fully tested)
- [ ] Include prompts or session logs if possible (super helpful!)
- [ ] Confirm you understand what the code does
- [ ] If you have access to Codex, run `codex review --base origin/main` locally and address the findings before asking for review
- [ ] Resolve or reply to bot review conversations after you address them

AI PRs are first-class citizens here. We just want transparency so reviewers know what to look for. If you are using an LLM coding agent, instruct it to resolve bot review conversations it has addressed instead of leaving them for maintainers.

## Current Focus & Roadmap 🗺

We are currently prioritizing:

- **Aurora Context Engine**: Improving DAG-based compaction and multi-layer recall
- **Memory Stack**: Vega memory backend + Aurora Memory Module integration
- **Stability**: Fixing edge cases in Telegram channel and gateway
- **Performance**: Optimizing token usage and compaction logic
- **Multi-Agent**: Sub-agent orchestration and bounded execution improvements

Check the [GitHub Issues](https://github.com/choiceoh/Deneb/issues) for "good first issue" labels!

## Report a Vulnerability

We take security reports seriously. Report vulnerabilities via [GitHub Security Advisories](https://github.com/choiceoh/Deneb/security/advisories) or open an issue on the repository.

### Required in Reports

1. **Title**
2. **Severity Assessment**
3. **Impact**
4. **Affected Component**
5. **Technical Reproduction**
6. **Demonstrated Impact**
7. **Environment**
8. **Remediation Advice**

Reports without reproduction steps, demonstrated impact, and remediation advice will be deprioritized. Given the volume of AI-generated scanner findings, we must ensure we're receiving vetted reports from researchers who understand the issues.
