---
title: Amaranth ERP API map
summary: Signed internal API map for topsolar Amaranth logistics, sales, purchase, partial accounting, and 임직원(personal/human) HR — same HMAC session as eap/board.
read_when:
  - Extending the groupware tool beyond 전자결재·게시판
  - Looking up logis/purchase/financial/personal/human list endpoints
  - Checking which ERP menus this tenant can actually call
  - Extending groupware people / leave / attendance reads
---

# Amaranth ERP API map (topsolar)

Investigation notes for `https://tsgw.topsolar.kr` ERP + 임직원업무관리 surfaces (물류·영업·구매·회계 일부·인사/근태/연차).
Uses the **same** session + `wehago-sign` HMAC as 전자결재/게시판 — not Douzone partner OpenAPI.

Last surveyed: **2026-07-16** (pass 3 — 임직원/HR inventory). Status: **confirmed** (live POST + rows), **ok-empty** (SUCCESS, zero rows with captured filters), **menu-only** (screen opens; list body TBD), **no-access** (menu absent for this login).

Auth, session file, and signing: [groupware-amaranth.md](/tools/groupware-amaranth).

## Architecture

```
Chat tool groupware(area=sales|stock|po|receive|ship|price|…)
                │
        HMAC client (scripts/dev/groupware-reader/lib/client.mjs)
                │
   Micro-frontends: /modules/{financial|logis|purchase|system|bp|personal|human}/
                │
   Signed POST /{micro}/{screen}/{op}
```

Shell SPA lazy-loads module bundles under `/modules/<micro>/`. Hash routes:

```text
#/{MODULE}/{PAGE}/{SCREEN}
#/BL/BLF0050/BLF0050   → 출고현황
#/PO/POM0010/POM0010   → 현재고현황
#/A/ACA2010/ACA2010    → 지출결의
#/HP/HPH0120/HPH0120 → 인사정보조회
```

API path convention (confirmed):

```text
/{microModule}/{screenLower}/{op}
  microModule = financial | logis | purchase | system | bp | personal | human | …
  screenLower = blf0050, poc0030, aca2010, hph0120, hpd0550, hrd0570, …
  op          = 0lo00001 | 0pu00002 | 0hp00001 | 0hr00001 | getList | …
```

## Top modules (this tenant)

`POST /gw/gw999A01` → user-authorized top modules (15). ERP-relevant:

| Name | `menuNo` | `microModuleCode` | `menuCode` | Notes |
|------|----------|-------------------|------------|--------|
| 회계관리 | `409000000` | `financial` | `A` | Expense / car / project — **not** full GL/FS |
| 물류공통관리 | `420000000` | `logis` | `BS` | Masters, unit price, settings |
| 영업관리 | `430000000` | `logis` | `BL` | Quote → order → ship → **매출마감** → collect |
| 구매/자재관리 | `440000000` | `purchase` | `PO` | PO → receive → stock → pay |
| 임직원업무관리 | `406000000` | `personal` (+ `human`) | `HP` | 인사·근태·연차·급여 — see HR section |

Also present: PORTAL, 전자결재(`eap`), 메일, 일정, 자원, 게시판, 업무관리, ONEFFICE, ONECHAMBER, 프로세스관리, **임직원업무관리(`personal`/`human`)** — mapped in the HR section below.

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

## 임직원 / 인사 / 조직 (`personal` · `human`) — pass 3

Top module: **임직원업무관리** `menuNo=406000000`, `microModuleCode=personal`, `menuGubun=HP`.
Admin/aggregate screens often use micro **`human`** (same HMAC session). Bundle inventory (JS scrape of `/modules/personal` + `/modules/human` asset-manifest chunks): **~770** `personal/*` paths, **~2500** `human/*` paths — only live-probed reads are listed below.

### Menu tree (leaves under `406000000`)

| Area | Example screens (menuCode) | micro |
|------|----------------------------|-------|
| 마이페이지 | 개인인사정보조회 `HPM0110`, 주소록 `HPM0310`/`HPM0410`, 업무보고 | `personal` |
| 근태관리 | 근태신청 `HPD0110`, 개인근태신청현황 `HPD0120`, 근무시간 `HPD0220`/`HPD025x`, 연차 `HPD0550`/`HPD0570` | `personal` / `human` |
| 인사관리 | 인사정보조회 `HPH0120`, 발령 `HPH0410`/`HPH0420`, 교육 `HPH0230`, 증명서 `HPH0310` | `personal` / `human` |
| 급여관리 | 급여명세서 `HPP0120`, 연말정산 `HPP0210`… | `personal` / `human` |
| 기타 | 경비청구 `APA*`, 개인지출결의 `NPA*`, 법정의무교육 `HPE0010`, 노무계약 `HPL0110` | mixed |

Children: `POST /gw/gw999A03` `{ "upperMenuNo": "406000000" }` (and recurse).

### Confirmed people / identity (wired or ready)

| Screen / role | Endpoint | Body / notes | Status |
|---------------|----------|--------------|--------|
| 사원 피커 | `POST /personal/APCodePicker/ApAperUserCode` | `{ helpTy: "APER_USER_INFO", searchText }` → `empCd`,`korNm`,… (~283 emp). Other `helpTy` values (`APER_DEPT`, `ORG`, `DIV`, …) still return the **same emp list shape** on this tenant — **not** a dept tree. | confirmed |
| 사원 상세 | `POST /personal/hph0120/0hp00001` | `{ empCd }` → `deptNm`,`divNm`,`korHcls`,`emgcTel`,`tel`,`brthDt`,`enrlFgNm`,… **Strip `rsrgNo` / address — never surface.** | confirmed (chat `area=people`) |
| 상세 옵션 | `POST /personal/hph0120/0hp00002` | `{ empCd }` → `visibleList`, `payinfoOption` | confirmed |
| 내 인사카드 | `POST /personal/hpm0110/selectEmpInfo` | `{}` or `{ empCd }` → `UpperInfo` / `Summary` | confirmed |
| 내 인사 상세 | `POST /personal/hpm0110/selectEmpDetail` | needs fuller body | SERVER ERROR (−1) |
| 표시 필드 | `POST /personal/hpm0110/getVisibleList` | `{}` | confirmed (HTTP 200 array) |

Chat wiring: `groupware` `area=people` → reader `listPeople` (+ `DENEB_PEOPLE_JSON` → wiki enrich + `org.Load` name match). **Amaranth has no separate org-chart POST found**; org affiliation for agents is still Deneb `org.json` + person `deptNm`/`divNm`. <!-- docref:ignore -->

### Confirmed leave / attendance (read)

| Screen | Endpoint | Body | Status / keys |
|--------|----------|------|----------------|
| 개인연차현황 | `POST /personal/hpd0550/0hp00001` | date range (`fromDt`/`toDt`) | confirmed — year list (`ycYy`) |
| 개인연차현황 | `POST /personal/hpd0550/0hp00002` | date range | confirmed — balances: `basicDy`,`useDy`,`addDy`,`deptNm`,`hclsNm`,… |
| 개인연차현황 | `POST /personal/hpd0550/0hp00003` / `0hp00004` | `{ yyyy }` | ok-empty (needs richer body) |
| 월별연차사용 | `POST /personal/hpd0560/0hp00007` | date range | ok-empty |
| 개인근태신청현황 | `POST /personal/hpd0120/0hp00001` | date range | confirmed — `totalCount`, `atPopUpCountInfos`, `atPopUpDetailInfos` |
| 연차현황 (관리) | `POST /human/hrd0570/0hr00001` | `{ yyyy }` | confirmed — **company-wide** rows (`divNm`,`deptNm`,`basicDy`,…) |
| 근태코드 | `POST /human/common/attend/getAttendCodeList` | `{}` | confirmed (~94 codes: `atCd`,`atNm`,…) |
| 금일/기간 출퇴근 | `POST /human/hrd0250/0hr00001` | requires `workDtFrom`+`workDtTo` | body shape known; query still FAIL (−97) without full filters |
| | `POST /human/hrd0250/getEmployeeList` | same dates | ok-empty with dates only |
| 개인근무시간 | `POST /personal/hpd0220/0hr00001`… | date range | FAIL (−1) — body TBD |

### Other live HR probes

| Endpoint | Status | Notes |
|----------|--------|-------|
| `POST /personal/hph0310/getIssuFgList` | confirmed | 증명서 발급구분 (`issuFg`,`issuNm`) |
| `POST /personal/hpp0120/0hp00000` | confirmed | payslip meta (`fileName`,`langFg`) — treat as sensitive |
| `POST /personal/hpp0120/0hp00001` | ok-empty | payslip list needs period/body |
| `POST /personal/hpp0130/getUserBirthday` | ok-empty | birthday helper |
| `POST /personal/APCodePicker/ApGroupCode` | ok-empty | |
| `POST /human/hrd0510/getTreeList` | confirmed | **not org chart** — attend/config code tree (`clasCd`,`ctrNm`,…) |
| `POST /human/common/annualleave/getAnnualLeaveInfoOfEmployee` | needs `coCd` | |
| `POST /human/codepickers/CP0001A0001` | 500 | body TBD |

### Bundle inventory highlights (not all live-probed)

Useful screen prefixes seen in JS (read vs mutate mixed — **do not wire writes** into chat):

- **People / HR master (`human/hrh*`):** `hrh0110`…`hrh0950` (personnel master, career, certificates, org-side HR admin)
- **Attendance admin (`human/hrd*`):** `hrd0110`…`hrd1970` (config, approval, aggregates, today-board)
- **Payroll (`human/hrp*` / `personal/hpp*`):** large surface — **sensitive**; prefer explicit operator ask
- **Personal apps (`personal/hpd*`, `hph*`, `hpm*`):** employee self-service mirrors of above
- **Pickers:** `/personal/APCodePicker/*`, `/human/codepickers/CP0001A0001`, `/human/common/annualleave/*`, `/human/common/attend*`

Assets: `/modules/personal/asset-manifest.json`, `/modules/human/asset-manifest.json`.

### Explicit HR gaps

| Gap | Notes |
|-----|-------|
| **조직도 / 부서 트리 API** | No dedicated dept/org-tree POST found. `ApAperUserCode` helpTy aliases ≠ dept list. Affiliation = person detail `divNm`/`deptNm` + Deneb `org.json`. <!-- docref:ignore --> |
| 주소록 `HPM0310` list body | Menu present; `/personal/hpm0310/0hp00001` 404 — real op names still in lazy chunks / need Playwright capture |
| `hpd0220` / `hrd0250` full filter body | Date keys alone insufficient |
| Wire leave/attendance into chat | Not yet — candidates: personal leave `hpd0550`, company leave `hrd0570` (permission-sensitive) |

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

Micro frontend assets (debug only): `/modules/financial`, `/modules/logis`, `/modules/purchase`, `/modules/system`, `/modules/bp`, `/modules/personal`, `/modules/human` (+ each `asset-manifest.json`). <!-- docref:ignore -->

## Still open (pass 3+)

| Gap | Notes |
|-----|-------|
| `aca2010/getList` request body | Screen loads helpers only; 조회 click did not emit list POST with dates <!-- docref:ignore --> |
| `ACA3010` 경비전표 | Direct hash bounced to `#/` |
| `ACN4000` 프로젝트 list API | Need leaf navigation + network capture |
| `BSB0020` 거래처단가 | Hash opened; no business POST captured |
| Wider date windows for ok-empty screens | 수금/견적/판매계획/지급/매입마감/발주미납 may have rows outside SPA default range |
| HR org/dept tree | See 임직원 section — no Amaranth org-chart POST yet |
| HR address-book / attend board bodies | Playwright capture on `HPM0310`, `HPD0250`/`HRD0250` |
| Wire more HR into chat | people done; leave/attend/org still open |

## Discovery cheatsheet

```bash
set -a && source ~/.deneb/groupware.env && set +a
cd scripts/dev/groupware-reader
```

Probe from Node (same HMAC client as eap/board):

1. Top modules — `POST /gw/gw999A01` `{}` → `name`, `menuNo`, `microModuleCode`
2. Children — `POST /gw/gw999A03` `{ "upperMenuNo": "430000000" }` (영업) or `"406000000"` (임직원)
3. HR bundle scrape — GET `/modules/personal/asset-manifest.json` (+ `human`) → download JS chunks → grep `/personal/` `/human/`
4. List — e.g. `POST /logis/blg0070/0lo00001` with `clsDtFr` / `clsDtTo` (`YYYYMMDD`); sum `clsgAm` only in local logs — never commit row PII

Playwright: login → open `#/BL/BLG0070/BLG0070` (etc.) → capture POST bodies under `/logis`, `/purchase`, `/financial` (date keys differ per screen).

## Field-level facts confirmed live (2026-07-16)

Verified against full-history reads (2020→) while porting this contract into
`choiceoh/solarflow` (`rpa/amaranth-reader`). These are payload facts, not <!-- docref:ignore -->
guesses — reuse instead of re-probing:

| Screen | Confirmed facts |
|--------|-----------------|
| 출고 `blf0050` | Line seq is **`isuSq`** (not `isuSeq`). Amounts `isuUm`/`isugAm`/`isuvAm`/`isuhAm`. `isureqNb` (출고요청번호) filled ~99% — the only path to 현장명 for module wholesale rows (`pjtNm` is filled only on 유지관리/공사 rows; module rows carry customer `trNm`/`trAddr` only). ~10.6k rows 2020→. |
| 매출마감 `blg0070` | **`isuNb`+`isuSq` join each closing line to its 출고 line** (`clsNb` is the closing number — never matches 출고번호). Line seq `clsSq`. **Partial closings are normal** (one 출고 → many closings). `trNm` often empty (only `trCd`) — resolve customer via the joined 출고. Rows carry **cost/profit columns** (Amaranth's own realized margin — treat as ground truth). ~7.7k rows 2023→. |
| 입고 `pof0020` | Line seq **`rcvSq`**; unit price **`rcvUm`** (not `unitAm`); PO link `poNb`; location `lcNm`; remarks `dRemarkDc`/`hRemarkDc`. ~1.7k rows 2020→. |
| 현재고 `pom0010` | Also carries `iopenQt`/`ircvQt`/`iisuQt` (기초/입고누계/출고누계) beside `jegoQt`/`gayongQt`. Quantities only — 금액/재고단가 lives in the 수불부 screen (uncaptured). |
| 미수채권 `bll0030` | Works as per-customer as-of balance: `cardCd`/`cardNm`/`arAmRest` (기준일=`toDt`). |
| 발주 `poc0030` / 매입마감 `pog0050` | Both return live rows over full range (45 / 591) with the documented bodies plus the paging block. |
| 창고 코드 | `A200`/`A400`/`F100` match SolarFlow warehouse codes 1:1; `J100`/`J200`/`J300` are business-type virtual warehouses (공사매출/유지관리/임대매출), not physical. `divNm` is 100% 탑솔라(주) on this login. |

Still uncaptured (SPA-body capture via Playwright request hook needed): 주문
`blc0030`, 수금 `blh0040`, 지급 `poh0040`, 재고수불부(금액판), 출고요청 상세
(현장명 소스).

## Wiring note (Deneb)

| Today | ERP |
|-------|-----|
| Chat tool `groupware` areas | approval, board, sales, stock, po, receive, ship, price, people |
| Reader adapters | `summarySales`, `listStock`, `listPurchaseOrders`, `listReceiving`, `listShipments`, `listItemPrices`, `listPeople` |
| Still unwired | 미수채권, 매입마감, 수금, 지출결의 list body, 연차/근태/주소록, … |

Usage examples:

- sales: `groupware(area="sales", action="summary", folder="ytd")`
- stock: `groupware(area="stock", action="list", query="모듈")`
- po / receive / ship: `folder=month|ytd`, optional `query` keyword
- price: `groupware(area="price", action="list", query="인버터")` (→ itemCd `I-*`)
- people: `groupware(area="people", action="list", query="김")` → 이름·부서·직급/호칭·휴대폰·생년월일 + 위키 인물 보강·생성·조직도 읽기 매칭
- raw lines: `query="lines:모듈"` or `query="라인: YYYYMMDD:YYYYMMDD"`

Defaults: list output is **품목 집계**; po/receive/ship also append 거래처 상위.
Amounts use supply fields (`clsgAm` / `rcvgAm` / `pohAm` / `isugAm`). Keep mutate off the chat tool.

### Wired chat areas (amount / qty fields)

| Area | Endpoint | Prefer | Notes |
|------|----------|--------|-------|
| sales | `/logis/blg0070/0lo00001` | `clsgAm` | summary totals + top lines |
| stock | `/purchase/pom0010/0pu00000` | `jegoQt` / `gayongQt` | `searchType=coCd`; **품목코드 집계**(창고 합산) |
| po | `/purchase/poc0030/0pu00001` | `pohAm` | default folder=`ytd`; 품목 집계 |
| receive | `/purchase/pof0020/0pu00002` | `rcvgAm` | default `month`; 품목 집계 |
| ship | `/logis/blf0050/0lo00001` | `isugAm` | default `month`; 품목 집계 |
| price | `/logis/bsb0010/0lo00001` | `purchUm`/`stdUm`/`staUm` | no period; prefer itemCd filter |
| people | `/personal/APCodePicker/ApAperUserCode` + `/personal/hph0120/0hp00001` | `korNm`/`deptNm`/`korHcls`/`emgcTel`/`brthDt` | strips `rsrgNo`; query=name required; wiki 인물 upsert/create + org.json name match (no org write) |

## Explicit non-goals

- Partner OpenAPI / `@lumir-company/amaranth-sdk`
- Writing vouchers, stock adjusts, approve from chat
- Documenting live amounts, customer names, or tokens in git
- Claiming 재무제표 access for this tenant

Never commit credentials, session JSON, or live tokens.
