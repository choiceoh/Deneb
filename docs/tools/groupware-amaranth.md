---
title: Amaranth10 groupware APIs
summary: Internal signed HTTP surface for topsolar Amaranth (전자결재 · 게시판 · ERP map link), session auth, and Deneb wiring notes.
read_when:
  - Extending the groupware tool or phone-event approval enrich
  - Mapping new Amaranth eap/board endpoints
  - Designing work-feed approve/reject chips
---

# Amaranth10 groupware APIs (topsolar)

Investigation notes for Deneb’s srv4 reader (`scripts/dev/groupware-reader/`) against
`https://tsgw.topsolar.kr`. Prefer these **signed internal APIs** over DOM scrape;
Playwright is login / session refresh only.

Partner OpenAPI (`@lumir-company/amaranth-sdk` / api99u02\*) needs Douzone partner
creds and is **not** what topsolar uses today.

Last surveyed: 2026-07-15. Status legend: **confirmed** (live POST), **list-only**,
**candidate** (JS static / not mutated), **TBD**.

## Architecture (Deneb)

```
Consumers: chat tool `groupware` · phone enrich · CLI read.mjs
                │
        Go thin bridge (platform/groupware.Run)
                │
   Domain adapters (lib/actions.mjs) — approval + board
                │
   Signed client (HMAC wehago-sign) + session cache
                │
   Playwright login only on miss / 401
```

Product split (planned):

| Concern | Surface |
|---------|---------|
| Read (list/body/line + attachment titles; selected attachment on demand) | Deferred tool `groupware` + phone enrich |
| Write (승인/반려) | Work-feed chips → `miniapp.workfeed.action.run` — **not** the chat tool |

See also: [page-agent-browser.md](./page-agent-browser.md) (tool env + phone path) ·
[groupware-erp-api-map.md](./groupware-erp-api-map.md) (물류·영업·구매·회계 일부 API 지도, 2026-07-16).

## Env & session (not in git)

| Item | Location |
|------|----------|
| Creds | `~/.deneb/groupware.env` → systemd `EnvironmentFile` on srv4 |
| Vars | `DENEB_GROUPWARE_URL`, `COMPANY`, `USER`, `PASSWORD` |
| Session cache | `~/.deneb/groupware-session.json` (mode `0600`) |

Session fields used by the client: `url`, `token` (`auth_a_token`), `hashKey`
(`hash_key`), plus emp metadata from login. Cache TTL ~12h; refresh on 401 /
auth-ish result codes (`136`, `601`, Korean auth messages).

### HMAC request signing

For each POST to pathname `P`:

1. `transaction-id` = 32 hex random
2. `timestamp` = unix seconds (string)
3. `wehago-sign` = Base64( HMAC-SHA256( `token + transaction-id + timestamp + P`, `hashKey` ) )
4. Headers: `Authorization: Bearer {token}`, `timestamp`, `transaction-id`,
   `wehago-sign`, `Content-Type: application/json;charset=UTF-8`

Warm list/read is typically **~60–100ms** with a warm session (vs tens of seconds for
browser scrape).

## Menu / box codes (전자결재)

From `/gw/gw999A03` under upper menu `1000900` (전자결재):

| Folder (Deneb) | Korean | `menuNo` / box |
|----------------|--------|----------------|
| `pending` | 미결문서 | `1001000` |
| `done` | 기결문서 | `1001100` |
| `cc` | 수신참조문서 | `1001200` |
| `total` | 전체결재문서 | `1001500` |

Runtime uses these constants (`lib/actions.mjs` `BOX`); do not depend on the menu
tree API at request time.

## Confirmed APIs — 전자결재

### List (미결 · 기결 · 수신참조) — **confirmed**

```
POST /eap/eap106A03
{ "boxList": "1001000,", "listCount": "20" }
```

- `boxList`: `{menuNo},` (trailing comma matches SPA)
- Response: `resultData.EaPortletDocList[]` — titles, doc no, drafter, dates,
  `DOC_ID` / `doc_id`

### List (전체결재문서) — **confirmed**

```
POST /eap/eap126A04
{
  "boxCodes": ["10","20","30","40","50","60"],
  "pageCode": "UBA",
  "upperMenuNo": "1000900",
  "menuNo": "1001500"
}
```

- Response: `resultData.docList[]`

### Document body — **confirmed**

```
POST /eap/eap111A23
{ "docId": "99178" }
```

- Response: `resultData.doc_contents` (HTML), `doc_title`, `doc_no`, `form_nm`,
  drafter fields, etc.
- Deneb converts data-shaped HTML tables to **GFM Markdown tables**, expanding
  `rowspan` / `colspan` into rectangular blank cells. One-row layout tables (금액
  label/value shards) stay readable prose instead of fake tables. The existing
  native `MarkdownContent` and Andromeda `AssistantText` renderers already parse
  and draw GFM tables — no groupware-specific UI renderer.
- Non-table HTML collapses to text via `htmlToText` (cap ~16k).

### Approval line — **confirmed**

```
POST /eap/eap126A05
{ "docId": "99178" }
```

- Response: array of line users / roles / status.
- Observed: `act_id` `3000` ≈ 결재; `app_sts` / line status `30` ≈ 승인 (display
  names vary: 승인 · 진행 · 예결).

### Attachment list — **confirmed**

```
POST /eap/eap110A90
{ "docId": "99178" }
```

- Response `resultData.list[]`: `fileId`, `dispFileNm`, `fileNm`, `fileSize`,
  `fileExtsn`, `fileKey`, `filePath`, `fileSeq`
- Example on doc `99178`: `1. 지출영수증.jpg` (`fileKey` 196658, ~2.6MB)

### Attachment download — **confirmed**

ECM module (same HMAC). `fileSn` must be a **scalar** (or use `fileSnList` for
multi); a JSON array in `fileSn` fails.

Optional meta enrich:

```
POST /ecm/ecm001A04
{
  "moduleGbn": "EAP",
  "authKeyMap": "{\"compSeq\":\"1000\",\"empSeq\":\"2226\",\"docId\":\"99178\",\"migYn\":\"0\"}",
  "fileSn": "196658",
  "condition": "99"
}
```

Binary download (verified octet-stream + Content-Disposition):

```
POST /ecm/ecm001A03
{
  "moduleGbn": "EAP",
  "authKeyMap": "{\"compSeq\":\"…\",\"empSeq\":\"…\",\"docId\":\"…\",\"migYn\":\"0\"}",
  "fileSn": 196658
}
```

- `authKeyMap` values come from session (`compSeq`, `empSeq`) + `docId`
- No server-side text API for JPG/PDF — download to temp, then extract:
  text-layer PDF via `pdftotext`; scanned PDF/image via **PaddleOCR-VL**
  (fleet `DENEB_OCR_VL_URL`, ~1s/page) with `tesseract` fallback. Scanned PDFs
  are rasterized (`pdftoppm`, first 2 pages).
- Preview-only: `POST /ecm/ecm001A07` (Synap viewer path; not for extraction)
- Legacy `/ecm/ecmapi/ecm001A03.do` → 404 on this tenant

Deneb default: `read` lists attachment titles/size only (**no download**). The agent inspects those titles and calls `groupware(action="attachment", doc_id="…", attachment="number or filename")` for exactly one file only when its content is needed. Selected PDF/image extraction uses PaddleOCR-VL → tesseract fallback.

## Confirmed APIs — 게시판

### Recent notices — **confirmed** (implemented)

```
POST /board/APIHandler/getNewNoticeListForPortlet
{ "page": "1", "pageSize": "20" }
```

- Response: `resultData.articleList[]` — `art_title`, `art_seq_no`, `cat_seq_no`,
  `mbr_nick`, `write_date`

### Article body — **confirmed** (not yet wired in reader)

```
POST /board/APIHandler/ViewPost
{ "art_seq_no": 17106 }
```

Optional SPA fields: `adminPage` (`N`), `externalYn`, `menuCode`, `pageCode`,
`moduleCode`.

| Field | Meaning |
|-------|---------|
| `resultData.art.art_content` | Body HTML |
| `resultData.art.contents_text` | Plain summary |
| `resultData.art.sub_content` | Often empty |
| `resultData.view_perm_yn` | `"Y"` when readable |

### Not the body — **confirmed empty for main content**

```
POST /board/APIHandler/ViewPostSubContent
{ "art_seq_no": 17106 }
→ sub_content: ""   // auxiliary; do not treat as main article
```

`readBoard` in `lib/actions.mjs` still falls back to meta + SubContent; wire
`ViewPost` → `art_content` / `contents_text` next (P2).

## Approve / reject — **high confidence, untested mutate** (do not call)

Same signed `/eap/*` + `wehago-sign` as reads. Primary mutate (JS
`fnApproval` → ajax):

```
POST /eap/eap110A21
```

| Field | Role |
|-------|------|
| `docID` | Document id |
| `actID` | Line action type (e.g. `3000` = 결재) |
| `docLineMSeq` / `docLineSSeq` | Line identity (from `eap126A05` / popup) |
| `docLineSts` | **Action code** (not the read-side `app_sts` meaning) |
| `docComment` | Opinion (often `""` in SPA; may need prior comment API) |
| `iframeHtml` | Often `""` |

**`docLineSts` action codes** (button → `fnApproval(code)`):

| code | Meaning |
|------|---------|
| `30` | 승인 |
| `40` | 전결 |
| `50` | 반려 |
| `90` | 보류 |
| `100` | 거부 |
| `888` / `999` / `1000` | 발신반려 / 수신반려 / 전체반려 |

Supporting endpoints:

| Path | Role |
|------|------|
| `/eap/eap111A20` | Popup pre-query — `{ docID, docLineSts }` → seq / flags |
| `/eap/eap110A08` | Popup options load `{ docID, formID, actID }` (read-ish) |
| `/eap/eap110A01` | Receive-confirm / special path |
| `/eap/eap110A49` | Multi-app password / capability check |
| `/eap/eap110A09` | Multi-app proc (`fnMultiAppDocApprovalProc`) |
| `/eap/eap105A04` | Multi-approval UI side |

Example **inferred** approve payload for pending line on doc `99178`
(`eap126A05`: user line `act_id=3000`, `app_sts=20` 진행, m/s seq `3`/`1`) —
**never sent in investigation**:

```json
{
  "docID": "99178",
  "actID": "3000",
  "docLineMSeq": 3,
  "docLineSSeq": 1,
  "docLineSts": "30",
  "docComment": "",
  "iframeHtml": ""
}
```

Success shape (SPA): `resultCode === 0` and `resultData.resultValue === 0`.

**Important:** on the **read** line, `app_sts=30` means “already approved”; on
**write**, `docLineSts="30"` means “perform approve”. Same digit, different
fields.

### Product safety (planned)

- Chat `groupware` tool stays **read-only**.
- Writes only via work-feed chips `approval:approve` / `approval:reject` →
  `miniapp.workfeed.action.run` + `RunActionWithEffect` (Amaranth **before**
  settle; failure keeps card).
- Confirm UI: title, doc no, amount, line, action; default-disable 전결/`1000`.
- Dry-run: verify own line `app_sts==20` via `eap126A05` before any mutate.
- Mutate requires `DENEB_GROUPWARE_ACT=1` (set only by `ActApproval` / feed path; bare CLI stays read-safe).
- Line selection targets **only** the caller's line with `app_sts=20` (진행), matched by `user_id` (not `emp_seq` — real payloads use `user_id`). Already-approved (30) / downstream (70) lines are refused.
- No retract/회수 API found in EAP JS — an approve is effectively irreversible from here; undo in the Amaranth UI.
- First live mutate only on disposable / sandbox docs.
- Audit: docID, docLineSts, actID, seqs, client, time, API result.

## Related discovery APIs

```
POST /gw/gw999A01   # top modules authorized for this login
POST /gw/gw999A03   # menu tree (e.g. upper 1000900 → box codes;
                    # ERP: 409000000 회계, 420000000 물류공통,
                    #      430000000 영업, 440000000 구매/자재)
```

Useful for discovery/debug; not required on the eap/board hot path.
ERP list endpoints and permission notes: [groupware-erp-api-map.md](./groupware-erp-api-map.md).

## Code map

| Path | Role |
|------|------|
| `scripts/dev/groupware-reader/read.mjs` | CLI |
| `scripts/dev/groupware-reader/lib/session.mjs` | Playwright login + cache |
| `scripts/dev/groupware-reader/lib/client.mjs` | HMAC `apiPost` |
| `scripts/dev/groupware-reader/lib/actions.mjs` | list/read adapters |
| `gateway-go/internal/platform/groupware/` | Go runner (`Run`, `ReadApproval`) |
| `gateway-go/.../runtimeops/groupware.go` | Deferred tool `groupware` |

## Roadmap snapshot

| Phase | Scope | Status |
|-------|--------|--------|
| P0 | Session + approval list/read body + line | Done |
| P1 | Attachment titles only on read → agent-selected single-file download/extract; PaddleOCR-VL for image·scanned PDF | Done |
| P2 | Board `ViewPost` on `read` | Done |
| P3 | Approve/reject (`eap110A21`) + feed chips (native/Andromeda) | Wired; **live mutate untested** |
| P4 | Phone enrich API-first; DOM/Page Agent last resort | In progress |
| P5 | Read-only ERP lists (매출마감·출고·입고·현재고·발주) | Mapped; **not wired** |

## Smoke (no secrets in output)

```bash
set -a && source ~/.deneb/groupware.env && set +a
cd scripts/dev/groupware-reader && npm install
npm run login-check
node read.mjs --action list --area approval --folder pending
node read.mjs --action read --area approval --folder pending --query '다과비'
```

Never commit credentials, session JSON, or live tokens.
