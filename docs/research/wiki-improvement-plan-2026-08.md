# 위키 개선 방안 2026-08

**Status:** implemented (2026-08-23) — W1~W17 전 항목 착지. 구현 결과는 §11, 잔여는 §12. (어휘는 `improvement-ideas.md`와 동일)
**Audience:** Deneb 운영자 + 차기 AI 세션(구현 레인)
**Scope:** 위키 서브시스템 전체 — 저장소(`domain/wiki`)·자율 유지보수(dreamer / verify / review / research / scout / digest)·회상(recall)·계측(bench·원장)·클라이언트 표면. 검색 엔진 재작성·Graphify 투자·위키 UI 신설은 범위 밖(§9).
**Methodology (2026-08-23):** 코드(`gateway-go/internal/domain/wiki` 22K LOC 외) + 라이브 위키(`~/.deneb/wiki`, 읽기 전용) + 14일 게이트웨이 저널 + 위키 git 스냅샷 이력 + 프로덕션 패리티 `make recall-health` 실측(8m17s)을 10개 영역 조사 에이전트가 병렬로 훑고, 73개 발견을 3렌즈(증거 재검증·이력/중복·리스크) 패널이 건별 판정했다. **오늘 하루 동안 다른 레인이 위키 PR 5건을 랜딩하고 `wikicurate --apply`를 2회 적용했으므로(§2), 이 문서는 그 이후(12:16 KST) 상태를 기준선으로 잔여 항목만 제안한다.**

> **읽는 법.** 항목은 `W<번호>`로 식별한다(구현 PR 제목·커밋에 인용). 각 항목은 **무엇을 → 왜(증거) → 어디서(파일·함수) → 검증 → 롤백** 순. 우선순위 P0~P3 · 작업량 S/M/L · RSI 단계(P1 절차 외부화 / P2 slow loop / P3 verifier 공진화 / P4 도구 번들 / P5 복리화) 태그. **이 문서는 작성 시점 스냅샷**이다 — 현행 동작의 단일 진실원은 루트 `CLAUDE.md` + `docs/agent-rules/wiki-layout.md` + 모듈 `CLAUDE.md`.

> **상태 갱신 (2026-08-23 저녁).** 초안(12:16 KST) 직후 같은 날 저녁까지 **W1·W2(1단계)·W3·W5(수리 포함)·W6 가드·W8-1·W9(나이틀리)·W10(계약)·W11-4가 전부 착지**했다(#4600·#4603·#4601·#4605·#4606·#4615·#4608·#4611 — §2). 회상 레인에서는 [#4596](https://github.com/choiceoh/Deneb/pull/4596) 재측정(골드 정리 후 main P@1 91.9 · r@8 97.5)과 [#4604](https://github.com/choiceoh/Deneb/pull/4604) 멀티턴 재작성이, 드리머 레인에서는 improvement-ideas 5.7·5.8·후속(#4594·#4598·#4612)이 뒤를 이었다. 착지 범위와 잔여는 §0 표의 '상태' 칼럼과 각 W 절 머릿의 상태줄로 구분한다 — 상태줄이 없는 절(W4·W7·W12~W17)은 미착지.

관련 문서: [improvement-ideas](improvement-ideas.md) (전 영역 백로그 — 본 문서는 그 위키 전용 보강) · [wiki-retrieval-improvement-cycle-2026-07](wiki-retrieval-improvement-cycle-2026-07.md) (회상 레버 기각 기록) · [wiki-retrieval-baseline-2026-08-23](wiki-retrieval-baseline-2026-08-23.md) (같은 날 재측정 — 골드 정리·멀티턴 재작성 착지) · [recursive-self-improvement-roadmap](recursive-self-improvement-roadmap.md) · `docs/agent-rules/wiki-layout.md`.

---

## 0. 한 줄 요약 (TL;DR)

**핵심 진단 한 문장:** 위키는 "페이지가 모자라서"가 아니라 **LLM 판정이 위키를 바꾸기 직전의 결정적 전제조건(게이트)이 없어서** 스스로를 오염시킨다 — verify 자동이동이 거래 원장을 네 폴더로 찢고, 드리머 update가 페이지 정체성(id/summary)을 덮어쓰고, 그 id를 verify가 '동일 ID 중복'으로 자동 병합해 대표페이지가 사라지며, supersede 오용이 살아있는 로그를 30일 뒤 아카이브한다. 오늘 데이터는 손으로 고쳤지만(§2) 메커니즘은 전부 코드에 남아 있다 — **P0은 전부 재발 차단 가드**다.

| # | 항목 | P | 작업량 | RSI | 상태 (08-23 저녁) |
|---|---|---|---|---|---|
| W1 | verify 오분류 자동이동 레이아웃 가드 (서브폴더 민팅·원장·프로젝트 슬롯·존재하지 않는 경로 금지) | **P0** | S | P3 | 착지 [#4600](https://github.com/choiceoh/Deneb/pull/4600) — 잔여: 입력 필터(4)·프롬프트 가이드(5) |
| W2 | 드리머 정체성 불변식 — id/summary fill-only, 리타겟 시 본문만 append, 대표 전용 필드는 대표에만 | **P0** | M | P3 | 1단계 착지 [#4603](https://github.com/choiceoh/Deneb/pull/4603) — 잔여: DeriveID(3)·id_mismatch(4)·수리(5) |
| W3 | verify '동일 ID' 자동 병합 조건 강화 + `FoldDuplicate` 슬롯/코드/타입 백스톱 | **P0** | S | P3 | 착지 [#4601](https://github.com/choiceoh/Deneb/pull/4601) — 잔여: type 백스톱(iv) |
| W4 | 리타겟(`retargetDreamUpdate`) 수용 조건 — 카테고리·페이지 종류 일치 + 제목 토큰 겹침 | P1 | M | P3 | 착지 [#4617](https://github.com/choiceoh/Deneb/pull/4617) |
| W5 | `MarkSuperseded` 전제조건 + `detectStaleSuperseded` 슬롯 스킵 + ymn 로그 등 데이터 수리 (08-30 전) | **P0** | M | P3 | 착지 [#4605](https://github.com/choiceoh/Deneb/pull/4605)(수리 포함) — 잔여: 주제 앵커·연대순·강등 UX |
| W6 | 메일 재분류 신호 정밀도 — 메일↔메일 엣지 비소유·자사명 클라이언트 키 차단·tri-state 모호성·archived 필터·스냅샷/캡 (무장 이후 긴급) | P1 | S | P3 | 착지 [#4606](https://github.com/choiceoh/Deneb/pull/4606) — 잔여: 스냅샷·구조화 원장·스레드 신호 |
| W7 | 거래 원장 위치 결정(A/B) + 17장 복귀 + `cmd/wikicurate` '조직→인물' 규칙 수정 + 골드 26경로 선repoint | P1 | S–M | n/a | 착지 [#4620](https://github.com/choiceoh/Deneb/pull/4620) — A안(프로젝트/거래 단일 정본) + 라이브 18장 복귀 |
| W8 | 인물 코퍼스 정책 — dossier 200행 캡 버그·스텁 전용 강등·본문 동명이인 가드·신원키 중복 검출·스텁 related 스킵·summary 규약·효용 분모 | P1 | M | P5 | 착지 [#4615](https://github.com/choiceoh/Deneb/pull/4615)·[#4625](https://github.com/choiceoh/Deneb/pull/4625)·[#4627](https://github.com/choiceoh/Deneb/pull/4627) — 잔여: 신원키 중복 페이지·summary 규약 |
| W9 | 계측 복구 — nightly recall-health가 한 번도 안 돌았음(go 미발견·tee 마스킹)·벤치 확장암 비패리티·골드 repoint 도구·골드 백업 | P1 | S | P3 | 착지 [#4608](https://github.com/choiceoh/Deneb/pull/4608) — 잔여: 확장암 패리티·골드 repoint·백업 편입 |
| W10 | 본문 [[위키링크]] 복구(33% 깨짐) + `PreserveUpdated` 메타-전용 쓰기 계약(선행) + 빈 본문 생성 가드 | P1 | M | P3 | 착지 [#4608](https://github.com/choiceoh/Deneb/pull/4608)·[#4625](https://github.com/choiceoh/Deneb/pull/4625) |
| W11 | 저장/성능 — 벡터 캐시 181MB JSON→바이너리+비동기 로드(부팅 −2.7s), 위키 git ignore/gc, 백업에서 재생성 캐시 제외, by-hash refresh | P2 | M | n/a | 착지 [#4611](https://github.com/choiceoh/Deneb/pull/4611)·[#4626](https://github.com/choiceoh/Deneb/pull/4626)·[#4633](https://github.com/choiceoh/Deneb/pull/4633) — 잔여: embedindex 포팅 |
| W12 | 프로덕션 쿼리확장 관측성·예산 가드(4s 천장 vs 1.5s 프리플라이트) | P2 | M | P5 | 착지 [#4625](https://github.com/choiceoh/Deneb/pull/4625) |
| W13 | 회상 라우팅 계측(`route_followed`) + 수요 원장 트리거 | P2 | M | P5 | 착지 [#4623](https://github.com/choiceoh/Deneb/pull/4623) — 잔여: route_followed |
| W14 | cite v2 + 원문 질의(privacy 게이트) → traffic gold 증식 | P2 | M | P5 | 착지 [#4629](https://github.com/choiceoh/Deneb/pull/4629) — 잔여: rawQuery |
| W15 | 표면 — Trust Inbox 'wiki-maint' 카드(모호 클래스만)·`get_page` 메타/출처·챗 `wiki_move` 툴·죽은 `wiki.*` RPC 정리 | P2 | M | P5/P4 | 착지 [#4622](https://github.com/choiceoh/Deneb/pull/4622) — 잔여: wiki-maint 카드·wiki_move 툴 |
| W16 | 린트 패밀리 3레인(CI fixture 린트 · verify advisory · 쓰기 가드) + `wiki-layout.md` 드리프트 패치 | P2 | M | P1/P3 | 착지 [#4622](https://github.com/choiceoh/Deneb/pull/4622) — 잔여: 린트 2종 |
| W17 | P3 묶음 — 로그 헤더 문법/회전 경계, 날짜형 로그 페이지 정책, inert 규칙 주석, 공인 인물 태그, 프로젝트 화면 stage/질문 칩, cues 수치 갱신 | P3 | S | — | 착지 [#4623](https://github.com/choiceoh/Deneb/pull/4623) |

---

## 1. 현황 진단 (2026-08-23 실측)

### 1.1 규모·구조

- 페이지 **1,161~1,169** (조사 중 `wikicurate` 2회 적용으로 변동) — 프로젝트 769 / 인물 306 / 업무 49 / 기타 15 / 시스템 15 / 사용자 5. 프로젝트 폴더 74(코드 폴더 69 + 의도적 비코드 5: 금호타이어·남도에코-케이블·부산8호-rps·쏠리스-spa·프로젝트-템플릿), 대표.md 74/74(pl2-kia-epc-002는 12:16 신설).
- 대표 머리말 충전율: stage 68 · kinds 68 · code 66 · client 62 · sites 52 · cues 19 · program 4 (/73). 메일분석 476장(프로젝트 슬롯 388 + 미연결 버킷 **88**). 거래 원장 **프로젝트/거래 55 · 인물/거래 4 · 업무/거래 3 · 인물/&lt;회사&gt;.md(type: deal) 11**. 본문 [[위키링크]] 517 중 **149 타깃 부재 + 20 basename-only**(33%).
- 인물 306장 중 **~267장이 템플릿 외 산문 0인 스텁**(2026-07-27 주소록 동기화 1회가 204장 생성), 201장은 생성 후 무수정. 30일 회상 원장에서 인물은 919건 중 34건(read 32건은 전부 백그라운드 태스크, 사용자 대화 inject 2건).
- 저장: `.semantic-cache.json` **181MB**(JSON 십진 텍스트, 6,887벡터×2048d; float16이면 28MB, 고아 키 0) · 위키 git 620커밋 49MB(loose 39MB/2,328객체 vs pack 6.8MB) · `.wiki.db`(4월 고아 SQLite)+`-shm/-wal`·드리머 원장 3종이 git 추적 · 일일 백업 117MB 중 69MB가 재생성 가능 캐시.

### 1.2 자율 유지보수 건강

- **wiki-dream** ~3회/일, quality 74~85 · precision≈1 · utility 0.55~0.63. verify 원장 61건(stale_deadline 50 — 08-28 이후 #4588 `closeStaleDues`가 걷음) 매 사이클 재보고. 14일 **auto-move 21건(정당한 이동 0건, 전부 `프로젝트/거래/*`·프로젝트 슬롯)**, auto-merge 8건(5건이 잘못 스탬프된 id 동일 경로), 리다이렉트 4건(대표 제안이 로그/자식 페이지로).
- **wiki-review** 2h·146사이클/14d, 관찰 모드 재제안 584줄(고유 5) → #4583 dedup으로 종결(배포됨). 실이동 16건 중 signal=related 9건 전부 메일↔메일 엣지 근거, signal=title 7건 중 5건이 `client: 탑솔라`(자사명) 프로젝트로 오배정. **`DENEB_MAIL_RECLASS_DOMAIN=1` 12:09 무장**(드롭인) — 가드(W6)보다 먼저.
- **wiki-research** 6h(49사이클), **wiki-scout** 12h(21턴, 락 busy 12회), **noti-digest** 12h(22), supernote 휴면(폴더 미설정), 졸음/90일 보존 감지는 구현됐으나 데이터 나이상 아직 발화 불가(첫 발화 ~09-11 / ~10-30).

### 1.3 회상(recall) 기준선: 프로덕션 패리티 실측

| 지표 | 값 | 비고 |
|---|---|---|
| main gold 162 (161 유효) | **p@1 82.0 · r@8 93.8 · mrr 0.867** | 08-08 베이스라인 82.0/95.0; −1.2pp=2케이스, 그중 1건 dead gold(`hyundai-energy-rank`) |
| RECALL_UTIL | distinct 298 · hits 919 · used 184 | 41일 cite **2건** — use 신호 사실상 죽음 |
| RECALL_COVERAGE | 56/72 = 77.8% | 08-08 81.4% → 로스터 증가분 미커버 16 |
| RECALL_HEALTH | 83.1 | retrieval 86.7 · coverage 77.8 |
| 확장(expansion) 암 | 10/10 실패 | qwen 씽킹 예산 소진 — 사실상 OFF 측정; 프로덕션 tiny는 dsv4-nothink(모델 불일치) |
| nightly recall-health | **도입(07-19) 이후 0회 실행** | srv4 러너 PATH에 go 없음 + 파이프(tee)가 exit 마스킹 → 45회 전부 12초 만에 녹색 |
| 골드 부패 | hard 10/27 · multiturn 8/16 · typed 5/78 dead | main gold 26경로가 `프로젝트/거래/` 아래 → W7 전 repoint 필수 |
| 프리플라이트 | 87회 중 wiki=0(deadline) 15회 | 13회는 17:00 cron(email-analysis-full); client 2회 |

> **재측정 (2026-08-23 오후, [#4596](https://github.com/choiceoh/Deneb/pull/4596)).** 위 표는 12:16 KST 스냅샷이다. 같은 날 오후 재측정에서 'hard 셋 병목'의 대부분이 **죽은 골드**였음이 확인됐고 — 골드 36경로 repoint 후 main(197케이스)은 **P@1 91.9 · r@8 97.5**, multiturn 실패는 지칭 해결 문제로 판명돼 [#4604](https://github.com/choiceoh/Deneb/pull/4604)의 프리플라이트 콘텍스트 재작성으로 착지(재작성 시 r@8 100%), backfill 쿼리 확장은 구조적으로 발동하지 않는 것으로 판명·weak-primary RRF는 측정 후 기각됐다. 상세는 [wiki-retrieval-baseline-2026-08-23](wiki-retrieval-baseline-2026-08-23.md).

### 1.4 테스트 커버리지

`domain/wiki` 80.7% · `wikiwork` 61.9% · `chat/recall` 82.2% · `wikitool` 69.4% · `domain/knowledge` 77.7%.

---

## 2. 오늘(2026-08-23) 이미 랜딩·적용된 것: 재제안 금지

| 변경 | 내용 | 상태 |
|---|---|---|
| [#4583](https://github.com/choiceoh/Deneb/pull/4583) | related≤3·tags≤8 apply 하드캡(`meta_cap.go`), 미연결 메일 검색 ×0.25(`IsUnlinkedMailAnalysisPath`), 리뷰 observe 페이로드 dedup(`appendObserved`), 미해결 질문 14일 시효→'벤더 침묵', `cmd/wikicurate` | main · 배포됨 |
| [#4587](https://github.com/choiceoh/Deneb/pull/4587) | 드림 팩트 원장 `.dream-fact-ledger.jsonl`(합성·병합·아카이브·재분류 op 단위, `detail`=사유) + DreamReport 실측 카운터 | main · 배포됨 |
| [#4588](https://github.com/choiceoh/Deneb/pull/4588) | 드리머 쓰기 계약 — 수요 게이트(`dreamer_demand.go`), 7일+ due 자동 해제(`dreamer_due.go`), stage/program persist, 형제 딜(`siblingDeals`) 제목거리 병합 금지, 인물 마커 스텁 30일+무회상 archive(5e-2), MEMORY 카테고리 증류, 30KB+ 페이지 큐레이션 | main · 배포됨 |
| [#4589](https://github.com/choiceoh/Deneb/pull/4589) | 죽은 miniapp 메서드 정리(`memory.search_status`/`search_doctor` 포함) | main · 배포됨 |
| [#4593](https://github.com/choiceoh/Deneb/pull/4593) | 회상·`mail_archive`에서 wiki↔mail 인물 불일치를 중재 없이 표시; wikicurate P2(kia-002 대표 신설, 대표 '병합된 중복 문서' 잔재 접기) | main · 배포됨 |
| [#4592](https://github.com/choiceoh/Deneb/pull/4592) | 회상 수요 원장 조사(助詞) 탈락 | main · 배포됨 |
| [#4594](https://github.com/choiceoh/Deneb/pull/4594) | (improvement-ideas 5.7) 드리머 반증 증거 큐 — 다이제스트/dream 카드 '틀린 기억' 정정을 비평 프롬프트 반증 블록으로 주입 | main · 배포됨 |
| [#4598](https://github.com/choiceoh/Deneb/pull/4598) | (improvement-ideas 5.8) 드리머 변경 기록 + 선택적 되돌리기 — `.dream-cycle-changes.jsonl` + RevertDreamCycle/RevertDreamPages + workfeed 되돌리기 액션 | main · 배포됨 |
| [#4600](https://github.com/choiceoh/Deneb/pull/4600) | **W1** — `IsLayoutManagedPath` + `moveAllowedFor`(스냅샷 존재·레이아웃·deal 타입) + `recategorizedPath` 서브폴더/프로젝트 목적지 거부 + move 전용 캡 3·이동 로그에 LLM 사유. 판정은 advisory로 잔존 | main · 배포됨 |
| [#4601](https://github.com/choiceoh/Deneb/pull/4601) | **W3** — id 동일+제목 정규화 동일만 merge Fix(제목 상이는 'id 충돌 수리 대상' advisory) + `refuseLayoutSlotFold`(같은 폴더 슬롯·다른 코드 대표 쌍 거부 — 메일 Message-ID 가드와 같은 마지막 관문) | main · 배포됨 |
| [#4603](https://github.com/choiceoh/Deneb/pull/4603) | **W2 1단계** — `retargetedFrom` 기록, id 항상 fill-only, 리타겟 시 summary/type/confidence/due/resource도 fill-only, 대표 전용 필드(client/sites/kinds/stage/program)는 대표에만, wikitool 레이아웃 슬롯 제목 유지 | main · 배포됨 |
| [#4604](https://github.com/choiceoh/Deneb/pull/4604) | (회상, W 백로그 밖 — #4596 후속) 멀티턴 재작성 — 지시어 마커+신호어 과소 턴에서 직전 사용자 턴 꼬리를 질의에 붙여 자립화 | main · 배포됨 |
| [#4605](https://github.com/choiceoh/Deneb/pull/4605) | **W5** — `supersedePrecondition`(대표·로그·메일분석·거래원장이 OLD 쪽이면 거부) + `detectStaleSuperseded` 동일 술어 스킵 + **데이터 수리 라이브 적용**(ymn·kia-002 로그, sunkean·nde-sun 메일 2통 — `updated` 보존) | main · 배포됨 |
| [#4606](https://github.com/choiceoh/Deneb/pull/4606) | **W6 가드** — 신호1 대표엣지(`IsProjectRepPage`) 한정, tri-state(모호 판정의 도메인 폴백 차단), archived/superseded 후보 제외, 도메인 캡 3, `ownCompanyKeys` 자사명 client 키 차단 | main · 배포됨 |
| [#4608](https://github.com/choiceoh/Deneb/pull/4608) | **W9(나이틀리)+W10(계약)** — recall-health go 툴체인 탐색·`set -o pipefail`·`RECALL_HEALTH score=` 자기검증(nightly-drift `shell: bash`), `UpdatePageMetaOnly`(본문 불변·`updated` 보존 — `PruneDeadRelatedLinks` 전환), update-on-missing 빈 content 0B 생성 거부 | main · 배포됨 |
| [#4611](https://github.com/choiceoh/Deneb/pull/4611) | **W11-4** — by-hash refresh: 이동 페이지 임베딩 재사용(사라진 경로의 hash→벡터 맵에서 이관 — 재임베딩·캐시 전체 재기록 없음) | main · 배포됨 |
| [#4612](https://github.com/choiceoh/Deneb/pull/4612) | (드리머 자가개선) 품질 원장 `.dream-quality.jsonl`·합성 인덱스 상한 120·비평-수요 접지·사용자 모델 모순 advisory·수요 인지 케이던스 단축 | main · 배포됨 |
| [#4615](https://github.com/choiceoh/Deneb/pull/4615) | **W8-1** — `loadWikiPeople` 전량 로드+응답 tail 200 캡(201번째 이후 106명 매칭 복구), 이메일 본문 ∪ 프론트매터 `emails` | main · 배포됨 |
| `wikicurate --apply` ×2 (11:57 b838e60 527파일 · 12:16 b079854) | G2/스코폴라민 분리·맥코넬→페인스타인·vaultwarden id 수리·고아 메일 4통 파일링·기타/거래 비움·보배 SPA 복귀·related/tags 캡 194장·질문 시효 29장·kia-002 대표.md 신설·대표 6장 잔재 폴드. **단** 회사 원장 10장을 `인물/&lt;회사&gt;.md`로 이동(W7), ISW 페이지 미복원, 원장 `sunkean`·메일 2통 archived+superseded 유지(W5), 인물 53장 `updated` bump(W10) | 라이브 적용 |
| 운영자 드롭인 | `DENEB_MAIL_RECLASS_DOMAIN=1` (12:09:57), `DENEB_WIKI_QUERY_EXPANSION=backfill` | 라이브 |
| 본 세션 응급조치 | `프로젝트/pl2-kia-epc-002/기아-al광주-2공장-태양광-모듈-입찰.md`의 `id: pl2-kia-epc-002`(08-21 드리머가 덮어쓴 값)를 원래 `기아-al광주-태양광-모듈-입찰`로 복구 — 12:16 신설 대표.md와 id가 충돌해 다음 verify(~17:00)가 '동일한 ID' 자동 병합으로 대표를 다시 접을 상태였음(W3 메커니즘). 데이터 1줄, 백업 보관 | 라이브 적용 |

**닫힌 결론(데이터로 기각, 재오픈 금지):** 리랭커 교체·유형별 융합 라우팅·xProvence 프루닝·순진 다중쿼리·후보창 확대·HyDE·rrfK 스윕·융합 가중치 재스윕(1:3:4 확정)·문서측 큐/related 대량 정리([wiki-retrieval-improvement-cycle-2026-07](wiki-retrieval-improvement-cycle-2026-07.md)) · 레거시/비코드 폴더 재이관(08-08 대정비: 잔여 비코드 폴더는 전부 의도적) · 7번째 카테고리 · Graphify 투자 · 검색엔진 재작성 · 위키 UI 신설 · 외부 리뷰 18건(OpenWiki·Rekal·Wigolo·SearchOS·MemOps·MemCon·Cerebras KB·Tencent·SkillCorpus 등, 메모리 `closed-reviews`).

---

## 3. P0: 재발 차단 가드 (LLM 판정 적용 전 결정적 전제조건)

### W1. verify 오분류 자동이동 레이아웃 가드 (P0 · S · RSI P3)

**착지 (2026-08-23, [#4600](https://github.com/choiceoh/Deneb/pull/4600)).** 하위 1·2·3(캡·사유 로그) 완료 — 자동이동은 flat 비-레이아웃 페이지로 좁혀지고 이동 로그에 LLM 사유가 남는다. 잔여: 4(LLM 입력 필터 — 토큰 ~70% 절감), 5(프롬프트 가이드). 원장 17장 데이터 수리는 W7 §8-1 결정 후.

**무엇.** `verify.go:detectMisclassifications`(5b)가 인덱스 전 항목(1,169행)을 LLM에 주고 `confidence=high`면 `verify_apply.go:recategorizedPath`로 `Fix{move}`를 붙이는데, 이 함수는 **경로의 첫 세그먼트만 바꾸고 서브폴더를 보존**한다(`newCat + "/" + rest`, 테스트 `verify_apply_test.go`가 `프로젝트/거래/x.md → 업무/거래/x.md`를 기대값으로 고정). 레이아웃 소유 경로(`프로젝트/거래/` 원장, 프로젝트 코드폴더 슬롯, 메일분석/자료)·`type: deal`·LLM이 반환한 경로의 스냅샷 존재 여부를 전혀 검사하지 않는다.

**왜(증거).** 14일 auto-move 21건 전부가 `프로젝트/거래/*.md`(→ 기타/거래 16·인물/거래 6·업무/거래 2, 경비-식대·경비-접대비 포함)와 `프로젝트/pl1-bbw-wnd-001/spa.md → 기타/pl1-bbw-wnd-001/spa.md`; 정당한 이동 0. `deals.go:UpsertDealPage`는 `프로젝트/거래/&lt;slug&gt;.md` 고정 경로에만 쓰고 `counterparty_anchor.go:knownCounterparties`는 그 폴더만 읽으므로 이동된 원장은 회상 거래처 앵커에서 사라지고 다음 결재 캡처 때 원위치에 재생성 → **핑퐁**(정종호 07-16 생성→07-19 이동→07-23 재생성→07-25 이동→07-27 재생성→07-29 이동; 금비전자 프로젝트→인물→업무→프로젝트→기타). 08-23 01:07 `auto-move failed … no such file` 3건은 LLM이 이미 옮겨진 경로를 다시 낸 것 — 존재 검증 부재. 위키 git: 기타/거래·인물/거래·업무/거래 페이지 전부 `dream:` 커밋의 R(rename). 사유(`f.Detail`)는 로그·원장·리포트 어디에도 남지 않는다(#4587 팩트 원장이 `detail`을 기록하기 시작 — 그 위에 얹을 것).

**어디서.**

1. `verify.go:detectMisclassifications` 결과 루프: Fix 부착 조건 = ① `entries[r.Path]` 존재 ② 레이아웃 소유 경로 아님 — `project_layout.go`에 `IsLayoutManagedPath(rel)` 술어 신설(`ProjectNameOf` ok ‖ `IsProjectRawDataPath` ‖ `IsDealLedgerPath` ‖ `IsMailAnalysisPath` ‖ `IsMaterialPath`) ③ `entry.Type != "deal"` ④ 목적지 != 프로젝트. 탈락은 advisory finding으로만.
2. `verify_apply.go:recategorizedPath`: `rest`에 `/`가 있으면 `""`(서브폴더 민팅 금지 — 타깃은 항상 `<cat>/&lt;file&gt;.md`), `newCat=="프로젝트"`면 `""`. 테스트 기대값 교체.
3. `applyVerifyFixes`: move 전용 캡 `maxAutoMovesPerCycle=3`(merge/archive 캡 15와 분리) + 로그에 `reason=f.Detail`; 같은 from→to 실패 24h 억제.
4. LLM 입력을 flat 비-레이아웃 페이지로 필터(토큰 ~70% 절감 — 08-19 2048 절단 재발 방지; 8192 인상은 #4554로 랜딩됨).
5. (보조) 프롬프트에 dreamer 카테고리 가이드 + '프로젝트/ 하위·*/거래/ 원장 지적 금지' 1줄.

**검증.** `go test -run 'TestRecategorizedPath|TestDetectMisclassifications|TestApplyVerifyFixes' -count=1 ./internal/domain/wiki` (httptest LLM 응답: 스냅샷 밖 경로·`프로젝트/거래/정종호.md high→인물`·`프로젝트/pl1-x/spa.md high→기타`·`기타/김부장.md high→인물` 중 마지막만 Fix) → `make check` → `scripts/dev/live-test.sh restart && smoke` → 라이브 2사이클 저널에 `auto-moved … from=프로젝트/` 0건. **롤백:** revert(페이지는 git 스냅샷).

**하지 말 것.** 자동이동 전체를 끄지 말 것(`기타/김부장.md → 인물/`은 정당 용례) · '거래'를 7번째 카테고리로 승격하지 말 것 · `UpsertDealPage`가 이동된 원장을 따라가게 고치지 말 것(산개 정당화) · 프롬프트 문구만 고치고 끝내지 말 것.

### W2. 드리머 정체성 불변식: id/summary/title 덮어쓰기 차단 (P0 · M · RSI P3)

**착지 — 1단계 (2026-08-23, [#4603](https://github.com/choiceoh/Deneb/pull/4603)).** 하위 1 완료(`retargetedFrom`·id 항상 fill-only·리타겟 시 summary/type/confidence/due/resource fill-only·대표 전용 필드는 대표에만). 2는 부분 — wikitool은 **레이아웃 슬롯 페이지**만 제목 유지(일반 편집 대상의 제목 갱신은 기존 계약 유지). 잔여: 3(`DeriveID` 경로 파생)·4(`id_mismatch` advisory)·5(잔여 데이터 수리 — pl1-gsn 대표·skn 로그 id 등).

**무엇.** `dreamer_apply.go:mergeDreamUpdate`는 리다이렉트 여부와 무관하게 모든 update에서 `existing.Meta.ID/Summary/Resource/Type/Confidence/Due`를 LLM 값으로 **무조건 교체**하고(본문은 append), Sites/Kinds/Client를 대상이 대표인지 확인 없이 기록한다. `wikitool/wiki.go:updateWikiWritePage`는 더 강해 **Title까지 무조건 교체**(title이 필수 인자라 LLM이 매번 새 제목을 넘김).

**왜.** G2 페이지 오염(07-30 `dream:` a9aba64: id memomind-one→dating-app-scopolamine-threat + summary·resource 교체 + 스코폴라민 섹션 append), 모닝레터-08-14(08-17 create가 08-14로 리다이렉트돼 id/summary 교체), pl1-gsn 대표(id=pl2-gsn-dev-001), kia 자식 페이지(id=pl2-kia-epc-002 + client/sites/kinds 스탬프), wiki-research가 완도 로그를 '솔리스 인수 업무 분장'으로 개명. 30일간 기존 페이지 id 교체 **24건**(dream 21·wiki-research 1·wikicurate 수리 2) — vaultwarden 하나가 vaultwarden→…→isw-family로 표류. id는 `similar.go:matchByID`(리다이렉트 1순위)·verify 동일-ID 병합·`link_prune` 5단계·`graph_query` byID/findSeed·graphify 노드 id·index.md 첫 칼럼·검색 부스트 필드로 **정체성 키로 소비**되는데 생산 측 규칙은 프롬프트 한 줄('짧은 kebab-case 식별자')뿐이고 전체 인덱스가 프롬프트에 주입돼 타 페이지 id 복사를 유도한다(맥코넬 id=feinstein-casualty, 모닝레터-08-19 id=topsolar-projects-2026-08-19는 create 시점 필드 교차).

**어디서.**

1. `mergeDreamUpdate`: `ID` fill-only(비어 있을 때만); `wikiUpdate.RetargetedFrom` 필드 추가 → 리타겟된 update는 Summary/Type/Confidence/Resource/Due도 fill-only + 본문 append 헤더 `## [날짜] 보강 (원 제안: &lt;경로&gt;)`; Sites/Kinds/Client는 `IsProjectRepPage(u.Path)`일 때만. (#4588이 같은 함수에 Stage/Program을 추가했으므로 리베이스 필수.)
2. `updateWikiWritePage`: 기존 페이지가 있으면 Title 유지(개명은 별도 인자 없이는 금지), ID fill-only, Summary는 명시 `replace_summary=true` 없이는 fill-only.
3. id 계약 외부화: `page.go:DeriveID(relPath, meta)`(대표=code, 로그=code+"-log", 그 외=경로 슬러그) — `WritePage/UpdatePage` 경계에서 id가 비면 파생값으로 채움; 기존 id 변경은 명시 API(`Store.RenameID`)로만; LLM id는 파생값과 토큰을 공유할 때만 채택(아니면 무시+로그). 프롬프트·wikitool 스키마에서 id를 'update 시 생략·create 시 선택'으로 강등.
4. verify advisory `id_mismatch`(코드형 id≠자기 code·비대표의 코드형 id 등 결정적 4부류만 `Fix{set_id}` 자동, 나머지 advisory — 영문 로마자 id 69건 다수는 정상이라 일괄 수정 금지; graphify 노드 id·link_prune byID 해석 churn 주의).
5. 잔여 데이터 수리(가드 배포 **후**): pl1-gsn 대표 id→pl1-gsn-dev-001, skn 로그 id→pl2-skn-epc-001-log, pl1-agr 로그 id·부재 related 정리, `기타/isw-가족-소유.md` 복원 여부(§8).

**검증.** `TestMergeDreamUpdate_KeepsIdentityOnRetarget`·`TestMergeDreamUpdate_RepOnlyFieldsSkipChildPage`·`TestUpdateWikiWritePage_DoesNotRetitleExisting`·`TestDeriveID_ProjectSlots`; 라이브는 다음 dream 사이클 후 `git -C ~/.deneb/wiki log -p --since=1.day | grep '^-id:'` 0건. **롤백:** 필드 단위 revert.

### W3. verify '동일 ID' 자동 병합 증폭기 차단 (P0 · S · RSI P3)

**착지 (2026-08-23, [#4601](https://github.com/choiceoh/Deneb/pull/4601)).** id 동일만으로는 Fix를 주지 않고(정규화 제목까지 같아야 exact, 아니면 '동일한 ID(제목 상이)' advisory), `FoldDuplicate`에 `refuseLayoutSlotFold`(같은 폴더 in-folder 쌍·다른 코드 폴더의 대표 쌍) 백스톱. 잔여: (iv) `type` 백스톱 — 정확일치 병합의 type 일치 조건(메일 Message-ID·슬롯 가드가 대부분 흡수하지만 명시적 검사는 없음). 근본 원인 차단은 W2(#4603 착지).

**무엇.** `verify.go:detectDuplicates`는 제목 정규화 동일 **또는 id 완전 동일**이면 `exactDupFinding(Fix merge)`를 내고 `applyVerifyFixes`가 사이클당 15건까지 `FoldDuplicate`(본문을 '## 병합된 중복 문서' 아래 append 후 원본 삭제)를 자동 적용한다. `FoldDuplicate`의 유일한 백스톱은 메일분석 Message-ID 가드뿐.

**왜.** W2의 id 덮어쓰기와 한 사이클 안에서 연쇄('update(id 스탬프) → verify(동일 ID) → fold'): 14일 auto-merge 8건 중 5건 — wdo 08-13 대표←로그, dsv 08-17·nde-sun 08-22 대표←로그(로그 삭제 후 재생성), vaultwarden←ISW(ISW 지식은 git에만 잔존), **pl2-kia-epc-001 대표←pl2-kia-epc-002 대표**(08-20 23:28 — 002 대표 소실의 원인; 이후 드리머가 002 대표 재생성을 자식 페이지로 리다이렉트). 12:16 wikicurate가 신설한 002 대표와 자식 페이지가 다시 동일 id였고(본 세션 응급조치로 해소), `pl2-skn-epc-001/로그.md`는 지금도 id==대표 code라 대표에 id=code가 스탬프되는 순간 접힌다. #4588의 `siblingDeals` 가드는 **다른 코드 폴더 대표 쌍**만 막는다 — 같은 폴더 대표↔로그·카테고리 교차·비프로젝트 동일 id는 미커버.

**어디서.** `detectDuplicates`: id 동일만으로는 Fix 금지 → (id 동일 AND 제목 정규화키 동일)일 때만 exact; id만 같으면 advisory '동일한 ID(제목 상이)' = id 충돌 수리 대상. `verify_apply.go:FoldDuplicate` 하드 백스톱: (i) 같은 프로젝트 폴더의 대표/로그/상세 슬롯 조합 거부 (ii) 둘 다 대표인데 `ProjectFolderOf` 다르면 거부 (iii) 카테고리 다르고 제목키 다르면 거부 (iv) `type` 다르면 거부(deal vs entity — 업무/&lt;회사&gt; 개요 vs 거래 원장). 정확일치 병합도 type 일치 조건 추가.

**검증.** `TestDetectDuplicates_SameIDDifferentTitleIsAdvisory`·`TestFoldDuplicate_RefusesRepVsLogSameProject`·`TestFoldDuplicate_RefusesTwoRepPagesDifferentCodes`·type 불일치 케이스; 라이브 2사이클 `auto-merged` 0건. **롤백:** 규칙 revert; 복원은 git 스냅샷.

**하지 말 것.** '유사한 ID'(dist≤1/2)까지 auto-merge로 넓히지 말 것(pl2-kia-epc-001/002·kum-001을 묶음) · G2/vaultwarden/맥코넬 분리 재제안 금지(적용됨).

### W5. `MarkSuperseded` 전제조건 + stale-superseded 슬롯 스킵 + 데이터 수리 (P0 · M · RSI P3)

**착지 (2026-08-23, [#4605](https://github.com/choiceoh/Deneb/pull/4605)).** (1) 데이터 수리 **라이브 적용 완료**(ymn·kia-002 로그 superseded_by 제거, sunkean·nde-sun 메일 2통 superseded_by+archived 제거 — `updated` 보존), (2) 전제조건은 레이아웃 술어(대표·로그·메일분석·거래원장 = OLD 금지) 하드 거부(섀도 없이 바로 — 거부 대상이 결정적이고 에러에 사유 포함), (3) `detectStaleSuperseded` 동일 술어 스킵. 잔여: 주제 앵커·연대순 전제조건, 거부 시 related 강등 UX(호출자).

**무엇.** `store_merge.go:MarkSuperseded`는 경로 빈값/동일 여부만 확인하고 `superseded_by`를 쓴다. 호출자 4곳(wiki 챗툴 `markSupersededPages`·knowledge 어댑터·드리머 `markDreamSuperseded`·merge)이 전부 LLM 입력이라 '같은 주제의 낡은 값 대체'가 아닌 관계(사업 승계, 거래처 원장→프로젝트 대표, 메일분석→대표, **로그→자기 대표**)도 그대로 기록되고, `verify.go:detectStaleSuperseded`(30일)가 이를 장기 방치로 보고 **아카이브해 회상에서 제거**한다(validityFactor 0.3×0.15=0.045). 부산8호 M4 하드필터 사건(메모리 `wiki-migration-completed-20260808`)과 동형.

**왜.** 라이브: `프로젝트/pl2-ymn-epc-001/로그.md`(11섹션·21KB, updated 07-30) `superseded_by→자기 대표` → **~08-30 자동 아카이브 예정**; `pl2-kia-epc-002/로그.md` 동일 오용; `프로젝트/거래/sunkean.md` + nde-sun 메일 2통은 07-18 wiki-research가 찍은 superseded_by로 08-18 아카이브됨(11:57 이동 후에도 플래그 유지 — 거래처 앵커에서 제외). `supersedes` 키는 파서 미인식(0건). 메일분석 페이지에 superseded_by를 쓰는 경로를 막는 가드가 없다.

**어디서.** (1) **즉시 데이터 수리**(`Store.UpdatePage` 메타만, `Updated` 보존 — W10 계약 또는 수동): ymn 로그·kia-002 로그 superseded_by 제거, `프로젝트/거래/sunkean.md`·nde-sun 메일 2통 archived/superseded_by 제거(log.md 기록). (2) `MarkSuperseded` 전제조건 3종 — old가 레이아웃 소유 원시데이터(원장·메일분석·로그) 또는 대표/로그 슬롯이면 거부; 주제 앵커(같은 코드폴더 ‖ 제목 비불용어 토큰 ≥1 공유 ‖ Code/ID 동일); 연대순(new.Updated ≥ old.Updated). 실패 시 typed error → 호출자는 related 추가로 강등 + `supersede downgraded to related reason=` 감사 로그(wiki 툴은 모델에 사유 반환). 1~2주 섀도(카운터 로그만) 후 하드. (3) `detectStaleSuperseded`에서 `IsProjectRepPage`/로그 슬롯 스킵.

**검증.** sunkean·부산8호·ymn 픽스처 유닛; 라이브 스캔 '코드폴더 교차/원장·메일분석 발 superseded_by' 0. **롤백:** 가드 제거. 영구 env 스위치는 두지 않는다(opinionated 기본값).

---

## 4. P1

### W4. 리타겟 수용 조건: 약한 제목 매치로 다른 페이지에 착륙하지 않게 (P1 · M · RSI P3)

`dreamer_apply.go:retargetDreamUpdate`(create 중복감지·update-on-missing 둘 다)는 `findExistingPage→FindSimilarPages(limit 1)` 첫 히트로 경로를 바꾼다. 단계 id→code→slug→'title'인데 마지막은 제목이 아니라 **본문 포함 BM25 전문검색**(`similar.go:matchByTitle` → `search.go` 페이지 전체 필드, AND 실패 시 OR 폴백, 플로어 0.6 = raw≥1.5라 단일 공통 토큰으로 통과)이며 카테고리 접두만 보고 대표/로그/상세 슬롯을 구분하지 않는다. 결과: 08-19 동하메디칼 대표 update가 `pl1-agr-dev-001/로그.md`에(로그에 id/client/kinds 스탬프·부재 related), 08-21 kia-002 대표 재생성이 3팀 모듈입찰 자식 페이지에, 07-30 스코폴라민 제안이 G2 안경 페이지에, 08-17 pl2-gsn 대표 제안이 pl1-gsn 대표에. **수정:** Reason별 수용 조건 — id/code/slug는 (a) 카테고리 동일 AND (b) 페이지 종류 동일(rep↔rep, 로그↔로그, 상세↔상세; 대표 제안은 절대 로그/상세로 가지 않음)일 때만; 'title' Reason은 추가로 `normalizeTitleKey` 동일 또는 제목 토큰 Jaccard ≥0.5일 때만, 아니면 리타겟 없이 제안 경로에 생성 + `weak similar match ignored` 로그. `matchByTitle`은 title/summary/id 필드 전용 질의(또는 제목 토큰 2차 필터)로. 테스트: 스코폴라민→G2 픽스처가 새 페이지로, 동하메디칼 대표→pl1-agr 대표 또는 신규, kia-002 대표 제안이 자식 페이지로 가지 않음. **하지 말 것:** 리타겟 자체를 끄지 말 것(`TestWikiDreamer_CreateOnExistingPathConvertsToUpdateNotOverwrite`가 지키는 검증된 가드) · 임계값만 올리지 말 것(점수는 코퍼스 IDF 의존) · LLM 중복 판정으로 대체하지 말 것(메모리 `genesis-deterministic-replay-gate`).

### W6. 메일 재분류 신호 정밀도: 무장 이후 긴급 (P1 · S · RSI P3)

**가드 착지 (2026-08-23, [#4606](https://github.com/choiceoh/Deneb/pull/4606)).** 신호1 대표엣지 한정·자사명 client 키 차단(`ownCompanyKeys`)·tri-state(모호→도메인 폴백 차단)·archived/superseded 후보 제외·도메인 캡 3 완료. 잔여: 사이클 첫 이동 전 `SnapshotGit`·log.md `signal=` 기록·관찰 원장 구조화(`ObservedRefiles`)·메일 보존 시계 `Created` 기준·`wikicurate refile --dry-run`·오배정 14건 수리(§8-5 운영자 확인).

- **신호1(related)이 메일↔메일 엣지를 소유 증거로 취급:** `mail_reclassify.go:reclassifyTarget`은 related에 `ProjectNameOf`만 적용해 `프로젝트/&lt;proj&gt;/메일분석/<다른 메일>.md` 엣지도 소유로 센다(분석기의 RELATED_PROJECTS는 대표 후보만 검증). 14일 signal=related 이동 9건 전부 대표 엣지 0·메일 엣지 1~2 → '[탑선] Teaser·NDA'×3→충남 영농형, '[SKI E&S] O&M' 스레드가 두 프로젝트로 분할 등. 11:57 캡이 183c80f6a의 엣지를 1개로 만들어 '광주 남구 안전관리대행'이 태안 소원면 프로젝트로 이동 대기(5장). → **`IsProjectRepPage(r)`인 엣지만 신호1**; 메일엣지는 비소유.
- **신호2(title)의 거래처 키가 자사명:** `project_anchor.go:uniqueProjectIn`은 own-key 없으면 client-key 단일 프로젝트로 확정 — 활성 프로젝트 중 pl1-cny-dev-001만 `client: 탑솔라`라 `[탑솔라(주)]` 제목이 own-key를 못 찾을 때마다 충남 영농형으로(7건 중 5건 오배정: 임동중흥·안성스타필드×2·선그로우 견적·기아 이천덕평). 도메인 경로엔 `topsolar.kr` 블록리스트가 있으나 제목 경로엔 없음. → `ownCompanyKeys={탑솔라,탑솔라주,topsolar}` 단일 정본(블록리스트 옆)으로 client 후보 스킵; pl1-cny 대표 client 값 정정(운영자 확인); 오배정 5건 수리(안성스타필드 2통→pl2-asf-epc-001, 나머지 버킷 복귀).
- **무장됐는데 가드가 없다:** `DENEB_MAIL_RECLASS_DOMAIN=1`이 12:09에 켜졌지만 (a) 신호1 '≥2 프로젝트 모호' `""` 반환이 그대로 도메인 신호로 떨어짐(tri-state 필요), (b) 후보가 archived/superseded를 안 거름(08-18 아카이브된 sunkean 2통도 계속 제안됐음), (c) 도메인 이동 전 git 스냅샷·전용 캡 없음(중복 병합 경로만 SnapshotGit). 현재 도메인 후보 0건이라 즉시 피해는 없지만 신규 미연결 메일마다 커진다 → **메일 레인 1순위 PR**: tri-state(`target, signal, ambiguous`)·archived/superseded 스킵·`mailDomainMaxMovesPerCycle=3`·사이클 첫 이동 전 `SnapshotGit`·log.md 이동 엔트리에 `signal=`. 관찰 원장은 #4583 dedup 위에 count/first/last 구조화(`ObservedRefiles map`)·Info 로그 첫 회만·버킷에서 사라진 키 자동 정리.
- **고아 88장의 정직한 잔류:** 52장은 기파일링 증거 0인 뉴스레터/SaaS 도메인(율촌 11·freightos 9·cursor 5…), 최대 나이 67일(90일 보존은 ~10-18부터 자연 드레인), 30일 회상 inject의 13%가 고아에서(read 8) → #4583 ×0.25 감점 타당. 결정적 재파일링 기대치 **6~8장**(자기키 제목 2 + 새 '스레드 제목' 신호 4~5 — Re/FW 제거 정규화 제목이 정확히 한 활성 프로젝트에 기파일링), ~80장은 잔류가 정답. `detectStaleMailAnalyses`는 `Updated` 우선이라 재분류 related-append가 보존 시계를 리셋 → 메일은 `Created` 기준으로(1줄). 구현은 `wikicurate refile --dry-run`(신호별 신뢰표, high만 이동, 캡 20, 스냅샷 선행) — 신호1·2 수정 **후**에만 실행(기존 오배정을 물려받지 않게). **하지 말 것:** cap 10 상향·LLM 재파일링·뉴스레터를 프로젝트/7번째 카테고리로·코드 기본값 armed·관찰 기간 연장.

### W7. 거래 원장 위치 결정(A/B) + 복귀 + wikicurate 규칙 (P1 · S–M)

라이브 `type: deal` 페이지가 프로젝트/거래 55 외에 **인물/거래 4·업무/거래 3**(verify 자동이동 산물, 경비-식대·경비-접대비 포함)·**인물/&lt;회사&gt;.md 11**(11:57 wikicurate가 '인명·조직=인물' 택소노미로 이동 + 광주일보사 신설). `deals.go`(원장=`프로젝트/거래/<slug>`)·`knownCounterparties`(그 폴더만 스캔)·`IsDealLedgerPath`·`wiki_approval_deal.go`(경비-&lt;kind&gt; 파일링)·`wiki-layout.md` 모두 프로젝트/거래 단일 정본이라 옮겨진 17장은 거래처 앵커·미팅 KnownNames에서 사라지고 다음 캡처 때 중복 재생성된다. 더구나 '조직→인물' 이동 규칙은 이제 **main의 `cmd/wikicurate/main.go`에 코드로 있어** 재실행마다 재발. **운영자 결정 1개(§8):** (A, 권고) 회사 원장은 프로젝트/거래로 복귀(`Store.MovePage` 17장 — 게이트웨이 가동 중엔 `miniapp.memory.move_page`, 정지 창이면 wikicurate 패턴) + W1 가드 + wikicurate 규칙 제거 + 불변조건 'category==인물 ⇒ type∈{entity,""}'; (B) 인물/업무 원장도 정본 인정 → `knownCounterparties`를 type:deal 전수 스캔으로, `UpsertDealPage`에 slug 전역 조회, 문서 개정. 어느 쪽이든 린트 `wiki_deal_ledger_lint.py`(type:deal ∧ 경로∉프로젝트/거래 ‖ 동일 slug 다중 카테고리 → 위반). **선행 조건:** main gold 26경로가 `프로젝트/거래/` 아래 — 이동 전 골드 repoint(W9)와 이동 전후 `recall-bench` 두 줄을 PR Verification에. **하지 말 것:** 업무/&lt;회사&gt;.md(type entity 개요: 화신일렉트릭·바로·한국전기기술·다스코 등)를 원장에 병합하지 말 것(다른 슬롯; 링크로 충족) · 같은 회사 표기 분기(바로(주)/바로 주식회사, JOCA 3표기)의 근본은 `UpsertDealPage` slug 별칭(`counterparty_anchor` 재사용) — 별도 과제.

### W8. 인물 코퍼스 정책 (P1 · M · RSI P5)

**1 착지 (2026-08-23, [#4615](https://github.com/choiceoh/Deneb/pull/4615)).** 캡을 응답 tail 행 수로 이동해 306장 전량 로드(201번째 이후 106명 매칭 복구) + 이메일 본문 '## 연락처' ∪ 프론트매터 `emails`. 잔여: 2~6(스텁 강등·동명이인 가드·신원키 중복 검출·스텁 related 스킵·summary 규약·효용 분모).

1. **기능 버그(S):** `handlerminiapp/knowledge/people.go:loadWikiPeople`이 `ListPages(인물)` 사전순 순회 중 `maxPeopleWikiRows=200`에서 break → 306장 중 **201번째(인물/이방엽.md)부터 106명이 people.list·person.dossier(#4578) 매칭에서 보이지 않음**; 이메일도 본문 '## 연락처' 라인만 읽어(`contactSectionEmails`) fm emails만 있는 페이지 39장은 매칭 불가. → 캡은 응답 tail 행 수에만, emails는 `Meta.Emails ∪ contactSectionEmails`. 테스트 '201번째 페이지 매칭'.
2. **스텁 강등:** 검색 `validityFactor`는 archived/superseded/나이만 보고 importance·type·본문 길이로 강등하지 않아 306장 전부 임베딩+summary('탑솔라 소속' 104·'탑솔라그룹 소속' 75) 2.5× 부스트 → 프로브 '탑솔라 회사 개요' 4~8위가 스텁, 프리플라이트 8슬롯을 잠식. #4588의 5e-2는 마커 스텁(~45, 드림 시드)만 archive하고 contacts-sync 스텁 234장은 미대상. → **`archived` 오버로딩 대신 전용 강등**: `isPersonStub(page)`(템플릿·마커 제거 후 산문 <20룬 ∧ pid 없음 ∧ org.json 미포함 ∧ 30d 회상 0) → validityFactor에 stub 팩터(또는 `stub: true` 마커) + 승격 규칙(드리머/메일분석이 담당·관계를 채우거나 pid/org 배정·회상 hit 시 해제) + 시딩은 '강등 상태로 시작'(레지스트리는 contacts 스토어 2,709건이 담당). archived는 회상 증거에 '⚠ 보관됨' 경고를 붙여 연락처 질문에 부적합. 검증: 위키 사본에서 typed(인물 21)·bm25(pid 12)·main 골드 전후 비교 무손실 + 프로브 4~8위 스텁 소멸. 267장 쓰기는 `updated` 보존(W10)·by-hash refresh(W11) 뒤 단일 스냅샷 배치로.
3. **본문 동명이인 가드:** `person_emails.go:identityEmails`는 ≥2 회사 도메인이면 fm emails를 쓰지 않지만, 같은 동기화의 `contacts.go:mergeContactsByName→enrichPersonPage`는 이름만으로 전화·이메일을 합쳐 본문에 쓴다(김성환: topsolar+bmenergy 전화·이메일 한 페이지, 후보 15건). → ambiguous면 소속 일치 연락처만 렌더, 없으면 `homonym_pending: true` 마커 + verify finding `homonym`(Trust Inbox 카드 1회, W15) — 분리/병합 적용은 운영자 승인(메모리 `person-homonym-conflation`: 자동분리 금지).
4. **신원키 중복 검출기:** 직급 접미 제목(이영민·이영민-차장·이영민-팀장 — 동일 휴대폰 2/3; 오명석/오명석감사 동일 이메일; 14건)은 CJK 제목거리 규칙상 영구 미감지. → `person_seed.go` 시드 제목을 `personPageName`으로 정규화 + 기존 검사를 `listPeopleByName` 키로; verify `detectIdentityDuplicates`(fm∪본문 이메일·≥9자리 전화 키; 같은 개인형 이메일이면 자동 fold, 전화만 같으면 advisory; info@/sales@ 공용 주소 제외). 병합 후 typed 골드 직급 경로 repoint.
5. **스텁 related 노이즈:** `verify.go:enrichRelatedLinks`가 related 0인 모든 페이지에 임베딩 이웃 2개를 넣어 템플릿 동일 스텁끼리 related 고정(701/774가 인물→인물) → 그래프 RRF 엣지('강동민' 프로브에 강동화 0.865·강민수 0.801). → 스텁/강등 페이지는 `enrichRelatedLinks` 스킵, `suggestRelated` 후보에서 같은 카테고리 스텁 제외. **일회성 대량 related 정리는 07-21 벤치 무승부로 기각됨 — 가드만.**
6. **summary 규약·시딩·효용 분모(S):** summary를 '## 소속 · 직책'에서 파생('&lt;소속&gt; · &lt;직급&gt; — <담당 한 줄>'), 출처 문구('주소록 기반 자동 생성')는 변경 이력으로; org.json 미보유 87명은 페이지로 채우지 않음(dossier/recall_org가 플레이스홀더 처리); 공인 4건(툴시 개버드·미어샤이머·삭스·캐스케이디아)은 `kind: public` 태그로 people.list/핫워드 제외. 인물 효용은 recall 주입이 아니라 dossier/people.list·sender_context·recall_org·핫워드 소비 — 그 경로에 `RecordRecallEvents(read, session 'dossier:'…)` 기록해 분모 교정(현재 cite 전역 2건은 경로 부분문자열 매칭 구조상 기대치).
**하지 말 것:** 스텁 삭제(`ResolvePersonPaths`·recall_org·sender_context·dossier·핫워드가 페이지를 키로 씀) · 카테고리 부스트/길이 정규화 같은 랭킹 레버 1차 투입 · 주소록 전원 재시딩 · 동명이인 자동 분리 · 골드 repoint 없이 병합.

### W9. 계측 복구: verifier가 실제로 돌게 (P1 · S · RSI P3)

**첫 bullet 착지 (2026-08-23, [#4608](https://github.com/choiceoh/Deneb/pull/4608)).** 나이틀리 recall-health가 실제로 돈다 — go 툴체인 탐색(없으면 명시적 실패)·`set -o pipefail`·`RECALL_HEALTH score=` 라인 자기검증. 잔여: 확장암 비패리티(재측정 #4596에서 '구조적으로 미발동' 판명 — 관측성 요구는 W12와 합류)·`gold_repoint.py` 도구화·골드 백업·analysis-xl 기준선. 단 골드 36경로는 #4596에서 수동 repoint·main 재측정까지 착지.

- **nightly recall-health 0회 실행:** `.github/workflows/nightly-drift.yml`의 잡이 `scripts/dev/recall-health.sh | tee -a $GITHUB_STEP_SUMMARY`라 srv4 러너 PATH(~/go-sdk/go/bin 없음)에서 `go: 명령어를 찾을 수 없음`으로 즉사해도 tee의 exit 0으로 녹색(07-20 첫 런부터 45회 동일). → 스크립트 상단 go PATH 주입(또는 setup-go), 스텝 `shell: bash` + pipefail, 끝에 `RECALL_HEALTH score=` 라인 자기검증(없으면 exit 1 — advisory 잡이라도 '안 돌았는데 녹색' 금지), health-v3 패턴으로 −2pp 이상 하락 시 추적 이슈 갱신(페이징 아님).
- **벤치 확장암 비패리티:** `recall-health.sh`는 expander를 `qwen3.6-35b-a3b`(웜홀)로 고정하고 '프로덕션 tiny 미러'라 주석했지만 프로덕션 tiny는 `dsv4-nothink`; 웜홀 qwen은 `enable_thinking=false`를 무시해 오늘 10/10 실패 → 사실상 OFF 측정. → recall-bench에 `expansion: attempted/ok/failed` 요약 + ok==0이면 'EXPANSION ARM DEAD — not parity' 경고(nightly는 exit 1); 기본 모델은 **공유 dsv4 금지**(07-21 프로덕션 헬퍼 경합 사건) — 비공유 암 또는 '비패리티 명시'.
- **골드 부패·백업:** 11개 골드 파일 중 hard 10/27·multiturn 8/16·a1sim 8/16·typed 5/78·bm25 2/24·main 1/161·traffic 1/7 dead(코드폴더 개명 전 경로); `cmd/recall-bench` `deadGoldCases/mismatchedGoldCases`는 stderr 경고뿐. 골드는 `~/.deneb` 루트라 **백업 `DefaultTargets` 밖**(단일 사본). → `scripts/audit/gold_repoint.py`(위키 git `--follow --name-status`로 dead 경로 자동 재지정, .bak 보존), nightly에 11셋 dead-count + `--content` 병행 채점, 골드를 백업 대상에 추가(또는 레포 인접 저장소 — 레포 안은 업무 데이터라 금지).
- **analysis-xl 450 현재값 미측정**(오늘 8분 런만) — 마지막 정직값 73.1/90.2; nightly 복구 후 첫 값이 새 기준선.

### W10. 본문 위키링크 복구 + `PreserveUpdated` 계약 + 빈 본문 가드 (P1 · M · RSI P3)

**계약·빈 본문 가드 착지 (2026-08-23, [#4608](https://github.com/choiceoh/Deneb/pull/4608)).** `UpdatePageMetaOnly`(본문 변경 시 에러, `updated` 보존 — `PruneDeadRelatedLinks`가 전환해 사용 중) + update-on-missing의 빈 content 0B 생성 거부. 잔여: [[위키링크]] 복구(`PruneDeadWikiLinks`·`dead_link` advisory·~150장 재작성)·`createDreamPage` 최소 길이 게이트·`empty_page` advisory·기존 0B 3건 수리·`wiki_updated_churn_lint`.

- **[[링크]] 33% 깨짐:** `ExtractWikiLinks`는 graph/snapshot/project_refs에서만 소비되고 죽은 링크 복구는 `link_prune.go:PruneDeadRelatedLinks`(related 전용)뿐; 149 부재(07-19 fleet rename 전 경로 62·코드폴더 내 삭제/개명 36·타 카테고리 26·bare 17·메일 8) + malformed 1(`[[신호:action/tools#watch]]`) + Go resolver가 못 푸는 basename-only 20. 08-08 대정비 메모리도 '본문 링크는 안 고치므로 sed 스윕 별도'로 인지한 갭. → `Store.PruneDeadWikiLinks`(link_prune 사다리 재사용: 정확경로→레거시→유일 basename→유일 제목→유일 ID → 정식 경로로 재작성; 모호/부재는 advisory) + verify finding `dead_link` + wikitool write/ingest 시 타깃 검증 경고. ~150장 본문 재작성은 아래 계약 뒤에.
- **`PreserveUpdated` 계약(선행):** 메타만 고치는 쓰기(related 캡·superseded 제거·id=code·MarkSuperseded)가 `updated`를 갱신해 'updated≤7d·본문<300B' 9→44로 늘었고 stale/신선도 신호가 내용 변경과 분리됨. → `UpdatePage` 옵션(또는 body hash 불변+Updated 미지정이면 보존); MarkSuperseded·캡 스윕·PruneDead*·wikicurate가 사용; CI 린트 `wiki_updated_churn_lint.py`(git diff에서 updated만 바뀐 커밋 비율). 다수 수리(W5·W7·W8·W10)의 **공통 선행 조건**.
- **빈 본문 생성 가드:** `dreamer_apply.go:updateDreamPage`의 existing==nil 분기가 `page.Body=u.Content`를 그대로 써 0B 페이지 3건(기타/모닝레터-2026-08-19·인물/제용범·기타/mattress-industry-4-tricks) → `TrimSpace(u.Content)==""`면 생성 거부(로그), `createDreamPage` 최소 길이 게이트, verify advisory `empty_page`, 기존 3건 결정적 수리.

---

## 5. P2

### W11. 저장/성능/운영 (P2 · M)

**4 착지 (2026-08-23, [#4611](https://github.com/choiceoh/Deneb/pull/4611)).** by-hash refresh — refresh가 사라진 경로의 캐시 벡터를 hash→벡터 맵으로 모으고 새 경로의 해시가 일치하면 이관(이동 시 재임베딩·캐시 전체 재기록 없음). W7·W8·W10 대량 수리의 선행 조건이 충족됐다. 잔여: 1(캐시 포맷·비동기 로드)·2(git ignore/gc)·3(백업 캐시 제외).

1. **벡터 캐시:** `.semantic-cache.json` 181MB는 stale 키가 아니라 **포맷**(6,887 벡터 × 2048d를 십진 텍스트 12.6B/float로) — float32 56MB·float16 28MB(실벡터 1,180개 프로브: float16 max|Δcos| 3.5e-5, top-8 overlap 1.0). `semantic.go:SetEmbedder→loadCache`가 부팅 경로에서 **동기**(61회 부팅 중앙값 2.79s = mailstore→autonomous 3.76s의 74%; 14일 재시작 62회=랜딩 케이던스; 부팅 직후 RSS HWM 1.87GB), `saveCache`는 변경마다 전체 181MB 재직렬화(marshal 0.87s + fsync + rename). `Store.Forget`은 writeMu 아래서 이 저장을 동기 호출(잠재 1s+ 정지). → 스키마 v4: 매니페스트 JSON + `.semantic-cache.f16`(또는 float32 — 벤치 바이트 동일성 우선이면 먼저 float32) 플랫 바이너리, `loadCache` 지연/병렬(4개 캐시 181+89+75+5MB errgroup, 또는 warm 고루틴으로), v3 리더 1릴리스 유지. 기대: 부팅 −2.7s, 파일 −85%, 일시 heap 335MB→~60MB. 같은 wire 형태의 embedindex(mail/diary/workfeed)는 후속. **SQLite 경로는 금지**(go.mod에 드라이버 없음·4월 `.wiki.db` 폐기 전례·CGO/arm64 표면).
2. **위키 git:** loose 39MB/2,328(08-10 이후 ~120/일) vs pack 6.8MB, gc는 auto 6,700에서만; 60커밋 loose의 절반이 파생 파일(log.md 12.2MB·index.md 7.2MB·.recall-hits 2.2MB·.deals 1.0MB). `gitsnap.go:wikiGitIgnore`는 5줄뿐이고 `ensureGitRepo`는 `.gitignore`를 1회만 쓴다. → ignore에 `.recall-hits.jsonl`·`.verify-findings.json`·`.dream-selfcompare.jsonl`·`.dream-fact-ledger.jsonl`(#4587)·`.wiki.db*` 추가 + 라이브 .gitignore 조정(reconcile), `gc.auto 512`+`autoDetach false`(30s 스냅샷 타임아웃 안). `.deals.jsonl`·index.md·log.md는 추적 유지(되돌리기 힌트·감사 원장). 일회성 `git rm --cached`(.wiki.db/-shm/-wal·원장 3종·`.bak` 12개)는 **운영자 git 조작**.
3. **백업:** `backup.go:writeArchive`는 .tmp/.lock/.partial만 제외 → 매일 wiki 117MB 중 69MB + diary 캐시 75MB가 재생성 가능 파생물. → 캐시 제외 상수 + 'memory backup shipped' 라인에 bytes 기록. 복원 후 첫 부팅 재임베딩(1,165쪽≈215배치, BM25-only 창) 허용·헤더에 문서화. wiki/.git은 제외 금지(진짜 기억).
4. **by-hash refresh:** `semantic.go:refresh`는 같은 경로 해시만 비교해 이동/리파일 페이지를 전부 재임베딩 + 전체 캐시 재기록(오늘 18 moves에서 관측). → `byHash` 맵으로 동일 해시 벡터 복사(청크 라인은 본문 상대라 정확). W7·W8·W10의 100+장 이동 전에.

### W12. 프로덕션 쿼리확장 관측성·예산 가드 (P2 · M · RSI P5)

프로덕션 expander(`wiki_query_expander.go`, tiny 역할, 4s 천장)는 실패 시 `Debug`만 남기고 카운터가 없어 14일 저널에 'expansion' 라인 0 — 켜져 있는지·몇 번 발화하는지 알 수 없다. `search.go:backfillWithExpansion`은 `SearchWithOptions` 안에서 같은 ctx로 도는데 프리플라이트 예산은 1.5s(`recallPreflightTimeout`) → 언더필 쿼리에서 LLM 호출이 예산을 넘기면 wiki 소스 **1차 결과까지** deadline으로 버려진다. 12일치 wiki=0(deadline) 15/87(13건은 17:00 cron — 부하 후보, client 2건; 인과 미검증). → Info 1줄 `wiki expansion: fired under_fill=n/limit terms=k dur=ms ok|err`; ctx 잔여 <600ms면 확장 생략, expander 타임아웃 `min(4s, 잔여-100ms)`; 확장 전 스냅샷을 partial로 반환(확장은 순수 append). 측정: 주간 deadline 비율 17%→<2%, main gold byte-identical.

### W13. 회상 라우팅 계측 (P2 · M · RSI P5)

`recall_route.go`(#4472)는 집계/최상급/시간형/관계 4정규식으로 '[회상 라우팅] deal_ledger/wiki index/mail_archive/graphify' 힌트를 붙이는 수동 넛지 — 발화 로그 없음·모델의 추종(route_followed) 미기록·recall-bench는 페이지 히트만 채점(잔여 미스 ~6건=집계·그래프·시간 질의는 라우팅 문제 — 메모리 `recall-measurement-rot` ⑨). 수요 원장 `.recall-misses.jsonl`은 cue+무증거 조건이라 27일간 파일 자체가 생성된 적 없음(`RecallDemandTerms` 항상 빈 값). → `appendRoutingHint` Info 로그(shape) + 턴 종료 시 tool call 목록 대조 `event:"route_followed"` 원장 + 라우팅 골드(5+12문항)를 tool-call 정확도로 채점; 추종률 <70%일 때만 ledger-total 형태 한정 deal_ledger 합계를 결정적 1줄 선주입. 수요 원장 트리거에 '주입 후 같은 턴에 wiki/knowledge 재검색' 추가. 프롬프트 캐시 안전(꼬리 주입).

### W14. cite v2 + 원문 질의 → traffic gold 증식 (P2 · M · RSI P5)

`.recall-hits.jsonl` 1,229줄: inject 386·read 739(대부분 wiki-research/system 자동)·**cite 2건(41일)**. `recall_injected.go:matchCitedPaths`는 답변 본문에 주입 경로 또는 페이지 제목(≥3자) 부분문자열이 있을 때만 기록하는데 client 주입의 61%가 uuid/해시형 메일분석 제목·63%가 20자 초과 슬러그라 한국어 답변에 등장할 수 없고, inject의 query는 사용자 문장이 아니라 신호어열. 결과 `mine_traffic_gold.py` 조인은 285 inject 중 7건(질문이 키워드열, 1건은 6일 만에 dead). knowledge 어댑터 read는 Session 없이 기록(조인 불가). → (a) 매처에 frontmatter title/aliases·프로젝트 코드·wikitool provenance 푸터·[[위키링크]] 앵커 추가(under-count 설계 유지) (b) inject에 `rawQuery`(사용자 메시지 앞 120자) — **client:* 세션만·절단·W11 gitignore 선행**(원장은 git 추적+오프사이트 백업이라 raw 사용자 문장·알림 텍스트 영속 금지) (c) 어댑터 read에 세션키. 측정: 주간 client use ≥10, traffic gold 4주 ≥30케이스. **하지 말 것:** inject=use 계수·매칭 느슨화로 가짜 신호.

### W15. 표면: 기존 표면 확장만 (P2 · M · RSI P5·P4)

- **Trust Inbox 'wiki-maint' 카드(모호 클래스만):** `workfeed_self_correction.go` 복제(Source `wiki-maint`, approval:approve/reject, wire 변경 0) — **동명이인 분리 후보·stale_deadline 건별 mark(due_done/clear_due/ignore)·(무장 전이라면) 도메인 리파일**만. verify move/merge를 승인 카드로 바꾸지 말 것(운영자 자율 위임 원칙; 잘못된 자동행동은 W1·W3 가드로 제거). 거절은 `.wiki-maint-decisions.json` 30d 쿨다운. #4588 `closeStaleDues` 배포 후 stale_deadline 잔량 확인하고 착수.
- **`get_page` 메타/출처:** out에 `stage, client, sites, program, kinds, sources, supersededBy, archived, lastWriter{who,at}`(#4587 팩트 원장 → 없으면 git 스냅샷 접두 파싱). 양 클라 페이지 뷰에 메타 1줄(편집은 열지 않음 — 메타 수정 경로는 챗 `wiki write` 단일). out은 로컬 구조체라 `//deneb:wire` 생성기 무영향, Kotlin `WikiPagePayload`·TS `WikiPage` 수기 추가.
- **챗 `wiki_move` 툴:** wiki 툴 12액션에 move/merge 없음 → 운영자가 챗에서 리파일 불가(클라 `move_page`만). `wiki_forget` 분리 근거대로 별도 툴(백그라운드 프리셋 제외·untrusted 게이트 irreversible), reason을 log.md에. 툴 스키마 변경은 배포당 1회(프롬프트 캐시 허용).
- **죽은 RPC:** `handler/wiki/wiki.go`의 `wiki.*` 8메서드는 레포 내 소비자 0(sop_miner 픽스처 문자열뿐) — 2주 저널 호출 0 확인 후 제거. (`memory.search_status/doctor`는 #4589로 완료.)
- **프로젝트 화면 stage·질문 칩:** 라이브 열린 질문 11건(최고령 13일)이라 수요 작음 — 보류(P3). '스카우트 70 미해결'은 60일 누적 시도 키 과대 해석.

### W16. 린트 패밀리 3레인 + 문서 드리프트 패치 (P2 · M · RSI P1·P3)

배치 원칙: (1) CI-safe 유닛린트 `scripts/audit/wiki_*_lint.py` + `test_*.py`(fixture 트리, 읽기 전용, `make python-test` 자동 발견 — 라이브 위키는 운영자/주기 실행) (2) `verify.go` advisory(`title_rule` 패턴) (3) 쓰기 가드(Store/wikitool/dreamer). **1차 묶음:** CI 린트 3종 `wiki_layout_lint`(원장 위치·code≠folder·대표 없는 폴더·필드 공백 ratchet — 의도적 비코드 폴더 5개 허용)·`wiki_id_lint`(rep/log id=code·id 날짜≠제목 날짜)·`wiki_dead_link_lint`(부재/malformed/basename-only) + verify advisory 3종 `dead_link`·`empty_page`·`superseded_misuse` + 가드 4종(W1·W2·W5·W10 빈 본문) — 순서는 **가드 → 데이터 수리 → CI 린트 → advisory**. 추가 린트: `wiki_site_gate_lint`(현장 상세가 stage 제안/견적/입찰이면 위반 — `wikiWriteSite`에 stage 검사 0), `wiki_client_lint`(그룹명·법인 접미어·배열), `wiki_log_header_lint`(W17). 조사용 `wiki_dq.py`(18체크, 스크래치)는 `scripts/audit/wiki_dq_lint.py`로 승격해 기준선 표(post-curate)를 ratchet 입력으로. advisory 전용(결정적 수정 금지): summary↔title 약한 불일치 50건(업무 거래처 프로필 정상), 오프토픽 태그 136p/189(영문 슬러그 동의어), 인물 org 불일치.
**`wiki-layout.md` 패치 목록(1회):** 드림 verify 5a~5f 표(5b LLM 오분류 자동이동+W1 가드·5c stale_deadline·5c-2 title_rule·5f unrecalled·#4588 5e-2 스텁·closeStaleDues·수요 게이트·siblingDeals) · 리뷰 태스크 flat 수리·signal-3 도메인(무장 env)·`questionsExpired` · 브리프 소비자 5곳(dreamer·research·scout·noti-digest·supernote) · 레이아웃 트리에 `현장/` 슬롯 + 깊이 허용 형태(자료|회의록|현장) · 메타데이터 캡(related 3/tags 8)·질문 14일 시효 절 · 인물 `pid:` · `RecordMeetingAttendanceByPath` 정정 · cues 수치 19/73 · 90일/120일 규칙에 '발화 전제(데이터 나이, 첫 발화 ~09-11/~10-30)' 주석 · 드리머 create-dedup은 슬롯 생성을 중복으로 보지 않음(W4). `make doc-ref-lint` 0 broken 유지. 35항목 드리프트 표는 조사 산출물(스크래치 `areas/docs-rules-drift.md`)에 있음.

---

## 6. P3 묶음 (W17)

- **로그 헤더 문법/회전 경계:** `## [YYYY-MM-DD] &lt;op&gt; | <주제>`를 강제하는 writer는 site_visit·attendance 둘뿐(드리머·wiki 도구는 자유 텍스트) — 라이브 H2 250 준수/242 비준수('## 요지', '## 핵심'); `project_log.go:RotateProjectLog`는 모든 H2를 섹션으로 세 '20섹션'이 엔트리 ~10개. → `wiki_log_header_lint.py` + 회전 경계를 날짜 헤더만 엔트리로(`splitLogEntries`) + 드리머/`wiki log` 헤더 정규화.
- **날짜형 로그 페이지 정책(운영자 확인 후):** `dreamer_guards.go:isDailyMailDigestPage`는 메일 다이제스트만 막아 모닝레터 3장(1장 0B·1장 id 오염)이 기타에 쌓임; 시스템/rsi-캘리브레이션-하베스트·*-아이디어-날짜(런타임 에이전트) 14장. → 드리머 create 경로 한정 `isDailyLogPage` 확장(다른 레인 시스템 리포트는 차단 금지), 런타임 에이전트 산출물은 단일 페이지+변경 이력 관례 프롬프트 명시, 모닝레터 3장 삭제는 승인 사항. 기타 세상뉴스는 `dreamer_apply.go` 카테고리 가이드상 설계 의도(7번째 카테고리·이동 불필요).
- **inert 규칙 주석·카운터:** 90일 메일분석 보존·120일 졸음 감지는 정확히 구현됐으나 데이터 나이로 0회 발화 — `mail_archive_candidates`/`dormant_candidates` 일일 카운터 기록(발화 시점 동작 확인용).
- **공인 인물 태그**(W8.6) · **프로젝트 화면 stage/질문 칩**(W15, 보류) · **cues 19/73 수치 갱신**(cue 백필 루프 편입은 07-21 벤치 음성으로 기각 — 문서 수치만).
- **dream 제안 원문 보존(디버깅):** `.dream-last-proposal.json`은 카운트뿐이라 kia-001←002 병합의 직접 트리거 필드가 추정으로 남음 — 사이클별 제안 JSON(path/id/title/summary 최소)을 `.dream-proposals/`에 보존할지 결정(결정적 리플레이에도 유리).

---

## 7. 실행 순서 (Now / Next / Later) 와 의존성

**Now (1주, 가드 레인 — 모두 S/M, 데이터 손실 차단):**

> **진척 (2026-08-23 저녁): 1~6 전부 같은 날 착지**(#4600·#4601·#4603·#4605·#4606·#4608·#4611 — 각 절 상태줄 참조). Now 잔여는 W2 2단계·W5 주제 앵커/강등 UX·W6 스냅샷·W9 확장암·골드 도구·W10 링크 복구뿐이며, **현재 최우선 블로커는 Next 7(W7 운영자 결정 §8-1)** — W11.4 선행이 이미 충족됐다. Next 8의 W8-1도 #4615로 착지.

1. W1 (verify 자동이동 가드) — 하루 3사이클마다 재발 가능, 가장 먼저.
2. W3 (동일 ID 병합 조건 + FoldDuplicate 백스톱) + W2 1단계(id/summary fill-only, 리타겟 필드) — 같은 PR 가능(`dreamer_apply.go` #4588 리베이스).
3. W5 데이터 수리(ymn 로그 08-30 전) + `MarkSuperseded` 섀도 가드 + stale 슬롯 스킵.
4. W6 가드 PR(tri-state·archived 필터·스냅샷/캡·메일엣지 비소유·자사명 키) — 무장 상태라 메일 레인 1순위.
5. W9 nightly 복구(go PATH·pipefail·self-check) — 이후 모든 변경의 회귀 감시 전제.
6. W10 `PreserveUpdated` 계약 + W11.4 by-hash refresh — 이후 모든 대량 수리의 선행.

**Next (1개월):**

7. W7 원장 위치 결정(§8) → 골드 26경로 repoint(W9) → 17장 복귀 + wikicurate 규칙 → 린트.
8. W8 인물(1 dossier 캡 버그 먼저 — 기능 버그 S; 2~6 정책).
9. W4 리타겟 수용 조건, W2 2단계(DeriveID·id_mismatch), W10 위키링크 복구·빈 본문 가드.
10. W16 린트 3레인 1차 묶음 + `wiki-layout.md` 패치.
11. W11.1~3 저장/성능(부팅·git·백업).

**Later (분기):** W12·W13·W14 계측/라우팅, W15 표면, W17.

**교차 의존(명시):** 데이터 수리는 **반드시 해당 가드 배포 후**(가드 전 복원은 다음 사이클이 같은 경로로 되돌림 — kia-001/002 유사-ID 원장 항목 잔존) · 원장 이동 전 골드 repoint · 대량 페이지 쓰기(원장 17·인물 ~260·링크 ~150·id 정규화)는 `PreserveUpdated`+by-hash refresh 뒤 **한 번의 스냅샷 배치**로(각각이 `updated` 리셋·181MB 캐시 재기록·재임베딩) · 인물 편집은 gold-neutral이 아님(recall-bench 회귀 확인 필수, 메모리 `recall-measurement-rot` ⑧) · `~/.deneb/wiki` 직접 mv/rm 금지(`Store.MovePage/MergePage` 또는 정지 창 wikicurate) · 가동 중 `cmd/wiki-restructure --apply` 금지.

---

## 8. 운영자 결정 필요 (구현 레인이 막히는 지점)

1. **거래 원장 위치** — (A, 권고) 프로젝트/거래 단일 정본 복귀 + wikicurate '조직→인물' 규칙 제거 vs (B) 인물/업무 원장 인정 + 소비자 전수 개정 (W7). *갱신: 선행인 W1 가드는 #4600 착지, 골드 26경로 repoint는 #4596에서 36경로 착지 — 결정만 남음.*
2. **pl2-kia-epc-002 정본 코드** — 폴더 pl2-kia-epc-002(2팀 EPC) vs 자식 페이지 code pl3-kia-mod-001(3팀 모듈 입찰): 별개 딜이면 자식을 pl3 폴더로 분리할지.
3. **`기타/isw-가족-소유.md` 복원 여부**(세계정세류 기타 페이지 가치) · **모닝레터 3장 삭제** · **날짜형 로그 페이지 정책**.
4. **verify 5b(LLM 오분류) 존치** — 14일간 정당 이동 0·실패 4·토큰 1,169행/사이클: W1 가드+입력 필터 후 1~2주 관찰 → 완전 advisory화 검토. *갱신: W1 가드 #4600 착지(입력 필터 잔여) — 08-23 저녁부터 관찰 시계 진행 중.*
5. **pl1-cny-dev-001 대표 `client: 탑솔라`의 올바른 값**(영농조합법인? 공란?) 및 오배정 5통 정답 프로젝트.
6. **일회성 git 조작**(운영자): `.wiki.db/-shm/-wal`·드리머 원장 3종·`.bak` 12개 `git rm --cached`; 회사 표기 분기(선그로우 3표기·기아 4페이지·현대≈현대에너지솔루션) 정본 표기.
7. **id 계약** — LLM 계약에서 id를 완전히 제거(100% 경로 파생)할지 fill-only로 남길지 (W2.3).

---

## 9. 명시적 비-제안 (Out of Scope / Rejected)

- §2 '닫힌 결론' 전부 · `DENEB_WIKI_REVIEW_AUTOMERGE=1` 무장(관찰 판정 7건뿐) · 리뷰 cap 10 상향 · 관찰 기간 연장 · 코드 기본값 armed · WIKI.md를 챗 컨텍스트 파일로 승격 · 인물 스텁 삭제/LLM 채움/주소록 전원 재시딩/동명이인 자동 분리 · 인물 카테고리 부스트/길이 정규화 · SQLite 벡터 저장·NVFP4 양자화·FTS 영속화·청킹 축소·index/log.md gitignore·git 히스토리 재작성·wiki/.git 백업 제외 · 새 위키 유지보수 패널/탭 · Trust Inbox 재설계·도시에/다이제스트 재제안 · 클라에서 stage/client/sites 직접 편집 · 62회/14d 재시작을 결함으로 취급(랜딩 케이던스) · 골드셋을 레포 안으로 · inject=use 계수 · recall-bench `--content`를 기본값으로(경로 기반과 병행이 정답) · 2026 외부 소스(xMemory·SGER·LLM Wiki v2 confidence/forgetting·TOKI bitemporal 전체) — 전부 S/M 범위에서 현물 대비 이득 없음(TOKI/StateAuditor/Karpathy pin에서 **'연산자 전제조건 + audit row + 사람 교정 pin'** 원칙만 이식 — W1·W3·W5·W17 pin 후보).

---

## 10. 측정·검증 계획 (전후 지표)

| 지표 | 현재(08-23) | 목표 | 측정 |
|---|---|---|---|
| verify auto-move from=프로젝트/* | 21/14d | 0 | 저널 `auto-moved misclassified page` |
| verify auto-merge via 동일 ID(제목 상이) | 5/14d | 0 | 저널 `auto-merged` + verify 원장 |
| dream 커밋의 `-id:` 헝크 | 21/30d | 0 | `git log -p` 에서 `^-id:` 헝크 카운트 |
| 거래 원장 위치 | 프로젝트 55 / 인물 15 / 업무 3 | 프로젝트 100%(A) | `wiki_deal_ledger_lint` |
| superseded_by 오용(슬롯·원장·메일) | 4+ | 0 | `wiki_dq` [o] |
| 깨진 본문 링크 | 149+20 / 517 | 자동 복구분 0 + advisory | `wiki_dead_link_lint` |
| 고아 메일 | 88 | ~80(정직 잔류) | 버킷 카운트 |
| 메일 오배정(자사명 키·메일엣지) | 5+9 | 0 신규 | 저널 `unlinked mail re-filed signal=` |
| nightly recall-health 실행 | 0/45 | 매일 RECALL_BENCH 라인 | step summary |
| main gold p@1/r@8 | 82.0/93.8 | 무회귀(±0.6pp) · dead 0 | `make recall-health` |
| 골드 dead(11셋) | hard 10/27 … | 0 | `gold_repoint.py` + dead-count 라인 |
| 인물 dossier 매칭 | 200/306 | 306/306 | `miniapp.person.dossier {name:"황재훈"}` |
| 프로브 '탑솔라 회사 개요' 4~8위 스텁 | 5 | 0 | 프로덕션 read-only RPC |
| 부팅 wiki enabled→notebook enabled | 2.79s | <0.3s | 저널 |
| `.semantic-cache` 크기 | 181MB | ~30–60MB | ls |
| 위키 git loose | 39MB | ≤12MB | `count-objects -vH` |
| 일일 백업 wiki 부분 | 117MB | ~2MB(+.git) | shipped bytes 로그 |
| client use(read/cite) | ~1/주 | ≥10/주 | `.recall-hits.jsonl` |
| traffic gold | 7 | ≥30 (4주) | `mine_traffic_gold.py` |
| wiki=0(deadline) | 15/87 | <2% | 저널 |

---

## 11. 구현 결과 (2026-08-23, W1~W17 전량 착지)

| 항목 | PR | 무엇이 바뀌었나 |
|---|---|---|
| W1 | [#4600](https://github.com/choiceoh/Deneb/pull/4600) | `IsLayoutManagedPath` 신설 · 자동이동에 경로 존재·레이아웃 소유·`type: deal` 게이트 · 서브폴더 민팅 금지 · move 캡 3 · 사유 로깅 |
| W3 | [#4601](https://github.com/choiceoh/Deneb/pull/4601) | id 동일 **+ 제목 동일**일 때만 자동 병합(그 외 advisory) · `FoldDuplicate` 슬롯/코드/타입 백스톱 |
| W2 | [#4603](https://github.com/choiceoh/Deneb/pull/4603) | id/summary fill-only · `retargetedFrom` · 대표 전용 필드 격리 · 슬롯 페이지 제목 보호 |
| W5 | [#4605](https://github.com/choiceoh/Deneb/pull/4605) | `supersedePrecondition` · stale-superseded 슬롯 스킵 · **라이브 5장 수리**(ymn 로그 08-30 아카이브 저지) |
| W6 | [#4606](https://github.com/choiceoh/Deneb/pull/4606) | 메일↔메일 엣지 비소유 · 자사명 client 키 차단 · tri-state 모호성 · archived 제외 · 도메인 캡 3 |
| W9·W10 | [#4608](https://github.com/choiceoh/Deneb/pull/4608) | nightly recall-health 실행 복구 · `UpdatePageMetaOnly` · 빈 본문 생성 차단 |
| W11 | [#4611](https://github.com/choiceoh/Deneb/pull/4611)·[#4626](https://github.com/choiceoh/Deneb/pull/4626)·[#4633](https://github.com/choiceoh/Deneb/pull/4633) | 이동 시 임베딩 재사용 · 원장/캐시 gitignore+`gc.auto=512` · 백업 캐시 제외 · **벡터 캐시 v4**(매니페스트+float32 blob, v3 자동 마이그레이션) |
| W8 | [#4615](https://github.com/choiceoh/Deneb/pull/4615)·[#4625](https://github.com/choiceoh/Deneb/pull/4625)·[#4627](https://github.com/choiceoh/Deneb/pull/4627) | people 200행 캡 버그(106명 가시화) · 스텁 강등 0.45 + related 노이즈 차단 · 본문 동명이인 병합 중단 + `homonym` advisory |
| W4 | [#4617](https://github.com/choiceoh/Deneb/pull/4617) | 리타겟 수용 조건(슬롯 종류 + 제목 토큰 겹침), 거부 시 제안 경로 유지 |
| W7 | [#4620](https://github.com/choiceoh/Deneb/pull/4620) | 거래 원장 단일 정본(A안) · wikicurate 규칙 교정 · **라이브 18장 복귀** · `wiki_deal_ledger_lint` |
| W15·W16 | [#4622](https://github.com/choiceoh/Deneb/pull/4622) | `get_page` 구조 메타 · 죽은 `wiki.*` RPC 8종 제거 · `wiki-layout.md` verify 5a~5f 표·불변식·신호 표 |
| W13·W17 | [#4623](https://github.com/choiceoh/Deneb/pull/4623) | 라우팅 shape 로깅 · 엔트리 단위 로그 회전 · `wiki_log_header_lint` |
| W12 | [#4625](https://github.com/choiceoh/Deneb/pull/4625) | 확장 발화 Info 로그 · ctx 잔여 600ms 미만이면 확장 생략(1차 결과 보호) |
| W14 | [#4629](https://github.com/choiceoh/Deneb/pull/4629) | cite 매처가 프론트매터 title·프로젝트 코드까지 인식(메시지 ID 파일명 문제 해소) |

**측정.** 고쳐진 `make recall-health`가 도입 이래 처음 완주 — main gold 198케이스 **p@1 83.2 · r@8 95.9 · mrr 0.885**, 로스터 커버리지 **98.6%**, 종합 **92.6**. 거래 원장 린트 0건. 인물 골드 36개 중 스텁은 1개이고 그 케이스의 정답은 동반 골드 경로에 있어 강등이 무해함을 구조적으로 확인.

**운영 사고 1건.** 검증 벤치를 병렬로 돌려 메모리 여유가 6%까지 떨어지자 earlyoom이 `nemotron-embed.service`(:8002)를 종료했고, 그 시간대 프로덕션 회상이 BM25로 강등돼 있었습니다. 재시작으로 복구 — **벤치는 순차 실행**이 규칙.

---

## 12. 잔여 (다음 사이클)

> **후속 구현 (2026-08-23 야간, PR 5건 착지).** §12에 적혀 있던 자동 항목은 아래 표로 소진됐고, 남은 것은 운영자 결정과 P3 이하 항목뿐이다.

| 항목 | PR | 무엇이 바뀌었나 |
|---|---|---|
| W9 | [#4638](https://github.com/choiceoh/Deneb/pull/4638) | 골드셋(`wiki-qa-gold*.jsonl`) 백업 편입(글롭 타깃 지원) · `scripts/audit/gold_repoint.py` — git rename 체인/유일 basename/유일 title/프로젝트 폴더 title 4단계, 모호하면 보고만. 라이브 적용 **repointed 14 · unresolved 0** |
| W15 | [#4639](https://github.com/choiceoh/Deneb/pull/4639) | `Store.HomonymPersonPages` + wiki-maint Trust Inbox **동명이인 카드**(증거만, apply 액션 없음, 결정 키 `homonym:` 네임스페이스) |
| W11 | [#4640](https://github.com/choiceoh/Deneb/pull/4640) | 벡터 blob 포맷을 `pkg/vectorutil`로 단일화하고 **embedindex 캐시 v3**(매니페스트+`.f32`)로 이식 — mail 89MB·diary 76MB·workfeed 5MB 대상. blob 먼저 쓰고 매니페스트 나중, blob 유실/절단 시 캐시 전량 폐기 후 재임베딩 |
| W8 | [#4641](https://github.com/choiceoh/Deneb/pull/4641) | `DuplicatePersonGroups` — 직급·괄호 수식어를 벗긴 기본 이름으로 인물 페이지 중복 검출, `person-duplicate` advisory(Fix 없음). 라이브 3그룹(이영민 3장·박상수 2장·선향란 2장) |
| W13 | [#4642](https://github.com/choiceoh/Deneb/pull/4642) | `RecordRoutingOutcome` — 라우팅 힌트가 가리킨 도구를 그 턴이 실제로 불렀는지 대조해 `followed` 로깅(consume-once, 힌트 없는 턴은 슬롯 비움) |

**측정(골드 repoint 후 재실측).** main gold 198케이스 **p@1 83.8 · r@8 96.4 · mrr 0.890**, 커버리지 98.6%, 종합 **92.9** (repoint 전 83.2/95.9/92.6). 벤치는 규칙대로 단일 순차 실행.

**남은 것**

- **W8**: summary 규약, 공인 `kind: public`. 중복 인물 그룹의 실제 병합은 운영자 판단(#4641은 탐지까지).
- **W9**: 벤치 확장암 패리티(qwen vs 프로덕션 dsv4-nothink), analysis-xl 재측정. 나이틀리 로그에서 확인된 부작용: 확장 모델이 **씽킹으로 출력 예산을 소진해 빈 응답**을 내는 사례가 반복된다(`expansion skipped (finish_reason=length)`) — 확장 경로 MaxTokens/씽킹 설정 점검 필요.
- **W11**: 부팅 병렬 로드(blob 전환으로 파싱이 memcpy가 됐으므로 실측 후 필요할 때만), `filestore/semindex`(40MB)는 별도 구현이라 미이식.
- **W13/W14**: `rawQuery`(client 세션 한정·절단), `followed` 로그가 쌓인 뒤 힌트 문구 튜닝.
- **W15**: 인물 중복 카드(#4641 finding의 카드 표면), 챗 `wiki_move` 툴.
- **운영자 결정 대기**(§8): ISW 페이지 복원, 모닝레터 3장, 날짜형 로그 정책, pl2-kia-epc-002 정본 코드, pl1-cny-dev-001 client, 추적 중인 `.bak`·`.wiki.db` `git rm --cached`.

---

## 13. 변경 로그

| 날짜 | 작성자 | 내용 |
|---|---|---|
| 2026-08-23 | Claude (Fable 5) | 초안 — 10영역 조사 + 3렌즈 검증 결과를 17항목(W1~W17)으로 정리; 같은 날 랜딩된 #4583/#4587/#4588/#4589/#4593·wikicurate 2회·무장 드롭인 반영; kia-002 자식 페이지 id 충돌 응급조치 기록 |
| 2026-08-23 | ZCode (GLM-5.3) | 1차 상태 갱신 — 초안 직후 저녁까지 착지된 W1·W2(1단계)·W3·W5(수리 포함)·W6 가드·W8-1·W9(나이틀리)·W10(계약)·W11-4를 #4600·#4603·#4601·#4605·#4606·#4615·#4608·#4611로 표기(§0 상태 칼럼·각 절 상태줄·§2 표·§7~8 갱신). 회상 재측정 #4596·멀티턴 재작성 #4604·드리머 5.7/5.8/후속 #4594·#4598·#4612 반영. 원문 진단·증거는 스냅샷 그대로 유지 |
| 2026-08-23 | Claude (Fable 5) | 3차 — 후속 구현 5건 착지(#4638 골드 백업·repoint · #4639 동명이인 카드 · #4640 embedindex blob · #4641 인물 중복 탐지 · #4642 라우팅 준수율). §12를 후속 결과표 + 잔여로 재작성, 골드 repoint 후 재실측(92.9) 기록 |
| 2026-08-23 | Claude (Fable 5) | 2차 — W4·W7·W12·W13·W14·W15·W16·W17 및 W8/W10/W11 잔여까지 전량 구현·랜딩(PR 14건). §11 구현 결과·§12 잔여 신설, Status를 implemented로 |
