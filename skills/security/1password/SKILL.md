---
name: 1password
version: "1.0.0"
category: security
description: "Set up and verify 1Password CLI secret references for Deneb credentials. Use when configuring op:// apiKeyRef or botTokenRef values, checking op auth, or migrating plaintext keys out of deneb.json. NOT for printing raw secrets or granting broad vault access."
metadata:
  {
    "deneb":
      {
        "emoji": "🔐",
        "requires": { "bins": ["op"] },
        "tags": ["security", "secrets", "1password", "op", "apiKeyRef", "botTokenRef"],
        "related_skills": ["healthcheck"],
        "install":
          [
            {
              "id": "brew",
              "kind": "brew",
              "formula": "1password-cli",
              "bins": ["op"],
              "label": "Install 1Password CLI (brew)",
            },
          ],
      },
  }
---

# 1Password

Use this skill when Deneb should use 1Password secret references instead of
plaintext credentials in `deneb.json`.

## When to Use

- The user wants to configure `models.providers.*.apiKeyRef`
- The user wants to configure `channels.telegram.botTokenRef`
- The user asks how to move API keys out of Deneb config
- `op://...` references fail and need verification
- 1Password CLI auth needs setup or status checking

## Procedure

1. Check whether `op` is installed.
2. Verify authentication with `op whoami`.
3. Verify the specific reference with `op read op://Vault/Item/field`.
4. Tell the user to put only the reference in `deneb.json`:

```json
{
  "models": {
    "providers": {
      "openrouter": {
        "baseUrl": "https://openrouter.ai/api/v1",
        "api": "openai",
        "apiKeyRef": "op://Deneb/OpenRouter/api_key"
      }
    }
  }
}
```

For Telegram:

```json
{
  "channels": {
    "telegram": {
      "botTokenRef": "op://Deneb/Telegram/bot_token",
      "chatID": 123456789
    }
  }
}
```

5. For long-running Gateway use, prefer `OP_SERVICE_ACCOUNT_TOKEN` in the
   process environment or `~/.deneb/.env`.
6. Restart the Gateway after changing provider config.

## Guardrails

- Never print raw secrets back to the user.
- Do not ask the user to paste raw API keys into chat.
- Do not run `op read` for unrelated vaults or broad personal/private vault
  material.
- Prefer a Deneb-specific vault and least-privilege service account.

## Verification

- `op whoami` succeeds.
- `op read <apiKeyRef>` succeeds locally.
- Deneb model calls use the provider without a plaintext `apiKey`.
