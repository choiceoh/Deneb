# wormhole

A thin OpenAI-compatible router — the **wormhole api** — that fans one `/v1`
endpoint out to many model backends (local vLLM and cloud providers alike),
picked by the requested **model name**. External clients (Claude Code, scripts,
future apps) point at one URL with one token; the upstream provider keys stay
inside wormhole.

## What it does

A **multi-protocol transparent proxy**. wormhole speaks both wire APIs on the
front and forwards each to a backend of the matching protocol — **no
cross-translation**. Per request it:

1. authenticates the client against the wormhole token (`Authorization: Bearer`
   or `x-api-key`),
2. resolves the requested `model` to an upstream backend,
3. rewrites the upstream URL, injects that backend's key in the right shape
   (Bearer for OpenAI, `x-api-key` for Anthropic), and (optionally) rewrites the
   `model` id,
4. streams the response straight back — so streaming, tool calls, and every
   parameter ride through untouched.

The insight: a client that already speaks Anthropic (Claude Code, the Anthropic
SDK) just hits `/v1/messages` and rides straight through to an Anthropic backend
— there's nothing to translate. OpenAI clients hit `/v1/chat/completions` and
reach an OpenAI backend the same way. Each model declares its `protocol`
(`openai` default, or `anthropic`); hitting the wrong endpoint for a model gets
an actionable `400`.

## Explicit or auto model selection

On the same endpoint a client either **names a model** (`"model": "dsv4"`) to hit
that backend directly, or sends **`"model": "auto"`** to delegate the choice. Auto
runs a **local-first fallback chain**: the configured `auto` candidates are tried
in order — local first — committing to the first that connects with a non-5xx
status and falling through on an unreachable or 5xx backend. The egress guard
still applies (a local-only request skips cloud candidates), and only candidates
matching the endpoint's protocol are considered. (Task-aware routing — pick by
request size/effort — can layer on top of this chain later.)

## Thinking routing (effort)

For a model that declares a `toggleKwarg` (the vLLM `chat_template_kwargs` boolean
that disables its thinking phase — `thinking` for DeepSeek V4, `enable_thinking`
for Qwen3), wormhole runs Deneb's **Ares effort classifier** on each request and
turns thinking **off** for an obviously-simple turn (short, no code, no
analysis/planning signal, no attachment) — injecting the toggle into the request
before forwarding. It's the same `Decide()` the chat pipeline uses, so any client
hitting wormhole (Claude Code, scripts) gets the same routing for free, no
re-implementation. Routing is one-directional: it only ever turns thinking off for
a simple turn, never forces it on. A model with no `toggleKwarg` passes through
untouched.

## SparkFleet auto-discovery

wormhole can pull its local model list from **SparkFleet** (the GB10 fleet
manager) instead of hand-maintaining one. Point it at SparkFleet's control-plane:

```json
"sparkfleet": { "url": "http://127.0.0.1:18900", "token": "${FLEET_TOKEN}" }
```

wormhole polls SparkFleet's `GET /api/services` every 15s, and every **live vLLM**
becomes a routable model: `name` = the served model id, `url` = its `/v1` base,
`protocol` = openai, marked **local**, no key. Launch a model in SparkFleet and it
becomes reachable here within a poll — no config edit. This is a one-way coupling:
wormhole (the **data plane** — model access) reads SparkFleet (the **control
plane** — model lifecycle), never the reverse.

A configured `models` entry **wins** over a discovered one of the same name (so
you can still pin a key, protocol, or upstream id). A transient SparkFleet outage
keeps the last-known set (a stale entry just `502`s, and `auto` falls past it);
removing the `sparkfleet` block drops the discovered models on the next reload.

## Endpoints

- `POST /v1/chat/completions` — OpenAI clients → OpenAI-protocol backends.
- `POST /v1/messages` — Anthropic clients → Anthropic-protocol backends.
- `GET /v1/models` — lists the routable model names (configured + discovered).
- `GET /status` — rich live operational readout (feature flags + per-model
  protocol/local/thinking/source); token-gated. Powers the native management tab.
- `GET /metrics` — Prometheus text: request/error counts and cumulative latency,
  total and per model. The always-on visibility for the hot path (wormhole
  otherwise logs only errors); token-gated. Scrape it or `curl` it to see what is
  flowing. Divide `wormhole_model_latency_ms_sum` by `wormhole_model_requests_total`
  for the average latency per model.
- `GET /health` — liveness.

## Config validation

On load and on every hot-reload, wormhole logs warnings for a config that would
route wrong but parse fine — most usefully an **anthropic `url` that doesn't end
in `/v1`** (wormhole appends only `/messages`, so a bare base `404`s), plus
duplicate model names, unknown `protocol` values, and `auto` candidates that
aren't configured models. It never fails the load (a bad reload must not take down
the hot path); it surfaces the problem so you fix it before the first request does.

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
| `url` | upstream API base, e.g. `http://127.0.0.1:8000/v1` |
| `key` | upstream token (omit for keyless local vLLM) |
| `protocol` | `openai` (default) or `anthropic` — which front endpoint + auth shape |
| `upstreamModel` | rewrite the model id when forwarding (default: `name`) |
| `toggleKwarg` | vLLM kwarg that disables the model's thinking phase — enables thinking routing (see below) |
| `local` | override the local/cloud auto-detection (see below); default auto |

Top-level config: `localOnly: true` air-gaps the whole instance (every cloud
model is refused); `auto: ["dsv4", "claude"]` sets the ordered candidate list for
the reserved `auto` model name (`autoName` overrides the name; default `auto`);
`sparkfleet: { url, token }` auto-discovers local models (see above).

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
