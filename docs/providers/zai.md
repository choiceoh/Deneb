---
summary: "Use Z.AI (GLM models) with Deneb, including Coding Plan"
read_when:
  - You want Z.AI / GLM models in Deneb
  - You need a simple ZAI_API_KEY setup
  - You want to use Z.AI Coding Plan
title: "Z.AI"
---

# Z.AI

Z.AI is the API platform for **GLM** models. It provides REST APIs for GLM and uses API keys
for authentication. Create your API key in the Z.AI console. Deneb uses the `zai` provider
with a Z.AI API key.

## Coding Plan vs General API

Z.AI offers two API tiers:

| Tier            | Endpoint                      | Best for                                        |
| --------------- | ----------------------------- | ----------------------------------------------- |
| **Coding Plan** | `api.z.ai/api/coding/paas/v4` | Coding-optimized workloads with dedicated quota |
| **General API** | `api.z.ai/api/paas/v4`        | General-purpose GLM access                      |

Both tiers also have a CN (China region) variant using `open.bigmodel.cn` instead of `api.z.ai`.

## CLI setup

<Tabs>
<Tab title="Coding Plan (recommended)">

```bash
# Coding Plan Global
deneb onboard --auth-choice zai-coding-global

# Coding Plan CN (China region)
deneb onboard --auth-choice zai-coding-cn
```

</Tab>
<Tab title="General API">

```bash
# General API Global
deneb onboard --auth-choice zai-global

# General API CN (China region)
deneb onboard --auth-choice zai-cn
```

</Tab>
<Tab title="Auto-detect">

```bash
# Auto-detect the best endpoint for your API key
deneb onboard --auth-choice zai-api-key
```

Deneb probes all four endpoints in parallel and selects the first one that
accepts your key. Use this when you are unsure which tier your key belongs to.

</Tab>
</Tabs>

## Config snippet

```json5
{
  env: { ZAI_API_KEY: "sk-..." },
  agents: { defaults: { model: { primary: "zai/glm-5" } } },
}
```

## Available models

| Model         | Reasoning | Vision | Context window |
| ------------- | --------- | ------ | -------------- |
| `zai/glm-5`   | ✅        | ✅     | 256k           |
| `zai/glm-4.7` | ❌        | ✅     | 200k           |

## Notes

- GLM models are available as `zai/<model>` (example: `zai/glm-5`).
- `tool_stream` is enabled by default for Z.AI tool-call streaming. Set
  `agents.defaults.models["zai/<model>"].params.tool_stream` to `false` to disable it.
- See [/providers/glm](/providers/glm) for the model family overview.
- Z.AI uses Bearer auth with your API key.
- Endpoint auto-detection returns diagnostic details when all probes fail, making it
  easier to debug API key or network issues.
