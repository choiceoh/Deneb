# wormhole

A thin OpenAI-compatible router — the **wormhole api** — that fans one `/v1`
endpoint out to many model backends (local vLLM and cloud providers alike),
picked by the requested **model name**. External clients (Claude Code, scripts,
future apps) point at one URL with one token; the upstream provider keys stay
inside wormhole.

## What it does (first slice)

Pure pass-through for **OpenAI-compatible** upstreams (local vLLM, OpenRouter,
Kimi, MiMo, …). Per request it:

1. authenticates the client against the wormhole token,
2. resolves the requested `model` to an upstream backend,
3. rewrites the upstream URL, injects that backend's key, and (optionally)
   rewrites the `model` id,
4. streams the response straight back — so streaming, tool calls, and every
   OpenAI parameter ride through untouched.

Native **Anthropic-API** translation is a planned fast-follow (it will reuse
`internal/ai/llm`'s hardened Anthropic client). Until then, reach Claude through
an OpenAI-compatible front such as OpenRouter (see the example config).

## Endpoints

- `POST /v1/chat/completions` — the proxied chat endpoint.
- `GET /v1/models` — lists the configured model names so clients can discover.
- `GET /health` — liveness.

## Run

```bash
go run ./cmd/wormhole --config ~/.wormhole/config.json
# or: go build -o dist/wormhole ./cmd/wormhole && ./dist/wormhole
```

Config (`~/.wormhole/config.json` by default; see `config.example.json`). The
top-level `token` and each model `key` support `${ENV}` expansion, so secrets
live in the environment, not the file. Each model entry:

| field | meaning |
|---|---|
| `name` | the model id clients request |
| `url` | upstream OpenAI base, e.g. `http://127.0.0.1:8000/v1` |
| `key` | upstream bearer token (omit for keyless local vLLM) |
| `upstreamModel` | rewrite the model id when forwarding (default: `name`) |
| `local` | override the local/cloud auto-detection (see below); default auto |

Top-level `localOnly: true` air-gaps the whole instance (every cloud model is
refused).

## Local-first egress guard

A wormhole that fronts both local and cloud models is a place where one routing
slip could send private data to a cloud provider. wormhole auto-classifies each
model as **local** (loopback / private-network / `localhost` URL) or **cloud**
(anything else), logs the cloud models at startup so the egress surface is
visible, and lets a sensitive caller guarantee no cloud egress:

- per-request: send header `X-Wormhole-Local-Only: 1` → any cloud-backed model
  is refused with `403` for that request.
- per-instance: set `"localOnly": true` in the config → cloud models are always
  refused.

Override the auto-detection with a model's `"local"` field (e.g. mark an on-box
tunnel that egresses as `false`).

## Use from a client

```bash
curl http://localhost:18800/v1/chat/completions \
  -H "Authorization: Bearer $WORMHOLE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"dsv4","messages":[{"role":"user","content":"hi"}],"stream":true}'
```

Point Claude Code (or any OpenAI client) at `http://localhost:18800/v1` with the
wormhole token, and `model: dsv4` hits the local box while `model: claude` goes
to the cloud — one URL, one token.
