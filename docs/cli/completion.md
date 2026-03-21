---
summary: "CLI reference for `deneb completion` (generate/install shell completion scripts)"
read_when:
  - You want shell completions for zsh/bash/fish/PowerShell
  - You need to cache completion scripts under Deneb state
title: "completion"
---

# `deneb completion`

Generate shell completion scripts and optionally install them into your shell profile.

## Usage

```bash
deneb completion
deneb completion --shell zsh
deneb completion --install
deneb completion --shell fish --install
deneb completion --write-state
deneb completion --shell bash --write-state
```

## Options

- `-s, --shell <shell>`: shell target (`zsh`, `bash`, `powershell`, `fish`; default: `zsh`)
- `-i, --install`: install completion by adding a source line to your shell profile
- `--write-state`: write completion script(s) to `$DENEB_STATE_DIR/completions` without printing to stdout
- `-y, --yes`: skip install confirmation prompts

## Notes

- `--install` writes a small "Deneb Completion" block into your shell profile and points it at the cached script.
- Without `--install` or `--write-state`, the command prints the script to stdout.
- Completion generation eagerly loads command trees so nested subcommands are included.
