---
title: SolarFlow ERP analytics tool
summary: Read-only solarflow chat tool over the SolarFlow engine (margin, LC, forecast, turnover, NL search).
read_when:
  - Extending or debugging the solarflow chat tool
  - Adding a SolarFlow /api/calc/* endpoint to the tool
  - Understanding how Deneb queries SolarFlow vs the raw Amaranth ledger
---

# SolarFlow ERP analytics tool

Read-only chat tool `solarflow` that queries the topsolar **SolarFlow** engine
(`choiceoh/solarflow`, a separate Rust/axum ERP) over its `/api/calc/*` REST <!-- docref:ignore -->
surface. SolarFlow ingests the same Amaranth ledger the
[groupware](/tools/groupware-amaranth) tool reads, but exposes **derived**
intelligence Amaranth does not: margin, outstanding receivables, LC
maturity/limit timelines, supply forecast, receipt matching, inventory turnover,
landed cost, and a Korean natural-language search.

**groupware = raw ledger, solarflow = computed analytics.** Both read-only.

## Topology

On the gateway host the SolarFlow stack runs as local docker containers; the
analytics engine answers on `127.0.0.1:8081` with no auth (protected by the host
and tunnel, not by the API). Deneb calls it directly.

```text
Chat tool solarflow(action, query)
        │  HTTP POST (net/http)
   internal/platform/solarflow  ->  http://127.0.0.1:8081/api/calc/<endpoint>
```

## Env

| Var | Default | Role |
|-----|---------|------|
| `DENEB_SOLARFLOW_URL` | `http://127.0.0.1:8081` | engine root |
| `DENEB_SOLARFLOW_COMPANY_ID` | none | default company uuid (탑솔라 on this tenant) |
| `DENEB_SOLARFLOW_TOKEN` | none | optional bearer (sent only when set) |

The company id is env-driven (tenant config, not source). SolarFlow is
multi-company; the agent may override with a `company` uuid param, resolving ids
from a `search` or `customer` result.

## Actions

Natural language is the primary path: `action=search`, `query="모듈 재고"` routes
through SolarFlow's own intent classifier and the tool renders a Korean list.
Structured actions map to engine endpoints:

| action | endpoint | notes |
|--------|----------|-------|
| `search` | `search` | NL query; `query` required |
| `inventory` | `inventory` | 재고 집계 |
| `margin` | `margin-analysis` | 마진 (summary, items, 24m trend) |
| `customer` | `customer-analysis` | 거래처 (sales, collection, outstanding, margin) |
| `outstanding` | `outstanding-list` | 미수금; needs `customer` name or `customer_id` |
| `receipt_match` | `receipt-match-suggest` | 수금 매칭; needs customer + `receipt_amount` |
| `turnover` | `inventory-turnover` | 회전율; `horizon` days (default 90) |
| `supply_forecast` | `supply-forecast` | 수급 전망; `horizon` months (default 6) |
| `order_risk` | `order-fulfillment-risk` | 수주 충당 위험도 |
| `lc_maturity` | `lc-maturity-alert` | LC 만기; `horizon` days (default 7) |
| `lc_limit` | `lc-limit-timeline` | 한도 복원; `horizon` months (default 6) |
| `lc_fee` | `lc-fee` | LC 수수료 |
| `landed_cost` | `landed-cost` | 수입 원가 |
| `exchange_compare` | `exchange-compare` | 환율 환산 비교 |
| `price_trend` | `price-trend` | 단가 추이; `period` quarterly or monthly |

`price-forecast-strategy` is intentionally not wired: it is a market-observation
calculator, not an ERP query, and takes inputs the agent does not have on hand.

`horizon` is one int mapped per action (days_ahead, months_ahead, or days). The
customer-name to uuid resolution for `outstanding` and `receipt_match` goes
through `customer-analysis` (exact match wins; a single substring match falls
through; otherwise a disambiguation list).

## Output

- `search` renders a compact Korean list (title, key data, linked `*_id`).
- Every other action returns a Korean header line plus the JSON payload with long
  arrays capped to `limit` (default 20, max 50) so summaries and totals stay
  intact while row lists stay bounded. The agent synthesizes numbers for the user.

## Code map

| Path | Role |
|------|------|
| `gateway-go/internal/platform/solarflow/solarflow.go` | Config, FromEnv, StatusLine, Run, dispatch, customer resolution |
| `gateway-go/internal/platform/solarflow/format.go` | search and generic (array-capping) formatters |
| `gateway-go/internal/pipeline/chat/tools/hostops/solarflow.go` | chat tool `ToolSolarflow` and action aliases |

Schema source is `gateway-go/internal/pipeline/chat/toolwire/schema/tool_schemas.json`
(`make tool-schemas`); registration is in
`gateway-go/internal/pipeline/chat/toolwire/core/register.go`.

## Non-goals

- No writes: no vouchers, adjustments, or approvals through this tool.
- No `price-forecast-strategy` (market calculator, not an ERP read).
- Do not commit company ids, customer names, or amounts to git.

## Smoke

```bash
# hermetic unit tests
cd gateway-go && go test ./internal/platform/solarflow/...
# live integration against the real engine (skipped without the flag)
DENEB_SOLARFLOW_LIVE=1 go test ./internal/platform/solarflow/ -run TestLiveEngine -v
```
