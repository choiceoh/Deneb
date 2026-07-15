---
title: Amaranth ERP API map
summary: Signed internal API map for topsolar Amaranth logistics, sales, purchase, and partial accounting — same HMAC session as eap/board.
read_when:
  - Extending the groupware tool beyond 전자결재·게시판
  - Looking up logis/purchase/financial list endpoints
  - Checking which ERP menus this tenant can actually call
---

# Amaranth ERP API map (topsolar)

Investigation notes for `https://tsgw.topsolar.kr` ERP surfaces (물류·영업·구매·회계 일부).
Uses the **same** session + `wehago-sign` HMAC as 전자결재/게시판 — not Douzone partner OpenAPI.

Last surveyed: **2026-07-16** (pass 2). Status: **confirmed** (live POST + rows), **ok-empty** (SUCCESS, zero rows with captured filters), **menu-only** (screen opens; list body TBD), **no-access** (menu absent for this login).

Auth, session file, and signing: [groupware-amaranth.md](/tools/groupware-amaranth).

## Architecture

```
Chat/CLI (future area=logis|purchase|financial)
                │
        HMAC client (scripts/dev/groupware-reader/lib/client.mjs)
                │
   Micro-frontends: /modules/{financial|logis|purchase|system|bp}/
                │
   Signed POST /{micro}/{screen}/{op}
```

Shell SPA lazy-loads module bundles under `/modules/<micro>/`. Hash routes:

```text
#/{MODULE}/{PAGE}/{SCREEN}
#/BL/BLF0050/BLF0050   → 출고현황
#/PO/POM0010/POM0010   → 현재고현황
#/A/ACA2010/ACA2010    → 지출결의
```

API path convention (confirmed):

```text
/{microModule}/{screenLower}/{op}
  microModule = financial | logis | purchase | system | bp | …
  screenLower = blf0050, poc0030, aca2010, …
  op          = 0lo00001 | 0pu00002 | getList | SYB0010_selectTradeList | …
```

## Top modules (this tenant)

`POST /gw/gw999A01` → user-authorized top modules (15). ERP-relevant:

| Name | `menuNo` | `microModuleCode` | `menuCode` | Notes |
|------|----------|-------------------|------------|--------|
| 회계관리 | `409000000` | `financial` | `A` | Expense / car / project — **not** full GL/FS |
| 물류공통관리 | `420000000` | `logis` | `BS` | Masters, unit price, settings |
| 영업관리 | `430000000` | `logis` | `BL` | Quote → order → ship → **매출마감** → collect |
| 구매/자재관리 | `440000000` | `purchase` | `PO` | PO → receive → stock → pay |

Also present (non-ERP for this map): PORTAL, 전자결재(`eap`), 메일, 일정, 자원, 게시판, 업무관리, ONEFFICE, ONECHAMBER, 프로세스관리, 임직원업무관리.

Children: `POST /gw/gw999A03` with `{ "upperMenuNo": "<menuNo>" }`.

### Permission rule

- No menu in `gw999A01` / tree → treat as **no-access** (API may 404 or empty; do not rely on it).
- Empty `resultData: []` with `resultCode: 0` usually means **missing date/org filters**, not missing license.
- This login **does** open logis/purchase/sales lists with live rows when filters are set.
- Formal **재무제표·원장** menus are **not** in the accounting tree here → **no-access**.
- **매출** for ops is under 영업 `매출마감` (`logis`), not accounting FS.

## Confirmed list APIs — 영업 / 물류 (`logis`)

| Screen (KR) | Hash page | Endpoint | Primary date / filter keys | Status |
|-------------|-----------|----------|----------------------------|--------|
| 판매계획현황 | `BLA0020` | `POST /logis/bla0020/0lo00001` | `pYr`, `pMm` | ok-empty |
| 견적현황 | `BLB0040` | `POST /logis/blb0040/0lo00001` | `estDtFrom`, `estDtTo` | ok-empty |
| 주문현황 | `BLC0030` | `POST /logis/blc0030/0lo00001` | `from`, `to` | confirmed |
| 출고등록(list-ish) | `BLF0010` | `POST /logis/blf0010/0lo00001` | | ok-empty |
| 출고현황 | `BLF0050` | `POST /logis/blf0050/0lo00001` | `isuDtFrom`, `isuDtTo` | confirmed |
| 매출마감현황 | `BLG0070` | `POST /logis/blg0070/0lo00001` | `clsDtFr`, `clsDtTo` | confirmed |
| 수금현황 | `BLH0040` | `POST /logis/blh0040/0lo00001` | `rcpDtFr`, `rcpDtTo` | ok-empty |
| 미수채권상세 | `BLL0030` | `POST /logis/bll0030/0lo00001` | `fromDt`, `toDt` | confirmed |
| 미수채권 (footer/agg) | `BLL0030` | `POST /logis/bll0030/0lo00002` | same as above | confirmed |
| 품목단가등록 | `BSB0010` | `POST /logis/bsb0010/0lo00001` | item filters / paging | confirmed |

### 매출마감 amount fields

Row amounts (prefer supply excl. VAT for Deneb wiki style):

| Field | Meaning |
|-------|---------|
| `clsgAm` | 공급가액 (use this) |
| `clsvAm` | 부가세 |
| `clshAm` | 합계 (VAT 포함) |
| `exchAm` | 외화/환산 금액 |
| `clsQt` | 수량 |
| `clsDt` / `clsYm` | 마감일 / 마감월 |
| `vatFgDocNm` | e.g. 과세매출 |

List body also carries org/item filters: `divCds`, `deptCds`, `empCds`, `itemCds`, `trCds`, `pjtCds`, `whCds`, …

## Confirmed list APIs — 구매 / 자재 (`purchase`)

| Screen (KR) | Hash page | Endpoint | Primary filters | Status |
|-------------|-----------|----------|-----------------|--------|
| 발주현황 | `POC0030` | `POST /purchase/poc0030/0pu00001` | `poDtFr`, `poDtTo` | confirmed |
| 발주현황 (alt shape) | `POC0030` | `POST /purchase/poc0030/0pu00002` | | confirmed (`data` + `columns` + `menuDesc`) |
| 발주미납현황 | `POC0040` | `POST /purchase/poc0040/0pu00001` | `baseDt`, `dueDtFr`, `dueDtTo` | ok-empty |
| 입고현황 | `POF0020` | `POST /purchase/pof0020/0pu00002` | `rcvDtFrom`, `rcvDtTo` | confirmed |
| 매입마감현황 | `POG0050` | `POST /purchase/pog0050/0pu00001` | `clsDtFr`, `clsDtTo` | ok-empty |
| 지급현황 | `POH0040` | `POST /purchase/poh0040/0pu00001` | `payDtFr`, `payDtTo` | ok-empty |
| 현재고현황 | `POM0010` | `POST /purchase/pom0010/0pu00000` | `yyyy`, `whCds`, `searchType`, … | confirmed |
| 결재연동 helper | — | `POST /purchase/common/approval/0lo00013` | `moduleCds` | confirmed |

### Useful stock / receive fields

| Field | Meaning |
|-------|---------|
| `jegoQt` / `gayongQt` | 현재고 / 가용 |
| `rcvQt`, `rcvgAm`, `rcvhAm` | 입고수량·공급가·합계 |
| `itemCd`, `itemNm`, `itemgrpNm` | 품목 |
| `trCd`, `trNm` | 거래처 |
| `whNm`, `lcNm` | 창고 / 장소 |

## Confirmed / partial — 회계 (`financial`)

Tenant accounting L2 (from menu tree): 프로세스갤러리, **지출결의/경비관리**, 업무용승용차관리, 회계기초정보(프로젝트).

| Screen | Hash | Endpoint | Status |
|--------|------|----------|--------|
| 지출결의 | `ACA2010` | `POST /financial/aca2010/getList` | route confirmed; **list filter body still TBD** (SUCCESS, often empty) |
| | | `POST /financial/aca2010/getAperDivEmpInfo` | confirmed |
| | | `POST /financial/aca2010/getSmenuIni` | confirmed |
| 경비전표 | `ACA3010` | — | hash did not land in pass 2 (`#/` fallback) — **menu-only** |
| 프로젝트관리 | `ACN4000` | — | screen opens; business list POST not captured yet — **menu-only** |
| Common | — | `POST /financial/common/getSysCfg` | confirmed |
| | | `POST /financial/docuCommon/getTaxJounalList` | ok-empty |
| | | `POST /financial/docuCommon/getIsNonprofit` | confirmed |
| OneAI expense | — | `POST /financial/oneAi/expenseItems/selectExpenseItems` | FAIL without proper body |

**No-access (this login):** 재무제표, 손익/재무상태표, 총계정원장 등 정식 회계보고 메뉴 — not in `gw999A03` under `409000000`.

## Masters / system helpers

Often loaded beside ERP grids:

| Endpoint | Role |
|----------|------|
| `POST /system/orbit/getMenuOptions` | Screen options |
| `POST /system/orbit/getGridSettingList` | Grid layout (`empSeq`, `menuCode`, `gridId`) |
| `POST /system/orbit/getRelativeProcess` | Process gallery link |
| `POST /system/productionapi/0sy00002` | Control codes |
| `POST /system/syb0020/getConfirmSearchType` | Search UI |
| `POST /system/syb0010/SYB0010_selectTradeList` | 거래처 list (needs full body) |
| `POST /logis/logisCommon/getCompanyInfo` | Company profile |
| `POST /logis/logisCommon/selectGisu` | 기수 |

Micro frontend assets (debug only): `/modules/financial`, `/modules/logis`, `/modules/purchase`, `/modules/system`, `/modules/bp` (+ each `asset-manifest.json`).

## Still open (pass 2+)

| Gap | Notes |
|-----|-------|
| `aca2010/getList` request body | Screen loads helpers only; 조회 click did not emit list POST with dates |
| `ACA3010` 경비전표 | Direct hash bounced to `#/` |
| `ACN4000` 프로젝트 list API | Need leaf navigation + network capture |
| `BSB0020` 거래처단가 | Hash opened; no business POST captured |
| Wider date windows for ok-empty screens | 수금/견적/판매계획/지급/매입마감/발주미납 may have rows outside SPA default range |
| Wire into chat `groupware` tool | Still eap/board only |

## Discovery cheatsheet

```bash
set -a && source ~/.deneb/groupware.env && set +a
cd scripts/dev/groupware-reader
```

Probe from Node (same HMAC client as eap/board):

1. Top modules — `POST /gw/gw999A01` `{}` → `name`, `menuNo`, `microModuleCode`
2. Children — `POST /gw/gw999A03` `{ "upperMenuNo": "430000000" }` (영업 등)
3. List — e.g. `POST /logis/blg0070/0lo00001` with `clsDtFr` / `clsDtTo` (`YYYYMMDD`); sum `clsgAm` only in local logs — never commit row PII

Playwright: login → open `#/BL/BLG0070/BLG0070` (etc.) → capture POST bodies under `/logis`, `/purchase`, `/financial` (date keys differ per screen).

## Wiring note (Deneb)

| Today | ERP |
|-------|-----|
| Chat tool `groupware` areas | approval, board, sales, stock, po, receive, ship, price |
| Reader adapters | `summarySales`, `listStock`, `listPurchaseOrders`, `listReceiving`, `listShipments`, `listItemPrices` |
| Still unwired | 미수채권, 매입마감, 수금, 지출결의 list body, … |

Usage examples:

- sales: `groupware(area="sales", action="summary", folder="ytd")`
- stock: `groupware(area="stock", action="list", query="모듈")`
- po / receive / ship: `folder=month|ytd`, optional `query` keyword
- price: `groupware(area="price", action="list", query="인버터")`

Amounts use supply fields where available (`clsgAm` / `rcvgAm` / `pohAm` / `isugAm`). Keep mutate off the chat tool.

## Explicit non-goals

- Partner OpenAPI / `@lumir-company/amaranth-sdk`
- Writing vouchers, stock adjusts, approve from chat
- Documenting live amounts, customer names, or tokens in git
- Claiming 재무제표 access for this tenant

Never commit credentials, session JSON, or live tokens.
