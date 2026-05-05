# 1Password Secret References

Deneb can read model provider API keys from 1Password CLI secret references.
This keeps plaintext provider keys out of `deneb.json`.

## Requirements

- 1Password CLI (`op`)
- One authentication method:
  - Desktop app integration for interactive local use
  - `OP_SERVICE_ACCOUNT_TOKEN` for long-running Gateway or headless use

For long-running Deneb, prefer a 1Password service account with access limited
to a Deneb-specific vault.

## Provider Config

Use `apiKeyRef` instead of `apiKey` under `models.providers`.

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

`apiKeyRef` currently supports 1Password references beginning with `op://`.
If `apiKeyRef` is present, it wins over `apiKey`. Remove plaintext `apiKey`
after migration.

## Telegram Config

Use `botTokenRef` instead of `botToken` under `channels.telegram`.

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

`botTokenRef` wins over `botToken` when both are present.

## Gateway Environment

For service account auth, put only the service account token in
`~/.deneb/.env` or the process environment:

```sh
OP_SERVICE_ACCOUNT_TOKEN=ops_...
```

Deneb resolves `apiKeyRef` by running:

```sh
op read op://Deneb/OpenRouter/api_key
```

Deneb resolves `botTokenRef` the same way, using the configured Telegram
reference. The resolved values are used only at runtime for authentication.

## Verify

Check that the CLI can read the reference before restarting Deneb:

```sh
op read op://Deneb/OpenRouter/api_key
op read op://Deneb/Telegram/bot_token
```

Then restart the Gateway so provider configs are reloaded.

## Guardrails

- Do not paste raw provider keys into chat.
- Do not store raw provider keys in `deneb.json`.
- Do not grant Deneb's service account access to personal/private vaults.
- Avoid exposing OTP, SSH private keys, or unrelated vault items through Deneb.
