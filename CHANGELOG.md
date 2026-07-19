# Changelog

## [4.124.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.123.0...deneb-v4.124.0) (2026-07-19)


### ✨ Features

* **andromeda:** TS state-register — tsc 타입체커 기반 워크스테이션 상태 w/r 맵 ([#4003](https://github.com/choiceoh/Deneb/issues/4003)) ([f5674b1](https://github.com/choiceoh/Deneb/commit/f5674b159b694e37f5f8a8271431e171e6b4df5c))
* **andromeda:** 그리드 행 우클릭 컨텍스트 메뉴 (메일·할일) — 감사 W7 잔여분 ([#3959](https://github.com/choiceoh/Deneb/issues/3959)) ([31a2fe5](https://github.com/choiceoh/Deneb/commit/31a2fe53d8a1928fe7c4a5597859c80331ee104a))
* **audit:** add analysis-path recall gold-set miner + doc/baseline refresh ([#3969](https://github.com/choiceoh/Deneb/issues/3969)) ([6074aa1](https://github.com/choiceoh/Deneb/commit/6074aa116a7522ea71d8d74ef923d58325cba2b7))
* **audit:** doc-ref-lint — 문서 코드참조 validate-or-freeze 게이트 (arXiv:2607.13285) + 실측 rot 수정 ([#3966](https://github.com/choiceoh/Deneb/issues/3966)) ([835e7bc](https://github.com/choiceoh/Deneb/commit/835e7bcf09504be7acbdaf7c929413dfee2ff8e2))
* **audit:** doc-ref-lint 경고 전수 클린 — 휴리스틱 개선 + 문서 rot 수리 ([#3991](https://github.com/choiceoh/Deneb/issues/3991)) ([4453301](https://github.com/choiceoh/Deneb/commit/445330129a0b51cc173ad6575b88ef162185df09))
* **audit:** state-register 생성기 — 세션 공유상태 크로스-패키지 read/write 맵 ([#3967](https://github.com/choiceoh/Deneb/issues/3967)) ([4266ed7](https://github.com/choiceoh/Deneb/commit/4266ed70c45a9ad543ecbf1ed5de2fd8c12eee2b))
* **audit:** 라인 앵커 드리프트 검출 — 심볼 범위 대조 + --fix 스냅 (주간 자가감사 크론 등재) ([#3976](https://github.com/choiceoh/Deneb/issues/3976)) ([3263fdc](https://github.com/choiceoh/Deneb/commit/3263fdc133b74e4a775a1de61edb503d2f127765))
* **audit:** 서칭·문서정합 배치 — --fix 앵커수리·메모리 감사·미언급 큐레이션·workfeed state-register ([#3973](https://github.com/choiceoh/Deneb/issues/3973)) ([8e96a65](https://github.com/choiceoh/Deneb/commit/8e96a65648f6548362181819c69f3e3f1ba47295))
* **browse:** 사이드카 systemd 자동재시작 + env 충돌 분리(DENEB_BROWSE_URL) ([#3965](https://github.com/choiceoh/Deneb/issues/3965)) ([9997378](https://github.com/choiceoh/Deneb/commit/9997378a41dc13fa2e78099a7c2d55ea752e3699))
* **browse:** 상주 실브라우저 사이드카 + browse 도구 — 로그인 벽 페이지 서버측 열람 ([#3957](https://github.com/choiceoh/Deneb/issues/3957)) ([81e8e69](https://github.com/choiceoh/Deneb/commit/81e8e69976440c0a1c87d6942a92e375d77fa437))
* **chat:** code_search 네이티브 런타임 도구 — 시맨틱 코드 검색을 codegraph_explore 동급으로 승격 ([#4011](https://github.com/choiceoh/Deneb/issues/4011)) ([8beb6bf](https://github.com/choiceoh/Deneb/commit/8beb6bff285ad0a909a96e944e9c795a3071511b))
* **chat:** 리치 응답 기본화 — 실질 내용이면 카드/페이지가 디폴트, 프로즈는 짧은 대화용 ([#3941](https://github.com/choiceoh/Deneb/issues/3941)) ([d05caa2](https://github.com/choiceoh/Deneb/commit/d05caa21cce24135c9fb16230a21285deb6a4ef2))
* **client-android:** Kotlin state-register — K1 프론트엔드 타입해석 상태 w/r 맵 ([#4006](https://github.com/choiceoh/Deneb/issues/4006)) ([9607e00](https://github.com/choiceoh/Deneb/commit/9607e00cc3e322e9f3354d04ada5e0b0c6172f40))
* **codesearch:** XProvence 리랭커 연결 — 위키 회상 공용 사이드카(:8004)로 융합 상위 재정렬 (P@1 10→11/13) ([#3999](https://github.com/choiceoh/Deneb/issues/3999)) ([3016c55](https://github.com/choiceoh/Deneb/commit/3016c55645f4c6809835a905d70cf865824dd9e9))
* **codesearch:** 시맨틱 코드 검색 — Nemotron 임베딩 + CodeGraph FTS RRF 융합 ([#3984](https://github.com/choiceoh/Deneb/issues/3984)) ([2e59ddc](https://github.com/choiceoh/Deneb/commit/2e59ddc02d834a343975ebc1b51417de1f7c1a43))
* **deploy:** 배포 시 시맨틱 코드 인덱스 증분 갱신 — code_search 런타임 도구 프로드 신선도 ([#4013](https://github.com/choiceoh/Deneb/issues/4013)) ([313115b](https://github.com/choiceoh/Deneb/commit/313115b387b95c9d836e04bd3b1eebb4c844374d))
* **dev:** rpcmap→CodeGraph 합성 엣지 — 문자열-키 디스패치를 callers/impact에 편입 ([#3972](https://github.com/choiceoh/Deneb/issues/3972)) ([886056f](https://github.com/choiceoh/Deneb/commit/886056fc3b7d561b0821a368e3d76af5f752dd72))
* **dev:** 시맨틱 코드 인덱스 신선도 — codegraph sync 훅에 증분 재임베딩 편승 (120s 디바운스·헬스 게이트) ([#3996](https://github.com/choiceoh/Deneb/issues/3996)) ([64bb1ae](https://github.com/choiceoh/Deneb/commit/64bb1ae36aefab0bd9b33391044d9acc96f823a0))
* **embedding:** graph/bm25 RRF weight knobs + adapter long-input truncation ([#3960](https://github.com/choiceoh/Deneb/issues/3960)) ([c0a259a](https://github.com/choiceoh/Deneb/commit/c0a259a3ad97982536e297682f5d4a4feff33c6a))
* **embedding:** Nemotron cutover plumbing — DENEB_EMBEDDING_URL lever, query/passage role wiring, Nemotron sidecar + unit ([#3955](https://github.com/choiceoh/Deneb/issues/3955)) ([0bbebb9](https://github.com/choiceoh/Deneb/commit/0bbebb9b2daa3f6e5890fedf4e03930db2c67154))
* **embedding:** NVFP4 serving via eugr container + RRF semantic weight knob ([#3956](https://github.com/choiceoh/Deneb/issues/3956)) ([ae9adc8](https://github.com/choiceoh/Deneb/commit/ae9adc8639f5333d1d739ede29fc9aae11aa5f71))
* **genesis:** incumbent-only bench on skip cycles — DENEB_META_BENCH_ON_SKIP calibration knob ([#3985](https://github.com/choiceoh/Deneb/issues/3985)) ([4e3811a](https://github.com/choiceoh/Deneb/commit/4e3811ad9e07ffbdd231cd1febe53724ef9080f8))
* **genesis:** kb-interview 선제 제안 카드 — 위키 지식도메인 갭 감지→워크피드 질문 카드 (P5 수요생성) ([#4000](https://github.com/choiceoh/Deneb/issues/4000)) ([69d98e8](https://github.com/choiceoh/Deneb/commit/69d98e8eb57615bab7a31adeb7aa8314a47e28bd))
* **native:** browser translate cache — segment LRU so revisited pages apply instantly ([#3944](https://github.com/choiceoh/Deneb/issues/3944)) ([93df848](https://github.com/choiceoh/Deneb/commit/93df8489fce75d02b25b6753cb6ccc7f86a935ac))
* **native:** persist browser translate cache across app restarts (encrypted section store) ([#3948](https://github.com/choiceoh/Deneb/issues/3948)) ([1d00c27](https://github.com/choiceoh/Deneb/commit/1d00c27137cb2e6803b1b58e08782e0ef61362bf))
* **native:** 결재 첨부 열람 + 오프라인 일기·인물 검색 — 모바일 파리티 갭 2건 ([#3981](https://github.com/choiceoh/Deneb/issues/3981)) ([d22aa10](https://github.com/choiceoh/Deneb/commit/d22aa10dc857b2dafaf89d37c9599122c2ea9cb8))
* **recall:** recall-bench --content — 콘텐츠 인지형 히트 판정 (폴더개명 강건) ([#4001](https://github.com/choiceoh/Deneb/issues/4001)) ([f83785b](https://github.com/choiceoh/Deneb/commit/f83785b0fadce55a4d5824daf0e420303095e8a9))
* **recall:** recall-bench --dump-signals — per-case 신호·per-모드 랭킹 덤프 (오프라인 재융합 스윕 기반) ([#3987](https://github.com/choiceoh/Deneb/issues/3987)) ([3a174fc](https://github.com/choiceoh/Deneb/commit/3a174fc892e14a900ed3b476e386a7a7d1966241))
* **recall:** recall-bench에 카테고리×모드 진단(--by-category) + held-out 분할(--holdout-pct/--split) ([#3982](https://github.com/choiceoh/Deneb/issues/3982)) ([f402b23](https://github.com/choiceoh/Deneb/commit/f402b23ae32068f2c2af8e74d489e336535711f1))
* **retrieval:** extend semantic ranking across workflows ([#4004](https://github.com/choiceoh/Deneb/issues/4004)) ([d4c9c6c](https://github.com/choiceoh/Deneb/commit/d4c9c6c81bd75cd57806f4196a9b1d01c462cdd4))
* **rsi:** per-epoch calibration bench target — producer 5 vs default 10 (P5-2 window) ([#4005](https://github.com/choiceoh/Deneb/issues/4005)) ([6459741](https://github.com/choiceoh/Deneb/commit/64597419c5b890c407b017cd306556a4e72f7106))
* **rsi:** Polaris 재개 왕복 테스트 + runtime-error 공급 파이프 수리 + 캘리브레이션 하베스트 자동화 ([#3983](https://github.com/choiceoh/Deneb/issues/3983)) ([b907439](https://github.com/choiceoh/Deneb/commit/b9074395d0311b0a4d9dfb87b1fa7297aedc898e))
* **skills:** make bundled skills deletable via persistent tombstones ([#3977](https://github.com/choiceoh/Deneb/issues/3977)) ([0994f8a](https://github.com/choiceoh/Deneb/commit/0994f8a7771df4b80ba6d734bbbf6858912fcf61))
* **state-register:** go/types 타입체크 v2 — 과소근사 제거 (미해석 1164→0건, 사이트 190→265) ([#3994](https://github.com/choiceoh/Deneb/issues/3994)) ([078e0f9](https://github.com/choiceoh/Deneb/commit/078e0f91d5666d5e016dd0955939b8eac800a0f8))
* **web:** detailed chunked YouTube summarization — full-transcript coverage, parallel per-segment passes ([#3943](https://github.com/choiceoh/Deneb/issues/3943)) ([fe529de](https://github.com/choiceoh/Deneb/commit/fe529de250287c503a8ab9628af10d9ec37cd294))
* **wiki:** demand-grounded dreamer + freshness-SLO research targeting + repair worklist ([#3988](https://github.com/choiceoh/Deneb/issues/3988)) ([4178b51](https://github.com/choiceoh/Deneb/commit/4178b51eae6781e36001751b0e73f4e4c9a1106a))
* **wiki:** metadata-contamination verification loop + nightly recall-health/worklist + multi-project mail linking nudge ([#3993](https://github.com/choiceoh/Deneb/issues/3993)) ([764c080](https://github.com/choiceoh/Deneb/commit/764c080c22306826ec211830d90883978aa011f4))
* **wiki:** program axis + code-folder display aliases + filing/naming conventions (Phase A) ([#3997](https://github.com/choiceoh/Deneb/issues/3997)) ([b6656ce](https://github.com/choiceoh/Deneb/commit/b6656ce9e27b1ee0503773d32d1e57051f67bd71))
* **wiki:** project stage field (제안→운영 lifecycle) + site-docs stage gate ([#3986](https://github.com/choiceoh/Deneb/issues/3986)) ([b4d6849](https://github.com/choiceoh/Deneb/commit/b4d6849ab36fa1133d5a2582735011c50069cee8))
* **wiki:** stage vocabulary — 개발(자체개발 인허가) + 납품(기자재 이행 트랙) ([#3990](https://github.com/choiceoh/Deneb/issues/3990)) ([82cc66b](https://github.com/choiceoh/Deneb/commit/82cc66b9ae2ca6085b81f56d8cf6b526ce20745f))
* **wiki:** 크로스인코더 리랭크 실배선 — xprovence 사이드카 + 페이지헤드 문서 + force knob (P@1 +3.4pp) ([#3992](https://github.com/choiceoh/Deneb/issues/3992)) ([37b76c3](https://github.com/choiceoh/Deneb/commit/37b76c33ccc1c72ff7069dbc61fda4f7e283a4be))
* **workstation:** 활용 2탄 — 알림 복귀 내비·모닝 브리핑 투어·효용 원장 관찰 카드 ([#3951](https://github.com/choiceoh/Deneb/issues/3951)) ([97f97df](https://github.com/choiceoh/Deneb/commit/97f97dffdd9e3e28d0eae45f41a86f25d4712ca7))
* **workstation:** 활용 3탄 — 결재 검토 모드·컨텍스트 팔로우·효용 원장 자기조정 ([#3954](https://github.com/choiceoh/Deneb/issues/3954)) ([dbea2df](https://github.com/choiceoh/Deneb/commit/dbea2df5fff773c98517b7ec6c2ff09cb355b00f))
* 모닝레터 마감줄 롱프레스 완료 처리 (deneb-ui longpress + 위키 due_done) ([#3979](https://github.com/choiceoh/Deneb/issues/3979)) ([6200730](https://github.com/choiceoh/Deneb/commit/620073020c3e24ce6542f9b3a01a412a14f1fe83))


### 🐛 Bug Fixes

* **audit:** doc-ref-lint 검출 품질 — 취소선/외부심볼/파일텍스트 폴백 + 심볼 rot 2건 수정 ([#3971](https://github.com/choiceoh/Deneb/issues/3971)) ([4cb2b44](https://github.com/choiceoh/Deneb/commit/4cb2b44ec219306fc643549dab85ba22dae6bdfc))
* **audit:** Harness Handbook 이식 자기감사 보강 — 실CI 배선·모호참조 표면화·short-var 전파 ([#3968](https://github.com/choiceoh/Deneb/issues/3968)) ([23396b3](https://github.com/choiceoh/Deneb/commit/23396b3266f67315fda69f544833425a6cbd8b81))
* **chat:** auto-steer ignores automation runs riding the user's session key ([#3945](https://github.com/choiceoh/Deneb/issues/3945)) ([802cbd2](https://github.com/choiceoh/Deneb/commit/802cbd213ee42b3eae00eb30b1a4578b7786102c))
* **compaction:** 보존지침 근접중복 병합 — 5슬롯 표현마모 방지 ([#3974](https://github.com/choiceoh/Deneb/issues/3974)) ([7b2e2be](https://github.com/choiceoh/Deneb/commit/7b2e2beee6db890b60bd77c6a20008a247744d33))
* **deploy:** auto-deploy watcher ack 대기 15→90s — deploy-watch handoff 예산(75s) 정렬로 허위 unverified ERROR 제거 ([#4008](https://github.com/choiceoh/Deneb/issues/4008)) ([a819cdb](https://github.com/choiceoh/Deneb/commit/a819cdb6c71616bf9e8ee4c0628aa9592131e35b))
* **deploy:** setup-nemotron-embed restarts active units (enable --now skips running units) ([#3963](https://github.com/choiceoh/Deneb/issues/3963)) ([bb076ac](https://github.com/choiceoh/Deneb/commit/bb076ac4203d436676c0656db292b2dd2b14469c))
* **document:** OCR 표 반복 루프 감지 → Table Recognition 재시도 폴백 ([#3949](https://github.com/choiceoh/Deneb/issues/3949)) ([9c39f03](https://github.com/choiceoh/Deneb/commit/9c39f03a386fffa9842ebfd52d3c4fad55508853))
* **genesis:** raise skill-relevance classifier MaxTokens — reasoning models ignore thinking=disabled and starved the verdict ([#4007](https://github.com/choiceoh/Deneb/issues/4007)) ([bf2d5e8](https://github.com/choiceoh/Deneb/commit/bf2d5e8e7ccde73a86d678f16fb0be0484fb875f))
* **hooks:** 동시성 가드 차단 메시지 명확화 — 사용자 취소와 구분 ([#3975](https://github.com/choiceoh/Deneb/issues/3975)) ([d109424](https://github.com/choiceoh/Deneb/commit/d109424920aeaaf82ede9c83c9192ef8758e5d8c))
* **mailanalysis:** 분석 도입 카드 압축 규칙 — 메타 라벨-값 나열·제목 재기술 금지, stat 최대 2개 ([#3980](https://github.com/choiceoh/Deneb/issues/3980)) ([9945348](https://github.com/choiceoh/Deneb/commit/9945348a892e12d8708e2e23d1edf37554c1ff8f))
* **native:** pin wiki mirror owner on bulk refresh to block cred-switch leak ([#3935](https://github.com/choiceoh/Deneb/issues/3935)) ([78d18e6](https://github.com/choiceoh/Deneb/commit/78d18e6378db1c7100320f3a6a4a380516bc4edf))
* **native:** swap browser bookmark actions — bar opens the list, add/remove moves to More ([#3953](https://github.com/choiceoh/Deneb/issues/3953)) ([4f52cb1](https://github.com/choiceoh/Deneb/commit/4f52cb13a771198754dae4469e6f65cbab8a4255))
* **native:** 채팅 메시지 중복 겹침 수정 — 트랜스크립트 id 결정화로 재로드 시 re-key 방지 ([#3964](https://github.com/choiceoh/Deneb/issues/3964)) ([c1add65](https://github.com/choiceoh/Deneb/commit/c1add65e465eec1586046b5512bde92121b19545))
* **recall:** recalibrate semantic cosine floors for the Nemotron embedder ([#3970](https://github.com/choiceoh/Deneb/issues/3970)) ([d1d5a94](https://github.com/choiceoh/Deneb/commit/d1d5a94481a63e699f154a42be1d1375c38b4395))
* **server:** HTTP 응답 인코딩 실패 클라 이탈은 Debug로 강등 — httputil.LogEncodeError 분류 헬퍼 ([#4009](https://github.com/choiceoh/Deneb/issues/4009)) ([677dcf8](https://github.com/choiceoh/Deneb/commit/677dcf86316bb0a24736d4e06959bc9e8de7fa27))
* 런타임 실측 버그 일괄 수리 — embed 어댑터 클램프·캐시 디렉토리·스플래시 NPE·디스패치 타이머 문서 ([#3978](https://github.com/choiceoh/Deneb/issues/3978)) ([63ad489](https://github.com/choiceoh/Deneb/commit/63ad489dfa84dd8d2333968daf63a9105e79fccd))


### ⚡ Performance

* **document:** OCR 콘텐츠 해시 결과 캐시 — 동일 첨부 재-OCR 0ms ([#3952](https://github.com/choiceoh/Deneb/issues/3952)) ([0a4cdb3](https://github.com/choiceoh/Deneb/commit/0a4cdb3fdabc6c68599a7d3b72eda7f7a69287ba))
* **embedding:** raise Nemotron max-model-len 4096→8192 (long diary entries embed unclipped) ([#3962](https://github.com/choiceoh/Deneb/issues/3962)) ([315188a](https://github.com/choiceoh/Deneb/commit/315188a8f2152fe15c5410fcc8912ce4c1d2a4c5))
* **gateway:** 메일 리스트 stale-while-revalidate + 웹 FetchCache TTL 30분 ([#3947](https://github.com/choiceoh/Deneb/issues/3947)) ([53c4e20](https://github.com/choiceoh/Deneb/commit/53c4e209203f0ad46f6884e386ab5227573574ce))
* **retrieval:** stabilize semantic ranking pipelines ([#4014](https://github.com/choiceoh/Deneb/issues/4014)) ([c895d6e](https://github.com/choiceoh/Deneb/commit/c895d6e4e0fa4cbbce2227171c3e8d5be4dc5bf6))
* **web:** cache detailed YouTube summaries by transcript hash (repeat links skip the multi-minute pipeline) ([#3946](https://github.com/choiceoh/Deneb/issues/3946)) ([ca00f19](https://github.com/choiceoh/Deneb/commit/ca00f19cf4ac1275f87045cd8635e0a128f29ed3))

## [4.123.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.122.0...deneb-v4.123.0) (2026-07-18)


### ✨ Features

* add failure intervention shadow router ([#3921](https://github.com/choiceoh/Deneb/issues/3921)) ([68eef0a](https://github.com/choiceoh/Deneb/commit/68eef0a4066cb4f27e0c06ed1415425f69898c42))
* **andromeda:** 인터랙티브 카드 응답 표시·회신 개선 — 카드 응답 말풍선 + 최신 답변만 콜백 허용 ([#3933](https://github.com/choiceoh/Deneb/issues/3933)) ([56b42be](https://github.com/choiceoh/Deneb/commit/56b42be58b8684eb7a8f11727304d18e065becd6))
* **chat:** deneb-html 스타일 다양화 — 테마 3종 + 유틸리티 마이크로 디자인 시스템 ([#3939](https://github.com/choiceoh/Deneb/issues/3939)) ([b88fc17](https://github.com/choiceoh/Deneb/commit/b88fc17f73f00c17bfdd4e3ff9063ec371c12e56))
* **chat:** deneb-html 클라이언트 베이스 스타일시트 — 일관 타이포 + 생성 3배 단축 ([#3938](https://github.com/choiceoh/Deneb/issues/3938)) ([bca0693](https://github.com/choiceoh/Deneb/commit/bca0693fb6830b503668c1a1681721ed96ce9d18))
* **chat:** 웹페이지형 인터랙티브 응답(deneb-html) + 카드 저작 인라인 계약·열화 구제 ([#3936](https://github.com/choiceoh/Deneb/issues/3936)) ([7f6b5b6](https://github.com/choiceoh/Deneb/commit/7f6b5b63054e65ad35a646030d2589b3c5a854c2))
* **chat:** 카드 발명태그 별칭화(3구현)·deneb-html 프리뷰 누출 방지·생성중 스켈레톤 ([#3937](https://github.com/choiceoh/Deneb/issues/3937)) ([8c0f5f4](https://github.com/choiceoh/Deneb/commit/8c0f5f479143243621cb12d44d2dd33163fb180c))
* **dev:** rebuild concurrency guard with repo-canonical source + user-hook shim ([#3918](https://github.com/choiceoh/Deneb/issues/3918)) ([96f4862](https://github.com/choiceoh/Deneb/commit/96f4862ac92683d4144d4b0b08bbfeb632dfc14e))
* **dev:** 동시작업 가드 프로드 Bash 쓰기 탐지 + ci-check 셸 테스트 [#3917](https://github.com/choiceoh/Deneb/issues/3917) 정합 ([#3930](https://github.com/choiceoh/Deneb/issues/3930)) ([4cb3274](https://github.com/choiceoh/Deneb/commit/4cb327439521cb9c61f124b9d5a6d3f08efbf3ee))
* **gateway:** 결재 분석 전례 대조 — 과거 유사 결재 회상 주입 (프롬프트 v5) ([#3922](https://github.com/choiceoh/Deneb/issues/3922)) ([18a4215](https://github.com/choiceoh/Deneb/commit/18a4215eb7c794b364ec4ff1b4106752169f93c4))
* **genesis:** deterministic backlog drain — route=genesis opportunities become skills ([#3932](https://github.com/choiceoh/Deneb/issues/3932)) ([c0659c2](https://github.com/choiceoh/Deneb/commit/c0659c20f120e602966a0baf784592e41c984e98))
* **genesis:** runtime-error miner — restart-surviving rolling window + warn-level rescue signatures ([#3926](https://github.com/choiceoh/Deneb/issues/3926)) ([6efab0c](https://github.com/choiceoh/Deneb/commit/6efab0caeafd217c8cd25c53d516570eb0af54e4))
* **morning:** fix layout and let main fill semantic slots ([#3925](https://github.com/choiceoh/Deneb/issues/3925)) ([f78ce27](https://github.com/choiceoh/Deneb/commit/f78ce2775352d9c80fba698880b8711bb4c9771b))
* **rsi-bench:** RHAE-style attempt-efficiency weighting on dispatch-land ([#3940](https://github.com/choiceoh/Deneb/issues/3940)) ([4ee53d8](https://github.com/choiceoh/Deneb/commit/4ee53d8ed1b95ec37922b491798ab5f19c6dcc95))
* **workstation:** 도구 활용 확장 — 능동 지침·spotlight·date 점프·todo prefill·계측 ([#3927](https://github.com/choiceoh/Deneb/issues/3927)) ([bee77c5](https://github.com/choiceoh/Deneb/commit/bee77c564001d082cace6ca0ef7ccc7a55f889fd))
* **workstation:** 활용 2탄 — 알림 복귀 내비·모닝 브리핑 투어·효용 원장 관찰 카드 ([#3931](https://github.com/choiceoh/Deneb/issues/3931)) ([fa69236](https://github.com/choiceoh/Deneb/commit/fa69236699b6998b10caa6f9d6175408a8e2d67e))


### 🐛 Bug Fixes

* **andromeda:** finish window.confirm sweep — 현장지도 날짜질문·업데이터 재시작을 앱 다이얼로그로 ([#3924](https://github.com/choiceoh/Deneb/issues/3924)) ([d7cea05](https://github.com/choiceoh/Deneb/commit/d7cea058ba2dcee5fff5101049b5a55ea5e19f42))
* **andromeda:** 분할 스트립·데네브 패널 리오픈 탭 겹침 해소 ([#3923](https://github.com/choiceoh/Deneb/issues/3923)) ([0ec0d5a](https://github.com/choiceoh/Deneb/commit/0ec0d5a05ca8b91cde61863d54eef00da1eccf4e))
* **audit:** honest bounded-step dispatch contract for incremental structural findings ([#3934](https://github.com/choiceoh/Deneb/issues/3934)) ([150a1de](https://github.com/choiceoh/Deneb/commit/150a1dec894e52d7acc25b6ec2aeba8e9685129d))
* **native:** make local caches transactional ([#3928](https://github.com/choiceoh/Deneb/issues/3928)) ([72bcd55](https://github.com/choiceoh/Deneb/commit/72bcd55b8bc8e3c9756c145c1855f3540229d187))


### 🔧 Internal

* **android:** centralize persisted JSON recovery ([#3920](https://github.com/choiceoh/Deneb/issues/3920)) ([b19b4ee](https://github.com/choiceoh/Deneb/commit/b19b4ee2c0fe71b5e6f9937ab7ddc5787e105ffd))

## [4.122.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.121.0...deneb-v4.122.0) (2026-07-18)


### ✨ Features

* **andromeda:** undo 토스트·앱 컨펌 통일 + 폴리시 묶음 + 코드 신택스 하이라이트 ([#3913](https://github.com/choiceoh/Deneb/issues/3913)) ([1a9294f](https://github.com/choiceoh/Deneb/commit/1a9294fac2a45ec00ca755218941eb90ed71cfed))
* **andromeda:** 줌(Ctrl+휠 영속) + 전역 오프라인 배너 ([#3910](https://github.com/choiceoh/Deneb/issues/3910)) ([5609e03](https://github.com/choiceoh/Deneb/commit/5609e03ead4572bb343088d0b70f5852309bd34e))
* **andromeda:** 챗 파리티 — 응답 복사·빈 상태 + 편집-재전송·응답 변형 ‹n/N› ([#3906](https://github.com/choiceoh/Deneb/issues/3906)) ([368e607](https://github.com/choiceoh/Deneb/commit/368e60779a7861bdc5173409d4bd0ca841b92adc))
* **andromeda:** 첨부 스테이징·썸네일 + 데스크톱 상주성 (트레이·OS알림·배지·창영속·단일인스턴스) ([#3908](https://github.com/choiceoh/Deneb/issues/3908)) ([b4decee](https://github.com/choiceoh/Deneb/commit/b4decee39f919f829bd78a46af164407cec99f4e))
* **modelpicker:** per-model rolling 24h usage in the picker (runs, tokens, cache reads) ([#3894](https://github.com/choiceoh/Deneb/issues/3894)) ([fc8c9ad](https://github.com/choiceoh/Deneb/commit/fc8c9adf7a0ca7ad9a8b27bf6c5b718cedf8fa17))
* **modelrole:** main falls back to the coding subscription before local lightweight ([#3892](https://github.com/choiceoh/Deneb/issues/3892)) ([42921dc](https://github.com/choiceoh/Deneb/commit/42921dc6c45d375231428e031a08e625df41a8a1))
* **notify:** sidecar health woven into heartbeat — down/recovery alerts ([#3914](https://github.com/choiceoh/Deneb/issues/3914)) ([9e4a497](https://github.com/choiceoh/Deneb/commit/9e4a497f0e4bb947e839eb62e77d4447c14bc45e))
* **sessions:** 대화 이름변경 RPC + 드로어 검색·더 보기 (miniapp.sessions.rename) ([#3907](https://github.com/choiceoh/Deneb/issues/3907)) ([38762f9](https://github.com/choiceoh/Deneb/commit/38762f9dd66edad643cc06170d33a18a24b5f9de))
* **wormhole:** kimi request-shaping profile — translator quirk normalization + 400 diagnostics ([#3903](https://github.com/choiceoh/Deneb/issues/3903)) ([a061034](https://github.com/choiceoh/Deneb/commit/a061034ecd172fa879ab86d39e46d08b75cb0eec))


### 🐛 Bug Fixes

* **android:** guard offline wiki mirror bulk refresh against wipe and cred-switch leak ([#3897](https://github.com/choiceoh/Deneb/issues/3897)) ([81a3963](https://github.com/choiceoh/Deneb/commit/81a39631e31c8f783abd2c4790ee203d6c4213ab))
* **andromeda:** Cargo.lock에 W6 플러그인 크레이트 반영 (rust --locked 레인 복구) ([#3912](https://github.com/choiceoh/Deneb/issues/3912)) ([ecb7899](https://github.com/choiceoh/Deneb/commit/ecb7899f1d955f257692795b9af51aa19139e4b3))
* **andromeda:** 감사 결함 5종 — 죽은 토큰·이중 제출·빈 오늘 랜딩·대화 60개 잘림·업데이터 동의 UX ([#3904](https://github.com/choiceoh/Deneb/issues/3904)) ([f9a8f65](https://github.com/choiceoh/Deneb/commit/f9a8f652592d7e548fbeadf0d0a512f0e55878e7))
* **audit:** rsi-bench cache moves to the state dir — stop dirtying the deploy tree ([#3915](https://github.com/choiceoh/Deneb/issues/3915)) ([b56ff39](https://github.com/choiceoh/Deneb/commit/b56ff39adc56364e20d887f2e1160fe505508327))
* **chat:** fallback walk logs blame the role that actually failed ([#3899](https://github.com/choiceoh/Deneb/issues/3899)) ([b6bbaa5](https://github.com/choiceoh/Deneb/commit/b6bbaa5783bcf2d85fb986061907a2144c927fbf))
* **chat:** timed-out runs with tool rounds are budget exhaustion, not stalls ([#3900](https://github.com/choiceoh/Deneb/issues/3900)) ([c5bc8f8](https://github.com/choiceoh/Deneb/commit/c5bc8f817172077fe3dbfb40b950de9a7a532901))
* **deploy:** bge-m3 unit — Restart=always + sync repo copy to live GPU config ([#3909](https://github.com/choiceoh/Deneb/issues/3909)) ([443218d](https://github.com/choiceoh/Deneb/commit/443218d86a7b9b4c7f089ef4702a990e1e9c81fa))
* **deploy:** resolve user-space npm under systemd timer PATH for groupware-reader ci ([#3916](https://github.com/choiceoh/Deneb/issues/3916)) ([3d2a44d](https://github.com/choiceoh/Deneb/commit/3d2a44dc4ba5995b4389023e5dc219cc91a886bb))
* **llm:** drop blank text/thinking blocks from the anthropic wire ([#3898](https://github.com/choiceoh/Deneb/issues/3898)) ([e56ddc4](https://github.com/choiceoh/Deneb/commit/e56ddc473ddf5910a5a03c8608cfb6aa50a2ace1))
* **llm:** order tool_result blocks first in user messages on the anthropic wire ([#3896](https://github.com/choiceoh/Deneb/issues/3896)) ([2538fdc](https://github.com/choiceoh/Deneb/commit/2538fdc46fc0eb0854ca74c1aa632428ebe96fe4))

## [4.121.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.120.0...deneb-v4.121.0) (2026-07-17)


### ✨ Features

* **agent:** reduce repeated skill workflow overhead ([#3888](https://github.com/choiceoh/Deneb/issues/3888)) ([4492c03](https://github.com/choiceoh/Deneb/commit/4492c031bf3263ad53b7ebc03de12246011b4596))
* **android:** predictive back 제스처 옵트인 ([#3873](https://github.com/choiceoh/Deneb/issues/3873)) ([d2c0d3e](https://github.com/choiceoh/Deneb/commit/d2c0d3e5e2b47f199e4b74f5bc482aaabd42c631))
* **android:** 공유 타깃 확장 — PDF·CSV 문서와 다중 이미지 공유 수신 ([#3866](https://github.com/choiceoh/Deneb/issues/3866)) ([d181122](https://github.com/choiceoh/Deneb/commit/d181122c910024d6a982768079a5566530e8a047))
* **android:** 콜드 스타트 마감 — SplashScreen 커스텀 exit + 클라우드 프로파일 ([#3881](https://github.com/choiceoh/Deneb/issues/3881)) ([68a1f96](https://github.com/choiceoh/Deneb/commit/68a1f967a55f2b5e7adb6df43db46b0da3f67882))
* **andromeda:** 오늘 콕핏 — KPI 스트립·일정 타임라인·마감 레이더·결재 섹션 ([#3863](https://github.com/choiceoh/Deneb/issues/3863)) ([527d5a5](https://github.com/choiceoh/Deneb/commit/527d5a5450584024453313185385c4bafbda10f4))
* **andromeda:** 타일 분할 워크스페이스·커맨드 팔레트·데네브 화면 조종 버스 ([#3850](https://github.com/choiceoh/Deneb/issues/3850)) ([58c6d1b](https://github.com/choiceoh/Deneb/commit/58c6d1b6a243a1a31c524a01ea07a92d48289d73))
* **chat:** difficulty-based main/main2 routing — simple turns ride the second subscription ([#3872](https://github.com/choiceoh/Deneb/issues/3872)) ([23fd950](https://github.com/choiceoh/Deneb/commit/23fd950e6fa1a5323c630fc4a3fe8b8233db4a13))
* **gateway:** workstation 챗 도구 — 데네브가 안드로메다 화면을 직접 조종 ([#3861](https://github.com/choiceoh/Deneb/issues/3861)) ([fb2c5df](https://github.com/choiceoh/Deneb/commit/fb2c5dfc406c95148ced8f10dc735d4ff5fb7441))
* **modelrole:** main2 role — mutual failover pair for two main-tier subscriptions ([#3869](https://github.com/choiceoh/Deneb/issues/3869)) ([7ad4080](https://github.com/choiceoh/Deneb/commit/7ad4080e5facab3090d9fb26a0570c7cf6990977))
* **native:** JetBrains Mono 코드 폰트 번들 + 빈 상태 마감 (아이콘·안내) ([#3884](https://github.com/choiceoh/Deneb/issues/3884)) ([9555e93](https://github.com/choiceoh/Deneb/commit/9555e9327f6f26ebbd3a25e4d288264aad3a445d))
* **native:** offline wiki mirror — full corpus on device, change-feed incremental sync ([#3862](https://github.com/choiceoh/Deneb/issues/3862)) ([bb963e8](https://github.com/choiceoh/Deneb/commit/bb963e814b4a1ef8c07f3a11514f3200707ae8e9))
* **native:** 메시지 등장 애니메이션 — 새 버블 페이드+정착 (animateItem) ([#3882](https://github.com/choiceoh/Deneb/issues/3882)) ([0dd548e](https://github.com/choiceoh/Deneb/commit/0dd548e5f34111d237c88bee97894dfebd24d6d2))
* **native:** 메시지 액션 메뉴 — 사용자 버블 롱프레스·봇 ⋯ 시트 (복사·텍스트 선택·공유·보낸 시각) ([#3868](https://github.com/choiceoh/Deneb/issues/3868)) ([002e636](https://github.com/choiceoh/Deneb/commit/002e636a058c4214e23e4dd19e66701a666d0480))
* **native:** 스트리밍 질감 — 부드러운 높이 성장 + 인라인 캐럿 ([#3876](https://github.com/choiceoh/Deneb/issues/3876)) ([3ef5c11](https://github.com/choiceoh/Deneb/commit/3ef5c11a2883e2a8b3029bb98aca8e5840d964e2))
* **native:** 편집 후 다시 보내기 + 응답 변형 ‹ n/N › 탐색 ([#3870](https://github.com/choiceoh/Deneb/issues/3870)) ([37ebd0a](https://github.com/choiceoh/Deneb/commit/37ebd0a222631c85b6df6d3cc2c7b1c5e029fe6b))


### 🐛 Bug Fixes

* **android:** block cross-account wiki mirror serve on credential switch ([#3877](https://github.com/choiceoh/Deneb/issues/3877)) ([538f766](https://github.com/choiceoh/Deneb/commit/538f766d7065fff774aec6b217ebbc3908df599a))
* **android:** self-heal pending queues ([#3883](https://github.com/choiceoh/Deneb/issues/3883)) ([a4a9bcb](https://github.com/choiceoh/Deneb/commit/a4a9bcbb822c7605c8fa417b985e0652d3bb643b))
* **gateway:** CORS 허용 헤더에 X-Deneb-Client-Kind 추가 ([#3890](https://github.com/choiceoh/Deneb/issues/3890)) ([71070a5](https://github.com/choiceoh/Deneb/commit/71070a5d659b172e8cabce054e80f5c0227bb27b))
* **gateway:** 대화 제목 영속화 — 재시작마다 드로어가 세션 키로 회귀하던 결함 ([#3865](https://github.com/choiceoh/Deneb/issues/3865)) ([d8d2ba7](https://github.com/choiceoh/Deneb/commit/d8d2ba7ff1c05931cafef2765849431c514fe610))
* **llm:** repair orphaned tool_use/tool_result pairing before provider send ([#3867](https://github.com/choiceoh/Deneb/issues/3867)) ([29d2edb](https://github.com/choiceoh/Deneb/commit/29d2edb507750e28d21c0112c859e33cbe2b6437))
* **modelpicker:** local anthropic-front providers fall back to reachability probe (false offline) ([#3864](https://github.com/choiceoh/Deneb/issues/3864)) ([a7ba1b6](https://github.com/choiceoh/Deneb/commit/a7ba1b6363da26c217da37c1927c5e6b096b0df3))


### ⚡ Performance

* **ci:** Go unit 샤딩 + 테스트 sleep 축소 ([#3853](https://github.com/choiceoh/Deneb/issues/3853)) ([298bcb8](https://github.com/choiceoh/Deneb/commit/298bcb81d724a08965d59cf42c4352f0e6dbaad1))
* **llm:** kimi K2.7 profile — cache_control 재개·usage 과소계량 수리·wormhole 엔트리 헤더 ([#3875](https://github.com/choiceoh/Deneb/issues/3875)) ([816e1d4](https://github.com/choiceoh/Deneb/commit/816e1d48a66d31ba8f91b8438bc952aa021d72e3))
* **native:** FontFamily 프로바이더 remember 메모이제이션 (리컴포지션 캐시 무효화 방지) ([#3887](https://github.com/choiceoh/Deneb/issues/3887)) ([ce4964a](https://github.com/choiceoh/Deneb/commit/ce4964a1588fedf99b2867f56892b33ea4db3e66))


### 🔧 Internal

* **android:** coalesce keyed section cache loads ([#3855](https://github.com/choiceoh/Deneb/issues/3855)) ([bb844a2](https://github.com/choiceoh/Deneb/commit/bb844a2befe55d7549bd87c4c53f2bd4c8bf51ea))
* **android:** harden transcript cache ownership ([#3874](https://github.com/choiceoh/Deneb/issues/3874)) ([9a8e2b9](https://github.com/choiceoh/Deneb/commit/9a8e2b99cf7ea9a462419d2309099da6739c6e7d))
* **android:** reuse owned cache envelopes ([#3871](https://github.com/choiceoh/Deneb/issues/3871)) ([b010431](https://github.com/choiceoh/Deneb/commit/b010431cd57b5ec9cce8d2ac2f733c886ea9337d))
* **android:** reuse stored JSON recovery for memories ([#3889](https://github.com/choiceoh/Deneb/issues/3889)) ([9068e6d](https://github.com/choiceoh/Deneb/commit/9068e6ddced334f71911f6962429faab29b35ad7))
* **android:** self-heal persistent caches ([#3878](https://github.com/choiceoh/Deneb/issues/3878)) ([1683108](https://github.com/choiceoh/Deneb/commit/1683108ba866820132cbbeb16fd7f55e0ca1648c))
* **android:** unify stored JSON recovery ([#3885](https://github.com/choiceoh/Deneb/issues/3885)) ([52c1b93](https://github.com/choiceoh/Deneb/commit/52c1b93c53dbc99dfebb4bca8b289b71527964f2))
* **deps:** Refine·extended-icons·play-review 제거 및 DIY 대체 ([#3879](https://github.com/choiceoh/Deneb/issues/3879)) ([b33e6c6](https://github.com/choiceoh/Deneb/commit/b33e6c66cd00f659c371567694f5333a19e716b7))

## [4.120.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.119.0...deneb-v4.120.0) (2026-07-16)


### ✨ Features

* **andromeda:** consume gateway change-feed (mail/approvals/wiki) in native-sync catch-up ([#3851](https://github.com/choiceoh/Deneb/issues/3851)) ([17696ff](https://github.com/choiceoh/Deneb/commit/17696ff101ee2286737bd48d0379af9b5df2fb88))
* **browser:** Readability식 본문 스코어로 CMS 셀렉터 미히트 시 primary root 승격 ([#3843](https://github.com/choiceoh/Deneb/issues/3843)) ([0d0c3e8](https://github.com/choiceoh/Deneb/commit/0d0c3e83d56f73c2ceab6d9f8aec2d16d109395e))
* **deploy:** systemd socket activation for the gateway HTTP listener (zero refused window) ([#3844](https://github.com/choiceoh/Deneb/issues/3844)) ([9fba108](https://github.com/choiceoh/Deneb/commit/9fba108e70a2f81b2b6e5b36c45e01a32f63556f))


### 🐛 Bug Fixes

* **deploy:** deploy-watch records inherited candidates under their own deploy head ([#3847](https://github.com/choiceoh/Deneb/issues/3847)) ([b56da8d](https://github.com/choiceoh/Deneb/commit/b56da8dbacb3dcc4e2193be64828e499b6f8b3b0))


### ⚡ Performance

* **native:** coalesce section cache loads ([#3849](https://github.com/choiceoh/Deneb/issues/3849)) ([e1687e9](https://github.com/choiceoh/Deneb/commit/e1687e98bfd24b2cc8bc764356f2b7bd8a293b54))
* **textsearch:** Hangul posting expansion + hoist IDF to cut mail_archive latency ([#3846](https://github.com/choiceoh/Deneb/issues/3846)) ([d28853b](https://github.com/choiceoh/Deneb/commit/d28853b6859f37147211a84ba54e0126e55fb879))

## [4.119.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.118.0...deneb-v4.119.0) (2026-07-16)


### ✨ Features

* **deploy:** turn-aware idle gate — swap when no agent run is in flight ([#3842](https://github.com/choiceoh/Deneb/issues/3842)) ([fd0bb5b](https://github.com/choiceoh/Deneb/commit/fd0bb5b1af048aa8d124d0ca93cc583b9870c088))
* **gateway:** mail/approvals change-feed — arrival + cross-client mutations push-invalidate (Phase B-2) ([#3841](https://github.com/choiceoh/Deneb/issues/3841)) ([d9db2d5](https://github.com/choiceoh/Deneb/commit/d9db2d5d9d54481b455a0df226caa3ad4148a5a5))
* **gateway:** wiki/org change-feed over nativesync — push invalidation for client snapshots (Phase B-1) ([#3840](https://github.com/choiceoh/Deneb/issues/3840)) ([6bdc901](https://github.com/choiceoh/Deneb/commit/6bdc9015b0ff273b4ec21b7dbf7041cc6ce7caeb))
* **groupware:** agent judges PROJECT_FILE for approval→project wiki ([#3829](https://github.com/choiceoh/Deneb/issues/3829)) ([61e8f13](https://github.com/choiceoh/Deneb/commit/61e8f13fa128455d09933978354a9c03d4d277e7))
* **groupware:** ingest 수신참조 approvals into knowledge lane + letter highlights ([#3835](https://github.com/choiceoh/Deneb/issues/3835)) ([74612d1](https://github.com/choiceoh/Deneb/commit/74612d1eacc7048cbd1f5dc0398e0676c19cfe2d))
* **native:** disk-backed section snapshots — instant cold start, offline last-known (Phase A) ([#3839](https://github.com/choiceoh/Deneb/issues/3839)) ([3b2bc1d](https://github.com/choiceoh/Deneb/commit/3b2bc1d2b44d821455b4dbe9582294a0f51b0293))
* **native:** session-cache the wiki browse loop (category page lists + page bodies) ([#3838](https://github.com/choiceoh/Deneb/issues/3838)) ([472fe6e](https://github.com/choiceoh/Deneb/commit/472fe6e0e03a9834b13b0aee5006d7594ba7499f))
* **native:** session-cache the 더보기 section fetches (2min TTL, force on PTR, invalidate on mutation) ([#3837](https://github.com/choiceoh/Deneb/issues/3837)) ([2f88d94](https://github.com/choiceoh/Deneb/commit/2f88d941dd09dfc903a4aeb6f57f36097acef787))
* **native:** silent stale-while-revalidate on live-tab re-activation (mail 60s · calendar 120s) ([#3836](https://github.com/choiceoh/Deneb/issues/3836)) ([8a0a28f](https://github.com/choiceoh/Deneb/commit/8a0a28f4fcafc61cc6a77f416f2534964e99e146))
* **native:** 하단 탭 상시-알라이브 전환 (LiveTabPane) — 즉시 전환·상태 보존 ([#3834](https://github.com/choiceoh/Deneb/issues/3834)) ([7bc60c8](https://github.com/choiceoh/Deneb/commit/7bc60c8116ca4975dc84fea5db5c6789b546fa8d))


### 🐛 Bug Fixes

* **genesis:** unstarve generation token budget + L4 dispatch contract discipline ([#3833](https://github.com/choiceoh/Deneb/issues/3833)) ([1761539](https://github.com/choiceoh/Deneb/commit/1761539882167eb0a33430b8646da831bc071b24))
* **mail:** sender review gates bulk noise only; keep bodies readable ([#3831](https://github.com/choiceoh/Deneb/issues/3831)) ([33a513b](https://github.com/choiceoh/Deneb/commit/33a513b763c70460433a1f7f9029f6de7fb2c726))

## [4.118.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.117.0...deneb-v4.118.0) (2026-07-16)


### ✨ Features

* **andromeda:** 결재 반려 사유·첨부 바이너리·choices 배선 ([#3823](https://github.com/choiceoh/Deneb/issues/3823)) ([0149415](https://github.com/choiceoh/Deneb/commit/0149415f387efc1d8a00ce78df6cdeb986e4be55))
* **groupware:** phone approval push triggers radar scan instead of relay card ([#3825](https://github.com/choiceoh/Deneb/issues/3825)) ([3b8afcc](https://github.com/choiceoh/Deneb/commit/3b8afcc7aa871de195d105a02484a214f32f3bb6))
* **groupware:** radar list-fail feed alert + feed→approval deep-link ([#3824](https://github.com/choiceoh/Deneb/issues/3824)) ([29c0b81](https://github.com/choiceoh/Deneb/commit/29c0b8146803793c9d2833031592993dbf4c0355))


### 🐛 Bug Fixes

* **andromeda:** 결재 분석 로딩 문구 — 캐시조회 vs LLM 구분 ([#3828](https://github.com/choiceoh/Deneb/issues/3828)) ([94c0fd0](https://github.com/choiceoh/Deneb/commit/94c0fd0013e7a326a21281c973591909e75b3fb0))

## [4.117.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.116.0...deneb-v4.117.0) (2026-07-16)


### ✨ Features

* **andromeda:** approval attachments + markdown parity (sticky/합계/footnotes) ([#3819](https://github.com/choiceoh/Deneb/issues/3819)) ([6c54c7a](https://github.com/choiceoh/Deneb/commit/6c54c7a4679187e6ca1759c691d9b318370a0d1e))
* **client:** 결재 최근목록·무한스크롤·settings 캐시와 피드 스와이프 폴리시 ([#3818](https://github.com/choiceoh/Deneb/issues/3818)) ([6e6ac08](https://github.com/choiceoh/Deneb/commit/6e6ac0808826e2f20d0e66f0990082da12e919ec))
* **groupware:** approval price memory — item/expense cost capture + prompt injection ([#3815](https://github.com/choiceoh/Deneb/issues/3815)) ([e4fb1b2](https://github.com/choiceoh/Deneb/commit/e4fb1b21f4994a5ec1e55f305f052a2d6ff7b0e5))
* **groupware:** inject selected approval attachments into analysis prompt ([#3822](https://github.com/choiceoh/Deneb/issues/3822)) ([6f761a5](https://github.com/choiceoh/Deneb/commit/6f761a53aae62bef7485cdb634e0bac52bbdfd28))
* **runtime:** recover completed tools after restart ([#3817](https://github.com/choiceoh/Deneb/issues/3817)) ([764e1e9](https://github.com/choiceoh/Deneb/commit/764e1e9aec672b899327040834a54a151e356722))
* **sitemap:** 상세에서 현장 상태 칩으로 변경 ([#3814](https://github.com/choiceoh/Deneb/issues/3814)) ([1f04a07](https://github.com/choiceoh/Deneb/commit/1f04a075a69b2cd6230c50d8b62b7523f8d47b96))
* **sitemap:** 현장 페이지 생성·일정 편집·미배치 상세 ([#3820](https://github.com/choiceoh/Deneb/issues/3820)) ([4f84126](https://github.com/choiceoh/Deneb/commit/4f8412678328fb623816f842b67de7a4d63d3d38))


### 🐛 Bug Fixes

* **groupware:** attach analysis fields on approval feed cards ([#3810](https://github.com/choiceoh/Deneb/issues/3810)) ([b6c02cc](https://github.com/choiceoh/Deneb/commit/b6c02cc68f20456ace0d8605ff26f44f5a7f2f30))
* **skills:** auto-load exact trigger procedures ([#3812](https://github.com/choiceoh/Deneb/issues/3812)) ([0d695c6](https://github.com/choiceoh/Deneb/commit/0d695c67d75e764d16c59ad387b9467cd6546fa0))
* **wiki:** discard stale graph corpus cache after concurrent forget ([#3816](https://github.com/choiceoh/Deneb/issues/3816)) ([6988b6c](https://github.com/choiceoh/Deneb/commit/6988b6c79215ab659ce9e4773141a3b80e25a606))

## [4.116.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.115.0...deneb-v4.116.0) (2026-07-16)


### ✨ Features

* **client:** swipe between feed and approvals, drop mail from pivot ([#3807](https://github.com/choiceoh/Deneb/issues/3807)) ([d79e9db](https://github.com/choiceoh/Deneb/commit/d79e9dbc0e72656d7f766486632cfddd89c4d648))


### 🐛 Bug Fixes

* **andromeda:** interrupt paragraphs for GFM tables in approval bodies ([#3800](https://github.com/choiceoh/Deneb/issues/3800)) ([931c649](https://github.com/choiceoh/Deneb/commit/931c6490c3e64f2b878fef6126d1b04585cec96d))
* **andromeda:** markdown normalizers + approval body polish ([#3806](https://github.com/choiceoh/Deneb/issues/3806)) ([fb42ff6](https://github.com/choiceoh/Deneb/commit/fb42ff6488cae02ecab611e9b73c8b8e1e182d06))
* **groupware:** analyze and wiki before approval feed cards ([#3803](https://github.com/choiceoh/Deneb/issues/3803)) ([c88c695](https://github.com/choiceoh/Deneb/commit/c88c6957e268a736f2d18bbdf320f412c4857dac))
* **groupware:** publish approval analysis directly to feed ([#3808](https://github.com/choiceoh/Deneb/issues/3808)) ([14a3bda](https://github.com/choiceoh/Deneb/commit/14a3bda1a35b72bb1243fa5f5481e3c97969ad5e))
* **groupware:** 매출 요약을 기간·건수·매출만 표시하고 금액 괄호 숫자 제거 ([#3805](https://github.com/choiceoh/Deneb/issues/3805)) ([ce82342](https://github.com/choiceoh/Deneb/commit/ce823429099778229a0f380a2c05e48f2cfa8b3c))
* **sitemap:** 기본 필터를 공사중(개설)만 표시 ([#3802](https://github.com/choiceoh/Deneb/issues/3802)) ([323742c](https://github.com/choiceoh/Deneb/commit/323742cb35fa1b3ca52f6c9b43e20b6fa3ba3e62))


### 🔧 Internal

* **skills:** preserve only required process ([#3801](https://github.com/choiceoh/Deneb/issues/3801)) ([735a159](https://github.com/choiceoh/Deneb/commit/735a1591461ddc90d9ac0cb882046adcefac97e5))

## [4.115.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.114.0...deneb-v4.115.0) (2026-07-16)


### ✨ Features

* **groupware:** approval body cache + folder hint + wiki log of analyses ([#3796](https://github.com/choiceoh/Deneb/issues/3796)) ([d8eda32](https://github.com/choiceoh/Deneb/commit/d8eda32ac0425e1d42f07b27e836761b4d84eab8))
* **skills:** prefer outcome-driven task contracts ([#3797](https://github.com/choiceoh/Deneb/issues/3797)) ([4808df2](https://github.com/choiceoh/Deneb/commit/4808df28ec45fec2201405f716847bdbc0d9cad0))

## [4.114.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.113.0...deneb-v4.114.0) (2026-07-16)


### ✨ Features

* **groupware:** default 모듈·인버터 scope + sales period tabs rework ([#3794](https://github.com/choiceoh/Deneb/issues/3794)) ([e6a3cb8](https://github.com/choiceoh/Deneb/commit/e6a3cb8645af677d7afc0485f554473ad66c0bcd))

## [4.113.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.112.0...deneb-v4.113.0) (2026-07-16)


### ✨ Features

* **client:** 그룹웨어 플랫 탭·게시판 본문 시트·새로고침 ([#3790](https://github.com/choiceoh/Deneb/issues/3790)) ([24629d4](https://github.com/choiceoh/Deneb/commit/24629d495e188e50fbc4c87dc640d52992030ae7))


### ⚡ Performance

* **chat:** translate stable prompt controls to English ([#3793](https://github.com/choiceoh/Deneb/issues/3793)) ([30ab0bf](https://github.com/choiceoh/Deneb/commit/30ab0bf89a161ce1a179e830097d8b1d1b8d0bd1))

## [4.112.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.111.0...deneb-v4.112.0) (2026-07-15)


### ✨ Features

* **client:** native rows for groupware ERP snapshot ([#3786](https://github.com/choiceoh/Deneb/issues/3786)) ([c418f3b](https://github.com/choiceoh/Deneb/commit/c418f3b24845d621bfbb148756648c4aa7a6281e))
* **client:** pivot to mail, quiet pending filter, approval detail reorder ([#3788](https://github.com/choiceoh/Deneb/issues/3788)) ([e6af3c3](https://github.com/choiceoh/Deneb/commit/e6af3c336106c5772aea8ff5676e1829d5e40395))
* **client:** zune-style pivot header + groupware surface upgrades ([#3785](https://github.com/choiceoh/Deneb/issues/3785)) ([40db5ae](https://github.com/choiceoh/Deneb/commit/40db5ae49e7a7522cf56a9ca3d306ca79c5d1595))
* review unknown mail senders ([#3783](https://github.com/choiceoh/Deneb/issues/3783)) ([32d1ad7](https://github.com/choiceoh/Deneb/commit/32d1ad733573947a71706a28fb41ecb46f53651b))
* **sitemap:** 모바일 지도 심미 개선 — 시도 라벨·핀 외곽선·마감 ([#3784](https://github.com/choiceoh/Deneb/issues/3784)) ([7b35057](https://github.com/choiceoh/Deneb/commit/7b350576bbb223d99a0e6560e144c1ebd664fd9a))
* **sitemap:** 모바일 지도 핀치 줌·팬 (데스크톱 휠 줌 대응) ([#3782](https://github.com/choiceoh/Deneb/issues/3782)) ([2773972](https://github.com/choiceoh/Deneb/commit/27739721d0a31a967b759dc8a5ef3703be6579b4))


### 🔧 Internal

* load specialist guidance just in time ([#3787](https://github.com/choiceoh/Deneb/issues/3787)) ([ca86274](https://github.com/choiceoh/Deneb/commit/ca862747b1e57237704396c741a58938d3cdf239))

## [4.111.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.110.0...deneb-v4.111.0) (2026-07-15)


### ✨ Features

* **wiki:** 현장 공정 일정 지도 타임라인 + 임박 검사일 서피싱 ([#3772](https://github.com/choiceoh/Deneb/issues/3772)) ([9a44aab](https://github.com/choiceoh/Deneb/commit/9a44aabaa906e6c08f038d257c4facf4f707780f))

## [4.110.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.109.0...deneb-v4.110.0) (2026-07-15)


### ✨ Features

* **client:** 전자결재 피드 카드 딥링크·소스 표현 ([#3770](https://github.com/choiceoh/Deneb/issues/3770)) ([15a3836](https://github.com/choiceoh/Deneb/commit/15a38365f779652e555025eb572eb025a8aac381))
* **groupware:** 결재 AI 분석·날짜별 표면 + ERP 허브 ([#3768](https://github.com/choiceoh/Deneb/issues/3768)) ([c6bf0a4](https://github.com/choiceoh/Deneb/commit/c6bf0a4802c2b87fa553b5a76ac3a8c9833fbbf5))
* **groupware:** 기존 API 라우팅·레터 방치표시 보강 ([#3764](https://github.com/choiceoh/Deneb/issues/3764)) ([7b7cac0](https://github.com/choiceoh/Deneb/commit/7b7cac00d08d23e33e7832b8c2add09ecea93245))
* **groupware:** 미결 에스컬레이션·레터 통합 ([#3761](https://github.com/choiceoh/Deneb/issues/3761)) ([959615e](https://github.com/choiceoh/Deneb/commit/959615ed79d967c847021b85283073c737502576))
* **groupware:** 반려 사유 E2E 입력·전달 ([#3771](https://github.com/choiceoh/Deneb/issues/3771)) ([924893f](https://github.com/choiceoh/Deneb/commit/924893f328a8e5dd2b9991949fb3fe0be8c12d19))
* **groupware:** 사원 조회 — 이름·부서·직급/호칭·휴대폰·생년월일 ([#3758](https://github.com/choiceoh/Deneb/issues/3758)) ([73fb0ec](https://github.com/choiceoh/Deneb/commit/73fb0ec996eb12fb0f589093fc88ddb90734b4b0))
* **groupware:** 중요 게시판 공지 Radar ([#3769](https://github.com/choiceoh/Deneb/issues/3769)) ([cbe9a00](https://github.com/choiceoh/Deneb/commit/cbe9a00799209a3e22eb4cd10143cf92778febca))
* **wiki:** 현장 페이지 일괄 시더 — 대표 Sites 부트스트랩 ([#3763](https://github.com/choiceoh/Deneb/issues/3763)) ([47bdca1](https://github.com/choiceoh/Deneb/commit/47bdca16dad450866c1c63cf5cd4abb42b76fc20))


### 🐛 Bug Fixes

* **build:** config-cache-safe git sha via ValueSource (androidApp) ([#3759](https://github.com/choiceoh/Deneb/issues/3759)) ([75229ed](https://github.com/choiceoh/Deneb/commit/75229eda33ef11e7b386563b82967c47b959d075))
* **chat:** Business 도구 카테고리를 deal_ledger에 맞춤 ([#3766](https://github.com/choiceoh/Deneb/issues/3766)) ([3f75072](https://github.com/choiceoh/Deneb/commit/3f75072d4585bae95fbe688fad74320f5165f88d))
* **client:** separate approval sections and restore act buttons ([#3778](https://github.com/choiceoh/Deneb/issues/3778)) ([72d2d41](https://github.com/choiceoh/Deneb/commit/72d2d414f709fd6202d4edf8c0bfba745d511320))
* **client:** 결재 본문 마크다운 표 렌더 ([#3774](https://github.com/choiceoh/Deneb/issues/3774)) ([dae33e5](https://github.com/choiceoh/Deneb/commit/dae33e5b7a69c4c4ab7728e1f3a28f80b28f7315))
* **client:** 그룹웨어·결재 화면 퀄리티 정리 ([#3779](https://github.com/choiceoh/Deneb/issues/3779)) ([e882133](https://github.com/choiceoh/Deneb/commit/e88213300fb7c5b6c36e27174ae17416632d695d))
* **groupware:** prod reader 경로·Playwright 의존성 ([#3773](https://github.com/choiceoh/Deneb/issues/3773)) ([628cbbd](https://github.com/choiceoh/Deneb/commit/628cbbd3759201e4d57652aad0495c75c934c22d))
* **wiki:** abort SeedSitePages on ListPages error ([#3765](https://github.com/choiceoh/Deneb/issues/3765)) ([84c734d](https://github.com/choiceoh/Deneb/commit/84c734d1b96d3fb5a1522abffd8441c19040aa0b))


### 🔧 Internal

* **agent:** remove dead stream retry wrapper ([#3767](https://github.com/choiceoh/Deneb/issues/3767)) ([1a89b7a](https://github.com/choiceoh/Deneb/commit/1a89b7a8e1af43a8537bb6b0bd3da579f34daa1c))

## [4.109.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.108.1...deneb-v4.109.0) (2026-07-15)


### ✨ Features

* **browser:** aviation21 WordPress adblock and translate body ([#3750](https://github.com/choiceoh/Deneb/issues/3750)) ([f76f6bd](https://github.com/choiceoh/Deneb/commit/f76f6bded920d761078b0e3b3502986eb45ed329))
* **browser:** block Yandex RTB and Forumotion ads for RU news sites ([#3746](https://github.com/choiceoh/Deneb/issues/3746)) ([a4069ab](https://github.com/choiceoh/Deneb/commit/a4069ab1d1d4b3bcd4958b9cbccd1fb56f5f9e6a))
* **browser:** Eurasian Times Newspaper adblock and translate body ([#3751](https://github.com/choiceoh/Deneb/issues/3751)) ([73e51e7](https://github.com/choiceoh/Deneb/commit/73e51e7046589429b2b7d623febfdf6edaebaada))
* **browser:** lightweight network adblock for translate WebView ([#3739](https://github.com/choiceoh/Deneb/issues/3739)) ([e55e616](https://github.com/choiceoh/Deneb/commit/e55e616df6c1425074f1fa145b8ce8d9df59ab35))
* **browser:** Substack adblock pixels and translate body selectors ([#3749](https://github.com/choiceoh/Deneb/issues/3749)) ([562845b](https://github.com/choiceoh/Deneb/commit/562845b10f07e1c8d5df6e95fe4172290aa4b3c2))
* **browser:** widen adblock coverage and show per-page block count ([#3745](https://github.com/choiceoh/Deneb/issues/3745)) ([46a6c7f](https://github.com/choiceoh/Deneb/commit/46a6c7fdaa7b932a449559c9a4ad412b51308e22))
* **groupware:** ERP 조회 확장 — stock/po/receive/ship/price ([#3755](https://github.com/choiceoh/Deneb/issues/3755)) ([3704327](https://github.com/choiceoh/Deneb/commit/37043275a3cb0ed20541d5afb07a38ee3c443f4c))
* **groupware:** 매출마감 조회 — area=sales summary (YTD/당월/오늘) ([#3754](https://github.com/choiceoh/Deneb/issues/3754)) ([1eb2474](https://github.com/choiceoh/Deneb/commit/1eb24742c31226385680a2a7a38e0c5a9df70a37))
* **groupware:** 미결 Radar·RefID 정합 자동화 ([#3756](https://github.com/choiceoh/Deneb/issues/3756)) ([664dfb0](https://github.com/choiceoh/Deneb/commit/664dfb062d39504e86d09f723e8e9017377abcd3))
* **groupware:** 에이전트 사용성 — 딥퍼링 요약에 attachment 노출·첨부 번호 중복 제거·크기 표기 ([#3747](https://github.com/choiceoh/Deneb/issues/3747)) ([b103e67](https://github.com/choiceoh/Deneb/commit/b103e67d04c60f93cf927f7ea1645958e363dc1d))
* **groupware:** 첨부 OCR을 PaddleOCR-VL로 승격 + 스캔 PDF 래스터화·노이즈 억제 ([#3740](https://github.com/choiceoh/Deneb/issues/3740)) ([f5dfd71](https://github.com/choiceoh/Deneb/commit/f5dfd7110fa4d54989b1e464ae0a28b323e450e7))
* **groupware:** 표 구조 보존·첨부 선택 읽기 ([#3743](https://github.com/choiceoh/Deneb/issues/3743)) ([8bee4e2](https://github.com/choiceoh/Deneb/commit/8bee4e2c804a6d29ea0ea84538646f2d3ba169c0))
* **sitemap:** client-android 현장 지도 포팅 + andromeda 휠 줌·팬 ([#3737](https://github.com/choiceoh/Deneb/issues/3737)) ([d82ecba](https://github.com/choiceoh/Deneb/commit/d82ecbac006a6bb637c4b4ab26f820afc6842510))
* **wiki:** 현장 공통 포맷 — 공정 일정 필드 + write-site 작성 경로 ([#3757](https://github.com/choiceoh/Deneb/issues/3757)) ([c2358a8](https://github.com/choiceoh/Deneb/commit/c2358a8f0967d0a030daa4b087db78be99499048))
* **wiki:** 현장 서브페이지 데이터 모델 + 지도 상태 필터 ([#3744](https://github.com/choiceoh/Deneb/issues/3744)) ([b68ae20](https://github.com/choiceoh/Deneb/commit/b68ae203b5bf96e34d9e95c9b3390e536c6ea7bd))


### 🐛 Bug Fixes

* **browser:** improve translate coverage for topwar/topcor/russiadefence ([#3748](https://github.com/choiceoh/Deneb/issues/3748)) ([16bd4b7](https://github.com/choiceoh/Deneb/commit/16bd4b7a5cd7e429fe381c57c7adae5ec47620f9))
* **sitemap:** DenebType style props + spotless emptyBlockedResponse ([#3741](https://github.com/choiceoh/Deneb/issues/3741)) ([e52f720](https://github.com/choiceoh/Deneb/commit/e52f72083a8b14b5eeaab39d6a186d3fba3b70e5))


### 🔧 Internal

* **agent:** remove dead executeOneTool wrapper ([#3742](https://github.com/choiceoh/Deneb/issues/3742)) ([cc79fed](https://github.com/choiceoh/Deneb/commit/cc79fedc49beb4456245910f904dbb48b5e62d61))

## [4.108.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.108.0...deneb-v4.108.1) (2026-07-15)


### 🐛 Bug Fixes

* **groupware:** act opt-in 가드·결재선 user_id 매칭·첨부 예산·JS 테스트 ([#3735](https://github.com/choiceoh/Deneb/issues/3735)) ([0485c0f](https://github.com/choiceoh/Deneb/commit/0485c0f1b17fca2a56e87b967f092ed77677bf4e))

## [4.108.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.107.0...deneb-v4.108.0) (2026-07-15)


### ✨ Features

* **groupware:** Amaranth HMAC 리더·툴·피드 승인칩 ([#3733](https://github.com/choiceoh/Deneb/issues/3733)) ([f480684](https://github.com/choiceoh/Deneb/commit/f48068476ca0ef31a67b61a1866936e7ebb0c81f))

## [4.107.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.106.0...deneb-v4.107.0) (2026-07-15)


### ✨ Features

* **andromeda:** 관찰 팬 — miniapp.observe 동작·로그 대시보드 ([#3684](https://github.com/choiceoh/Deneb/issues/3684)) ([cf10bf5](https://github.com/choiceoh/Deneb/commit/cf10bf58940a895a7cbccd87a91b14cba8f924b0))
* **audit:** Health Bench 3.0 — structure+runtime+fitness geometric composite ([#3696](https://github.com/choiceoh/Deneb/issues/3696)) ([6bbddbb](https://github.com/choiceoh/Deneb/commit/6bbddbb75c41e658b28f9441a84b3aa95d369180))
* **audit:** RSI Bench 1.2 — Process+Utility composite with Utility ratchet ([#3711](https://github.com/choiceoh/Deneb/issues/3711)) ([c02a237](https://github.com/choiceoh/Deneb/commit/c02a23719b75dfab02a2805d985cfc92f687a424))
* **browser:** SPA/Shadow/iframe 번역 커버리지 + 서버 DeepL 캐시 ([#3705](https://github.com/choiceoh/Deneb/issues/3705)) ([99a0c92](https://github.com/choiceoh/Deneb/commit/99a0c9219de8fbefdcb2059fcdc4d192e6b21b45))
* **browser:** 번역 토글 기억 + 최근 방문 ([#3713](https://github.com/choiceoh/Deneb/issues/3713)) ([2d1dc29](https://github.com/choiceoh/Deneb/commit/2d1dc29cb20b103343548b7fdfb75bbcd4626f9f))
* **browser:** 시작 홈 URL ([#3716](https://github.com/choiceoh/Deneb/issues/3716)) ([a5d30ff](https://github.com/choiceoh/Deneb/commit/a5d30ff44be93800b2434a4b1e3bace9f0abec30))
* **chat:** office 도구 신설 — officecli로 Office 문서(.docx/.xlsx/.pptx) 읽기·편집 (eager) ([#3699](https://github.com/choiceoh/Deneb/issues/3699)) ([cc65a1d](https://github.com/choiceoh/Deneb/commit/cc65a1d605693c28862740dc2db0cecb553696af))
* **chat:** Page Agent 브리지로 실 Chrome 조작 browser 도구 추가 ([b020d6c](https://github.com/choiceoh/Deneb/commit/b020d6c2a494f89427b2ab8f719310924975536e))
* **client-android:** 관찰 탭에 health·퍼널·백그라운드·토큰 노출 ([#3686](https://github.com/choiceoh/Deneb/issues/3686)) ([eacffac](https://github.com/choiceoh/Deneb/commit/eacffac1221613aaf8d3003958f30b81987092c7))
* **compaction,chat:** 요약 거짓합의 가드 + deneb-ui 인터랙티브 3패턴 확장 ([#3727](https://github.com/choiceoh/Deneb/issues/3727)) ([8ccef6d](https://github.com/choiceoh/Deneb/commit/8ccef6dbf30ca196515891d5b4eca1eef234a332))
* **knowledge:** raise recall quality — gated rerank, layer RRF, files timeout notes ([#3689](https://github.com/choiceoh/Deneb/issues/3689)) ([0bc77cf](https://github.com/choiceoh/Deneb/commit/0bc77cf643b3ddd53cb66f6f98d6ab7d3db263d3))
* **meeting:** Plaud ASR 용어집 슬라이스·프로젝트 엔티티·자동 승격 ([#3721](https://github.com/choiceoh/Deneb/issues/3721)) ([cbc75c1](https://github.com/choiceoh/Deneb/commit/cbc75c1df74beab6e460e289bd1a6b6e475f6347))
* **meeting:** Plaud 승격 임계·캘린더 연결·피드 중복 제거·독성 격리 ([#3726](https://github.com/choiceoh/Deneb/issues/3726)) ([588cae5](https://github.com/choiceoh/Deneb/commit/588cae58aa66e2d5c0975079c0012c7676e99272))
* **rsi:** drop staged-source first-batch human review ([#3695](https://github.com/choiceoh/Deneb/issues/3695)) ([edd54e7](https://github.com/choiceoh/Deneb/commit/edd54e77c3f3125f8597f827f658b7c2cc999599))
* **rsi:** measure post-deploy impact ([#3704](https://github.com/choiceoh/Deneb/issues/3704)) ([68583c5](https://github.com/choiceoh/Deneb/commit/68583c5781755f636b12851a7f38753238a503a5))
* **rsi:** prioritize measured impact in dispatch ([#3719](https://github.com/choiceoh/Deneb/issues/3719)) ([20a77d5](https://github.com/choiceoh/Deneb/commit/20a77d529d3fd2fd523912946adae77d0aae43fb))
* **runtime:** expose verified composition manifest ([#3720](https://github.com/choiceoh/Deneb/issues/3720)) ([7363c39](https://github.com/choiceoh/Deneb/commit/7363c39373a5e940b190056b00f836d2a8c37b1a))
* **sitemap:** 현장 지도 — 한국 시군구·읍면 지도에 에너지원·특성·용량 인코딩 ([#3703](https://github.com/choiceoh/Deneb/issues/3703)) ([df0c299](https://github.com/choiceoh/Deneb/commit/df0c29984e7f36f87bb7ca4851770449382b3c5c))
* **watch:** near-dup 프레임 제거 + transcript-only detail ([#3715](https://github.com/choiceoh/Deneb/issues/3715)) ([2f21c1d](https://github.com/choiceoh/Deneb/commit/2f21c1d57ba1d9fa33b4c868296b25be463604cb))
* **web:** read X (single tweet) and Reddit via native handlers ([#3697](https://github.com/choiceoh/Deneb/issues/3697)) ([ec0ce8f](https://github.com/choiceoh/Deneb/commit/ec0ce8f6f05bec0c26f6cbe579460e8fbf33fc20))
* **web:** search+fetch 랭킹·조기중단·Brave 로케일·다양성 ([#3693](https://github.com/choiceoh/Deneb/issues/3693)) ([4c2185a](https://github.com/choiceoh/Deneb/commit/4c2185a51d2b064bdfc5f3087cea4aa085550e68))
* **web:** search+fetch 하이브리드 fill·상세 가용판정·KG·관측 ([#3694](https://github.com/choiceoh/Deneb/issues/3694)) ([e897a4c](https://github.com/choiceoh/Deneb/commit/e897a4cfca2df13383612148eab398763f034c8a))
* **web:** search+fetch 후보 랭킹·오버샘플·한글 Serper 로케일 ([#3690](https://github.com/choiceoh/Deneb/issues/3690)) ([a5db437](https://github.com/choiceoh/Deneb/commit/a5db43757f5811916c6a4a774774ba30fc00bff6))
* **wormhole:** add model circuit breaker ([#3723](https://github.com/choiceoh/Deneb/issues/3723)) ([6358e69](https://github.com/choiceoh/Deneb/commit/6358e6991bfeaa38ede16ef39b135bbe8d17d66b))


### 🐛 Bug Fixes

* **audit:** health-finding miner skips forbidden genesis surfaces before filing ([#3725](https://github.com/choiceoh/Deneb/issues/3725)) ([6e0aeae](https://github.com/choiceoh/Deneb/commit/6e0aeaedc34000070afa2022b23e03f4dc25f120))
* **audit:** RSI Bench L4 outcome land, soft resolve reconstruct, daily snapshot timer ([#3714](https://github.com/choiceoh/Deneb/issues/3714)) ([5c24eb6](https://github.com/choiceoh/Deneb/commit/5c24eb621fc221f61a66ca5ca03f0375ca18c97e))
* **browser:** 번역 브라우저 마지막 페이지 복원 ([#3707](https://github.com/choiceoh/Deneb/issues/3707)) ([1dd340d](https://github.com/choiceoh/Deneb/commit/1dd340dadae35a43ffdf91ff5d257a9b573ae52c))
* **chat:** drain in-flight turns during restart ([#3702](https://github.com/choiceoh/Deneb/issues/3702)) ([6b0a557](https://github.com/choiceoh/Deneb/commit/6b0a557e1aef2642623743fd72016fc5287d717f))
* **chat:** enqueue subagent notifications during restart drain ([#3732](https://github.com/choiceoh/Deneb/issues/3732)) ([7d113e3](https://github.com/choiceoh/Deneb/commit/7d113e3dea95bd6a27e1d81333e070e64ffc9075))
* **chat:** restore preference and wiki_forget tools dropped by health fanout ([#3685](https://github.com/choiceoh/Deneb/issues/3685)) ([790a8e2](https://github.com/choiceoh/Deneb/commit/790a8e269f014097cb29a951dfcff7988e2f33ca))
* **ci:** gofmt·python-test rot·genesis volatile hub finding 완화 ([#3718](https://github.com/choiceoh/Deneb/issues/3718)) ([4a1100f](https://github.com/choiceoh/Deneb/commit/4a1100fe2f37dc5712e8001affacbe674fe03f76))
* **docs:** remove capture heading gap ([#3729](https://github.com/choiceoh/Deneb/issues/3729)) ([8759593](https://github.com/choiceoh/Deneb/commit/87595933eb11cf70db9c83b95514e520f0d3feab))
* **htmlmd:** prevent slice-bounds panic on case-growing Unicode in raw tags ([#3680](https://github.com/choiceoh/Deneb/issues/3680)) ([09a12c4](https://github.com/choiceoh/Deneb/commit/09a12c4b2a791cd2f4bc08f204ff86a421299475))
* **mail:** SENTSINCE 거부 시 Date 헤더 후필터 폴백 — Andromeda 날짜 페이저 무한로딩 방지 ([#3709](https://github.com/choiceoh/Deneb/issues/3709)) ([085888e](https://github.com/choiceoh/Deneb/commit/085888e6b8a52e3ce82faa0b87257b27d9a4f323))
* **rsi:** reopen ineffective fixes on recurrence ([#3710](https://github.com/choiceoh/Deneb/issues/3710)) ([5b015db](https://github.com/choiceoh/Deneb/commit/5b015db4054f54bf7cd66149ac0219e1e69e63f7))
* **skill_lifecycle:** slim status payloads, raise MaxOutput, compact JSON ([#3691](https://github.com/choiceoh/Deneb/issues/3691)) ([99b0970](https://github.com/choiceoh/Deneb/commit/99b0970febe37b18343a330f0dfa23373aa027b9))
* **web:** search provider failure fallback, contract hygiene, fetch URL filter ([#3688](https://github.com/choiceoh/Deneb/issues/3688)) ([0b979b1](https://github.com/choiceoh/Deneb/commit/0b979b1f2d4398bb912c499eb03b892a4a2bcd76))


### ⚡ Performance

* **knowledge:** speed recall — graph corpus cache, skip agent rerank, 8s files ([#3687](https://github.com/choiceoh/Deneb/issues/3687)) ([9a8f49a](https://github.com/choiceoh/Deneb/commit/9a8f49a00453af0a3bd939b50f2e70afd9dee514))
* **mailarchive:** bound and cap per-message enrichment to cut latency ([#3692](https://github.com/choiceoh/Deneb/issues/3692)) ([621ecc5](https://github.com/choiceoh/Deneb/commit/621ecc52675cffca9bab0b560a662d5af78600fc))
* **web:** cut fetch latency — drop stealth sleep, gate LocalAI, Serper 10s ([#3682](https://github.com/choiceoh/Deneb/issues/3682)) ([2820e8f](https://github.com/choiceoh/Deneb/commit/2820e8f3b9498efa78bd47b3bdae60e348ff3f5f))


### 🔧 Internal

* **agent:** remove dead streaming attempt wrapper ([#3722](https://github.com/choiceoh/Deneb/issues/3722)) ([05b8e24](https://github.com/choiceoh/Deneb/commit/05b8e249e7bc92cd2988bb97d2c2686f75517f66))
* **rpc:** narrow miniapp handler wire contract ([#3730](https://github.com/choiceoh/Deneb/issues/3730)) ([15fbf2b](https://github.com/choiceoh/Deneb/commit/15fbf2b000d69d89ba247cdbdeb0240bbc25a824))
* **rsi:** narrow impact contract surface ([#3708](https://github.com/choiceoh/Deneb/issues/3708)) ([433fdef](https://github.com/choiceoh/Deneb/commit/433fdefe1493a2e3633b903a5a88c2c7f0976eca))
* **rsi:** stream and harden ledger reads ([#3683](https://github.com/choiceoh/Deneb/issues/3683)) ([73e72b2](https://github.com/choiceoh/Deneb/commit/73e72b213d2d493cb5e2685e1b5acddf78ef0d5d))

## [4.106.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.105.0...deneb-v4.106.0) (2026-07-15)


### ✨ Features

* **audit:** 스킬 검증 코퍼스 relevance 스윕 도구 — 오귀속 케이스 정화 ([#3677](https://github.com/choiceoh/Deneb/issues/3677)) ([65741b2](https://github.com/choiceoh/Deneb/commit/65741b2a4d6cce936df0190f6f56f8cbd65b6b0b))
* **search:** adopt qmd retrieval patterns ([#3678](https://github.com/choiceoh/Deneb/issues/3678)) ([8561adc](https://github.com/choiceoh/Deneb/commit/8561adc14d7a926b6e52dd14058bc996e147a05b))


### 🐛 Bug Fixes

* **genesis:** 스킬 검증 케이스 relevance 게이트 — 오귀속 코퍼스 오염 차단 ([#3673](https://github.com/choiceoh/Deneb/issues/3673)) ([fb2daac](https://github.com/choiceoh/Deneb/commit/fb2daac834f1d079f74dd8f8f59be82e6de98121))


### 🔧 Internal

* **health:** Health Bench 2.0 84.8→88.2 — contracts·fanout·tests·guides + baseline ([#3679](https://github.com/choiceoh/Deneb/issues/3679)) ([0d2d48b](https://github.com/choiceoh/Deneb/commit/0d2d48bd4127361e03fbbbb711bffa5e04c614f4))
* **rsi:** simplify status and dispatch ownership ([#3675](https://github.com/choiceoh/Deneb/issues/3675)) ([ec1f112](https://github.com/choiceoh/Deneb/commit/ec1f1128a0d830c3af02ec5bc30d508ef75c1c17))

## [4.105.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.104.0...deneb-v4.105.0) (2026-07-15)


### ✨ Features

* **chat:** append-only 선호 저장 + 위키 tombstone-forget 도구 ([#3664](https://github.com/choiceoh/Deneb/issues/3664)) ([e1b085b](https://github.com/choiceoh/Deneb/commit/e1b085b2532c87d97e574da63645ec3b2deb4246))
* **chat:** harden stream recovery and recall retrieval ([#3671](https://github.com/choiceoh/Deneb/issues/3671)) ([f906585](https://github.com/choiceoh/Deneb/commit/f9065858694a0ffc3883f9eee013bd0bdb2abb35))
* **chat:** 런타임 에이전트가 codegraph MCP로 자기 코드 조사 ([#3661](https://github.com/choiceoh/Deneb/issues/3661)) ([a8ad364](https://github.com/choiceoh/Deneb/commit/a8ad364541fd75782f63fd1b70370e4c31af2d86))
* **codegraph:** 검색 결과에 폴더 CLAUDE.md 맥락 자동 첨부 ([#3665](https://github.com/choiceoh/Deneb/issues/3665)) ([723af4e](https://github.com/choiceoh/Deneb/commit/723af4e847bc6d19104a7684cef7e20750ea9ca9))
* **codegraph:** 자기조사 인덱스 서치 최적화 (관련성·너지·신선도) ([#3663](https://github.com/choiceoh/Deneb/issues/3663)) ([f79d449](https://github.com/choiceoh/Deneb/commit/f79d4490456ca03de3f94084bef6df8cb897f9fc))


### 🐛 Bug Fixes

* **chat:** record skill usage after run completion ([#3662](https://github.com/choiceoh/Deneb/issues/3662)) ([17631c2](https://github.com/choiceoh/Deneb/commit/17631c280b9481d5ace389d9752f8eebab457bc7))
* **health:** andromeda·kotlin 테스트 intent-naming (Wave1 PR-B) ([#3654](https://github.com/choiceoh/Deneb/issues/3654)) ([7b0af73](https://github.com/choiceoh/Deneb/commit/7b0af73db6b6a3a17f55ed3dde10e67e2b6415ec))
* **health:** llm·events typed contracts (Wave1 PR-A) ([#3653](https://github.com/choiceoh/Deneb/issues/3653)) ([ef1f1f9](https://github.com/choiceoh/Deneb/commit/ef1f1f992b86e69a955cd917ceedb2e38f05ebff))
* **health:** server genesis 클러스터 skilllifecycle registrar 포트 (Wave1 PR-C) ([#3655](https://github.com/choiceoh/Deneb/issues/3655)) ([265a52b](https://github.com/choiceoh/Deneb/commit/265a52b9e4ebfb2916d70a41385887a4804bab8c))
* **health:** wiki revive unexported-return 복구 (export 재개방) ([#3658](https://github.com/choiceoh/Deneb/issues/3658)) ([68ecdf8](https://github.com/choiceoh/Deneb/commit/68ecdf8fe37d9fe2f4397fe7e2c3102f2a838b99))
* **health:** wiki 미사용 export unexport (Wave2 PR-E) ([#3657](https://github.com/choiceoh/Deneb/issues/3657)) ([3722f70](https://github.com/choiceoh/Deneb/commit/3722f70820e23fda38ee7a2a0cf7740babc25925))
* **rsi:** count dispatch retry history ([#3651](https://github.com/choiceoh/Deneb/issues/3651)) ([e157e63](https://github.com/choiceoh/Deneb/commit/e157e63e2c8dfeb24ff8d8dcae4a8a7c3a902127))
* **rsi:** harden dispatch accounting and recovery ([#3659](https://github.com/choiceoh/Deneb/issues/3659)) ([76063fb](https://github.com/choiceoh/Deneb/commit/76063fb63b3d0d466513816416c47f30258ad6a7))
* 가동 점검 — 런타임 실증 버그 2건 + 드리머 데이터손실 2건 ([#3660](https://github.com/choiceoh/Deneb/issues/3660)) ([0e4de32](https://github.com/choiceoh/Deneb/commit/0e4de3236d53fcb966e8f21f620ee2731228d00e))
* 가동 점검 후속 — genesis 로테이션/veto·RSI 패리티 3종·wiki dreamer ([#3666](https://github.com/choiceoh/Deneb/issues/3666)) ([bfd0de3](https://github.com/choiceoh/Deneb/commit/bfd0de3fcc5a593cbdbea7381d42ce5bb2ecba55))


### 🔧 Internal

* **rsi:** unify lifecycle orchestration ([#3670](https://github.com/choiceoh/Deneb/issues/3670)) ([a92a171](https://github.com/choiceoh/Deneb/commit/a92a1718253d7b4c6ddb54b81f94518a933d8ed7))
* **server:** split composition roots + thin serverwire Ports ([#3667](https://github.com/choiceoh/Deneb/issues/3667)) ([37899c4](https://github.com/choiceoh/Deneb/commit/37899c460a7838840a124df58621b830991acfc5))
* **wiki:** route consumers through stable wiki port ([#3668](https://github.com/choiceoh/Deneb/issues/3668)) ([866e88d](https://github.com/choiceoh/Deneb/commit/866e88d31e797424a44856c5e5043c2bb4dc2687))

## [4.104.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.103.0...deneb-v4.104.0) (2026-07-14)


### ✨ Features

* **genesis:** 메타 개정 구조성 계측(L1.5 함정)·K-후보 직교 탐색 렌즈 — Bilevel Autoresearch(2603.23420) 반영 ([#3645](https://github.com/choiceoh/Deneb/issues/3645)) ([98c6a27](https://github.com/choiceoh/Deneb/commit/98c6a271e466f9080c87152b207575054228a288))


### 🐛 Bug Fixes

* **native:** gate countdown expiry actions by interactivity ([#3650](https://github.com/choiceoh/Deneb/issues/3650)) ([597000b](https://github.com/choiceoh/Deneb/commit/597000b51379bf85ecde764ccccf0acec803f261))
* **rsi:** stop retrying declined dispatches ([#3647](https://github.com/choiceoh/Deneb/issues/3647)) ([dbd6c32](https://github.com/choiceoh/Deneb/commit/dbd6c32a7fad55528113bc686887125ff6344bde))


### 🔧 Internal

* Health Bench 2.0 점수 개선 — complexity tail·계약 축소·테스트 명명 (82.9→90 목표 1차) ([#3644](https://github.com/choiceoh/Deneb/issues/3644)) ([50aba65](https://github.com/choiceoh/Deneb/commit/50aba65311539dca7b00c46c5e53ea467b0dbb33))

## [4.103.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.102.0...deneb-v4.103.0) (2026-07-14)


### ✨ Features

* **rpc:** workfeed OnLadder 훅 — 졸업 카드 액션 디스패치 지점 ([#3627](https://github.com/choiceoh/Deneb/issues/3627)) ([ae06ed6](https://github.com/choiceoh/Deneb/commit/ae06ed652091169d7afdb4cc23fe5c8e1b109412))


### 🐛 Bug Fixes

* **rsi:** resolve GitHub CLI for dispatch outcomes ([#3640](https://github.com/choiceoh/Deneb/issues/3640)) ([2550867](https://github.com/choiceoh/Deneb/commit/2550867d5b7d45531aa489f5487d8f3c0e4d4fdd))

## [4.102.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.101.0...deneb-v4.102.0) (2026-07-13)


### ✨ Features

* **genesis:** 졸업 잠금 해제를 루프에 위임 — 증거 게이트 자동 실행 + 재잠금 비토 (오퍼레이터 지시 2026-07-14) ([#3626](https://github.com/choiceoh/Deneb/issues/3626)) ([fc067fc](https://github.com/choiceoh/Deneb/commit/fc067fc7fbc519aa23f523fe223c1bea956e497c))


### 🐛 Bug Fixes

* **deploy:** keep rollback watcher alive under systemd ([#3630](https://github.com/choiceoh/Deneb/issues/3630)) ([13fafe5](https://github.com/choiceoh/Deneb/commit/13fafe58d151d8bfebcd3559c309228c24a96b03))
* **deploy:** reject stale watcher acknowledgements ([#3631](https://github.com/choiceoh/Deneb/issues/3631)) ([d301257](https://github.com/choiceoh/Deneb/commit/d3012571033819f15eb0e7f691f28e8f6f5f7128))
* **genesis:** RSI 3차 적대적 리뷰 후속 — 이전 수정의 잔존 결함 교정 ([#3633](https://github.com/choiceoh/Deneb/issues/3633)) ([913f586](https://github.com/choiceoh/Deneb/commit/913f5869357f8830e26c4d44ec7d643577b199e5))
* **genesis:** RSI 코드 수준 평가 + 발견 전량 수정 (C1·C2·H1–H5·M2–M7) ([#3625](https://github.com/choiceoh/Deneb/issues/3625)) ([1983a24](https://github.com/choiceoh/Deneb/commit/1983a24db4d10b222719d5eae3c7c1c608c66254))
* **jsonlstore:** 오버사이즈 라인 skip — 손상 라인 하나가 스캔 전체를 죽이지 않게 ([#3637](https://github.com/choiceoh/Deneb/issues/3637)) ([bba9511](https://github.com/choiceoh/Deneb/commit/bba95114d7d27b9cac03223200969a20860ebf14))
* **rsi:** classify instant dispatch failures as environment errors ([#3632](https://github.com/choiceoh/Deneb/issues/3632)) ([50854b9](https://github.com/choiceoh/Deneb/commit/50854b9a02afa6b5a1613a0d02f4ba2bafc2be34))
* **rsi:** dispatch source improvements through Codex ([#3636](https://github.com/choiceoh/Deneb/issues/3636)) ([7f424b3](https://github.com/choiceoh/Deneb/commit/7f424b357253fea25358f1dfc2ab9c76750c3b9a))
* **rsi:** harden closed-loop delivery and dispatch ([#3628](https://github.com/choiceoh/Deneb/issues/3628)) ([cd74b65](https://github.com/choiceoh/Deneb/commit/cd74b65dba71259be0f538d814972095aa1e28a6))
* **rsi:** resolve Codex in systemd user services ([#3638](https://github.com/choiceoh/Deneb/issues/3638)) ([5c6affb](https://github.com/choiceoh/Deneb/commit/5c6affbc0053f4104e87112763ef596681c8a716))


### 🔧 Internal

* **chat:** split tool context contracts ([#3639](https://github.com/choiceoh/Deneb/issues/3639)) ([b3649f9](https://github.com/choiceoh/Deneb/commit/b3649f956780d17f80eeca0c53ff95c4c6af17d4))

## [4.101.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.100.0...deneb-v4.101.0) (2026-07-13)


### ✨ Features

* **andromeda:** RSI Pane 강화 — 진화 건강 카드·L2/L3 드릴·L4 후보 소스·자동배차 칩·상세 모달 ([#3595](https://github.com/choiceoh/Deneb/issues/3595)) ([e98e07a](https://github.com/choiceoh/Deneb/commit/e98e07ae9b2abf1d7bf7a7a44fda6fd8f445d109))
* close recursive self-improvement feedback loops ([#3622](https://github.com/choiceoh/Deneb/issues/3622)) ([2142504](https://github.com/choiceoh/Deneb/commit/2142504306cb785d6e8a8996f86aec7a70c38dad))
* **cursor:** auto worktree isolation + CodeGraph MCP wiring ([#3601](https://github.com/choiceoh/Deneb/issues/3601)) ([18def53](https://github.com/choiceoh/Deneb/commit/18def532606976b94e4a975cc69bedf7ad8d3d03))
* **genesis:** L2 회전에 genesis epoch 편입 — 3번째 진화 아티팩트 + 결정적 섀도 벤치 (P5-4 slice-2) ([#3617](https://github.com/choiceoh/Deneb/issues/3617)) ([9ab7b41](https://github.com/choiceoh/Deneb/commit/9ab7b41fc48b68405d603fc8dad43dde8b817bd5))
* **genesis:** L4 배차 결과 회계 — 마커에 랜딩/기각/실패 기록 + 랜딩률 집계 (졸업 사다리 증거) ([#3609](https://github.com/choiceoh/Deneb/issues/3609)) ([b0c9097](https://github.com/choiceoh/Deneb/commit/b0c9097913c36edbd74d5141b36a4dbd34c90ebb))
* **genesis:** L4 배차 계약 프롬프트 외부화 — 5번째 메타 아티팩트 (P5-4 slice-1) ([#3606](https://github.com/choiceoh/Deneb/issues/3606)) ([f1f7b8b](https://github.com/choiceoh/Deneb/commit/f1f7b8bfdb598be66200aca972a0fda64f606d34))
* **genesis:** 사다리 READY 전이 감시 — 증거 충족 시 능동 피드 카드 1회 발화 (ladder-watch) ([#3624](https://github.com/choiceoh/Deneb/issues/3624)) ([8d6d55a](https://github.com/choiceoh/Deneb/commit/8d6d55a983ec70574b65fb42e34707f34cf79951))
* **genesis:** 졸업 사다리 상시 심사 — 행별 증거 기계 판독 + GRAD 계기판 카드 (자율성 졸업의 증거화) ([#3620](https://github.com/choiceoh/Deneb/issues/3620)) ([24292f2](https://github.com/choiceoh/Deneb/commit/24292f2babe028e3b608da675dec64cdcb2f7bca))
* **genesis:** 커리큘럼 수요 소스 완결 — 실패한 사용자 요청 채굴 (P5-1 slice-4) ([#3603](https://github.com/choiceoh/Deneb/issues/3603)) ([c0a3600](https://github.com/choiceoh/Deneb/commit/c0a360001ef29fd72b7d4951f917f13e1e36b204))
* **genesis:** 판정자 프로브 커리큘럼 사다리 — drop 포화 시 제자리 약화 tier로 격상 (P3 라벨 기근 해소) ([#3602](https://github.com/choiceoh/Deneb/issues/3602)) ([92105d6](https://github.com/choiceoh/Deneb/commit/92105d63de90868a612bbc09bc22b7f227e35e6f))
* **native:** RSI 화면 강화 — 진화 건강 스코어보드 카드 + L4 큐 소스·자동배차 칩 ([#3591](https://github.com/choiceoh/Deneb/issues/3591)) ([5d518a7](https://github.com/choiceoh/Deneb/commit/5d518a75c3f5ba6dbca150b38ad895e7761d1cfa))
* **recall:** recall-health — 회상 평가 루프 닫기 (원장 효용·골드셋 커버리지·복합 점수·골드셋 공진화) ([#3599](https://github.com/choiceoh/Deneb/issues/3599)) ([fbbdf18](https://github.com/choiceoh/Deneb/commit/fbbdf18237f1ed35c743b55a105746bc26b382b8))
* **rsi:** 자가교정 루프 관측성·안정성 업그레이드 + 워크트리 가드 파일 경로 인식 ([#3605](https://github.com/choiceoh/Deneb/issues/3605)) ([188c400](https://github.com/choiceoh/Deneb/commit/188c4007c207fda19d731f77b52690044cb9bb3f))
* **wiki:** 드리머 효용접지 폐루프 — 회상-히트 원장·품질점수·오프라인 자기비평·적응 백로그드레인·프롬프트 외부화·프리필터 ([#3596](https://github.com/choiceoh/Deneb/issues/3596)) ([4b7d61a](https://github.com/choiceoh/Deneb/commit/4b7d61ae04440db00c7c3776ae268c11a0d21aca))


### 🐛 Bug Fixes

* **codegraph:** tighten index excludes + prefer node for exact symbols ([#3607](https://github.com/choiceoh/Deneb/issues/3607)) ([0e2c6f7](https://github.com/choiceoh/Deneb/commit/0e2c6f7918a90d826ab7a4ef6581101ed3cd6f68))
* **cursor:** harden worktree guard + bind CodeGraph to active-root ([#3604](https://github.com/choiceoh/Deneb/issues/3604)) ([d21d742](https://github.com/choiceoh/Deneb/commit/d21d7427d7d4710d338b017ad19384452d0de908))
* **dev:** codegraph-nudge F541 — 플레이스홀더 없는 f-string 접두사 제거 (main lint 오염 수리) ([#3611](https://github.com/choiceoh/Deneb/issues/3611)) ([a9dff18](https://github.com/choiceoh/Deneb/commit/a9dff18a2ce2edc271eb117a7cb60f213731a03e))
* **genesis:** RSI 상태·표면 티어·재오픈 캡 Go/Python 패리티 복구 ([#3610](https://github.com/choiceoh/Deneb/issues/3610)) ([0e8950e](https://github.com/choiceoh/Deneb/commit/0e8950ebe80fe338faa99e60026c13cdcc85f19e))
* **genesis:** verifier_broken 드리프트 신호를 must-catch 클래스 정확도로 스코프 ([#3608](https://github.com/choiceoh/Deneb/issues/3608)) ([a17ff7d](https://github.com/choiceoh/Deneb/commit/a17ff7d6081a3b62b20fc6701c9a6e6027e3a7b8))
* **native:** RSI '진화 건강' 카드 레이아웃 — L1 사이 18dp 간격 복구 + 지표 4+3 균형 배치 ([#3600](https://github.com/choiceoh/Deneb/issues/3600)) ([8942fc7](https://github.com/choiceoh/Deneb/commit/8942fc7244e557ae499c78a523bdef515b518b70))
* **native:** 표 tiny 열 폭 조임 — 헤더는 텍스트율(6dp)·숫자는 9dp로 분리('단계' 40→28dp) ([#3598](https://github.com/choiceoh/Deneb/issues/3598)) ([ab7b766](https://github.com/choiceoh/Deneb/commit/ab7b7662ef7a6a3c0d962369aea0428cde8371e7))
* **native:** 표 번호 열 폭 — 셀 데이터로 tiny 판정(헤더 제외) · '단계'류 짧은라벨 번호열도 좁게 ([#3594](https://github.com/choiceoh/Deneb/issues/3594)) ([9468cb1](https://github.com/choiceoh/Deneb/commit/9468cb11f4741340c338661a9d83193dae941e8f))
* **rsi:** abandoned L4 마커·스테일 워크트리 회수 ([#3623](https://github.com/choiceoh/Deneb/issues/3623)) ([61a4e19](https://github.com/choiceoh/Deneb/commit/61a4e199dbc499e680f8dedf6c13c3e83a063660))
* **rsi:** L4 마커 outcome 재배차·워크트리 검증·L2 freeze/status 패리티 ([#3614](https://github.com/choiceoh/Deneb/issues/3614)) ([187984b](https://github.com/choiceoh/Deneb/commit/187984b210470d6ff75ce9e287c6660810789110))
* **rsi:** L4 배차 교착·accepted 백로그 가시성·스윕 억제 ([#3612](https://github.com/choiceoh/Deneb/issues/3612)) ([680ec36](https://github.com/choiceoh/Deneb/commit/680ec36742de561eab32f0bb282ff9c4bfb82112))
* **rsi:** L4 스테일 워크트리 동기화·셋업 실패 시 다음 후보 ([#3615](https://github.com/choiceoh/Deneb/issues/3615)) ([d409c6f](https://github.com/choiceoh/Deneb/commit/d409c6f1f23b9afe1d9fba19306b6dd83a9c7f52))
* **rsi:** L4 재시도 브랜치 갱신·DispatchMarkerBlocks abandon 패리티 ([#3621](https://github.com/choiceoh/Deneb/issues/3621)) ([c942603](https://github.com/choiceoh/Deneb/commit/c94260365c119147343af75342f8b04b0909f012))
* **rsi:** 미흡수 봇 리뷰 지적 흡수 — L4 배차·스윕·Cursor 가드·RSI UI ([#3618](https://github.com/choiceoh/Deneb/issues/3618)) ([7fc2580](https://github.com/choiceoh/Deneb/commit/7fc2580657e7bfd0f8e92f6848e0727b915958d9))
* **rsi:** 배차 마커 status 병합·L2 진단 14일 카운트 패리티 ([#3616](https://github.com/choiceoh/Deneb/issues/3616)) ([7c05562](https://github.com/choiceoh/Deneb/commit/7c0556220f8deffe7f7011e78bf559a91f10e4a3))


### ⚡ Performance

* **mailarchive:** search·project_history의 IMAP 텍스트검색 폴백 제거 (미러가 정본) ([#3597](https://github.com/choiceoh/Deneb/issues/3597)) ([6a5b163](https://github.com/choiceoh/Deneb/commit/6a5b1639673e683dc16a803dadb232128a1f83c9))
* **mailarchive:** 한국어 검색 미스 시 CJK-무능 IMAP 폴백 스킵 + store 경로 로그화 ([#3592](https://github.com/choiceoh/Deneb/issues/3592)) ([1154bbe](https://github.com/choiceoh/Deneb/commit/1154bbe1a36e856a567efe780adb4f4d8dfd2786))

## [4.100.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.99.0...deneb-v4.100.0) (2026-07-13)


### ✨ Features

* **rsi:** RSI 화면용 데이터 노출 — 구조적 건강 블록·후보 자동배차 플래그·배차 허용목록 tool-quality ([#3588](https://github.com/choiceoh/Deneb/issues/3588)) ([03072bd](https://github.com/choiceoh/Deneb/commit/03072bd89a6041396440998dde7deb949181f03f))


### 🐛 Bug Fixes

* **andromeda:** 카드 stat 박스 제거 — 네이티브 파리티(카드 내 박스겹침 해소) ([#3590](https://github.com/choiceoh/Deneb/issues/3590)) ([0f9bd6f](https://github.com/choiceoh/Deneb/commit/0f9bd6fd79507b5a6a09e9401e2ad6706d8cb3b1))

## [4.99.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.98.0...deneb-v4.99.0) (2026-07-13)


### ✨ Features

* **native:** 카드 stat 수치 타이포·추세 칩 + 막대 그라디언트 · stat 박스 제거(카드 내 박스겹침 해소) ([#3586](https://github.com/choiceoh/Deneb/issues/3586)) ([38b4167](https://github.com/choiceoh/Deneb/commit/38b4167cae0af703ae3c604e17d19e5ef5db9132))

## [4.98.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.97.0...deneb-v4.98.0) (2026-07-13)


### ✨ Features

* **andromeda:** 우측 데네브 채팅 패널 기본 접힘 — 작업 영역 넓게, 우측 탭으로 열기 ([#3585](https://github.com/choiceoh/Deneb/issues/3585)) ([0358ad1](https://github.com/choiceoh/Deneb/commit/0358ad1db7089f4cee0b33ff88eba7b513894e1a))
* **andromeda:** 카드 비주얼 업그레이드 v1 — stat 수치 타이포(단위 서픽스·델타 칩)·막대차트 그라디언트/그리드 ([#3584](https://github.com/choiceoh/Deneb/issues/3584)) ([a9b4d4c](https://github.com/choiceoh/Deneb/commit/a9b4d4cd8e40dea9c9a666a3e885de59f147e379))
* **andromeda:** 카드 테이블 dense 모드 — 3열+ CJK 테이블 타입 한 단 낮춤(어절 fit·네이티브 파리티) ([#3581](https://github.com/choiceoh/Deneb/issues/3581)) ([839c42d](https://github.com/choiceoh/Deneb/commit/839c42de2967f1595d7bf5a56f4559a48cafeaad))


### 🐛 Bug Fixes

* **proactive:** "메일 N번" 인덱스 숫자에 속아 진행 서술이 피드 카드로 누출되던 것 수정 ([#3583](https://github.com/choiceoh/Deneb/issues/3583)) ([510c4bc](https://github.com/choiceoh/Deneb/commit/510c4bc09a62e087c77f10787aea3120dcb2bc67))

## [4.97.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.96.0...deneb-v4.97.0) (2026-07-13)


### ✨ Features

* **andromeda:** 능동형 질문 카드 데스크톱 답변 어포던스 — 피드 상세 답변 칩(action.run)·question 플래그 존중 ([#3564](https://github.com/choiceoh/Deneb/issues/3564)) ([43c5524](https://github.com/choiceoh/Deneb/commit/43c5524696b2cfc7de80f9524c53197ff91c0df7))
* **andromeda:** 데스크톱 카드 노드 렌더 파리티 — 카운트다운 라이브 틱·미결정 진행바·2글자 아바타/아이콘 폴백·secondary 뱃지 분리 ([#3571](https://github.com/choiceoh/Deneb/issues/3571)) ([409bf12](https://github.com/choiceoh/Deneb/commit/409bf12dfb27146699f0423b8737cf00e0defa9f))
* **andromeda:** 카드 노드 렌더 파리티 2탄 — box 정렬·이미지 대체박스·슬라이더 범위정규화·아이콘 매핑 확장 ([#3577](https://github.com/choiceoh/Deneb/issues/3577)) ([7fc26bb](https://github.com/choiceoh/Deneb/commit/7fc26bbc25b54a9093d402338c543ff1db90b4d8))
* **andromeda:** 카드 막대차트 세로 SVG 파리티 — 가로 CSS 막대→네이티브 캔버스형 세로 컬럼(값 라벨 상단·0=막대없음) ([#3573](https://github.com/choiceoh/Deneb/issues/3573)) ([8130a06](https://github.com/choiceoh/Deneb/commit/8130a0630175e84158de62a2beed6d477f750865))
* **chat:** deneb-ui 카드 채택률 분모 신호 — 카드 저작 턴 Info 로그(adoption-miss와 대칭) ([#3570](https://github.com/choiceoh/Deneb/issues/3570)) ([25d5e6c](https://github.com/choiceoh/Deneb/commit/25d5e6c98556d27ea52df3ebc933722517f31e76))
* **genesis:** deadcode-audit 델타 마이너 — P5-3 선제 L4 공급 2번째 슬라이스 ([#3569](https://github.com/choiceoh/Deneb/issues/3569)) ([e0eeaed](https://github.com/choiceoh/Deneb/commit/e0eeaed80b4ccb70875d21406c625619de8dddf8))
* **genesis:** 도구-품질 마이너 — agentlog 도구 오류·인자수리율을 도구설명 개선 후보로 (Lane A, 재귀표면 확대) ([#3578](https://github.com/choiceoh/Deneb/issues/3578)) ([245c094](https://github.com/choiceoh/Deneb/commit/245c094ba692021b67fed68155c8fd3a09090d5e))
* **genesis:** 도구-품질 마이너에 지연 트리거(회귀+도구별 기대치) + tool-quality 자동배차 졸업 ([#3580](https://github.com/choiceoh/Deneb/issues/3580)) ([69f84f0](https://github.com/choiceoh/Deneb/commit/69f84f01cffda80b8840d1a8a59573abed284c37))
* **genesis:** 커리큘럼 캘린더 커버리지-갭 수요 소스 — P5-1 slice-3 ([#3576](https://github.com/choiceoh/Deneb/issues/3576)) ([9a99e2e](https://github.com/choiceoh/Deneb/commit/9a99e2e1a58c436c82ea1ea1cbafe6e3e6d254cb))
* **mailanalysis:** 메일 능동 카드에 '결정=인터랙티브' 계약 — 결정 지점 선택지 칩(choices→answer) ([#3575](https://github.com/choiceoh/Deneb/issues/3575)) ([80071af](https://github.com/choiceoh/Deneb/commit/80071af5aaa2fc0aff17f498346fe163d6d50974))
* **native:** deneb-ui stat 노드 타일 크롬 — 데스크톱 .dui-stat 파리티 (+선존 spotless 정리·패치노트) ([#3579](https://github.com/choiceoh/Deneb/issues/3579)) ([8462e8f](https://github.com/choiceoh/Deneb/commit/8462e8f9bc92eeeb9c0b0fe0fb1399b40a574f47))


### 🐛 Bug Fixes

* **chat:** 채팅 화면 진입 시 맨 아래까지 자동스크롤 복원 ([#3554](https://github.com/choiceoh/Deneb/issues/3554) 회귀) ([#3572](https://github.com/choiceoh/Deneb/issues/3572)) ([166dd3c](https://github.com/choiceoh/Deneb/commit/166dd3c97c0fbe1d744e72d75df3e41b82129343))
* **mail:** CJK 인코딩 보낸이/제목 헤더 디코딩 — GB18030/GBK/Big5/Shift-JIS 추가 ([#3566](https://github.com/choiceoh/Deneb/issues/3566)) ([f87bbad](https://github.com/choiceoh/Deneb/commit/f87bbadb529a953a8a05dea8588c5b8a79974817))
* **native:** size numbering table columns to content, not weight share ([#3567](https://github.com/choiceoh/Deneb/issues/3567)) ([e98f9f6](https://github.com/choiceoh/Deneb/commit/e98f9f6e7ac1eef4f20bfdc2e197f2e591ff2721))


### 🔧 Internal

* **server:** 커리큘럼 env-digest를 runtime/curriculumenv로 추출 — composition root 책임 축소 ([#3574](https://github.com/choiceoh/Deneb/issues/3574)) ([a481ff7](https://github.com/choiceoh/Deneb/commit/a481ff7f619fb2f43bdc57bb227594b3bb51e6f4))

## [4.96.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.95.0...deneb-v4.96.0) (2026-07-13)


### ✨ Features

* **chat:** 능동형 deneb-ui 카드 2/2 — 하트비트 카드 규칙·저신뢰 verdict 사유·채택률 관측(run_card_health) ([#3561](https://github.com/choiceoh/Deneb/issues/3561)) ([573164d](https://github.com/choiceoh/Deneb/commit/573164d6da68cd16f37b20329d9ed70f21a953ed))
* **proactive:** 능동형 deneb-ui 카드 1/2 — 메일분석 카드 계약(양경로) + relay 카드 바이패스 ([#3560](https://github.com/choiceoh/Deneb/issues/3560)) ([dd8320f](https://github.com/choiceoh/Deneb/commit/dd8320fdbdd0021b4387063d98b47ce1e23bec59))
* **rsi:** 2차 패스 후보 3건 구현 — 커리큘럼 출처 접지 게이트 · BINEVAL 자문 방향 · SOP 마이너 ([#3557](https://github.com/choiceoh/Deneb/issues/3557)) ([715602b](https://github.com/choiceoh/Deneb/commit/715602b8942439a5ff99f0c83e1042b37b0ec93e))


### 🐛 Bug Fixes

* **chat:** 리뷰 반영 — 표 구분행 셀 단위 파싱(정렬콜론 허용·산문 대시 오탐 제거) ([#3562](https://github.com/choiceoh/Deneb/issues/3562)) ([e73d4f7](https://github.com/choiceoh/Deneb/commit/e73d4f72465e4612cc3efa0e7eeda4cc9f1b273e))
* **genesis:** 리뷰 반영 — 저신뢰 라우팅 사유 빈 reason 구분자 정리(annotateReason) ([#3563](https://github.com/choiceoh/Deneb/issues/3563)) ([b791e33](https://github.com/choiceoh/Deneb/commit/b791e33164b58f02967085685e228d5f77166c96))

## [4.95.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.94.0...deneb-v4.95.0) (2026-07-13)


### ✨ Features

* **genesis:** e-process 컷오버 메커니즘 + L3 실전 false-accept 라벨 수확 ([#3550](https://github.com/choiceoh/Deneb/issues/3550)) ([0ce16dc](https://github.com/choiceoh/Deneb/commit/0ce16dc85fec16ef15d92565e1e2793e3adcff10))
* **genesis:** RSI 2026H2 논문 애드덤 + 차기 커밋 후보 6건 전체 구현 ([#3555](https://github.com/choiceoh/Deneb/issues/3555)) ([ec65ce1](https://github.com/choiceoh/Deneb/commit/ec65ce1f3899aa53150013478ac196acecbda070))
* **genesis:** RSI P5-1 커리큘럼 환경 다이제스트 배선 — EnvDigest 클로저 주입 ([#3553](https://github.com/choiceoh/Deneb/issues/3553)) ([37574c8](https://github.com/choiceoh/Deneb/commit/37574c8e3a1046db2eedea69aeeaab11219ea5c0))


### 🐛 Bug Fixes

* **andromeda:** Cargo.lock 앱 버전 0.0.70 동기화 — release 커밋 이후 깨진 cargo check --locked 복구 ([#3552](https://github.com/choiceoh/Deneb/issues/3552)) ([465bfe9](https://github.com/choiceoh/Deneb/commit/465bfe99aab223c190521e9a67d4a5000e9a256e))
* **chat:** 마지막 줄이 입력창에 가리지 않게 — scrollToTrueBottom 으로 contentPadding 보정 ([#3554](https://github.com/choiceoh/Deneb/issues/3554)) ([177f81b](https://github.com/choiceoh/Deneb/commit/177f81b0446f63f29dfaccaa8be95165f32040ca))

## [4.94.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.93.0...deneb-v4.94.0) (2026-07-12)


### ✨ Features

* **chat:** 인터랙티브 카드 발화 트리거 격상 — 결정 지점 기본화 + collect 필수 명시 ([#3545](https://github.com/choiceoh/Deneb/issues/3545)) ([24b7798](https://github.com/choiceoh/Deneb/commit/24b779859ac52f51dd466d736984f3a8923cbca7))
* **genesis:** RSI P5-5 런타임 건강 자문 증거 — latency[#1](https://github.com/choiceoh/Deneb/issues/1) 신호 접지(게이트 불가침) ([#3540](https://github.com/choiceoh/Deneb/issues/3540)) ([ecfae43](https://github.com/choiceoh/Deneb/commit/ecfae432542daf9aba1c3a464d735375ae1af1d9))
* **genesis:** RSI P5-5 코드베이스 건강 자문 증거 — quality-bench 접지(게이트 불가침) ([#3541](https://github.com/choiceoh/Deneb/issues/3541)) ([dcfb889](https://github.com/choiceoh/Deneb/commit/dcfb889ab3841e64ad314f398eb43aeca788f332))
* **genesis:** 스킬 효용 기반 아카이브 — 수리불가 저성과 스킬 제거(가역), 유휴 커레이터의 사각 보완 ([#3547](https://github.com/choiceoh/Deneb/issues/3547)) ([5fdbd3c](https://github.com/choiceoh/Deneb/commit/5fdbd3cd026be5ea1bcef2724841df2a482b59e4))


### 🐛 Bug Fixes

* **chat:** imePadding을 입력바로 이동 — scrollBy follow-scroll 제거, 마지막 메시지 자가 정렬 ([#3537](https://github.com/choiceoh/Deneb/issues/3537) 후속) ([#3542](https://github.com/choiceoh/Deneb/issues/3542)) ([407e847](https://github.com/choiceoh/Deneb/commit/407e847158ffc8d8ad8a04011a1229a879a0350a))
* **chat:** 키보드 follow-scroll을 viewport 기반으로 — 마지막 메시지가 입력창 위로 완전히 올라옴 ([#3528](https://github.com/choiceoh/Deneb/issues/3528)) ([#3537](https://github.com/choiceoh/Deneb/issues/3537)) ([c2d8bcf](https://github.com/choiceoh/Deneb/commit/c2d8bcf2be44afe413562b2ad3f206e2c1fdbf87))
* **deploy:** publish-apk가 fossRelease R8 매핑(mapping.prt)을 APK 옆에 게시 — 크래시 리트레이스 재빌드 제거 ([#3546](https://github.com/choiceoh/Deneb/issues/3546)) ([24ca3d6](https://github.com/choiceoh/Deneb/commit/24ca3d60406ed0e3241e9307e214635457a27c25))
* **genesis:** 자가수정 후보의 유령 SKILL.md 경로 해소 — 실경로 해석 + read 레이아웃 폴백 ([#3544](https://github.com/choiceoh/Deneb/issues/3544)) ([ab352a7](https://github.com/choiceoh/Deneb/commit/ab352a7527ea9b9aa4904e99218e6c2c0505bc88))
* **native:** 스트림 취소 크래시 잔여 표면 봉쇄 — 취소 가능한 HTTP 스코프 4곳에 teardown-tolerant CEH ([#3340](https://github.com/choiceoh/Deneb/issues/3340) 후속) ([#3538](https://github.com/choiceoh/Deneb/issues/3538)) ([28442df](https://github.com/choiceoh/Deneb/commit/28442dfc63e65a76e10a0f9f230a478a9cea7989))
* **rsi:** 디스패치 즉시실패 시 마커 릴리스 — 환경 문제로 후보 소진 방지 ([#3543](https://github.com/choiceoh/Deneb/issues/3543)) ([9afef27](https://github.com/choiceoh/Deneb/commit/9afef27b2d818eec975e807d79991190f800f2e0))

## [4.93.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.92.0...deneb-v4.93.0) (2026-07-12)


### ✨ Features

* **andromeda:** RSI 허브 드릴다운 — L1/L4 카드 인라인 상세 ([#3509](https://github.com/choiceoh/Deneb/issues/3509)) ([45157f4](https://github.com/choiceoh/Deneb/commit/45157f4d133d7e4c8c720e72722e6d4ca2a9a124))
* **andromeda:** 데스크톱 표 정합 — fit-first 랩·어절 줄바꿈·숫자 열 자동 정렬·tabular figures ([#3517](https://github.com/choiceoh/Deneb/issues/3517)) ([e0ba611](https://github.com/choiceoh/Deneb/commit/e0ba611fbf14aab87bd2a44c31b18e046aa47bca))
* **genesis:** RSI P5-1 커리큘럼 레인 — 커버리지 갭 수요 채굴 (propose-only) ([#3515](https://github.com/choiceoh/Deneb/issues/3515)) ([faf0e48](https://github.com/choiceoh/Deneb/commit/faf0e48a4c4303b55bcb1498ed079f3737c104fc))
* **genesis:** RSI P5-5 외부 피트니스 접지 — feed-card 자문 증거(게이트 불가침) ([#3533](https://github.com/choiceoh/Deneb/issues/3533)) ([48de1df](https://github.com/choiceoh/Deneb/commit/48de1df83cba2e8b399c964c51cbae79697f6c9a))
* **native:** 산문 수직 리듬 — 리스트 항목 이중 패딩 역전 수정 + 메시지 코퍼스 하네스 ([#3524](https://github.com/choiceoh/Deneb/issues/3524)) ([04dc4dc](https://github.com/choiceoh/Deneb/commit/04dc4dcf54763c58d705e393ba415f6f411a8973))
* **native:** 수직 리듬 스케일 완성 — 시작/섹션 스택 해소·그룹 대비 강화·문단 경계 명확화 ([#3529](https://github.com/choiceoh/Deneb/issues/3529)) ([46581f2](https://github.com/choiceoh/Deneb/commit/46581f2ea6edb990450f75b7864554c55c9f3ab1))
* **native:** 채팅 마크다운 표에 카드 표 처방 이식 — 평균 비례 폭·밀집 타이포·어절 줄바꿈·숫자 열 자동 정렬 ([#3514](https://github.com/choiceoh/Deneb/issues/3514)) ([f3a4f42](https://github.com/choiceoh/Deneb/commit/f3a4f4252a16a4b2a67e20ace20466c21e42b204))
* **rsi:** L4 졸업 — health-finding 소스 디스패치 allowlist 개방 (첫 배치 7건 리뷰 클린) ([#3530](https://github.com/choiceoh/Deneb/issues/3530)) ([777ed69](https://github.com/choiceoh/Deneb/commit/777ed697d53eeb56962055eaf04d44ece4c09fda))
* **rsi:** P5 선제 L4 공급 — health-finding 마이너 + record RPC + staged 가시성 ([#3528](https://github.com/choiceoh/Deneb/issues/3528)) ([f17d98f](https://github.com/choiceoh/Deneb/commit/f17d98ff119b0baf71025ab765c1a5db5d22d2a3))


### 🐛 Bug Fixes

* **audit:** health-v2 이력 측정에 shallow-truncation 가드 — 로컬/CI 점수 패리티 ([#3526](https://github.com/choiceoh/Deneb/issues/3526)) ([d8d9690](https://github.com/choiceoh/Deneb/commit/d8d969071bff251b9f343ea7748ff030701d0877))
* **chat:** 스트리밍 턴을 요청 커넥션에서 분리 — 백그라운드 전환 시 답변 유실·세션 종료 해소 ([#3522](https://github.com/choiceoh/Deneb/issues/3522)) ([42229ea](https://github.com/choiceoh/Deneb/commit/42229eaa0344db77d60f77692f7a538a36ca25bf))
* **chat:** 스트리밍·비동기 턴의 마켓 레터 토큰 생노출 차단 — per-turn persist·async 파이널라이즈 치환 배선 ([#3516](https://github.com/choiceoh/Deneb/issues/3516)) ([30a7266](https://github.com/choiceoh/Deneb/commit/30a7266e04c630bdfbbaac8eb4c317013f40f16f))
* **deploy:** deploy-watch 롤백 재시작 경로 수리 — RefuseManualStop 거부 우회(kill -TERM MainPID) ([#3531](https://github.com/choiceoh/Deneb/issues/3531)) ([96876b1](https://github.com/choiceoh/Deneb/commit/96876b1b462443d098aa20a68e2b39900e4224dd))
* **genesis:** 커리큘럼 프롬프트 단언 규칙 명시 — 잘린 조각 단언 방지 (프로드 1사이클 실증 반영) ([#3527](https://github.com/choiceoh/Deneb/issues/3527)) ([58f8add](https://github.com/choiceoh/Deneb/commit/58f8addea6dc7c06b75ecdb75aeab1916c7f5252))
* **rsi:** coding-dispatch.sh 실행권한 복구 — systemd 203/EXEC 반복 실패 해소 ([#3532](https://github.com/choiceoh/Deneb/issues/3532)) ([c0f29d4](https://github.com/choiceoh/Deneb/commit/c0f29d43de2a4296cc2cd1cee1b5bab61f906cbc))
* **rsi:** 디스패치 상태 계약 정합 — 리뷰 승인(accepted) 후보를 우선 배차 ([#3534](https://github.com/choiceoh/Deneb/issues/3534)) ([7152d8c](https://github.com/choiceoh/Deneb/commit/7152d8cbefd55ba59f9ecb55fe6522a2c0f8d92f))


### 🔧 Internal

* **briefcase:** clarify artifact export stages ([708d1c5](https://github.com/choiceoh/Deneb/commit/708d1c5f610278f53f9478ea5658842c7223e2b4))
* **briefcase:** clarify artifact grading decisions ([e9a54a5](https://github.com/choiceoh/Deneb/commit/e9a54a54c9b9fb88d0a81f32da5a0c85ae11d4f3))
* **briefcase:** clarify episode validation boundaries ([eb60966](https://github.com/choiceoh/Deneb/commit/eb609669481a5208e96a22411698f6654e067802))
* **briefcase:** clarify pure grep traversal ([9508a8e](https://github.com/choiceoh/Deneb/commit/9508a8ea350c854a6e8c91ee3f7d91b688a7e035))
* **chat:** isolate link enrichment lifecycle ([979dc6c](https://github.com/choiceoh/Deneb/commit/979dc6cdeda09cbfdf6b6d40d8a412e724636690))
* **config:** clarify runtime resolution stages ([e34762b](https://github.com/choiceoh/Deneb/commit/e34762bd881ad14832858d0a68cfb8baf5c564ef))
* **data-gen:** clarify generation stages ([7f1b6ec](https://github.com/choiceoh/Deneb/commit/7f1b6eca4dc233a5afee3d97f3e1a30c0d565373))
* **denebui:** clarify structured HTML conversion ([2502bb0](https://github.com/choiceoh/Deneb/commit/2502bb075812d49928457271c160db537058001c))
* **genesis:** clarify failure evidence clustering ([1d2d461](https://github.com/choiceoh/Deneb/commit/1d2d461d952dfa2cfe6a6c9746ee0d68834dc2cc))
* **mailarchive:** clarify archive search decisions ([f57d98e](https://github.com/choiceoh/Deneb/commit/f57d98ec042f89b0b718cd44aaa382b6a778c2c3))
* **mailarchive:** isolate local state overlay ([1ee7814](https://github.com/choiceoh/Deneb/commit/1ee7814a33920939e5a28120006ffc5f56691a03))
* **mediatokens:** separate parsing decisions ([e1fac5f](https://github.com/choiceoh/Deneb/commit/e1fac5f2f2876be49652abebf9766ae2282acbdf))
* **recall-bench:** separate benchmark stages ([5b4cc17](https://github.com/choiceoh/Deneb/commit/5b4cc17933d742674bd8ce7249e5d4499291a028))
* **recall:** clarify org evidence decisions ([9c02214](https://github.com/choiceoh/Deneb/commit/9c02214bdf67ed28024ab4777721e349f6154100))
* **runtime:** move Fleet alert ownership ([66b3952](https://github.com/choiceoh/Deneb/commit/66b39520a6806e0f00b140895950237b93c5e40f))
* **runtimeops:** clarify deferred tool activation ([8ae8604](https://github.com/choiceoh/Deneb/commit/8ae860450aff6eccf0110243967929419847dc96))
* **runtimeops:** clarify exec cache decisions ([e4489e1](https://github.com/choiceoh/Deneb/commit/e4489e156d4c77649f97ca59f44cb0f7e5776765))
* **server:** isolate model maintenance wiring ([b67afb7](https://github.com/choiceoh/Deneb/commit/b67afb70f6cbe08987f36c45b75e82b8e18eb5f9))
* **toolreg:** split workspace registration groups ([268a311](https://github.com/choiceoh/Deneb/commit/268a3117bf84527f3c2662b2c39f0fe81963a6be))
* **wiki:** centralize contact identity contract ([447bdb3](https://github.com/choiceoh/Deneb/commit/447bdb39cb9bafffe14de5a23f751e32a1fc6b9e))

## [4.92.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.91.0...deneb-v4.92.0) (2026-07-12)


### ✨ Features

* **andromeda:** 미학적 폴리싱 — GridNotice 스피너/재시도·접근성·디자인 토큰 정합 ([265792f](https://github.com/choiceoh/Deneb/commit/265792f8dde4a043806a7479a0d87a7c204f4aff))
* **andromeda:** 재귀적 자가개선 루프 상태 페인 — L1-L4 상태 카드 ([#3497](https://github.com/choiceoh/Deneb/issues/3497)) ([b2d31ac](https://github.com/choiceoh/Deneb/commit/b2d31accf480395a2d5ec8c71648e553af267b12))
* **audit:** RSI lifecycle 로그 패턴 마이너 — rsi-lifecycle-mine.py ([5e7b4fa](https://github.com/choiceoh/Deneb/commit/5e7b4fa2ea09018979fb592e946058aab6579b9a))
* **audit:** RSI 루프 상태 감사 도구 — scripts/audit/rsi-loop-audit.py ([dc36942](https://github.com/choiceoh/Deneb/commit/dc3694257c1d735c26e31433bb39ac0d25f80989))
* **denebui:** 카드 렌더 폴리싱 — 상태 톤·코드 크롬·아이콘/차트 패리티·조형 계약 ([#3498](https://github.com/choiceoh/Deneb/issues/3498)) ([31ccf3c](https://github.com/choiceoh/Deneb/commit/31ccf3c4db783c76c720ac917097fe454f5cec3c))
* **native:** RSI 허브 드릴다운 — L1/L4 카드 인라인 상세 ([#3505](https://github.com/choiceoh/Deneb/issues/3505)) ([f62bb54](https://github.com/choiceoh/Deneb/commit/f62bb54abcee35635f74f5fcb87ca2d0423bc5d8))
* **native:** 미학적 폴리싱 — dead code 제거·shimmer 토큰 정합·Fleet 로딩 skeleton ([1cc2336](https://github.com/choiceoh/Deneb/commit/1cc23365c0abdd72853aa0395a19c330f4da4a70))
* **native:** 재귀적 자가개선 루프 상태 화면 (2/3) ([#3496](https://github.com/choiceoh/Deneb/issues/3496)) ([42033e0](https://github.com/choiceoh/Deneb/commit/42033e00c15d73660b3e49f4a20302524731c47a))
* **wiki:** ingest Supernote Manta handwritten notes via Google Drive ([#3494](https://github.com/choiceoh/Deneb/issues/3494)) ([3b00dd5](https://github.com/choiceoh/Deneb/commit/3b00dd57404595815bdbbf8938df4cb94913de1c))
* **wiki:** 현장 방문·회의 참석 자동 기억 (응고 격차 메우기) ([#3500](https://github.com/choiceoh/Deneb/issues/3500)) ([a44d138](https://github.com/choiceoh/Deneb/commit/a44d13853f2f38040291dcce20382e09313f5b23))


### 🐛 Bug Fixes

* **ci:** close Health Bench follow-up checks ([#3504](https://github.com/choiceoh/Deneb/issues/3504)) ([e165101](https://github.com/choiceoh/Deneb/commit/e16510192dbc723b9dd7901fc3e36d5bd9e67334))
* **denebui:** 관용 렌더러 — 프로즈에 붙은 펜스 오프너 인식, 인라인 공백 보존, ul 순서·표 섹션 보정 ([#3499](https://github.com/choiceoh/Deneb/issues/3499)) ([4ed1b71](https://github.com/choiceoh/Deneb/commit/4ed1b71b0dafc24be023458020e83c45e227c755))
* **denebui:** 관용 렌더러 2차 — 글루된 클로저·원라이너 펜스·아이콘 카탈로그 확충 ([#3503](https://github.com/choiceoh/Deneb/issues/3503)) ([dcb9816](https://github.com/choiceoh/Deneb/commit/dcb98162249fac5e9c13f9c5ecaba7179513936b))
* **denebui:** 렌더 정렬 3차 — 피드/푸시 펜스 누출 근본수정·채팅 이중 카드 제거·required 피드백·조형 관측 ([#3506](https://github.com/choiceoh/Deneb/issues/3506)) ([f227075](https://github.com/choiceoh/Deneb/commit/f227075e341c3d93ab05c6a8dab05fbb663fde2c))
* **native:** 코드 품질 — dead stability entries·FeedScreen key·Display 액션 no-op 수정 ([59853b2](https://github.com/choiceoh/Deneb/commit/59853b22ae5d2716570ffa3490b777b2811016d8))
* **rsi:** RSI 뷰어 한글화 + 카드 탭 상세(층 역할 설명) ([#3502](https://github.com/choiceoh/Deneb/issues/3502)) ([46772c2](https://github.com/choiceoh/Deneb/commit/46772c2bfd136487d1b0d41f06647fa8ced7ffdf))


### 🔧 Internal

* improve runtime health and Health Bench 2.2 ([#3501](https://github.com/choiceoh/Deneb/issues/3501)) ([4b3882f](https://github.com/choiceoh/Deneb/commit/4b3882f2652434fe5a2b3ec69e2d8eef4db9971f))
* **native:** GeneratingBackdrop 매직넘버 → DenebMotion.DurationSlow 토큰 ([3c3b50f](https://github.com/choiceoh/Deneb/commit/3c3b50f121e38e988818cd29bb61eb7bd84c099c))
* **native:** LogoAnimation.kt 삭제 — dead code (호출처 제로) + hardcoded purple 제거 ([df99472](https://github.com/choiceoh/Deneb/commit/df99472737502cff3e42d59221a5582b40490dc0))

## [4.91.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.90.0...deneb-v4.91.0) (2026-07-12)


### ✨ Features

* **codegraph:** rpcmap 이벤트 브로드캐스트 패턴 + 편집 후 자동 sync 훅 ([077f602](https://github.com/choiceoh/Deneb/commit/077f6025380337a5b7bb2a9e7325bcfa0ab27ae9))
* **genesis:** 런타임 에러 마이닝 — 반복 코드성 에러를 propose-only L4 후보로(에러 마이닝 소스 신설) ([#3491](https://github.com/choiceoh/Deneb/issues/3491)) ([ab8af73](https://github.com/choiceoh/Deneb/commit/ab8af731138384c884c4000d1c26f16df327553e))
* **rsi:** 재귀적 자가개선 루프 상태 RPC — miniapp.rsi.status + //deneb:wire(양 클라 생성) ([#3492](https://github.com/choiceoh/Deneb/issues/3492)) ([2f485dc](https://github.com/choiceoh/Deneb/commit/2f485dc9217e487724482d2d7a4ab8c488027326))
* **wiki:** OpenWiki 패턴 도입 — 브리프·질문 생애주기·인제스트 방어·wiki-scout·노티 기억화 ([#3484](https://github.com/choiceoh/Deneb/issues/3484)) ([10a6183](https://github.com/choiceoh/Deneb/commit/10a61835a6dd38dc101c0afd1dfbf272c0b4a639))
* **zcode:** codegraph-remind Read 매처 추가 + push 자동화 스크립트 ([b9cb893](https://github.com/choiceoh/Deneb/commit/b9cb893181fddee5ddc8489a279f4ec55ad611c0))
* **zcode:** 로컬 검증 커밋 래퍼 + 워크트리 정리 스크립트 ([3071eb2](https://github.com/choiceoh/Deneb/commit/3071eb26e5891487b4ed9f30e622c0bae45bb5d8))


### 🐛 Bug Fixes

* **gateway:** 네이티브 모델 설정 복구 — modelpicker 등록을 late 단계로 이동 ([#3457](https://github.com/choiceoh/Deneb/issues/3457) nil 스냅샷 회귀) ([#3490](https://github.com/choiceoh/Deneb/issues/3490)) ([cdd025e](https://github.com/choiceoh/Deneb/commit/cdd025e2466173e7790682c63917a373aa93d037))
* **genesis:** rejected-evolve 검증 드래프트 스킬당 dedup — 프로드 4중복 유출 차단 ([#3489](https://github.com/choiceoh/Deneb/issues/3489)) ([2f4ff60](https://github.com/choiceoh/Deneb/commit/2f4ff6076f93f6a9684780eef1f77fcb1e6bb419))
* **genesis:** 자가개선 스톨 하드닝 — sweep 필수-판정 계약·연속 무시 에스컬레이션(레버C)·dev 넛저 프로드 게이트 ([#3487](https://github.com/choiceoh/Deneb/issues/3487)) ([fe9cb9f](https://github.com/choiceoh/Deneb/commit/fe9cb9f456aeeea36904f7e5643226bfabfb8013))

## [4.90.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.89.0...deneb-v4.90.0) (2026-07-12)


### ✨ Features

* **audit:** RSI 루프 상태 계기판 — 층별 LIVE/DATA-GATED/STARVED 정직 분류 ([#3485](https://github.com/choiceoh/Deneb/issues/3485)) ([4b7c308](https://github.com/choiceoh/Deneb/commit/4b7c3084827e96462e76ffa5595497adb2f9bb01))
* **genesis:** P3 데이터게이트 해소 — 미묘 열화로 정직한 judge miss 생산(깊이) ([#3482](https://github.com/choiceoh/Deneb/issues/3482)) ([a265085](https://github.com/choiceoh/Deneb/commit/a265085e4b71a119cd2d321b27c6d2477325ec03))
* **genesis:** P3 루프 폐쇄 — evaluator epoch가 판정자 자기 오판을 grounding으로 소비(깊이) ([#3483](https://github.com/choiceoh/Deneb/issues/3483)) ([450c4e8](https://github.com/choiceoh/Deneb/commit/450c4e8ee7e5d0abd6043460e13a6c9536b16eef))
* **genesis:** 적대적 커버리지 폭 넓히기 — 행동적(도구 참조) 변이 + 레인 등록(활성화) ([#3480](https://github.com/choiceoh/Deneb/issues/3480)) ([1e27a75](https://github.com/choiceoh/Deneb/commit/1e27a755744abab6d7a3735655a78ecf5442c926))


### 🐛 Bug Fixes

* **genesis:** 리뷰어 피드백 배치 — download-token verify fail-closed·watch-expired mu·오퍼레이터 채택 롤백워치·메타 헤더/로드맵 정합 ([#3479](https://github.com/choiceoh/Deneb/issues/3479)) ([0663f48](https://github.com/choiceoh/Deneb/commit/0663f489090711d3f062f2690d3824dc902a46ef))
* **native:** M1 배터리 모드 §3.1 잔여 해소 — sync 드레인 · 활성 스트림 FGS 유지 · FCM 전달 게이트 ([#3478](https://github.com/choiceoh/Deneb/issues/3478)) ([9055b89](https://github.com/choiceoh/Deneb/commit/9055b8922f659e197195cecdb757f61cd591f496))
* **zcode:** guard finds worktree without session ID ([#3486](https://github.com/choiceoh/Deneb/issues/3486)) ([b2aae2d](https://github.com/choiceoh/Deneb/commit/b2aae2d43cdd46290ac7b64b336a3bab073c1e4c))


### 🔧 Internal

* raise code health with typed boundaries and maintainable tests ([#3481](https://github.com/choiceoh/Deneb/issues/3481)) ([2af4ef2](https://github.com/choiceoh/Deneb/commit/2af4ef29991779f94f0b999c2bd41994c8700e90))

## [4.89.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.88.0...deneb-v4.89.0) (2026-07-12)


### ✨ Features

* **deploy:** RSI L4 완전자동화 발판 — 배포 롤백 워치 + 코딩 디스패치(오퍼레이터 활성) ([#3465](https://github.com/choiceoh/Deneb/issues/3465)) ([1b017eb](https://github.com/choiceoh/Deneb/commit/1b017ebb0c17c6ca539321237d2584e5741257a9))
* **gateway:** APK 다운로드 단명 서명 토큰 — 장수 클라 토큰의 URL 노출 제거 ([#3455](https://github.com/choiceoh/Deneb/issues/3455)) ([18a6797](https://github.com/choiceoh/Deneb/commit/18a6797eb007c3b2cd8177ff5e1d1ef0eff6fb62))
* **genesis:** ①behavioral 검증 승격 — reproduction oracle이 실패 트레이스 tool 데이터로 behavioral 어서션 자동 저작 ([#3471](https://github.com/choiceoh/Deneb/issues/3471)) ([ac6a2a3](https://github.com/choiceoh/Deneb/commit/ac6a2a3081c6f39a37c77526f8234dff34b02680))
* **genesis:** L4 조건부 개방 — 게이트웨이 소스 자가편집 표면 선언 + 수용 게이트 회로 격리 (오퍼레이터 승인 2026-07-12) ([#3464](https://github.com/choiceoh/Deneb/issues/3464)) ([bab9784](https://github.com/choiceoh/Deneb/commit/bab978479241cb7ee6508f8cf78c00f31a4a0d18))
* **genesis:** P3 라벨 식량 공장 — judge-accuracy 상설 레인(심은결함 재생+false-reject 채굴) + charter 동결(전제③) ([#3463](https://github.com/choiceoh/Deneb/issues/3463)) ([a1cdbc7](https://github.com/choiceoh/Deneb/commit/a1cdbc7532882ec44126e469697bb9626e0838ae))
* **genesis:** RSI P1.5 — reproduction oracle(SEA Alg 8) + 메타 아티팩트 사이드카 리프레시 ([#3446](https://github.com/choiceoh/Deneb/issues/3446)) ([2ddfc5e](https://github.com/choiceoh/Deneb/commit/2ddfc5e51bab38ab252cbf78cf5640799f01c423))
* **genesis:** RSI P1.5 — 케이스 단위 flip gate(상쇄 회귀 차단) ([#3445](https://github.com/choiceoh/Deneb/issues/3445)) ([0ed91be](https://github.com/choiceoh/Deneb/commit/0ed91be120566b9c08bf478fc0adadad3705e362))
* **genesis:** RSI P2 — judge-degradation 벤치(BabelJudge) — evaluator epoch 제안의 결정적 fitness ([#3449](https://github.com/choiceoh/Deneb/issues/3449)) ([9aaaf09](https://github.com/choiceoh/Deneb/commit/9aaaf0985c6d11ad956db28a0da40425031c7467))
* **genesis:** RSI P2 — producer shadow-replay 벤치 — 두 프롬프트가 생성한 후보를 held-out flip으로 비교 ([#3450](https://github.com/choiceoh/Deneb/issues/3450)) ([915e10b](https://github.com/choiceoh/Deneb/commit/915e10baf9674d6c92d2d17c97da139959a7030d))
* **genesis:** RSI P2 — 피드 카드 원탭 채택/기각 ([#3456](https://github.com/choiceoh/Deneb/issues/3456)) ([8bc8252](https://github.com/choiceoh/Deneb/commit/8bc825266075a5112a7b04e5d011f3159a067151))
* **genesis:** RSI P2 관측성 — 메타 제안을 작업 피드 카드 + /health 스코어보드로 표면화 ([#3453](https://github.com/choiceoh/Deneb/issues/3453)) ([6094df6](https://github.com/choiceoh/Deneb/commit/6094df6b64215a8c44d868b4af5549fc5f1172c6))
* **genesis:** RSI P2 완결 — 벤치 게이트 자동 채택 + 메타 롤백 워치 (오퍼레이터 승인 제거) ([#3459](https://github.com/choiceoh/Deneb/issues/3459)) ([73d6e54](https://github.com/choiceoh/Deneb/commit/73d6e541a463b57249e30b9ccd759eb8e1f08caa))
* **genesis:** RSI P2 착수 — 주간 meta-evolution 슬로우 루프(propose-only) + 메타 경험 원장 ([#3448](https://github.com/choiceoh/Deneb/issues/3448)) ([a0be72f](https://github.com/choiceoh/Deneb/commit/a0be72f8786cc72165e24349dcf7751d193cf4b3))
* **genesis:** RSI P4 착수 — 스킬+도구 번들 페어링(propose-only) — evolve skip의 tool_gap 선언을 코딩 후보와 짝지음 ([#3452](https://github.com/choiceoh/Deneb/issues/3452)) ([1e8c9d2](https://github.com/choiceoh/Deneb/commit/1e8c9d22983315c7f293f6f2525a7ed71d6585fa))
* **genesis:** RSI 캘리브레이션 가속 노브 — 메타 주기·workout 주기·벤치 스케일 env ([#3461](https://github.com/choiceoh/Deneb/issues/3461)) ([263a418](https://github.com/choiceoh/Deneb/commit/263a418d5d19f059fdc89dc294f8b89c01052a43))
* **genesis:** 과거 챗 아카이브 스윕 노브 — validation-backfill 창·세션·목표 env (+ gofumpt 승계분 11파일) ([#3462](https://github.com/choiceoh/Deneb/issues/3462)) ([2cbb362](https://github.com/choiceoh/Deneb/commit/2cbb36208990c953352b437ad098b60d1be74c68))
* **genesis:** 라벨 파이프라인 가속 — 시간 기반 워치 해소 + rsi-backtest CLI (해소 0건 병목 수리) ([#3460](https://github.com/choiceoh/Deneb/issues/3460)) ([e9b8be8](https://github.com/choiceoh/Deneb/commit/e9b8be88dc2b30946e94e02c89870b067f266767))
* **genesis:** 적대적 커버리지 프로브 — 결정적 섹션-드롭 변이로 약한 held-out 케이스 자동 강화 ([#3473](https://github.com/choiceoh/Deneb/issues/3473)) ([3dc5927](https://github.com/choiceoh/Deneb/commit/3dc59270701a75faaa10f2e5fbe836a73e3bc9f8))
* **genesis:** 진화 궤적 자가감사(자기 브레이크) — 드리프트 감지 시 자동 채택 동결 + genesis 서브시스템 가이드 ([#3470](https://github.com/choiceoh/Deneb/issues/3470)) ([e206ac0](https://github.com/choiceoh/Deneb/commit/e206ac0ae2ee752e31926d9458ad0c763f6703b7))
* **genesis:** 첫 메타 제안 채택(업스트림) — evolve 규칙14 정량화 + 계약 게이트 tool_gap 앵커 ([#3454](https://github.com/choiceoh/Deneb/issues/3454)) ([2a9a741](https://github.com/choiceoh/Deneb/commit/2a9a741830b15d25df4ce4767374c5a34b4ed2ac))
* **zcode:** Claude 인프라 완전 패리티 — codegraph MCP 배선·CLAUDE.md 규칙 연결·clash 충돌 검사·pre-commit 게이트 ([5f6cbc2](https://github.com/choiceoh/Deneb/commit/5f6cbc27ee4e4dd30c7ac1c0723adb8c6399f313))
* **zcode:** 워크트리 진입 자동화 — init 훅 additionalContext 강화·가드 안내 개선·git stdout 정리 ([824222f](https://github.com/choiceoh/Deneb/commit/824222fad78178e002bde471599b463fa8fc3043))
* **zcode:** 자동 워크트리 격리 환경 + Claude 훅 브릿지 (CodeGraph 유도·경로별 룰 게이트) ([834cc02](https://github.com/choiceoh/Deneb/commit/834cc022d29f28e733937b5242b847deb60d4f98))


### 🐛 Bug Fixes

* **andromeda:** [#3438](https://github.com/choiceoh/Deneb/issues/3438) 플레이크 2건 탈레이스 — attach 재진입·ProjectHome AI 투영 비동기 단언을 waitFor로 ([#3442](https://github.com/choiceoh/Deneb/issues/3442)) ([5f1e807](https://github.com/choiceoh/Deneb/commit/5f1e807a53b04445bc2d5c4ad0ce9038616cbbb0))
* **deploy:** coding-dispatch 큐 파일명·병합·실행비트 수정 (배차 무동작 버그) ([#3469](https://github.com/choiceoh/Deneb/issues/3469)) ([f08af84](https://github.com/choiceoh/Deneb/commit/f08af846c8dd36833df2b1fdfb458f600d1705c2))
* **health:** responsibility-cochange가 컴포지션 루트(배선 계층)를 오탐하는 것 수정 ([#3474](https://github.com/choiceoh/Deneb/issues/3474)) ([3a74382](https://github.com/choiceoh/Deneb/commit/3a743821e4af76204d7cc6bc9e0f6cd2ac2a961b))


### 🔧 Internal

* **chat:** split run_prepare.go under the 700-LOC rule ([#3458](https://github.com/choiceoh/Deneb/issues/3458)) ([8bef3b4](https://github.com/choiceoh/Deneb/commit/8bef3b43995cad54adf1a7300a6eec3e23919ddb))
* improve runtime boundaries and code health ([#3457](https://github.com/choiceoh/Deneb/issues/3457)) ([1bd4c6f](https://github.com/choiceoh/Deneb/commit/1bd4c6f09443ec45e12970e1bced780a6a9086cb))

## [4.88.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.87.0...deneb-v4.88.0) (2026-07-11)


### ✨ Features

* **genesis:** RSI P1.5 — e-process 관측 배선(듀얼트랙) — 롤백/확인 lifecycle에 baseline-aware 판정·disagreement 라벨 병기 ([#3439](https://github.com/choiceoh/Deneb/issues/3439)) ([8bdcc4d](https://github.com/choiceoh/Deneb/commit/8bdcc4dc6933c436bc0f56b240e5314779f807f1))


### 🐛 Bug Fixes

* [#3438](https://github.com/choiceoh/Deneb/issues/3438) 후속 main 수리 — gofmt/gofumpt 16파일 정리·bootstrap 경계 테스트 계약 수정·andromeda 테스트 TZ 고정 ([#3441](https://github.com/choiceoh/Deneb/issues/3441)) ([12ecbb2](https://github.com/choiceoh/Deneb/commit/12ecbb2e56475c7dab0576a109e1c0d31e67f597))

## [4.87.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.86.0...deneb-v4.87.0) (2026-07-11)


### ✨ Features

* **andromeda:** 메일·위키·브리핑 답변 인쇄 — window.print + [@media](https://github.com/media) print 서브트리 격리 ([#3400](https://github.com/choiceoh/Deneb/issues/3400)) ([723cc60](https://github.com/choiceoh/Deneb/commit/723cc6099d7bd7b31063daf0e94a994353f18f92))
* **audit:** codebase-health를 14차원 정직 엄격 벤치로 재설계 (90.9→77.1) — Go 아키텍처/구조/문서·테스트 + 중복·부채 + 코틀린/TS/기타 언어 차원, 월드클래스 기준·Martin-D 배제 ([#3390](https://github.com/choiceoh/Deneb/issues/3390)) ([5850da9](https://github.com/choiceoh/Deneb/commit/5850da9e3a9284fa2ee9efe3a340ff2b577518df))
* **audit:** doc-draft — Deneb 로컬모델로 서브시스템 문서 초안 생성 (OpenWiki 아이디어 훔쳐 고침) ([#3396](https://github.com/choiceoh/Deneb/issues/3396)) ([9082368](https://github.com/choiceoh/Deneb/commit/9082368a604712441329938b7bac11b0191bc748))
* **audit:** runtime-health 벤치 — 7일 게이트웨이 로그 기반 런타임 건강 점수 /100 ([#3394](https://github.com/choiceoh/Deneb/issues/3394)) ([13e89a8](https://github.com/choiceoh/Deneb/commit/13e89a8584471140a60e7712151bb898f75e8837))
* **chat:** auto-activate skill requires_tools deferred tools on consult ([#3406](https://github.com/choiceoh/Deneb/issues/3406)) ([1f76634](https://github.com/choiceoh/Deneb/commit/1f7663474dfd00ba4714f4129d94b1f7a16fa42c))
* **chat:** replay deferred-tool activation across runs from transcript ([#3409](https://github.com/choiceoh/Deneb/issues/3409)) ([84fa1d9](https://github.com/choiceoh/Deneb/commit/84fa1d9454e933d885373a7569ae3ea47df571d5))
* **gateway:** Pydantic AI 전수 감사 2차 도입 — 배리어 세그먼트화·SSRF 강화·Retry-After·구조화 metadata ([#3414](https://github.com/choiceoh/Deneb/issues/3414)) ([ea5fd60](https://github.com/choiceoh/Deneb/commit/ea5fd600b9459ff24ee6830ccf3932279e63dbb5))
* **genesis:** RSI P1 — 개선 프롬프트를 meta 아티팩트로 외부화 (동작 중립) ([#3430](https://github.com/choiceoh/Deneb/issues/3430)) ([b52a630](https://github.com/choiceoh/Deneb/commit/b52a630b111fb37fba2c1b158de56209766dcfe8))
* **genesis:** RSI P1.5 ② — anytime-valid e-process 프리미티브 + 롤백 워치 영속화(SIGUSR1 증발 버그 수정)·pre-evolve 베이스라인 스냅숏 ([#3434](https://github.com/choiceoh/Deneb/issues/3434)) ([fae1f28](https://github.com/choiceoh/Deneb/commit/fae1f28edc7301a67321a822316134b23f5b9d95))
* **genesis:** RSI P1.5 ③ — 롤백 증거 영속화(재제안 방어) + 실패 트레이스→hard 케이스 증류 (P3의 결정적 절반) ([#3435](https://github.com/choiceoh/Deneb/issues/3435)) ([092031d](https://github.com/choiceoh/Deneb/commit/092031db27b84d3814f80d5dade98c390080ddfb))
* **genesis:** RSI P1.5 ⑤(완) — 교차스킬 confirmed exemplar 회수→evolve 프롬프트 few-shot + falseAcceptRate 스코어보드 ([#3437](https://github.com/choiceoh/Deneb/issues/3437)) ([742b132](https://github.com/choiceoh/Deneb/commit/742b1322f625c58ec715a7936b0e032b24dbabde))
* **genesis:** RSI P1.5 착수 — evolve 라이프사이클 certificate 원장 (아티팩트 버전·judge 모델·점수쌍·margin 귀속) ([#3433](https://github.com/choiceoh/Deneb/issues/3433)) ([451c214](https://github.com/choiceoh/Deneb/commit/451c2145f7534a19a0a506a5d900272499897cf7))
* **genesis:** 검증 케이스 가시/블라인드 풀 분할 + SkillHone(2606.08671) 검토 ([#3432](https://github.com/choiceoh/Deneb/issues/3432)) ([5d860ac](https://github.com/choiceoh/Deneb/commit/5d860ac60ffbd6ff0e6a4dae3c8a09df9b746861))
* **genesis:** 아이들 리뷰 백스톱 — heartbeat가 리뷰 공백을 최대 6h에서 차단 ([#3427](https://github.com/choiceoh/Deneb/issues/3427)) ([187c3b1](https://github.com/choiceoh/Deneb/commit/187c3b1b2e128d44f43506a48558f72aac4f01da))
* **media:** watch 프레임 선별을 장면전환 감지 기반으로 (균등 그리드는 폴백) ([#3422](https://github.com/choiceoh/Deneb/issues/3422)) ([7a3b27e](https://github.com/choiceoh/Deneb/commit/7a3b27e2fca06a8b1ce26754dfec094e4cd53ff1))


### 🐛 Bug Fixes

* **chat:** 모닝레터 시장 데이터에서 유로 제거 — 달러·구리만 ([#3391](https://github.com/choiceoh/Deneb/issues/3391)) ([50818ee](https://github.com/choiceoh/Deneb/commit/50818ee0b0eb9a7ca04b066831c5bc2ef845c7ea))
* **genesis:** RSI P1.5 ④ — min-delta 영구기각 웨지 수정(비판별 단언 격리) + 게이트 퍼즈 하니스 ([#3436](https://github.com/choiceoh/Deneb/issues/3436)) ([ab90b29](https://github.com/choiceoh/Deneb/commit/ab90b292cb83d391eb6d8ef55fb312b3fa3afd63))
* **genesis:** 아이들 리뷰 Codex P2 3건 — 실패 리뷰 2h 재시도·프로드 상태디렉토리 게이트·얇은 후보 프리필터 ([#3427](https://github.com/choiceoh/Deneb/issues/3427) 후속) ([#3428](https://github.com/choiceoh/Deneb/issues/3428)) ([bcc13f2](https://github.com/choiceoh/Deneb/commit/bcc13f2d4af4dc9e5a776cbc216feeb3b7d4eef1))
* **runtime:** harden recovery and health reporting ([#3397](https://github.com/choiceoh/Deneb/issues/3397)) ([9d74081](https://github.com/choiceoh/Deneb/commit/9d74081fa924701d746e260f4c1f62ac4231cb46))
* **runtime:** 헬스 계약 테스트 HOME 격리 — 호스트 실제 cron 저장소를 읽어 cron:3으로 오탐하던 환경 의존 제거 ([#3418](https://github.com/choiceoh/Deneb/issues/3418)) ([3d013fb](https://github.com/choiceoh/Deneb/commit/3d013fb277d487d158d5ee13811b73c450d1ecd8))


### ⚡ Performance

* **mail:** mail_archive 첨부 OCR 병렬화 — 다첨부 견적서 직렬→동시 추출 ([#3395](https://github.com/choiceoh/Deneb/issues/3395)) ([898d001](https://github.com/choiceoh/Deneb/commit/898d001327bf51d20c91fc46fbfa8ded296dc3f1))


### 🔧 Internal

* **agent:** before-tool-call 게이트를 HookCompositor 네이티브 합성으로 ([#3425](https://github.com/choiceoh/Deneb/issues/3425)) ([3d167ba](https://github.com/choiceoh/Deneb/commit/3d167ba64601bab4493f312aeeb55caf7eff3b8c))
* **agent:** centralize run state finalization ([#3419](https://github.com/choiceoh/Deneb/issues/3419)) ([f93b535](https://github.com/choiceoh/Deneb/commit/f93b535cec94820006a8be90eb9ecaf8ddb4d00b))
* **agent:** unify tool-turn lifecycle ([#3411](https://github.com/choiceoh/Deneb/issues/3411)) ([a14ec72](https://github.com/choiceoh/Deneb/commit/a14ec72067148dee0fc669f5bf8eb873e2506e09))
* **andromeda:** FleetPane 1178 LOC 분할 — 탭 뷰·카드는 FleetViews, 타입·헬퍼는 fleetHelpers (순수 이동) ([#3405](https://github.com/choiceoh/Deneb/issues/3405)) ([a033fe1](https://github.com/choiceoh/Deneb/commit/a033fe1d2d10b4a014880916b546ed301fb4929a))
* **andromeda:** WikiPane 모달 분리 — 이동·새 페이지·미저장 모달을 WikiModals로 (순수 이동) ([#3407](https://github.com/choiceoh/Deneb/issues/3407)) ([4bc37f2](https://github.com/choiceoh/Deneb/commit/4bc37f2cc957a56fcb1e7685ca848576036ae063))
* **andromeda:** 챗 surface 중복 제거 — ChatView·AIPanel 공유 로직 추출 (컴포저·첨부 파이프라인·모델 로딩·답변 액션) ([#3403](https://github.com/choiceoh/Deneb/issues/3403)) ([c4497c5](https://github.com/choiceoh/Deneb/commit/c4497c5b6a589f1d7c1f22a77d75135775f01888))
* **andromeda:** 컴포넌트 파일의 비컴포넌트 export 분리 — react-refresh 경고 7개 제거 ([#3393](https://github.com/choiceoh/Deneb/issues/3393)) ([025c6b3](https://github.com/choiceoh/Deneb/commit/025c6b30daf48d1240d860a5f0a5f877f1cf285c))
* **genesis:** evolver.go 2차 분할 — 1820→669, 관심사별 4파일+게이트 합류 (순수 이동) ([#3415](https://github.com/choiceoh/Deneb/issues/3415)) ([fdd619a](https://github.com/choiceoh/Deneb/commit/fdd619aa26aae4604f863a7f219b958cfeaf645c))
* **genesis:** tracker.go 1286→365 분할 — usage·lifecycle·activity 순수 이동 ([#3417](https://github.com/choiceoh/Deneb/issues/3417)) ([89f67be](https://github.com/choiceoh/Deneb/commit/89f67be6c30126facf61f108a5047ca77187f613))
* **llm:** isolate OpenAI stream content state ([#3404](https://github.com/choiceoh/Deneb/issues/3404)) ([5a80f88](https://github.com/choiceoh/Deneb/commit/5a80f88681f6fcb2d7efa9cbc78e7d37c150f36a))
* **mail:** mailbody cleaner.go 1412 LOC 4분할 — strip·cut·signals 순수 이동 ([#3408](https://github.com/choiceoh/Deneb/issues/3408)) ([ac8342a](https://github.com/choiceoh/Deneb/commit/ac8342af6c07c8d614d244b4f15c068530e18bf0))
* **native:** DenebFleetScreen 1080 LOC 4분할 — Pages·Models·Diagnostics 순수 이동 ([#3413](https://github.com/choiceoh/Deneb/issues/3413)) ([285fe1e](https://github.com/choiceoh/Deneb/commit/285fe1e9af09261da39f30a77342408aa9986477))
* **native:** DenebOrgChartScreen 1140 LOC 4분할 — Model·Content·Editors 순수 이동 ([#3410](https://github.com/choiceoh/Deneb/issues/3410)) ([793e9da](https://github.com/choiceoh/Deneb/commit/793e9da2f8e3cbdd94955839a21b17f41edd437e))
* **native:** GatewayClient 감량 1단계 — 워크피드·네이티브동기화를 DenebClientWorkfeed 확장으로 (1895→1384) ([#3420](https://github.com/choiceoh/Deneb/issues/3420)) ([3938d07](https://github.com/choiceoh/Deneb/commit/3938d07d9c2607b7b924662171c03cf8dd5d6b3b))
* **native:** GatewayClient 감량 2단계 — 세션/트랜스크립트 확장 분리 (1384→1214) ([#3421](https://github.com/choiceoh/Deneb/issues/3421)) ([4d10f17](https://github.com/choiceoh/Deneb/commit/4d10f177c1ed2f1d789c1c314792f04f1a096b90))
* **native:** GatewayClient 감량 3단계(완) — 상태·업데이트·푸시토큰을 DenebClientAdmin 확장으로 (1221→1147) ([#3423](https://github.com/choiceoh/Deneb/issues/3423)) ([6865279](https://github.com/choiceoh/Deneb/commit/6865279dc9b55dd0a41d6647d7fea6f4a9757681))
* **native:** split render preview harness by concern ([#3426](https://github.com/choiceoh/Deneb/issues/3426)) ([649e5b7](https://github.com/choiceoh/Deneb/commit/649e5b7ac7ee4a8a4789e217628a7cdeac6a4d86))
* raise code health to 90.1 ([#3438](https://github.com/choiceoh/Deneb/issues/3438)) ([0dbc45d](https://github.com/choiceoh/Deneb/commit/0dbc45d681a062ae09907f574f93ba8c132375cd))
* **runtime:** skill_lifecycle_tool.go 1482→408 분할 — status·validation·replay 순수 이동 ([#3424](https://github.com/choiceoh/Deneb/issues/3424)) ([bc4017f](https://github.com/choiceoh/Deneb/commit/bc4017f5fd42cd42d8a066a63b7c0a3b78c7f778))
* **runtime:** split agent stream lifecycle ([#3399](https://github.com/choiceoh/Deneb/issues/3399)) ([06a95b4](https://github.com/choiceoh/Deneb/commit/06a95b48420714d696efb3673cf5a71d28a1d25f))
* **runtime:** split health and stream orchestration ([#3398](https://github.com/choiceoh/Deneb/issues/3398)) ([96f5771](https://github.com/choiceoh/Deneb/commit/96f57717b34f27dc4db9e19ac405569a04edbda7))
* **runtime:** supervise process manager lifecycle ([#3401](https://github.com/choiceoh/Deneb/issues/3401)) ([5b25169](https://github.com/choiceoh/Deneb/commit/5b2516917575b4d1263b9217f4385cb23798c361))

## [4.86.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.85.0...deneb-v4.86.0) (2026-07-10)


### ✨ Features

* **audit:** 코드베이스 구조 건강도 평가 하네스 추가 ([#3377](https://github.com/choiceoh/Deneb/issues/3377)) ([94b0282](https://github.com/choiceoh/Deneb/commit/94b02824750ec318a745e7365fd0c453034105a4))


### 🐛 Bug Fixes

* **ci:** topology-parity의 systemctl --user 거짓 FAIL 수정 (XDG_RUNTIME_DIR) ([#3376](https://github.com/choiceoh/Deneb/issues/3376)) ([43eface](https://github.com/choiceoh/Deneb/commit/43efacef3ae31c09c1c75f0e30106fc70a906680))
* **genesis:** 자가코딩 리뷰 넛지 필수-판정 계약 + 미소비 재시도 단축 ([#3380](https://github.com/choiceoh/Deneb/issues/3380)) ([77d666b](https://github.com/choiceoh/Deneb/commit/77d666ba187aa327cf3921908104e19f90244f95))


### 🔧 Internal

* **android:** ChatModeScreen + DenebGatewayClient 관심사별 분할 (순수 이동) ([#3378](https://github.com/choiceoh/Deneb/issues/3378)) ([39c0d52](https://github.com/choiceoh/Deneb/commit/39c0d52125b5a6eb22c357eb17bbd49b3c7912fa))
* **chat:** drop superseded dead code ([#3374](https://github.com/choiceoh/Deneb/issues/3374)) ([cc5b345](https://github.com/choiceoh/Deneb/commit/cc5b345bf31439cf9a5a09a84c1e3ad9433de9c1))
* deadcode 미사용 래퍼 제거 + staticcheck SA4004 단순화 ([#3370](https://github.com/choiceoh/Deneb/issues/3370)) ([8c15843](https://github.com/choiceoh/Deneb/commit/8c15843b3998c9d25d1e5f01325e3e6148c08000))
* **genesis:** evolver.go를 관심사별 4파일로 분할 (순수 이동, -727줄) ([#3372](https://github.com/choiceoh/Deneb/issues/3372)) ([f266670](https://github.com/choiceoh/Deneb/commit/f2666707b2e237827f9699e25beaab23af3c887e))
* 중복 문자열 헬퍼 통합 + 스트림 누적 최적화 + parseMailDate 통합(KST 버그정정) + dreamer 중복 추출 ([#3379](https://github.com/choiceoh/Deneb/issues/3379)) ([0373541](https://github.com/choiceoh/Deneb/commit/0373541740661b03f976690aeb05e172f1185d4c))

## [4.85.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.84.2...deneb-v4.85.0) (2026-07-09)


### ✨ Features

* **chat:** deneb-ui 카드를 구조적 답변의 기본으로 전환 — 트리거 강화 + 마크다운표 경쟁 해소 ([#3365](https://github.com/choiceoh/Deneb/issues/3365)) ([a209475](https://github.com/choiceoh/Deneb/commit/a209475e0f621b7293859a942d9434fb9abe645f))
* **dev:** CodeGraph MCP 코드 내비 도입 — greppy 대체, Graphify(위키) 유지 ([#3366](https://github.com/choiceoh/Deneb/issues/3366)) ([130a3ca](https://github.com/choiceoh/Deneb/commit/130a3ca641395cf4730e0a7593106e30b6f90740))
* **dev:** greppy 코드 내비 도입 — CLAUDE.md 배선 + grep→greppy 유도 훅 ([#3359](https://github.com/choiceoh/Deneb/issues/3359)) ([9c89933](https://github.com/choiceoh/Deneb/commit/9c899336160fb64e178c841da45e775945ccc5ab))


### 🐛 Bug Fixes

* **genesis:** 자가교정 캡처 퍼널 3레버 — 결정적 실패-클러스터 승격(LLM 비의존)·applied 후 재발 시 쿨다운 재오픈·validation_case→self_correction 어포던스 ([#3367](https://github.com/choiceoh/Deneb/issues/3367)) ([e9dd037](https://github.com/choiceoh/Deneb/commit/e9dd0378c36d8d945f99fb30dff9f6bcc2518d79))

## [4.84.2](https://github.com/choiceoh/Deneb/compare/deneb-v4.84.1...deneb-v4.84.2) (2026-07-09)


### 🐛 Bug Fixes

* **skills:** 프로푸스 라이프사이클 로그 판정 라벨 정밀화 — create↔genesis 자동/수동 구분 ([#3360](https://github.com/choiceoh/Deneb/issues/3360)) ([e08b5ec](https://github.com/choiceoh/Deneb/commit/e08b5ec942d65e4624f567783db87eb74d67ee0d))
* **workfeed:** 카드 자동제목을 tiny 역할로 이관 — lightweight가 클라우드 추론모델(deepseek-v4-flash-api)로 폴백 시 thinking-off 미적용→빈응답→휴리스틱 폴백 회귀 방지 ([#3362](https://github.com/choiceoh/Deneb/issues/3362)) ([c0b5e96](https://github.com/choiceoh/Deneb/commit/c0b5e9698a44dfdd7bc239e3f9af6f9301ff4d00))

## [4.84.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.84.0...deneb-v4.84.1) (2026-07-09)


### 🐛 Bug Fixes

* **wiki:** 백업 폴더 프룬에 .bak-* 인픽스 네이밍 추가 — 마이그레이션 스냅샷 누출 차단 ([#3357](https://github.com/choiceoh/Deneb/issues/3357)) ([7e71a37](https://github.com/choiceoh/Deneb/commit/7e71a3732f653198c59902858a828492bfbe19b9))


### ⚡ Performance

* **chat:** subagents·todo 강등 + 도구 스키마 무손실 압축 ([#3353](https://github.com/choiceoh/Deneb/issues/3353) 후속) ([#3356](https://github.com/choiceoh/Deneb/issues/3356)) ([04b5c1f](https://github.com/choiceoh/Deneb/commit/04b5c1f22b9d82a3669641726abf62e949b7dca2))


### 🔧 Internal

* 코드모드(Code Mode) 제거 — git-worktree 코딩 세션·miniapp.code.* RPC·네이티브/andromeda UI·구현자 프롬프트 프로파일 ([#3354](https://github.com/choiceoh/Deneb/issues/3354)) ([ca47311](https://github.com/choiceoh/Deneb/commit/ca47311ed8cb51c38f30bd016f0236f136cf5342))

## [4.84.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.83.1...deneb-v4.84.0) (2026-07-09)


### ✨ Features

* **chat:** mail_archive를 eager로 승격하고 code_action은 다중도구 전용으로 안내 ([#3352](https://github.com/choiceoh/Deneb/issues/3352)) ([5cc0516](https://github.com/choiceoh/Deneb/commit/5cc0516f74ff7df2da493f79eca8bd0ebe015c53))
* **workfeed:** 문서 분석 산출물을 작업 피드 doc_analysis 카드로 발행 ([#3350](https://github.com/choiceoh/Deneb/issues/3350)) ([d1fec6e](https://github.com/choiceoh/Deneb/commit/d1fec6e18ddc6d3efdeaf4815632dc5c8c386252))


### 🐛 Bug Fixes

* **chat:** code_action에서 메일 첨부(PDF/DOCX) 읽기 허용 — mail_archive attachment 액션 allowlist 추가 ([#3351](https://github.com/choiceoh/Deneb/issues/3351)) ([91b7119](https://github.com/choiceoh/Deneb/commit/91b711962cf89e7d2eb5af2f5b867bb59b2a6f03))
* **compaction:** 요약기 환각 차단 — 없는 숫자·금액 지어내기 금지 + untrusted fence에 미검증 수치 단언 금지 ([#3347](https://github.com/choiceoh/Deneb/issues/3347)) ([1336618](https://github.com/choiceoh/Deneb/commit/1336618cf7ab29ed8033cc86b80bde7ba6e01af0))


### 🔧 Internal

* **chat:** notebook·deal_ledger를 deferred로 강등 + code_action 첨부 문구 정정 ([#3353](https://github.com/choiceoh/Deneb/issues/3353)) ([f5dc6d6](https://github.com/choiceoh/Deneb/commit/f5dc6d6a0ab09a695b0a69e325d63e1b8cb75225))

## [4.83.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.83.0...deneb-v4.83.1) (2026-07-09)


### 🐛 Bug Fixes

* **andromeda:** PDF 미리보기가 원본 소스로 뜨는 문제 — blob MIME 재스탬프 ([#3346](https://github.com/choiceoh/Deneb/issues/3346)) ([5910f1e](https://github.com/choiceoh/Deneb/commit/5910f1e2912d06bcab972d5d383b120c2d8e1fa0))
* **genesis:** Propus 리뷰 제안을 실행 前에 기록 — 배포 핫스왑에 취소돼도 라이프사이클 로그 유지 ([#3344](https://github.com/choiceoh/Deneb/issues/3344)) ([7fd0fd5](https://github.com/choiceoh/Deneb/commit/7fd0fd53bb365eb24286caacaa3df1de0a37e6d2))
* **skills:** 봇 리뷰가 잡은 자가개선 파이프라인 결함 7건 수리 (A1-A5,A7,A9 + 버스트 무력화) ([#3341](https://github.com/choiceoh/Deneb/issues/3341)) ([4c8fc3c](https://github.com/choiceoh/Deneb/commit/4c8fc3c4f87de952f80023e32c92cc5202fe0026))
* **wiki:** 인물·프로젝트 목록에서 백업 폴더 제외 — ListPages·검색 walk가 백업/히든 디렉토리 프룬 ([#3345](https://github.com/choiceoh/Deneb/issues/3345)) ([f787b7f](https://github.com/choiceoh/Deneb/commit/f787b7fe96c5c9c21b3edf1af876be8d1d4f5098))


### 🔧 Internal

* **gateway:** simplify lifecycle status and dispatch timeout handling ([#3342](https://github.com/choiceoh/Deneb/issues/3342)) ([9e31a4f](https://github.com/choiceoh/Deneb/commit/9e31a4f92ac53e7f620e3bc70836f33383e6bb28))

## [4.83.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.82.0...deneb-v4.83.0) (2026-07-09)


### ✨ Features

* **autonomous:** 하트비트 섀도 리플레이 드라이런 게이트 — 지시문 표면 evolve P1 ([#3322](https://github.com/choiceoh/Deneb/issues/3322)) ([bcab4f6](https://github.com/choiceoh/Deneb/commit/bcab4f6a9cb520c80ea1c8506e551aabbfecabd8))
* **mail:** mail_archive 동작별 위상 계측 — 느린 경로(IMAP 폴백·OCR) 소요시간 로깅 ([#3332](https://github.com/choiceoh/Deneb/issues/3332)) ([9484756](https://github.com/choiceoh/Deneb/commit/948475699b44a38205b450ef30bcb178f6cb9e3e))
* **native:** 크래시 리포터 — uncaught 예외 스택을 게이트웨이로 전송해 관측 가능하게 ([#3325](https://github.com/choiceoh/Deneb/issues/3325)) ([7241c99](https://github.com/choiceoh/Deneb/commit/7241c996e665e357252a36d6f7fdc731fce55e9a))
* **skills:** evolve 버스트 — 수용이 이어지는 동안 같은 스킬을 즉시 재진화 (loop-until-dry) ([#3326](https://github.com/choiceoh/Deneb/issues/3326)) ([29281e9](https://github.com/choiceoh/Deneb/commit/29281e968504d7d4c3518b25c4976d4d195a8a84))
* **skills:** 합성 워크아웃 레인 — 스킬을 자기 held-out 케이스로 상시 연습 (+백필 6h) ([#3324](https://github.com/choiceoh/Deneb/issues/3324)) ([bfd248d](https://github.com/choiceoh/Deneb/commit/bfd248de4c21db8acc2b2924b41381f1dd728316))


### 🐛 Bug Fixes

* **autonomous:** 레인 연속성 수리 — 워크아웃 증거가 스윕을 깨우고, 워크아웃은 evolve 가능한 스킬만 연습 ([#3331](https://github.com/choiceoh/Deneb/issues/3331)) ([08ab3fd](https://github.com/choiceoh/Deneb/commit/08ab3fde2f0a383ae0f76e8208ca06f328519231))
* **autonomous:** 워크아웃 레인을 프로덕션 state dir로 게이트 — dev live-test의 prod 사용로그 오염 차단 ([#3333](https://github.com/choiceoh/Deneb/issues/3333)) ([495afde](https://github.com/choiceoh/Deneb/commit/495afde45552c5cb5fdfae09100c7abf94439c21))
* **chat:** 압축이 오래된 스필 결과의 read_spillover 포인터를 보존 — 통째 stub/펜스제거로 유실되던 갭 (2차 OutputTrimmer 연장선) ([#3328](https://github.com/choiceoh/Deneb/issues/3328)) ([73cde14](https://github.com/choiceoh/Deneb/commit/73cde14856464bc9337dba79512c2408fd7fa541))
* **genesis:** Propus 리뷰 포크 출력 예산 2048→8192 — glm-5.2 추론이 예산 소진해 제안 로그가 전무하던 문제 ([#3338](https://github.com/choiceoh/Deneb/issues/3338)) ([3eca29d](https://github.com/choiceoh/Deneb/commit/3eca29df172f126ba5ef2f678c64f62a38f987d4))
* **genesis:** replay 실행기 출력 예산 1024→4096 — glm-5.2가 Thinking=disabled 무시하고 추론해 예산 소진 ([#3339](https://github.com/choiceoh/Deneb/issues/3339)) ([b0dedd5](https://github.com/choiceoh/Deneb/commit/b0dedd5a150dc2738ddb977a1393c691d60731fa))
* **native:** FCM 알림 표시 경로 전체 크래시 가드 — 푸시 수신 시 잔여 백그라운드 종료 차단 ([#3336](https://github.com/choiceoh/Deneb/issues/3336)) ([340d3e3](https://github.com/choiceoh/Deneb/commit/340d3e3ae64bbab40f0669b9f7d13e3e06577d3f))
* **native:** 백그라운드 전환 코루틴 취소 크래시 수정 — TaskScheduler.stop() cancel 가드 ([#3337](https://github.com/choiceoh/Deneb/issues/3337)) ([625121e](https://github.com/choiceoh/Deneb/commit/625121e9275da461723e2ab6c7dd1be08ad8ca0c))
* **native:** 알림 extras 필드별 파싱 — 문제 알림도 읽을 수 있는 필드는 살려 캡처 ([#3330](https://github.com/choiceoh/Deneb/issues/3330)) ([076a448](https://github.com/choiceoh/Deneb/commit/076a4485d86b2768467335f3d7f8d19bda006ee2))
* **skills:** 워크아웃 레인 완성도 — 공정 로테이션·증거 중복 억제·안정 시그니처·모델 기록 ([#3329](https://github.com/choiceoh/Deneb/issues/3329)) ([c82e894](https://github.com/choiceoh/Deneb/commit/c82e8943461001724b33ce0131377f3b9c502b4f))


### ⚡ Performance

* **mail:** 로컬 메일 저장소 기동 시 1회 자동 백필 — 과거 메일도 빠른 경로로 ([#3334](https://github.com/choiceoh/Deneb/issues/3334)) ([56858d8](https://github.com/choiceoh/Deneb/commit/56858d82c933913aa7ae95e1c2f8638ad2835317))

## [4.82.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.81.0...deneb-v4.82.0) (2026-07-09)


### ✨ Features

* **autonomous:** 검증 벤치 자동 백필 레인 — 실사용 트랜스크립트를 held-out 케이스로 결정론 수확 ([#3312](https://github.com/choiceoh/Deneb/issues/3312)) ([ff48ad4](https://github.com/choiceoh/Deneb/commit/ff48ad4cdaa13133f592d6e6a6177780b4ce53ba))
* **autonomous:** 자가개선 신호 케이던스·해상도 상향 — 스윕 12h + 실패 클러스터 모델 축 ([#3317](https://github.com/choiceoh/Deneb/issues/3317)) ([708363a](https://github.com/choiceoh/Deneb/commit/708363aca0b43b621dd83b97b17a963d37e1fe18))
* **autonomous:** 하트비트 픽스처 수확 — 지시문 표면 evolve P0 코퍼스 ([#3320](https://github.com/choiceoh/Deneb/issues/3320)) ([9eeb901](https://github.com/choiceoh/Deneb/commit/9eeb90172c8deffc6755e791234ab860c7b5421c))
* **skills:** 스킬 사용 기록에 해석된 모델 차원 추가 — 모델별 약점 신호 기반 ([#3314](https://github.com/choiceoh/Deneb/issues/3314)) ([dd54218](https://github.com/choiceoh/Deneb/commit/dd542180db15d954122cc889663f4f4a83043710))
* **skills:** 자가개선 편집 표면 선언 화이트리스트 — 금지 표면은 기록 시점 거부 ([#3315](https://github.com/choiceoh/Deneb/issues/3315)) ([2d36a4c](https://github.com/choiceoh/Deneb/commit/2d36a4ce6b8590405ffd59f45fa30ccb969a923a))
* **skills:** 커버리지 조건부 게이트 완화 — held-out 케이스 있는 스킬은 측정 기반 승격 ([#3313](https://github.com/choiceoh/Deneb/issues/3313)) ([3caae98](https://github.com/choiceoh/Deneb/commit/3caae98e2e0991b3994a21236cbed07957dbdca9))


### 🐛 Bug Fixes

* **chat:** native 동기 채팅 실행-경로 결함 — auto-steer 사문화·서브에이전트 완료 false 경보 ([#3319](https://github.com/choiceoh/Deneb/issues/3319)) ([c56d8d3](https://github.com/choiceoh/Deneb/commit/c56d8d3d0f8669d47324b8c1e59928f057da6401))
* **native:** 알림 리스너·푸시 경로 크래시 가드 — 타 앱 알림 언파싱 예외로 인한 무작위 백그라운드 종료 수리 ([#3321](https://github.com/choiceoh/Deneb/issues/3321)) ([907920c](https://github.com/choiceoh/Deneb/commit/907920ccdd3e1a4c92447908ae18afef99a8f9b7))

## [4.81.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.80.0...deneb-v4.81.0) (2026-07-09)


### ✨ Features

* **autonomous:** 자가개선 스윕에 실패 시그니처 클러스터링 — 플릿 증거 번들 도입 ([#3310](https://github.com/choiceoh/Deneb/issues/3310)) ([52cbe26](https://github.com/choiceoh/Deneb/commit/52cbe26da9aa9f4ca3ec54fa77f58392ec3318fd))
* **dev:** 퍼펫 빙의 개선 — coding role 유출 수정·role별 시트·show 확장 + 회상 스몰토크 게이트 ([#3306](https://github.com/choiceoh/Deneb/issues/3306)) ([3ffdbc3](https://github.com/choiceoh/Deneb/commit/3ffdbc3618e85e52e8150eaffcfaf27d29b21b12))
* **wiki:** 사용자 모델 기록 강화 — 같은턴 write-back·선호 가속 드림·합성 규칙 정교화 ([#3309](https://github.com/choiceoh/Deneb/issues/3309)) ([37fe2bb](https://github.com/choiceoh/Deneb/commit/37fe2bb06a1ec7b232e97883164b75506129b05e))


### 🐛 Bug Fixes

* **andromeda:** 메일 열람 시 날짜 페이저 튕김·미열람 수정 — Date 헤더와 수신일 불일치 가드 + 고정 행 ([#3308](https://github.com/choiceoh/Deneb/issues/3308)) ([3a9a34d](https://github.com/choiceoh/Deneb/commit/3a9a34d81b5b80c23222fc6236bb6172db5e1c4d))
* **chat:** 퍼펫 2차 실측 결함 수정 — 대형 출력 스필 포인터 파괴·빈 완료 무표시·홀드 워치독 ([#3311](https://github.com/choiceoh/Deneb/issues/3311)) ([27c60ec](https://github.com/choiceoh/Deneb/commit/27c60ec1c41931c0ecefde8da1c85951f149726c))
* **native:** 설정 모델 픽커에서 은퇴한 챗봇·분석 역할 선택창 제거 ([#3304](https://github.com/choiceoh/Deneb/issues/3304)) ([cc97165](https://github.com/choiceoh/Deneb/commit/cc97165115176bf87e3ce37b58ee4060e15490f2))

## [4.80.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.79.0...deneb-v4.80.0) (2026-07-08)


### ✨ Features

* **autonomous:** 자가개선 능동 레인 — 수정 재발 자동 승격 + 큐 기아 시 스윕 발굴 ([#3302](https://github.com/choiceoh/Deneb/issues/3302)) ([8603eb8](https://github.com/choiceoh/Deneb/commit/8603eb81de813d89260d8324aea17fceed9f20e7))
* **skills:** 반복 patch-first 거절을 구조 검토 후보로 승격 — 7일 창 2회 + 스킬당 dedup ([#3300](https://github.com/choiceoh/Deneb/issues/3300)) ([4959fb1](https://github.com/choiceoh/Deneb/commit/4959fb146d82e48a97d10c786f89d8fdde079a1b))
* **skills:** 자가개선 코딩 퍼널 가시성 — 큐 침묵을 소진/미발생으로 화면에서 구분 ([#3301](https://github.com/choiceoh/Deneb/issues/3301)) ([30b5a5c](https://github.com/choiceoh/Deneb/commit/30b5a5c0406a6f73380f302d63cfb1b0b67c397a))
* **wiki:** 인물 코드 전용 pid 필드 분리 — code:는 프로젝트 전용 유지 ([#3299](https://github.com/choiceoh/Deneb/issues/3299)) ([6b58fa0](https://github.com/choiceoh/Deneb/commit/6b58fa0602b9debbb22d36008c43a7bbb93a65dd))


### 🐛 Bug Fixes

* **wiki:** 인물 표준양식 담당·관계 플레이스홀더를 중립 텍스트로 — 검색 회귀 수정 ([#3297](https://github.com/choiceoh/Deneb/issues/3297)) ([7855084](https://github.com/choiceoh/Deneb/commit/785508422b696207a6955495865010dab30960b7))

## [4.79.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.78.0...deneb-v4.79.0) (2026-07-08)


### ✨ Features

* **chat:** 회상 시맨틱 확장 — 일기 + 세션 요약 (재랜딩) ([#3279](https://github.com/choiceoh/Deneb/issues/3279)) ([cbe6720](https://github.com/choiceoh/Deneb/commit/cbe672002b1544224b5b5acdd0d3c806c5f3b7d1))
* **org:** 조직도-위키 통합 — 통합검색 회상소스·인물 해소·네이티브 위키 링크 ([#3287](https://github.com/choiceoh/Deneb/issues/3287)) ([f6d0db5](https://github.com/choiceoh/Deneb/commit/f6d0db5b44aa4449536ac166e2a2cac5be914886))
* **skills:** self-evolve S2 보상 수확기 — ATDP r 레이어(user/self correction·isErr), dogfood 검증 ([#3288](https://github.com/choiceoh/Deneb/issues/3288)) ([565b18f](https://github.com/choiceoh/Deneb/commit/565b18f75cd01064ce7967467cbf3d8bf286e44d))
* **skills:** self-evolve 크로스표면 자기진화 컨트롤플레인 — ATDP credit assignment + validation gate ([#3286](https://github.com/choiceoh/Deneb/issues/3286)) ([3cfb319](https://github.com/choiceoh/Deneb/commit/3cfb31969277935592485548f9d5d87407060cbd))
* **wiki:** 딜 관여 외부 거래처 담당자에 인물 페이지 자동 생성 ([#3293](https://github.com/choiceoh/Deneb/issues/3293)) ([213c507](https://github.com/choiceoh/Deneb/commit/213c5079bae2cd57009cfb1ce2397b222734c5bf))
* **wiki:** 이메일=사람 정규 신원 키 — 주소록·조직도·인물·메일 통합 ([#3289](https://github.com/choiceoh/Deneb/issues/3289)) ([29b3105](https://github.com/choiceoh/Deneb/commit/29b310586bc7869ecd66ed71818f42afe4868eba))
* **wiki:** 이직(같은 사람 회사 이동)을 동명이인과 구분 — 전화 공유 신호 ([#3296](https://github.com/choiceoh/Deneb/issues/3296)) ([7c0c33f](https://github.com/choiceoh/Deneb/commit/7c0c33fd35b4b8d5cc99833ba5514234d169784b))
* **wiki:** 인물 페이지 표준 양식 — 자동생성 stub 통일 ([#3295](https://github.com/choiceoh/Deneb/issues/3295)) ([fa95f26](https://github.com/choiceoh/Deneb/commit/fa95f2654f0ed5cdb35457e37afcf55e560114e6))
* **wiki:** 자사 직원([@topsolar](https://github.com/topsolar).kr)에 인물 페이지 자동 생성 ([#3291](https://github.com/choiceoh/Deneb/issues/3291)) ([5096b98](https://github.com/choiceoh/Deneb/commit/5096b980de2467e7d0be72ee1026846d7dd58127))


### 🐛 Bug Fixes

* **chat:** 모닝레터 deneb-ui 서문 누출 backstop + 마감 중요도 필터 ([#3290](https://github.com/choiceoh/Deneb/issues/3290)) ([e1bac5c](https://github.com/choiceoh/Deneb/commit/e1bac5cd0e45baaa90e21c6a2b7ef76748823773))


### ⚡ Performance

* **chat:** 스캔 PDF OCR 페이지 병렬화 — 서버 배칭 활용해 다페이지 ~7배 ([#3282](https://github.com/choiceoh/Deneb/issues/3282)) ([f1d3392](https://github.com/choiceoh/Deneb/commit/f1d3392c488980a96903e6703fa60cae30c791d1))
* **wiki:** RRF 계수 튜닝 — rrfK 60→20 (R@8 97→98%, 100건 골드 스윕) ([#3285](https://github.com/choiceoh/Deneb/issues/3285)) ([2ae899c](https://github.com/choiceoh/Deneb/commit/2ae899c2daa1822e4a41890c953724bbab5a496d))
* **wiki:** 명시 엔티티 앵커를 Search 그래프-부스트에 rescue로 추가 — R@8 93→95% ([#3284](https://github.com/choiceoh/Deneb/issues/3284)) ([4a226f5](https://github.com/choiceoh/Deneb/commit/4a226f5cf7dee6d576a6a56bc9abbecaf65a297a))
* **wiki:** 회상 검색 품질 — RRF 융합 + 그래프-부스트 (골드셋 P@1 45.5→61.4%) ([#3283](https://github.com/choiceoh/Deneb/issues/3283)) ([f138c72](https://github.com/choiceoh/Deneb/commit/f138c729b41c626b35e5a3986501e6dc12fee506))


### 🔧 Internal

* **modelrole:** analysis 역할 제거 — 소비처를 main으로 통합 ([#3278](https://github.com/choiceoh/Deneb/issues/3278)) ([88dd97f](https://github.com/choiceoh/Deneb/commit/88dd97f6a4294adf68926af2e6bbff8547414ee2))

## [4.78.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.77.0...deneb-v4.78.0) (2026-07-07)


### ✨ Features

* **andromeda:** 노트북 자료 영역 3단계 높이 토글 — 접힘·기본·확대 ([#3272](https://github.com/choiceoh/Deneb/issues/3272)) ([33da011](https://github.com/choiceoh/Deneb/commit/33da0115384e782807c05178e1da7ba8ff3796be))
* **mailanalysis:** 자사명 거래처 자기원장 가드 — 추출기 nil-드롭+프롬프트 규칙 ([#3267](https://github.com/choiceoh/Deneb/issues/3267)) ([43341ff](https://github.com/choiceoh/Deneb/commit/43341fff7708f7a6ef2f04ab8c59b095c717756a))
* **mail:** Gmail API 백필·gmailpoll 미러·앱 get 저장소 서빙 — 앱 메일 본문 로컬화 + 이전 메일 확장 ([#3274](https://github.com/choiceoh/Deneb/issues/3274)) ([3a8303c](https://github.com/choiceoh/Deneb/commit/3a8303ce5dc7be2c51b21a15719aeff8fb0c20f7))


### 🐛 Bug Fixes

* **native:** 메일 제목 공유요소 모프 제거 — 상세 진입 시 제목 슬라이드 억제 ([#3271](https://github.com/choiceoh/Deneb/issues/3271)) ([4a8132b](https://github.com/choiceoh/Deneb/commit/4a8132b5ffbfb242e32d223069dafc0343f894d5))
* **wiki:** 메일분석 자동병합에 Message ID 가드 — 다른 메일 오병합 차단 ([#3269](https://github.com/choiceoh/Deneb/issues/3269)) ([72e0087](https://github.com/choiceoh/Deneb/commit/72e00877cf37503324d6863e362cac54b8d61809))


### ⚡ Performance

* **embedding+chat:** /embed 풀 병렬 + 회상 SearchBatch ([#3276](https://github.com/choiceoh/Deneb/issues/3276)) ([3a7aea9](https://github.com/choiceoh/Deneb/commit/3a7aea99de517a7f88a480779dba00c1db26db00))
* **embedding:** BGE-M3 GPU 가동 (GB10 sm_121) + per-embed WARN 로그 필터 ([#3275](https://github.com/choiceoh/Deneb/issues/3275)) ([9078fcb](https://github.com/choiceoh/Deneb/commit/9078fcb6b7821c48fdf9ea7de7e9b7b495782d7c))
* **mail:** 메일 아카이브 로컬 저장소 도입 — mail_archive 12.9s→47ms ([#3270](https://github.com/choiceoh/Deneb/issues/3270)) ([c4f9ec3](https://github.com/choiceoh/Deneb/commit/c4f9ec367e7053c9be3582c2787b0f92fb76f45a))

## [4.77.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.76.0...deneb-v4.77.0) (2026-07-07)


### ✨ Features

* **gateway:** 주간업무보고를 deneb-ui 카드로 — 서버 조립 소관별 접이식+현안 배지, LLM 무개입 ([#3263](https://github.com/choiceoh/Deneb/issues/3263)) ([59905c3](https://github.com/choiceoh/Deneb/commit/59905c31548499e47d77867ec4da89ca18af5e5d))


### 🐛 Bug Fixes

* **andromeda:** deneb-ui 인라인 렌더러 링크 지원 — 네이티브 InlineTokenizer 패리티 ([#3266](https://github.com/choiceoh/Deneb/issues/3266)) ([12f8cdf](https://github.com/choiceoh/Deneb/commit/12f8cdf4f2c44508ea4aa03768d73d75d8c9cb74))
* **native:** 렌더러 인라인 마크다운 전수 감사 — 표 셀·경보·인용 별표 수리 (3구현) ([#3264](https://github.com/choiceoh/Deneb/issues/3264)) ([229915b](https://github.com/choiceoh/Deneb/commit/229915bed9e2de30ab300504dfc8a240e4686489))
* **native:** 리스트 키·타임라인 제목 인라인 마크다운 — **키** 리터럴 별표 수리 ([#3260](https://github.com/choiceoh/Deneb/issues/3260)) ([497b361](https://github.com/choiceoh/Deneb/commit/497b361fc140675bdf4c7e5c43d366097f50026e))
* **native:** 슬라이더 역범위 크래시 가드 + 꺾은선 음수값 in-plot (3구현 감사 2라운드) ([#3265](https://github.com/choiceoh/Deneb/issues/3265)) ([9b5e8ba](https://github.com/choiceoh/Deneb/commit/9b5e8badd951f9e7fbbe40842268da16642cda00))

## [4.76.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.75.1...deneb-v4.76.0) (2026-07-07)


### ✨ Features

* **andromeda:** 노트북 상단 자료 영역 접기/펼치기 토글 ([#3254](https://github.com/choiceoh/Deneb/issues/3254)) ([5283de0](https://github.com/choiceoh/Deneb/commit/5283de06635bba78911f2b8064909a42e9df6260))
* **wiki:** 거래처를 프로젝트 위계 최상단으로 — client 필드·모아보기 그룹핑·회상 앵커·백필 도구 ([#3257](https://github.com/choiceoh/Deneb/issues/3257)) ([4878a53](https://github.com/choiceoh/Deneb/commit/4878a53e6180592595161bda7e9513eabae6e351))


### 🐛 Bug Fixes

* **chat:** 모닝레터 계약 경화 — 플레이스홀더 금지·기한초과 배지 예시·인라인 강조 표기 ([#3256](https://github.com/choiceoh/Deneb/issues/3256)) ([60c8f28](https://github.com/choiceoh/Deneb/commit/60c8f28edfa47dced0cddc67c7e8a9f1d1e76065))
* **chat:** 모닝레터 토큰 계약 정합 — 스킬 '플레이스홀더 금지'와 시세 자동 치환 설계 충돌 해소 ([#3258](https://github.com/choiceoh/Deneb/issues/3258)) ([bde0da5](https://github.com/choiceoh/Deneb/commit/bde0da590e0324c714d98a975aab40429814bf90))

## [4.75.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.75.0...deneb-v4.75.1) (2026-07-07)


### 🐛 Bug Fixes

* **chat:** 챗 무응답 3종 수리 — 히스토리 tool짝 수리·네이티브 스트림 복구·모닝레터 시세 자동 치환 ([#3252](https://github.com/choiceoh/Deneb/issues/3252)) ([fa17798](https://github.com/choiceoh/Deneb/commit/fa17798becd16287475e636b09713ca5feec06a1))


### ⚡ Performance

* **chat:** glm 경로 effort router 개통 + 읽기전용 배칭 유도 — effortRouted kwarg 전제 제거, 직렬 시대 프롬프트 지시 교체 ([#3250](https://github.com/choiceoh/Deneb/issues/3250)) ([0b622d0](https://github.com/choiceoh/Deneb/commit/0b622d0e484e8956a3dbc71c6e1a3b07f26a1b9f))

## [4.75.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.74.0...deneb-v4.75.0) (2026-07-07)


### ✨ Features

* **andromeda:** deneb-ui 데스크톱 패리티 라운드 + [#3233](https://github.com/choiceoh/Deneb/issues/3233) 리뷰 6건 반영 ([#3235](https://github.com/choiceoh/Deneb/issues/3235)) ([9da12f2](https://github.com/choiceoh/Deneb/commit/9da12f275e90346f6d1314a72321601f8c0d0d15))
* **andromeda:** 파일 미리보기를 인라인 탭에서 팝업 모달로 전환 ([#3245](https://github.com/choiceoh/Deneb/issues/3245)) ([31aebd6](https://github.com/choiceoh/Deneb/commit/31aebd6926339c3ebfed2bb17fd06615a5ce175e))
* **chat:** deneb-ui 저작 계약 확장 — 노드 카탈로그를 용도 동사와 함께 명시 ([#3229](https://github.com/choiceoh/Deneb/issues/3229)) ([f989a12](https://github.com/choiceoh/Deneb/commit/f989a1228aece6b86c137487342a2d33f8304342))
* **chat:** deneb-ui 파서 관용화+자동 보정 라운드 — 3구현 동기 (v2.1) ([#3247](https://github.com/choiceoh/Deneb/issues/3247)) ([237fb6c](https://github.com/choiceoh/Deneb/commit/237fb6c26eb5c2f0e47d3285af64ba4ce0ec3fe2))
* **chat:** 읽기전용 다중도구 턴 병렬 실행 — 직렬 대기 낭비(실측 124s/3d) 제거, $ref·변이 도구는 직렬 유지 ([#3249](https://github.com/choiceoh/Deneb/issues/3249)) ([3f59f34](https://github.com/choiceoh/Deneb/commit/3f59f348549824cc3e91269d97f5937e12761489))
* **chat:** 저작 계약 동기 — 표기 관례 프롬프트 + 이브닝레터 마스트헤드·긴급 배지 ([#3238](https://github.com/choiceoh/Deneb/issues/3238)) ([82908eb](https://github.com/choiceoh/Deneb/commit/82908eb10cc4fd3f8dcb6d9bd0c5faeade564d6a))
* **native:** deneb-ui 모션 레이어 — 카드 stagger·차트 draw-in·stat 카운트업·햅틱 ([#3234](https://github.com/choiceoh/Deneb/issues/3234)) ([eeecaa3](https://github.com/choiceoh/Deneb/commit/eeecaa3b8b14f6142f33d84ce4ee93af0a5a9427))
* **native:** deneb-ui 카드 시각 품질 패스 — 차트 값·축 라벨, 표 정렬·구분선, progress %, badge tint ([#3228](https://github.com/choiceoh/Deneb/issues/3228)) ([ac196f4](https://github.com/choiceoh/Deneb/commit/ac196f4581992c944d0fa03df7354b20db61ee43))
* **native:** 렌더러 2라운드 + 아침레터 정돈 — 마크다운 표 fit, stat 그리드, 라인차트 스케일, EUR 제거 ([#3231](https://github.com/choiceoh/Deneb/issues/3231)) ([1e75c42](https://github.com/choiceoh/Deneb/commit/1e75c42ef386f387096a8d6eb1af89423ccf5cd5))
* **native:** 새로고침 정직화 — 피드 PTR 실완료·실패 배너, 메일 스테일 스트립, 업로드 재시도, 상태 프리뷰 ([#3242](https://github.com/choiceoh/Deneb/issues/3242)) ([a529cde](https://github.com/choiceoh/Deneb/commit/a529cde7a61ecfb4bf0d8164ae6a172b5989460b))
* **native:** 아침레터 에디토리얼 리디자인 + 렌더러 3라운드 + 리뷰 반영 ([#3233](https://github.com/choiceoh/Deneb/issues/3233)) ([bf5c206](https://github.com/choiceoh/Deneb/commit/bf5c2064c82065699f6e0784a16ae8afcc257b54))
* **native:** 표면 폴리시 패스 — 모션 토큰 균질화·프레스 팬아웃·공유요소 2쌍·리듬 정렬 ([#3243](https://github.com/choiceoh/Deneb/issues/3243)) ([ffcc490](https://github.com/choiceoh/Deneb/commit/ffcc4902118f2737492dbd956c5316d0c7b08233))
* **native:** 햅틱 강화 — tap VirtualKey 승격, PTR 트리거 틱, 롱프레스 이중진동 수리, 삭제 reject 교정 ([#3248](https://github.com/choiceoh/Deneb/issues/3248)) ([69150e5](https://github.com/choiceoh/Deneb/commit/69150e55308fb5862a1d79b8710e2f0d94c3044c))
* **native:** 화면 전환 안무 + 공유요소 기반 — 드릴인 슬라이드·측면 페이드·메일 제목 morph ([#3239](https://github.com/choiceoh/Deneb/issues/3239)) ([25f7b9c](https://github.com/choiceoh/Deneb/commit/25f7b9c38dd606ec2c539c5df7247759fc985e13))


### 🐛 Bug Fixes

* **andromeda:** HWP 미리보기 글자 깨짐 수리 — 실 문서 섹션의 deflate 트레일링 패딩 허용 ([#3237](https://github.com/choiceoh/Deneb/issues/3237)) ([d43cac5](https://github.com/choiceoh/Deneb/commit/d43cac57935c3d4952266a265272f79f50d0c999))
* **gateway:** observatory 메모리 백로그 오독 수리 — MEMORY.md 증류 스탬프를 일기 포인터로 쓰던 가짜 91d 경보 제거 ([#3244](https://github.com/choiceoh/Deneb/issues/3244)) ([d5160a2](https://github.com/choiceoh/Deneb/commit/d5160a2bc4606938f50d96a868b0391ec3e2b8d0))
* **gateway:** 프로덕션 3일 로그·메트릭 분석 후속 수리 — wormhole 폴백·추론 오프·타임아웃·관측 위생 ([#3240](https://github.com/choiceoh/Deneb/issues/3240)) ([6c066ab](https://github.com/choiceoh/Deneb/commit/6c066ab1edd023d39ea4b1aafa5b0c98f45f71dd))
* **native:** [#3234](https://github.com/choiceoh/Deneb/issues/3234)·[#3235](https://github.com/choiceoh/Deneb/issues/3235) 리뷰 반영 — stagger 노드키·모션 flip·카운트업 정밀도·badge 화이트리스트·음수 열 ([#3236](https://github.com/choiceoh/Deneb/issues/3236)) ([4ddfdcb](https://github.com/choiceoh/Deneb/commit/4ddfdcbe6638b7d90d00baa6c67ab31ac8f7f483))


### 🔧 Internal

* **chat:** 인앱 브라우저 번역 DeepL 전용화 — translation 모델 역할·LLM fallback 폐기 ([#3232](https://github.com/choiceoh/Deneb/issues/3232)) ([ec7d7fd](https://github.com/choiceoh/Deneb/commit/ec7d7fd70b0fdcfea4e65c3287f648af09dddc9b))

## [4.74.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.73.0...deneb-v4.74.0) (2026-07-06)


### ✨ Features

* **gateway:** 회의 합성에 전사 교정 임무 + 업무 토픽지식 주입 — 교정 실험 시리즈 결론 반영 ([#3225](https://github.com/choiceoh/Deneb/issues/3225)) ([3493323](https://github.com/choiceoh/Deneb/commit/34933232c9d44f84f657c47c069774b44b5ddbb9))


### 🐛 Bug Fixes

* **gateway:** plaud 전사 풀을 공유 MCP 클라이언트 직결로 — 실행기 절단이 회의 중간을 들어내던 결함 수리 ([#3227](https://github.com/choiceoh/Deneb/issues/3227)) ([9f54598](https://github.com/choiceoh/Deneb/commit/9f54598153f30ffcc1536804c3fbc1df9d785c5c))
* **gateway:** plaud 회의 합성·gist 호출에 thinking off — max_tokens를 추론이 소진하던 첫 틱 실패 수리 ([#3223](https://github.com/choiceoh/Deneb/issues/3223)) ([1c0f1f6](https://github.com/choiceoh/Deneb/commit/1c0f1f6f44bf35fb04789e6abd46ebc790c14b39))
* **gateway:** 모델 픽커 한도 — 선언 모델 면제·발견 목록 비절단으로 가짜 offline 수정 ([#3226](https://github.com/choiceoh/Deneb/issues/3226)) ([286629a](https://github.com/choiceoh/Deneb/commit/286629af157ff3974baa6a94c516be5c9495e196))

## [4.73.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.72.0...deneb-v4.73.0) (2026-07-06)


### ✨ Features

* **chat:** mcpclient 견고성+운영 품격 — ready-future 초기화·프로세스그룹 계층 종료·stderr 진단링·Stats·공유 레지스트리 ([#3218](https://github.com/choiceoh/Deneb/issues/3218)) ([618f0f5](https://github.com/choiceoh/Deneb/commit/618f0f5aec2abdd2231907bd704a1f0e8a741291))
* **gateway:** Plaud 녹음 자동 분석 — 신규 녹음→회의 리포트→회의록 위키+워크피드 카드 ([#3221](https://github.com/choiceoh/Deneb/issues/3221)) ([47e6ea4](https://github.com/choiceoh/Deneb/commit/47e6ea45869b8caca2fd6388ff165d55ecd99646))


### 🐛 Bug Fixes

* **dev:** pr.sh land에 병렬 세션 가드 — 로컬 HEAD와 PR head 불일치 시 랜딩 거부 ([#3222](https://github.com/choiceoh/Deneb/issues/3222)) ([5e1e457](https://github.com/choiceoh/Deneb/commit/5e1e457aeab4a08b41f898277f7f9c68ff0e7305))

## [4.72.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.71.0...deneb-v4.72.0) (2026-07-06)


### ✨ Features

* **chat:** 외부 MCP 서버를 deferred 챗 도구로 소비 — stdio MCP 클라이언트 + DENEB_MCP_SERVERS 배선 ([#3216](https://github.com/choiceoh/Deneb/issues/3216)) ([372fba7](https://github.com/choiceoh/Deneb/commit/372fba79be3b72e60220993302005cc9023d4c86))
* **wiki:** 자료 인제스트 — URL·유튜브를 1급 위키 메모리로 (wiki action=ingest) ([#3214](https://github.com/choiceoh/Deneb/issues/3214)) ([027e03b](https://github.com/choiceoh/Deneb/commit/027e03bae2d45f982066029b058c0fe83ddbee8d))


### 🐛 Bug Fixes

* **chat:** MCP 소비 경로 보안·견고성 후속 — 자식 env 허용목록·string id 응답·에러 상한·도구명 클램프 ([#3217](https://github.com/choiceoh/Deneb/issues/3217)) ([80aef58](https://github.com/choiceoh/Deneb/commit/80aef58766c4ca9fdae730c8d4b2d6237fee9a4e))
* **deploy:** fossRelease 발행 시 서명 env 부재도 hard fail — debug 폴백 사고 재발 방지 ([#3210](https://github.com/choiceoh/Deneb/issues/3210)) ([fcdafea](https://github.com/choiceoh/Deneb/commit/fcdafeabcb1801d5c5c89d82ab9a6b272612ff69))

## [4.71.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.70.0...deneb-v4.71.0) (2026-07-05)


### ✨ Features

* **denebui:** 카드 상시관찰 + 인터랙티브 개방 + 우선 고려 + 정본 프리뷰 + 규칙 각인 ([#3206](https://github.com/choiceoh/Deneb/issues/3206)) ([a051d9b](https://github.com/choiceoh/Deneb/commit/a051d9b08daca4594b24efb8c9f996c8a75001c4))


### 🐛 Bug Fixes

* **bootstrap:** 다운그레이드 가드 자기판별을 macOS에서도 동작하게 — /proc 폴백 추가 ([#3207](https://github.com/choiceoh/Deneb/issues/3207)) ([d8bbe98](https://github.com/choiceoh/Deneb/commit/d8bbe98f1de3e10d4279e416ae7e07ad0e222d6f))

## [4.70.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.69.0...deneb-v4.70.0) (2026-07-05)


### ✨ Features

* **denebui:** deneb-ui 카드를 라벨 HTML 포맷으로 전면 전환 — JSON 수리계층 은퇴 ([#3202](https://github.com/choiceoh/Deneb/issues/3202)) ([a01b3bb](https://github.com/choiceoh/Deneb/commit/a01b3bb2d876a85ef006168bd9e8308f28579189))
* **mail:** 거래 조건 인용 검증 추출 — 물량·단가·지급조건 (사실 레이어 2단계 A) ([#3196](https://github.com/choiceoh/Deneb/issues/3196)) ([3171269](https://github.com/choiceoh/Deneb/commit/3171269a425a59a75adb35a3b433d7c9f82970f9))
* **wiki:** 거래 원장 v2 — 인용 검증 조건 영속 + 물량 MW 집계 (사실 레이어 2단계 B) ([#3198](https://github.com/choiceoh/Deneb/issues/3198)) ([e7ea196](https://github.com/choiceoh/Deneb/commit/e7ea19686562e44df6215e71c6c51c6face0ea84))
* **wiki:** 재견적 가격 변동 감지 — 프로젝트 현재 상태 자동 불릿 (사실 레이어 2단계 C) ([#3199](https://github.com/choiceoh/Deneb/issues/3199)) ([8c36f65](https://github.com/choiceoh/Deneb/commit/8c36f653b8270d520966d36fecfef31fcde5790b))

## [4.69.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.68.0...deneb-v4.69.0) (2026-07-05)


### ✨ Features

* **wiki:** kinds 2단 체계 — 태양광·기자재·풍력·기타 1차/2차 계층 ([#3192](https://github.com/choiceoh/Deneb/issues/3192)) ([50e5498](https://github.com/choiceoh/Deneb/commit/50e5498afc444cdb7d14c45974895b4895b4767b))

## [4.68.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.67.0...deneb-v4.68.0) (2026-07-05)


### ✨ Features

* **native:** 스킬·Propus·자가개선 화면 개편 — 활동 중심, 비기술 운영자 기준 ([#3188](https://github.com/choiceoh/Deneb/issues/3188)) ([3898bd2](https://github.com/choiceoh/Deneb/commit/3898bd22d606db5db5e9a422d3d2b6bccf97b92f))
* **wiki:** 프로젝트 현장(sites) 정본 필드 — 작성 규칙 확정·광역 정규화·회상 앵커 매칭 ([#3179](https://github.com/choiceoh/Deneb/issues/3179)) ([e809340](https://github.com/choiceoh/Deneb/commit/e809340718d7ac3138f8ca4456df808fd5cbcae2))


### 🐛 Bug Fixes

* **native:** WorkManager Room DB R8 keep — 빌드 571 기동 크래시 수리 ([#3191](https://github.com/choiceoh/Deneb/issues/3191)) ([8fd3892](https://github.com/choiceoh/Deneb/commit/8fd3892ccc47a85e45cabe38a5d362aff4d5cc87))

## [4.67.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.66.0...deneb-v4.67.0) (2026-07-05)


### ✨ Features

* **chat:** in-registry tool audit counters, cache expansion, and dry-run mode ([#3171](https://github.com/choiceoh/Deneb/issues/3171)) ([7cdee69](https://github.com/choiceoh/Deneb/commit/7cdee69edb8006e820ed4f1d0f70c89a59de18cb))
* **chat:** 신규 도구 5종 — workfeed·transcribe·ocr·market·org + sessions stats ([#3178](https://github.com/choiceoh/Deneb/issues/3178)) ([0096a55](https://github.com/choiceoh/Deneb/commit/0096a557a8af119c2e5791f8848cc07455b5b462))
* **chat:** 업스트림 유용 기법 2종 — 자동 스티어 + wormhole /v1/usage (+ main -race 수리) ([#3181](https://github.com/choiceoh/Deneb/issues/3181)) ([2bd33db](https://github.com/choiceoh/Deneb/commit/2bd33db4a08205eb2cf11d52acf8f7cba97fb077))

## [4.66.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.65.2...deneb-v4.66.0) (2026-07-05)


### ✨ Features

* **autonomous:** 하트비트 자가코딩 검토 레인 — 제안된 자가개선 후보 자동 소비 ([#3177](https://github.com/choiceoh/Deneb/issues/3177)) ([9cc821b](https://github.com/choiceoh/Deneb/commit/9cc821b52159b59562af4b59eef76c48c21bb8cd))
* **chat:** Hermes·OpenClaw 심층조사 도입 3종 — fence stale-task 방어 + 캐시 경계 회귀 테스트 + 외부 응답 바운드 ([#3173](https://github.com/choiceoh/Deneb/issues/3173)) ([2c42665](https://github.com/choiceoh/Deneb/commit/2c42665b8cf2ad4187d8b4ee50fbd6cb5c4eee10))
* **native:** 안드로이드 오토 1단계 — 운전 중 알림 읽어주기·음성 답장 ([#3144](https://github.com/choiceoh/Deneb/issues/3144)) ([a120dde](https://github.com/choiceoh/Deneb/commit/a120dde93eac78afb988314a417138f37c25de37))
* **native:** 안드로이드 오토 2단계 — 템플릿 카 화면 (업무 피드 브라우저) ([#3165](https://github.com/choiceoh/Deneb/issues/3165)) ([a547e74](https://github.com/choiceoh/Deneb/commit/a547e744424001ab8ada08fd8eddae71f83dc31e))
* **phone:** 알람·타이머 액션 — phone_write alarm/timer ([#3138](https://github.com/choiceoh/Deneb/issues/3138)) ([5ce5bde](https://github.com/choiceoh/Deneb/commit/5ce5bde663782dbfc757f93f4dceb7f14a5dfe8a))
* **phone:** 폰 액션 실행 결과 왕복 — 앱 회신 + 5초 fail-open 대기 ([#3169](https://github.com/choiceoh/Deneb/issues/3169)) ([7bb6e65](https://github.com/choiceoh/Deneb/commit/7bb6e6549d8b30067365d0c5d67ed43ad8ce6c6c))
* **wormhole:** Kai 업스트림 세부 도입 — 이미지 게이트(웜홀) + 어시스트 제스처 + 에러 폴백 ([#3167](https://github.com/choiceoh/Deneb/issues/3167)) ([d47628c](https://github.com/choiceoh/Deneb/commit/d47628cd162e40031dbab63f613b7acf5bcb40b8))


### 🐛 Bug Fixes

* **chat:** 링크 인리치먼트 병렬화(P4-3) + 하트비트 트리거 메시지 누락 복구 ([#3168](https://github.com/choiceoh/Deneb/issues/3168)) ([bf6965d](https://github.com/choiceoh/Deneb/commit/bf6965d1c14dd163e57937da28efabf223c7c923))
* **native:** compose-material3 1.12 알파 원복 — compileSdk 37 요구로 깨진 APK 발행 복구 ([#3180](https://github.com/choiceoh/Deneb/issues/3180)) ([81742a3](https://github.com/choiceoh/Deneb/commit/81742a3408b9760fadcc965bbe052ba27039d606))
* **native:** 서명 env 강건화 — 명시 오버라이드 경로 오타 hard fail + 비밀번호 인용 규칙 (리뷰 후속) ([#3163](https://github.com/choiceoh/Deneb/issues/3163)) ([b52f40a](https://github.com/choiceoh/Deneb/commit/b52f40a8270e47c82aa0f4676549de33809bb1b2))


### 🔧 Internal

* **gateway:** 보류 항목 집행 — 죽은 status-report 클러스터 retire·CallCodingLLM 삭제·iOS 번들ID de-Kai ([#3166](https://github.com/choiceoh/Deneb/issues/3166)) ([0843575](https://github.com/choiceoh/Deneb/commit/084357573e78ca52fb55ed904f6f156bed3ab02d))
* **mailanalysis:** gmailpoll 패키지를 mailanalysis로 개명 + miniapp.mail.* RPC alias ([#3175](https://github.com/choiceoh/Deneb/issues/3175)) ([8e497dc](https://github.com/choiceoh/Deneb/commit/8e497dcfc9941a1c01e738632004ac9a829f41f6))
* **native:** 클라 RPC를 miniapp.mail.* 정식 네임스페이스로 전환 (Andromeda 포함) ([#3176](https://github.com/choiceoh/Deneb/issues/3176)) ([6ac4d2e](https://github.com/choiceoh/Deneb/commit/6ac4d2eb262a37959b8d995dad14929a67b98f87))

## [4.65.2](https://github.com/choiceoh/Deneb/compare/deneb-v4.65.1...deneb-v4.65.2) (2026-07-05)


### 🐛 Bug Fixes

* **chat:** surface aborted empty sync replies ([#3155](https://github.com/choiceoh/Deneb/issues/3155)) ([17d64c4](https://github.com/choiceoh/Deneb/commit/17d64c4559537ecfeb35b0889e904a28eff22f3b))

## [4.65.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.65.0...deneb-v4.65.1) (2026-07-05)


### 🐛 Bug Fixes

* **native:** 피드 탭을 워크스페이스와 무관하게 — 챗봇 기본화 이후 피드가 설계상 빈 화면이 되던 것 수리 ([#3152](https://github.com/choiceoh/Deneb/issues/3152)) ([1036a1f](https://github.com/choiceoh/Deneb/commit/1036a1f680d9f00b59cdc2c3b9c089fa438b1a25))

## [4.65.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.64.0...deneb-v4.65.0) (2026-07-05)


### ✨ Features

* **calendar:** 회의 후 수확 — 업무 연결 일정 종료 시 결과 질문 푸시 ([#3114](https://github.com/choiceoh/Deneb/issues/3114)) ([0afc351](https://github.com/choiceoh/Deneb/commit/0afc351d7cca3867f4fc85f7488cde2278a6e98c))
* **chat:** read_spillover 페이지네이션 — offset/limit 라인 윈도우 + grep 점프 ([#3136](https://github.com/choiceoh/Deneb/issues/3136)) ([a19d0ef](https://github.com/choiceoh/Deneb/commit/a19d0ef65b02961c48d74c41d534270563397b94))

## [4.64.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.63.1...deneb-v4.64.0) (2026-07-05)


### ✨ Features

* **chat:** chart 도구 개선 — 도넛 수치 라벨·stacked/가로 바·y_unit 눈금·렌더+전송 1회화 ([#3134](https://github.com/choiceoh/Deneb/issues/3134)) ([d9fb6af](https://github.com/choiceoh/Deneb/commit/d9fb6af46874c339f1b2b18db0ff1547171ce972))
* **chat:** 도구 개선 탐구 — 분석 문서 + 로드맵 1~2단계(A·B·D·F) 구현 ([#3117](https://github.com/choiceoh/Deneb/issues/3117)) ([9bb5750](https://github.com/choiceoh/Deneb/commit/9bb575036b13236539e45b04108d9683188a5b55))
* **native:** 알림 파싱 개선 ([#3125](https://github.com/choiceoh/Deneb/issues/3125) 리뷰·개선판) — 구조화 추출 + 누적 페이로드 라인 dedup ([#3130](https://github.com/choiceoh/Deneb/issues/3130)) ([8707908](https://github.com/choiceoh/Deneb/commit/870790810ab3e60da5d635d244a2b5bdb4978c5e))
* **phone:** 앱 사용 리듬을 judgment 턴에서 캐시 전용으로 ([#3132](https://github.com/choiceoh/Deneb/issues/3132) 리뷰·개선판) ([#3133](https://github.com/choiceoh/Deneb/issues/3133)) ([aec133e](https://github.com/choiceoh/Deneb/commit/aec133ee0a23e931a1abb28c9aed64285f2a4c30))


### 🐛 Bug Fixes

* **chat:** main 빌드 수리 ([#3117](https://github.com/choiceoh/Deneb/issues/3117)×[#3121](https://github.com/choiceoh/Deneb/issues/3121) 의미 충돌) + 문서 추출 예산 (P4-2) ([#3123](https://github.com/choiceoh/Deneb/issues/3123)) ([25cccd2](https://github.com/choiceoh/Deneb/commit/25cccd2ee475e7b6cb4b611f961411249eac8239))
* **chat:** recordRunCompletion에 execStats 배선 누락 수리 — main 컴파일 복구 ([#3122](https://github.com/choiceoh/Deneb/issues/3122)) ([d221c69](https://github.com/choiceoh/Deneb/commit/d221c6915d3001228dd172aea258a98b24b4ed02))
* **chat:** 개별 도구 감사 후속 — 안전 게이트·업무 정확성·회복 힌트·코어 도구 4묶음 ([#3127](https://github.com/choiceoh/Deneb/issues/3127)) ([13d000d](https://github.com/choiceoh/Deneb/commit/13d000d3a11d1d08a3230855f7073c507334c60c))


### 🔧 Internal

* **chat:** decompose runAgentWithFallback into fallbackTurn stage methods ([#3120](https://github.com/choiceoh/Deneb/issues/3120)) ([8bef956](https://github.com/choiceoh/Deneb/commit/8bef95605b88d14eb2c84de8530359701fc4bde3))
* **chat:** extract executeAgentRun stages — persist/tail-inject/api-hooks/completion telemetry ([#3121](https://github.com/choiceoh/Deneb/issues/3121)) ([ca32b1a](https://github.com/choiceoh/Deneb/commit/ca32b1a6ba350bb9cc4b3208a3ac54516de89916))
* **chat:** extract prepareContextAndPrompt goroutine bodies into named prep stages ([#3126](https://github.com/choiceoh/Deneb/issues/3126)) ([3806ef3](https://github.com/choiceoh/Deneb/commit/3806ef3dbef11b995c65eb78cc6435c1d564daa9))
* **chat:** fold ambient and coding hook deps into AmbientDeps/CodingDeps (triple-mirror cleanup, cluster 1) ([#3129](https://github.com/choiceoh/Deneb/issues/3129)) ([801205f](https://github.com/choiceoh/Deneb/commit/801205feef00025de3f14159da0bd264ead8441b))
* **chat:** fold memory and skill-loop deps into MemoryDeps/SkillDeps (triple-mirror cleanup, cluster 2) ([#3131](https://github.com/choiceoh/Deneb/issues/3131)) ([5862551](https://github.com/choiceoh/Deneb/commit/586255109b4416a7feaf76760bb224af3cec2e56))
* **chat:** split assembleMessages into assembly and compaction stages ([#3128](https://github.com/choiceoh/Deneb/issues/3128)) ([6840a06](https://github.com/choiceoh/Deneb/commit/6840a063a2d274501fadaf2492fbadf86d6770e4))
* **chat:** split handleRunSuccess into silent-policy/persist/deliver stages ([#3124](https://github.com/choiceoh/Deneb/issues/3124)) ([66d7724](https://github.com/choiceoh/Deneb/commit/66d7724e736fd2d424a18dc1f7125d93aff2e6b1))
* **chat:** 파이프라인 개선 P0+P1+P3 — 죽은 배관 제거, 규칙 위반 수리, 출력 정화 통일, 테스트 방벽 ([#3118](https://github.com/choiceoh/Deneb/issues/3118)) ([e3820a1](https://github.com/choiceoh/Deneb/commit/e3820a19bc0d00b5ab5e1a41863941e1cb4ccd95))

## [4.63.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.63.0...deneb-v4.63.1) (2026-07-05)


### 🐛 Bug Fixes

* **gateway:** 다운그레이드 가드 후속 — (deleted) 경로 P0·거부 후보 격리·동버전 스탬프·게이트 선행 ([#3110](https://github.com/choiceoh/Deneb/issues/3110)) ([1748748](https://github.com/choiceoh/Deneb/commit/17487484446abcfced11d4eb906a70e0cf941e50))
* **native:** 피드 빈 화면 고착 수리 — 동기화 자가복구 + 당겨서 새로고침 ([#3113](https://github.com/choiceoh/Deneb/issues/3113)) ([940597e](https://github.com/choiceoh/Deneb/commit/940597ed10c53dc72c3a4d4b6018af5e0b884d1d))
* **wiki:** 봇 리뷰 유효 지적 일괄 수리 — 회상앵커 원문매칭·보관/대체 제외·원장 백필·벤치 채점 강화 ([#3109](https://github.com/choiceoh/Deneb/issues/3109)) ([46d638d](https://github.com/choiceoh/Deneb/commit/46d638d01f915b517438334f65996b594bd7cfca))

## [4.63.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.62.2...deneb-v4.63.0) (2026-07-05)


### ✨ Features

* **autonomous:** 하트비트 리서치 레인 — 새 데이터 다이제스트로 자가 리서치 항목 등록 ([#3104](https://github.com/choiceoh/Deneb/issues/3104)) ([d84c713](https://github.com/choiceoh/Deneb/commit/d84c713e073e536261558f9f17f364a9c113034b))
* **dev:** 메일 분석 고정 벤치 mail-bench.py — 함정 18지표 게이트 + 실메일 섀도런/당사자 앵커 ([#3086](https://github.com/choiceoh/Deneb/issues/3086)) ([9b3be16](https://github.com/choiceoh/Deneb/commit/9b3be16e7384d8eead14819ad6f44278cdea4bd0))
* **dev:** 위키 QA 벤치 — 골드셋 기반 recall/answer 채점 자 ([#3101](https://github.com/choiceoh/Deneb/issues/3101)) ([aba545c](https://github.com/choiceoh/Deneb/commit/aba545c4601d47a00ff6dd82839dfa9a9bc3436e))
* **gateway:** 다운그레이드 가드 — 구버전 바이너리로의 SIGUSR1 재시작을 수신측에서 거부 ([#3106](https://github.com/choiceoh/Deneb/issues/3106)) ([40a9bad](https://github.com/choiceoh/Deneb/commit/40a9baddf50e78e42a36a15fc649e9d5d1704fa8))
* **mail:** 거래처 앵커 — 당사자 라벨에 활성 거래처·연결 프로젝트 결정적 병기 ([#3096](https://github.com/choiceoh/Deneb/issues/3096)) ([7c71e35](https://github.com/choiceoh/Deneb/commit/7c71e35bf9d9aa9af96ec30c0557afa9a8b43166))
* **mail:** 날짜 앵커 결정적 주입 — 상대 날짜를 환산표 조회로 (모델 산술 약점 봉쇄) ([#3095](https://github.com/choiceoh/Deneb/issues/3095)) ([3be236a](https://github.com/choiceoh/Deneb/commit/3be236a7336fe970534ac75332bdffb760fc3e5f))
* **mail:** 메일 분석에 당사자 앵커 결정적 주입 — 분석 모델의 당사자 뒤집기 제거 ([#3091](https://github.com/choiceoh/Deneb/issues/3091)) ([1e160c4](https://github.com/choiceoh/Deneb/commit/1e160c4a1f2c9d9894473f7795a1653eb1235405))
* **phone:** 폰 도구 전면 앱 이전 — Termux/SSH 은퇴 ([#3099](https://github.com/choiceoh/Deneb/issues/3099)) ([db003d4](https://github.com/choiceoh/Deneb/commit/db003d420c5d10b61daa6ca7e093db992811be77))
* **wiki:** deal_ledger 도구 — 정형 거래 원장 채팅 집계 (사실 레이어 1단계) ([#3102](https://github.com/choiceoh/Deneb/issues/3102)) ([81d384c](https://github.com/choiceoh/Deneb/commit/81d384cd4eb2793eec58b2370d412f2c820c0c7f))
* **wiki:** 그래프 이웃 라벨을 의미 기반(거래처/프로젝트/기자재/인물)으로 ([#3094](https://github.com/choiceoh/Deneb/issues/3094)) ([b0fc3ea](https://github.com/choiceoh/Deneb/commit/b0fc3eaf9d054908ee0d56599972d7b6594cc2c3))
* **wiki:** 미해결 질문 루프 — 리서치 적립·모닝레터 승격 ([#3098](https://github.com/choiceoh/Deneb/issues/3098)) ([9ef16b1](https://github.com/choiceoh/Deneb/commit/9ef16b12c2b4c34f2015970f13903faff849d636))
* **wiki:** 프로젝트 가족 엣지 + 거래처 회상 앵커 ([#3097](https://github.com/choiceoh/Deneb/issues/3097)) ([c292765](https://github.com/choiceoh/Deneb/commit/c292765442f856eb8e30bfca2b7f890461eb9a3f))


### 🐛 Bug Fixes

* **autonomous:** 하트비트 빈파일 가드 — 제목뿐인 HEARTBEAT.md에 매 턴 3만 토큰 태우던 것 차단 ([#3100](https://github.com/choiceoh/Deneb/issues/3100)) ([1ecb4ce](https://github.com/choiceoh/Deneb/commit/1ecb4ce1f8f504a8d4af20da3546b7dbc87ddc16))
* **chat:** polaris 색인을 가독 프로즈로 + 0건 시 범위 안내 — 스니펫 JSON 누출·빈손 오해 수정 ([#3090](https://github.com/choiceoh/Deneb/issues/3090)) ([25ec632](https://github.com/choiceoh/Deneb/commit/25ec632ca86b936fab58ab15b8534a8b3ac76f98))
* **chat:** run.cache 관측 복구 — DENEB_ENGINE_METRICS_URL 오버라이드 (웜홀 뒤 엔진 /metrics 지정) ([#3092](https://github.com/choiceoh/Deneb/issues/3092)) ([6460ecd](https://github.com/choiceoh/Deneb/commit/6460ecd542d40155bcefffe9605224c77f583237))
* **chat:** 봇 리뷰 유효 지적 일괄 수리 — 챗봇 톤 가드 정밀화 + mail-bench 스크립트 강건화 ([#3108](https://github.com/choiceoh/Deneb/issues/3108)) ([70ca6a9](https://github.com/choiceoh/Deneb/commit/70ca6a9f90676aa0c8b953ab18b37ad33ac369a8))
* **chat:** 챗봇 워크스페이스 대화 규범 — 존댓말 고정·무요청 논평 금지·질문 전량 응답 ([#3093](https://github.com/choiceoh/Deneb/issues/3093)) ([e84a28b](https://github.com/choiceoh/Deneb/commit/e84a28bccde04db36a6cc978eddb3a8c768f316c))
* **chat:** 컴팩션 cheap 패스에서 fetch_tools 스키마 결과 보호 — 동일 재fetch 20% 낭비 제거 ([#3089](https://github.com/choiceoh/Deneb/issues/3089)) ([a174402](https://github.com/choiceoh/Deneb/commit/a174402a1bd5f42107185a35ce5c068f2b595771))
* **llm:** StripThinkingTags가 문서 속 &lt;thinking&gt; 언급에서 본문을 절단하던 것 수정 ([#3080](https://github.com/choiceoh/Deneb/issues/3080)) ([b024334](https://github.com/choiceoh/Deneb/commit/b0243349800646b760984c1b4e7d99e02ca1dbe0))
* **mail:** 봇 리뷰 유효 지적 일괄 수리 — 날짜앵커·당사자앵커·거래처 배선·아카이브 날짜창 ([#3107](https://github.com/choiceoh/Deneb/issues/3107)) ([08c38a5](https://github.com/choiceoh/Deneb/commit/08c38a5e81f4f1dc5823b6e5583c8ae74344a491))
* **mail:** 아카이브 날짜 필터 SENTSINCE 전환 + 검색 질의 토큰화 — 최근 메일 누락·다단어 검색 빈손 수정 ([#3088](https://github.com/choiceoh/Deneb/issues/3088)) ([76a0dc4](https://github.com/choiceoh/Deneb/commit/76a0dc4ad0bfa547f0d45530b498f008b31934b4))
* **native:** 빈/에러 상태 문구가 화면 왼쪽 끝에 붙던 것 수정 — 공용 헬퍼 중앙정렬 ([#3087](https://github.com/choiceoh/Deneb/issues/3087)) ([1579afb](https://github.com/choiceoh/Deneb/commit/1579afb984f125bfe0fc75fd3cf24cce7fefc294))
* **review-sweep:** 봇 리뷰 유효 지적 일괄 수리 — 하트비트·스킬리뷰·폰·run.cache·polaris ([#3105](https://github.com/choiceoh/Deneb/issues/3105)) ([ce165e8](https://github.com/choiceoh/Deneb/commit/ce165e867def40b64370f7cbd0af326b0a430310))
* **scripts:** 배포 버전 후퇴 경보 + 하네스 기본 게이트웨이 srv4로 ([#3085](https://github.com/choiceoh/Deneb/issues/3085)) ([dbe8743](https://github.com/choiceoh/Deneb/commit/dbe874339fec5e5ad3337b2dea0853a668e899ac))
* **wiki:** 검색·서빙 감사 후속 — 재시작 후 staleness 강등 복원·정렬 후 절단·cue 스니펫 은닉 ([#3081](https://github.com/choiceoh/Deneb/issues/3081)) ([1bec6fe](https://github.com/choiceoh/Deneb/commit/1bec6feeab8dcde1852e7de592c06c662111e489))
* **wiki:** 스토어 코어 하드닝 — 인덱스 스냅샷·병합 정규화 가드·related 영속·프론트매터 이스케이프 ([#3082](https://github.com/choiceoh/Deneb/issues/3082)) ([b57f826](https://github.com/choiceoh/Deneb/commit/b57f82612b65064d79977ce0adabdf129d368979))


### ⚡ Performance

* **genesis:** 스킬리뷰 포크 전용 미니 시스템 프롬프트 — 메인 세션 조립 ~50K 비상속 ([#3103](https://github.com/choiceoh/Deneb/issues/3103)) ([bd01277](https://github.com/choiceoh/Deneb/commit/bd01277f2d08278a5b59845a4bcf7acbd14bd9b7))

## [4.62.2](https://github.com/choiceoh/Deneb/compare/deneb-v4.62.1...deneb-v4.62.2) (2026-07-04)


### 🐛 Bug Fixes

* **llm:** glm 추론 폭주 3중 방어 — 명시 effort 존중·봉투 1MiB·length=에러 ([#3078](https://github.com/choiceoh/Deneb/issues/3078)) ([c83b787](https://github.com/choiceoh/Deneb/commit/c83b787e131433923cf11c6bb377b3405a1c1051))
* **skills:** evolve 프롬프트 위생 — 검증케이스 조각 정화·온도 0·꼬리 진단 ([#3077](https://github.com/choiceoh/Deneb/issues/3077)) ([121e243](https://github.com/choiceoh/Deneb/commit/121e2434fc59d546eeaec49295c4429da5514085))
* **skills:** evolver LLM 호출 비스트리밍 전환 — glm 스트리밍 JSON 누출/절단 방어 ([#3072](https://github.com/choiceoh/Deneb/issues/3072)) ([791fe85](https://github.com/choiceoh/Deneb/commit/791fe8503a565f9bc644c9fca27eb2e52cfe48de))
* **skills:** evolver 출력 예산 증액 — glm 추론이 완성 예산을 공유 ([#3074](https://github.com/choiceoh/Deneb/issues/3074)) ([4aa105b](https://github.com/choiceoh/Deneb/commit/4aa105ba0b015dc9fe961376d0cfe2716d09d61c))

## [4.62.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.62.0...deneb-v4.62.1) (2026-07-03)


### 🐛 Bug Fixes

* **andromeda:** 리뷰 잔여분 — 노트 저장 위장·첨부 중 세션 가드·HWP 상한/DIFAT·탭 동기화 외 ([#3066](https://github.com/choiceoh/Deneb/issues/3066)) ([335d5e4](https://github.com/choiceoh/Deneb/commit/335d5e4cbc72bc84cdc72764fa95fe25ef0ac2f3))
* **gateway:** 리뷰 잔여분 — provider ${ENV} 확장·위키 Created 영속·verify 레이스 가드·스킬 힌트 프리셋 게이트·MCP 위생 ([#3067](https://github.com/choiceoh/Deneb/issues/3067)) ([d6404de](https://github.com/choiceoh/Deneb/commit/d6404de151aa8d894d2c8b56997ab9bf90ce44ea))
* **native:** 전송 대기열 4종 — 세션 전환 오발사·programmatic 큐잉·실패 위장 드레인·유실 복원 ([#3069](https://github.com/choiceoh/Deneb/issues/3069)) ([d1cd3bb](https://github.com/choiceoh/Deneb/commit/d1cd3bb8cceca5baac4f7b30e58607976da0020d))
* **scripts:** A/B 배터리 채점 신뢰도 — JSON모드 거부 벌점·역할별 버딕트·프로덕션 계약 정렬 ([#3065](https://github.com/choiceoh/Deneb/issues/3065)) ([cc08005](https://github.com/choiceoh/Deneb/commit/cc08005204b6d9985a85ab5d98eee488020bdb7e))
* **skills:** read 도구 스킬 소비 집계 + 코딩 세션 기록 제외 ([#3064](https://github.com/choiceoh/Deneb/issues/3064)) ([1bc118a](https://github.com/choiceoh/Deneb/commit/1bc118a89acc99444c84e67068f1b1ab09fb83a8))
* **wiki:** 리뷰 잔여분 일괄 — 대표.md 교차매칭·link_prune·로그 회전 유실·종결 가드·리콜 앵커 ([#3068](https://github.com/choiceoh/Deneb/issues/3068)) ([fff5a39](https://github.com/choiceoh/Deneb/commit/fff5a39539faef1abc8ed4502ba0c2010db8eaab))

## [4.62.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.61.0...deneb-v4.62.0) (2026-07-03)


### ✨ Features

* **skills:** 스킬 자동 표면화 — 트리거 힌트 + 캡처 배선 + 사용 기록 위생 ([#3060](https://github.com/choiceoh/Deneb/issues/3060)) ([1b39285](https://github.com/choiceoh/Deneb/commit/1b39285e9a6b02a1c3ff3393222fd062d0531d4d))


### 🐛 Bug Fixes

* **skills:** 리뷰 입력 재균형(크론 제외) + 힌트 발화 계측 + 오버라이드 문서 ([#3063](https://github.com/choiceoh/Deneb/issues/3063)) ([9709197](https://github.com/choiceoh/Deneb/commit/9709197dff3e33aae1ca401e38aa0da30e538883))

## [4.61.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.60.0...deneb-v4.61.0) (2026-07-03)


### ✨ Features

* **scripts:** lightweight/tiny 모델 교체용 실부하 A/B 배터리 ([#3049](https://github.com/choiceoh/Deneb/issues/3049)) ([5f41dba](https://github.com/choiceoh/Deneb/commit/5f41dba161f252af60be4c1b81585dd3dd4c5117))


### 🐛 Bug Fixes

* **andromeda:** 파일 pane 미저장 편집 유실·빈 파일 저장·HWP 방어 수정 ([#3058](https://github.com/choiceoh/Deneb/issues/3058)) ([82f3d7a](https://github.com/choiceoh/Deneb/commit/82f3d7aa16987ce9ace920999341f6ab85f460de))
* **wiki:** 리뷰어 정비 작업을 조기 종료 밖으로 이동 — 조용한 사이클에도 실행 ([#3053](https://github.com/choiceoh/Deneb/issues/3053)) ([3bb617c](https://github.com/choiceoh/Deneb/commit/3bb617cf411686a3170f812b1fd40c73fab0d1aa))
* **wiki:** 큐 앵커·가중 검색 리뷰 후속 — 리댁션·캡 일원화·병합 보존·NaN 가드·검증 후 절단 ([#3052](https://github.com/choiceoh/Deneb/issues/3052)) ([7bf8574](https://github.com/choiceoh/Deneb/commit/7bf8574a4dde8734dea78aff106f749d392c7a45))
* 머지된 PR 리뷰 후속 일괄 — 편집 들여쓰기·체크포인트 경합·모델 조형·거래처 창·위키 가드 ([#3055](https://github.com/choiceoh/Deneb/issues/3055)) ([0eb7fb0](https://github.com/choiceoh/Deneb/commit/0eb7fb0c77008fdb3069e2c440b49e16b8d4a0de))

## [4.60.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.59.0...deneb-v4.60.0) (2026-07-03)


### ✨ Features

* **andromeda:** HWP 인앱 미리보기 — 텍스트·표·이미지 (순수 TS 파서 직접 구현) ([#3050](https://github.com/choiceoh/Deneb/issues/3050)) ([1e31736](https://github.com/choiceoh/Deneb/commit/1e317361540fa7cbba20f26013e6481d38c9e10b))
* **andromeda:** 파일 미리보기 + 라이브 편집 + 탭 — AionUi식 인앱 뷰어 ([#3048](https://github.com/choiceoh/Deneb/issues/3048)) ([91201fe](https://github.com/choiceoh/Deneb/commit/91201fed91b6ccc3004df67347ed4afa395ab00c))
* **wiki:** 드리머 종합 개선 — 구조 병합·update 폴백 dedup·즉시 첫 체크·partial 백프레셔·드림 피드 카드 ([#3051](https://github.com/choiceoh/Deneb/issues/3051)) ([34b3823](https://github.com/choiceoh/Deneb/commit/34b38234e0449061c7f0cc2e6f4753fea4640255))
* **wiki:** 드림 apply 구조 가드 — 진행 로그 리라우팅 + 일일 다이제스트 생성 차단 ([#3046](https://github.com/choiceoh/Deneb/issues/3046)) ([cd494be](https://github.com/choiceoh/Deneb/commit/cd494be89f6c4167717aeb7b81611da42f412000))

## [4.59.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.58.0...deneb-v4.59.0) (2026-07-03)


### ✨ Features

* **code:** 코드모드 품질 후속 — edit 공백 관용 자동 적용 + 체크포인트 tiny 한국어 라벨 ([#3035](https://github.com/choiceoh/Deneb/issues/3035)) ([b4ab613](https://github.com/choiceoh/Deneb/commit/b4ab613ef1ba3007afbc578b6042a06b619b7cc2))
* **gateway:** MCP 게이트웨이 — 데네브 기억을 외부 AI 도구의 표준 도구로 ([#3036](https://github.com/choiceoh/Deneb/issues/3036)) ([6efdb4e](https://github.com/choiceoh/Deneb/commit/6efdb4e41c0ca4eecb7d6fd20c618c3212c8b624))
* **mail:** 메일 우선순위에 활성 거래처 결합 신호 — 위키 메일분석 도메인 조인 (+2 증폭) ([#3039](https://github.com/choiceoh/Deneb/issues/3039)) ([a42c877](https://github.com/choiceoh/Deneb/commit/a42c877cde3d8078d5b79e6052d24d874719327d))
* **wiki:** 정체성 필드 가중 검색(BM25F-lite) — 본문 반복이 대표 문서를 밀어내지 않게 ([#3043](https://github.com/choiceoh/Deneb/issues/3043)) ([ac8bc0f](https://github.com/choiceoh/Deneb/commit/ac8bc0f88125c0c16e7c1b090baef57300a2b101))
* **wiki:** 큐 앵커(cue anchors) — 다른 어휘의 질문이 문서에 닿게 (Memora 아이디어 차용) ([#3040](https://github.com/choiceoh/Deneb/issues/3040)) ([8962eb0](https://github.com/choiceoh/Deneb/commit/8962eb01ebfaeb6b127fa604d4f146f674f57e92))


### 🐛 Bug Fixes

* **chat:** 일지 자동기록·드림턴 트리거를 sync 경로에도 배선 — 네이티브 턴이 일지·드리밍에서 누락되던 갭 ([#3044](https://github.com/choiceoh/Deneb/issues/3044)) ([813b62a](https://github.com/choiceoh/Deneb/commit/813b62abf1d96096ea40d0789e86f0abfdb1bafa))
* **localai:** 허브·pilot 원시 콜에 dsv4 thinking 토글 — 공유 3분기 셰이핑으로 통일 ([#3042](https://github.com/choiceoh/Deneb/issues/3042)) ([a33f6a4](https://github.com/choiceoh/Deneb/commit/a33f6a409aaf22775dd6671df11b1ebe20b0a267))
* **wiki:** 드림 사이클 LLM 콜에 thinking-off 셰이핑 — dsv4 추론이 합성 예산을 소진하던 실패 수리 ([#3041](https://github.com/choiceoh/Deneb/issues/3041)) ([1dac516](https://github.com/choiceoh/Deneb/commit/1dac51612c1a4f53462b0b7a33ace7a9c5270c48))
* **wiki:** 드림 합성 JSON 회수 파서 + 예산 8192 — 손상 배열이 사이클을 무산시키지 않게 ([#3045](https://github.com/choiceoh/Deneb/issues/3045)) ([60f9d53](https://github.com/choiceoh/Deneb/commit/60f9d53ec8c71628bf643bb1f8970531990a41cf))

## [4.58.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.57.0...deneb-v4.58.0) (2026-07-02)


### ✨ Features

* **code:** 코드모드 행동 분리 — 구현자 프롬프트 프로파일·코딩 프리셋 + 바인딩/체크포인트 수리 ([#3034](https://github.com/choiceoh/Deneb/issues/3034)) ([939e3a0](https://github.com/choiceoh/Deneb/commit/939e3a001606a1a293760f64e2d533ae2d1b0559))
* **wiki:** 구조 후속 — 깊이 가드·미분류 메일 재분류·회상 프로젝트 앵커 ([#3032](https://github.com/choiceoh/Deneb/issues/3032)) ([ad382e3](https://github.com/choiceoh/Deneb/commit/ad382e3a1fdcaac263285be2bddb8f607ecbbcd4))

## [4.57.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.56.0...deneb-v4.57.0) (2026-07-02)


### ✨ Features

* **andromeda:** 위키 트리 탐색 — 폴더 계층 그대로 접고 펼치는 레일 ([#3031](https://github.com/choiceoh/Deneb/issues/3031)) ([df4dc97](https://github.com/choiceoh/Deneb/commit/df4dc979cd5c5bdd4a53e5639271d8e79df7f5a7))
* **wiki:** 죽은 관련 링크 정리 — 리뷰어 위생 스윕 ([#3030](https://github.com/choiceoh/Deneb/issues/3030)) ([7121ad1](https://github.com/choiceoh/Deneb/commit/7121ad14957e79bcb2a140c3869c632b1d804c36))
* **wiki:** 중복 방어 3겹 — 쓰기 가드 + 위키 리뷰어(관찰모드·analysis) + 자가치유·보존정책 ([#3027](https://github.com/choiceoh/Deneb/issues/3027)) ([2f0d539](https://github.com/choiceoh/Deneb/commit/2f0d539fc1d18b0aa234c97bdb86d231f0056e9f))
* **wiki:** 프로젝트 종결/재개 + 졸음 감지 — 생애주기 완성 ([#3029](https://github.com/choiceoh/Deneb/issues/3029)) ([2916393](https://github.com/choiceoh/Deneb/commit/29163933c20cff38a2a384bd3523b8b80bf76338))

## [4.56.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.55.0...deneb-v4.56.0) (2026-07-02)


### ✨ Features

* **chat:** 응답 중 전송 대기열 — 답변 끝나면 자동 전송 ([#3026](https://github.com/choiceoh/Deneb/issues/3026)) ([7c06609](https://github.com/choiceoh/Deneb/commit/7c0660972c6efdc4132c1efe33c7d894aebd605c))
* **wiki:** 프로젝트 문서 스키마 정형화 — 프로젝트당 폴더 + 대표/로그/기자재/메일분석 슬롯 ([#3021](https://github.com/choiceoh/Deneb/issues/3021)) ([ab54b87](https://github.com/choiceoh/Deneb/commit/ab54b87399f11d249e6a5fdd0f78cb889cc09430))


### 🐛 Bug Fixes

* **andromeda:** Ctrl+C 복사가 코드 화면 전환으로 둔갑하던 단축키 충돌 수정 ([#3024](https://github.com/choiceoh/Deneb/issues/3024)) ([ce26e7f](https://github.com/choiceoh/Deneb/commit/ce26e7ff90c2256632167a9050461003bba529fc))

## [4.55.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.54.0...deneb-v4.55.0) (2026-07-02)


### ✨ Features

* **andromeda:** 노트북을 AI 작업대로 재구성 — 칩 자료 + 답변을 노트로 저장 ([#3016](https://github.com/choiceoh/Deneb/issues/3016)) ([937d15f](https://github.com/choiceoh/Deneb/commit/937d15f5b2775a234db331d421d9a7594234eae2))
* **andromeda:** 우측 Deneb 패널에 파일 첨부 (이미지 OCR·문서·녹음) ([#3018](https://github.com/choiceoh/Deneb/issues/3018)) ([27e4167](https://github.com/choiceoh/Deneb/commit/27e4167fdbfe2a4cf8e13b1791094e20ea472d4c))
* **andromeda:** 채팅 영역 전체를 무표시 드롭존으로 — 드래그 중일 때만 살짝 표시 ([#3019](https://github.com/choiceoh/Deneb/issues/3019)) ([fb150d8](https://github.com/choiceoh/Deneb/commit/fb150d8ba71600537370e93d14cd6d60239bb324))
* **wiki:** read 액션에 paths 묶음 조회 — 위키 왕복을 한 호출로 ([#3015](https://github.com/choiceoh/Deneb/issues/3015)) ([39f0a53](https://github.com/choiceoh/Deneb/commit/39f0a53eaee6f597c0cca7f270dc0080af901dc8))


### 🐛 Bug Fixes

* **chat:** 대기 상태 텍스트 정렬 통일 — 길어도 중앙정렬/2줄로 안 튀게 ([#3020](https://github.com/choiceoh/Deneb/issues/3020)) ([0454e17](https://github.com/choiceoh/Deneb/commit/0454e17fe737590171777c791e8dd20440d1be76))
* **chat:** 읽는 중 새 메시지 도착 시 바닥으로 안 끌려가게 (네이티브) ([#3014](https://github.com/choiceoh/Deneb/issues/3014)) ([9b1b6d8](https://github.com/choiceoh/Deneb/commit/9b1b6d83a6feb3966797dfddce4b063526934dfc))
* **chat:** 전송 직후 응답 대기 행까지 완전히 스크롤 (네이티브) ([#3017](https://github.com/choiceoh/Deneb/issues/3017)) ([eaf964c](https://github.com/choiceoh/Deneb/commit/eaf964c434a4638e95c5160699f804eafbbcf1da))
* **chat:** 전송 후 대화가 맨 아래까지 스크롤되도록 (네이티브) ([#3013](https://github.com/choiceoh/Deneb/issues/3013)) ([c37bb82](https://github.com/choiceoh/Deneb/commit/c37bb821dd0806455e7068f2fea80c1572009fd4))
* **chat:** 전송 후 입력창 안 비워지는 회귀 수정 (한글 IME) ([#3011](https://github.com/choiceoh/Deneb/issues/3011)) ([392cc5a](https://github.com/choiceoh/Deneb/commit/392cc5a92872b1efd894c01ed7936bca48636772))

## [4.54.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.53.0...deneb-v4.54.0) (2026-07-01)


### ✨ Features

* **andromeda:** 메일 상세에 분석/본문 토글 (분석 기본) ([#3004](https://github.com/choiceoh/Deneb/issues/3004)) ([1d6dded](https://github.com/choiceoh/Deneb/commit/1d6dded1e92686d76cc0bfcacfbccd001ae02d4e))
* **skills:** genesis 능동 캡처 전환 + 도구 부정단정 가드레일 ([#3006](https://github.com/choiceoh/Deneb/issues/3006)) ([fd9f914](https://github.com/choiceoh/Deneb/commit/fd9f91473a1f5d37489b176e7a618996c1b75547))
* **skills:** proactive evolve 활성화 — 최초 스킬 최적화 유도 ([#3007](https://github.com/choiceoh/Deneb/issues/3007)) ([f665db9](https://github.com/choiceoh/Deneb/commit/f665db93bf42b271b6187c8629f8ac136c59e18d))


### 🐛 Bug Fixes

* **native:** 챗봇 모드에서도 더보기에 모든 섹션 표시 ([#3008](https://github.com/choiceoh/Deneb/issues/3008)) ([c313b3f](https://github.com/choiceoh/Deneb/commit/c313b3faa9ca554d0338504bc7910a108183e299))

## [4.53.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.52.0...deneb-v4.53.0) (2026-06-30)


### ✨ Features

* **market:** copper in USD/tonne + big tiled 시장 card on the 오늘 dashboard ([#3003](https://github.com/choiceoh/Deneb/issues/3003)) ([1d35097](https://github.com/choiceoh/Deneb/commit/1d350973197be4ca188c3ea802d79c950426c3f0))
* **native:** 챗봇 모드에도 하단 5탭 + 기본 모드를 챗봇으로 ([#3001](https://github.com/choiceoh/Deneb/issues/3001)) ([d469403](https://github.com/choiceoh/Deneb/commit/d4694030ba353bb57231929a7b75aa962a96117d))

## [4.52.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.51.0...deneb-v4.52.0) (2026-06-30)


### ✨ Features

* **andromeda:** wiki opens pages in preview by default (edit on toggle) ([#2999](https://github.com/choiceoh/Deneb/issues/2999)) ([0d5c6c3](https://github.com/choiceoh/Deneb/commit/0d5c6c304f47a52bfb47cb16455eda331c0dcbb5))


### 🐛 Bug Fixes

* **andromeda:** repair auto-updater endpoint via rolling andromeda-latest manifest ([#2997](https://github.com/choiceoh/Deneb/issues/2997)) ([57d99f9](https://github.com/choiceoh/Deneb/commit/57d99f9fd2fae643261b0fd9af1c6c00dee3f45f))
* **andromeda:** 메일 AI 분석 카드를 흰색으로 — 회색 배경에 표·내용이 묻히던 문제 ([#3000](https://github.com/choiceoh/Deneb/issues/3000)) ([51cda8a](https://github.com/choiceoh/Deneb/commit/51cda8a1b8647b4eea82ff7f3dcea8d2a58b8c81))

## [4.51.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.50.0...deneb-v4.51.0) (2026-06-30)


### ✨ Features

* **miniapp:** 코드모드 모바일 네이티브 화면 추가 ([#2995](https://github.com/choiceoh/Deneb/issues/2995)) ([94b31eb](https://github.com/choiceoh/Deneb/commit/94b31ebdaf7eaea33d6617895ef9a5792a89feee))

## [4.50.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.49.0...deneb-v4.50.0) (2026-06-30)


### ✨ Features

* **andromeda:** wiki — compact rail, no stacking, and a collapsible Deneb panel ([#2992](https://github.com/choiceoh/Deneb/issues/2992)) ([0dcf730](https://github.com/choiceoh/Deneb/commit/0dcf7305706aba3525722b3bf142090c6aef5291))


### 🐛 Bug Fixes

* **native:** SSE 응답 한글 깨짐(�) — 멀티바이트 UTF-8 안전 디코드 ([#2994](https://github.com/choiceoh/Deneb/issues/2994)) ([dbd2cc5](https://github.com/choiceoh/Deneb/commit/dbd2cc582f82758511128b83a2e3a089f804b22c))

## [4.49.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.48.0...deneb-v4.49.0) (2026-06-30)


### ✨ Features

* **andromeda:** split the notebook top into a source list + detail pane ([#2989](https://github.com/choiceoh/Deneb/issues/2989)) ([e66e395](https://github.com/choiceoh/Deneb/commit/e66e39554f71cf4bcd9fb60c2989c11a3b1be633))
* **workfeed:** widen the mail-report card title to ~20 chars, drop the hard clamp ([#2991](https://github.com/choiceoh/Deneb/issues/2991)) ([ea8e61e](https://github.com/choiceoh/Deneb/commit/ea8e61e6c3541a9f77262c0673503a2268c7af64))

## [4.48.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.47.1...deneb-v4.48.0) (2026-06-30)


### ✨ Features

* **andromeda:** dock the Deneb chat at the bottom on the notebook view ([#2986](https://github.com/choiceoh/Deneb/issues/2986)) ([7b1c707](https://github.com/choiceoh/Deneb/commit/7b1c707ba5a41abc9d24a9de7278649db0ecd705))

## [4.47.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.47.0...deneb-v4.47.1) (2026-06-30)


### 🐛 Bug Fixes

* **mail:** 네이티브 메일 아카이브 견고화 (관측성·older_than·graceful degrade·IMAP 재사용) ([#2984](https://github.com/choiceoh/Deneb/issues/2984)) ([bd63804](https://github.com/choiceoh/Deneb/commit/bd63804e18ff2c85082194e61aeea5b23c6d4bc6))

## [4.47.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.46.0...deneb-v4.47.0) (2026-06-30)


### ✨ Features

* **andromeda:** rework the notebook pane around its actual workflow ([#2979](https://github.com/choiceoh/Deneb/issues/2979)) ([db21253](https://github.com/choiceoh/Deneb/commit/db212534528c13ade24e3783b43a0535475c6184))
* **andromeda:** 작업피드 행을 제목만 표기 (미리보기 줄 제거) ([#2981](https://github.com/choiceoh/Deneb/issues/2981)) ([14f2f56](https://github.com/choiceoh/Deneb/commit/14f2f567dad0c77cabba69ffc0e3885469eedd11))
* **calendar:** 일정 날짜·시간 입력 개선 (기본값·종료자동·칸분리·길이버튼) ([#2980](https://github.com/choiceoh/Deneb/issues/2980)) ([ca7db1d](https://github.com/choiceoh/Deneb/commit/ca7db1d099d412614cef2d595f8c82be8c9aad4f))
* **mail:** 메일 AI 분석 카드 접기/펼치기 토글 ([#2983](https://github.com/choiceoh/Deneb/issues/2983)) ([dbff334](https://github.com/choiceoh/Deneb/commit/dbff3344e6bc38e17705664fc3027fabc12a06aa))


### 🐛 Bug Fixes

* **andromeda:** 위키 내용 폭 — 접힘 기준을 뷰포트→워크영역(컨테이너)으로 ([#2977](https://github.com/choiceoh/Deneb/issues/2977)) ([2995c57](https://github.com/choiceoh/Deneb/commit/2995c579ed67f40c652e7c5c08b111042571cddd))
* **mail:** 네이티브 아카이브가 날짜범위(after/before) 쿼리를 직접 처리 + Gmail 폴백 제거 ([#2982](https://github.com/choiceoh/Deneb/issues/2982)) ([0091967](https://github.com/choiceoh/Deneb/commit/0091967be88b4d72119a672512866572c5f0a005))

## [4.46.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.45.0...deneb-v4.46.0) (2026-06-30)


### ✨ Features

* **andromeda:** order the project list by most-recently-updated ([#2976](https://github.com/choiceoh/Deneb/issues/2976)) ([5e258df](https://github.com/choiceoh/Deneb/commit/5e258df7d28c466de9496d1cd0ed4a5fc053c0a9))
* **andromeda:** 메일 받은편지함 날짜 페이저 (+ 공유 DayPager, 스킬 패널 리뷰 수정) ([#2975](https://github.com/choiceoh/Deneb/issues/2975)) ([dc42427](https://github.com/choiceoh/Deneb/commit/dc42427a6444d471149330ead7f53821e5f5b2bb))
* **market:** add 시장 card (FX/index/commodities) to the 오늘 dashboard ([#2971](https://github.com/choiceoh/Deneb/issues/2971)) ([0895f29](https://github.com/choiceoh/Deneb/commit/0895f29edb3dc3fb94ff8c09c8e0d5c77b868dd2))


### 🔧 Internal

* **workfeed:** 데스크톱 "작업피드" 표시 라벨을 "피드"로 변경 ([#2974](https://github.com/choiceoh/Deneb/issues/2974)) ([017b9d6](https://github.com/choiceoh/Deneb/commit/017b9d629a64c60daba52fcd28128166a845c5c8))

## [4.45.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.44.2...deneb-v4.45.0) (2026-06-30)


### ✨ Features

* **andromeda:** 데스크탑에 스킬 패널 추가 (목록·상세·Propus 로그) ([#2966](https://github.com/choiceoh/Deneb/issues/2966)) ([cf52556](https://github.com/choiceoh/Deneb/commit/cf52556eaf52b7c529ca8ebb3f3ab2f0da6af069))
* **feed:** 피드 안읽음 배지를 당일 피드만 집계 (네이티브) ([#2968](https://github.com/choiceoh/Deneb/issues/2968)) ([dd17f7a](https://github.com/choiceoh/Deneb/commit/dd17f7a539b52440df9c8bec1fcebb02c4ab42a7))
* **workfeed:** 작업피드 AI 분석 본문 기본 전체 펼침 + 접기 토글 ([#2970](https://github.com/choiceoh/Deneb/issues/2970)) ([7c25f50](https://github.com/choiceoh/Deneb/commit/7c25f50682ef4d2c4cd120953aee6d8a9597273b))

## [4.44.2](https://github.com/choiceoh/Deneb/compare/deneb-v4.44.1...deneb-v4.44.2) (2026-06-30)


### 🐛 Bug Fixes

* **andromeda:** deneb-ui 카드가 text 필드·문자열 list 항목도 렌더 — 모닝레터 깨짐 수정 ([#2961](https://github.com/choiceoh/Deneb/issues/2961)) ([64cbdb1](https://github.com/choiceoh/Deneb/commit/64cbdb180f2e5d621d66c709fb5db2b0f7d2db6d))
* **mail:** 상세 응답에 isUnread 추가 — 리스트 밖에서 연 메일도 자동 읽음 처리 ([#2963](https://github.com/choiceoh/Deneb/issues/2963)) ([abb4611](https://github.com/choiceoh/Deneb/commit/abb461123babe1d62ec013d77dd73b78299901de))
* **native:** deneb-ui text/badge 노드가 text 필드도 읽도록 — 모닝레터 깨짐 (폰) ([#2964](https://github.com/choiceoh/Deneb/issues/2964)) ([d30bef9](https://github.com/choiceoh/Deneb/commit/d30bef9488186844eb2634424e3f383eb4bf86d5))
* **project:** 프로젝트 화면 단일 열 레이아웃 + 가독성 개선 ([#2965](https://github.com/choiceoh/Deneb/issues/2965)) ([344d4a4](https://github.com/choiceoh/Deneb/commit/344d4a47d93e25a31558b6f952c210b8018bb89e))

## [4.44.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.44.0...deneb-v4.44.1) (2026-06-30)


### 🐛 Bug Fixes

* **calendar:** 인접월 일정이 월 목록에 새는 문제 (7월 일정이 6월 목록에 표시) ([#2958](https://github.com/choiceoh/Deneb/issues/2958)) ([b3dd4eb](https://github.com/choiceoh/Deneb/commit/b3dd4eb17486b36b8362f18734f1d0ab80e2d0ec))

## [4.44.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.43.0...deneb-v4.44.0) (2026-06-30)


### ✨ Features

* **code:** 코딩 세션 닫기(보관) — 워크트리 보존하며 목록에서 치움 ([#2955](https://github.com/choiceoh/Deneb/issues/2955)) ([670213f](https://github.com/choiceoh/Deneb/commit/670213f24145028959308d1da78a9a06c48b6338))


### 🔧 Internal

* **code:** 새 작업을 우측 폼에서 왼쪽 버튼 모달로 이동 ([#2953](https://github.com/choiceoh/Deneb/issues/2953)) ([25dacec](https://github.com/choiceoh/Deneb/commit/25dacece01fee8610d2cb0a96a1cacbd9feaf31e))

## [4.43.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.42.0...deneb-v4.43.0) (2026-06-29)


### ✨ Features

* **code:** 작업 상세에 PR 결과 링크 (miniapp.code.pr) ([#2947](https://github.com/choiceoh/Deneb/issues/2947)) ([f11db78](https://github.com/choiceoh/Deneb/commit/f11db78cdce62002aa8d9ea2c89102da804e320f))
* **code:** 코드 모드 우측에 작업 상세 패널 (진행 기록·검증) ([#2945](https://github.com/choiceoh/Deneb/issues/2945)) ([cc37428](https://github.com/choiceoh/Deneb/commit/cc3742818b0e6e28d9e65db3d1d69b9ef1374671))

## [4.42.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.41.0...deneb-v4.42.0) (2026-06-29)


### ✨ Features

* **code:** 코드 모드 세션 상태 점 (진행중 초록/멈춤 검정/문제 빨강) ([#2942](https://github.com/choiceoh/Deneb/issues/2942)) ([bd79441](https://github.com/choiceoh/Deneb/commit/bd794412ea106e61dcdba9f86a2b660de152745e))

## [4.41.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.40.0...deneb-v4.41.0) (2026-06-29)


### ✨ Features

* **code:** 코딩 모드 새 작업에서 작업 ID·제목 자동 생성 (입력 칸 제거) ([#2937](https://github.com/choiceoh/Deneb/issues/2937)) ([cc486e1](https://github.com/choiceoh/Deneb/commit/cc486e18ddab6c37f4b501ff61680d308c11113f))


### 🐛 Bug Fixes

* **andromeda:** sync work feed on proactive nudges + durable catch-up (작업 피드 동기화) ([#2940](https://github.com/choiceoh/Deneb/issues/2940)) ([4150e3c](https://github.com/choiceoh/Deneb/commit/4150e3c81fa44871a6384de075c9ba6ba947be8d))


### 🔧 Internal

* **code:** 코드 모드 우측 패널에서 중복 세션 목록 제거 ([#2939](https://github.com/choiceoh/Deneb/issues/2939)) ([a4fd8ba](https://github.com/choiceoh/Deneb/commit/a4fd8ba3c5dee6ce5d1dbb84659be3253c71cfe1))

## [4.40.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.39.0...deneb-v4.40.0) (2026-06-28)


### ✨ Features

* **andromeda:** coding mode center-chat layout (중앙 코딩 채팅) ([#2935](https://github.com/choiceoh/Deneb/issues/2935)) ([0df3259](https://github.com/choiceoh/Deneb/commit/0df325993785e978234873d67ad69a3f55cd5ad7))
* **andromeda:** wire the chat to coding sessions (코딩 모드 연결) ([#2934](https://github.com/choiceoh/Deneb/issues/2934)) ([5a14437](https://github.com/choiceoh/Deneb/commit/5a14437afcccf02cca7cb539c0fd367586478c41))
* **code:** coding mode autonomous lifecycle — 완전 자동 (no manual buttons) ([#2936](https://github.com/choiceoh/Deneb/issues/2936)) ([df2fe5c](https://github.com/choiceoh/Deneb/commit/df2fe5cf790ef33442e5b7969663d984d7aed6dc))
* **code:** 코딩 모드 턴 오케스트레이션 — 워크트리 스코프 도구 + 턴-종료 검증/체크포인트 ([#2932](https://github.com/choiceoh/Deneb/issues/2932)) ([9111768](https://github.com/choiceoh/Deneb/commit/91117689889d7d062434ebcdd0ec1ed0c755edc9))

## [4.39.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.38.0...deneb-v4.39.0) (2026-06-28)


### ✨ Features

* **chat:** pace streamed tokens so network-coalesced bursts render smoothly ([#2924](https://github.com/choiceoh/Deneb/issues/2924)) ([92b291f](https://github.com/choiceoh/Deneb/commit/92b291f4a775ed7814cff7260b100104b44b9d80))
* **client:** dismiss soft keyboard after sending a chat message ([#2918](https://github.com/choiceoh/Deneb/issues/2918)) ([819f324](https://github.com/choiceoh/Deneb/commit/819f32412f5244ecdede22f5efeb321e6bb00cf4))
* **client:** dismiss soft keyboard when the conversation is scrolled ([#2919](https://github.com/choiceoh/Deneb/issues/2919)) ([66235c4](https://github.com/choiceoh/Deneb/commit/66235c468d8bc317333d380f1e797c96d622e07f))
* **code:** git-worktree 바이브코딩 모드 — 게이트웨이 엔진 + Andromeda UI ([#2930](https://github.com/choiceoh/Deneb/issues/2930)) ([e0d25d0](https://github.com/choiceoh/Deneb/commit/e0d25d074882315f7aa73d8d4ed4a6b5b55021f6))
* **genesis:** refine low-yield lever filter — resolved denominator + Laplace smoothing ([#2915](https://github.com/choiceoh/Deneb/issues/2915)) ([e34f044](https://github.com/choiceoh/Deneb/commit/e34f04428c490aac34fb7676df1d060f8ee7fb3a))
* **letter:** 모닝/이브닝레터를 deneb-ui 카드로 (P1 표시 카드) ([#2925](https://github.com/choiceoh/Deneb/issues/2925)) ([031dccc](https://github.com/choiceoh/Deneb/commit/031dccc29d7dc7a1bfdf128161aa19992a48c81a))
* **native:** battery optimization — gateway FCM fallback + client M2/M3 + M1/M4 scaffolding ([#2922](https://github.com/choiceoh/Deneb/issues/2922)) ([a0b8f82](https://github.com/choiceoh/Deneb/commit/a0b8f8255b89d250cc315c0406af12a47e9a3291))
* **observatory:** AI 가독 자기개선 텔레메트리 — digest + 툴 + watchdog ([#2928](https://github.com/choiceoh/Deneb/issues/2928)) ([cdf0ea4](https://github.com/choiceoh/Deneb/commit/cdf0ea423e673075d8e4189b754d18c2db5d5dd2))
* **phone:** 폰 액션 SSH/Termux → 인앱 Intent (RFC + P1 전체) ([#2929](https://github.com/choiceoh/Deneb/issues/2929)) ([fae701b](https://github.com/choiceoh/Deneb/commit/fae701b6566c2c55b42160f78eb7674c5b4c3111))


### 🐛 Bug Fixes

* **chat:** remove streaming caret below replies + composer caret polish ([#2920](https://github.com/choiceoh/Deneb/issues/2920)) ([8b810a1](https://github.com/choiceoh/Deneb/commit/8b810a1d3ee6fa9654b0fc889b23802fe8561d1d))
* **letter:** 구리시세를 Yahoo COMEX(HG=F)로 — MetalpriceAPI 무료 XCU 불가 ([#2926](https://github.com/choiceoh/Deneb/issues/2926)) ([3b1bf6a](https://github.com/choiceoh/Deneb/commit/3b1bf6a8f4ad81527727b4df9bfa7f4bebc02752))
* **tools:** files 툴 max 파라미터를 flexInt로 (문자열 "10" 허용) ([#2927](https://github.com/choiceoh/Deneb/issues/2927)) ([fc39037](https://github.com/choiceoh/Deneb/commit/fc390379cd9fd69fed6b5c2abd244e8cbb44e432))

## [4.38.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.37.0...deneb-v4.38.0) (2026-06-27)


### ✨ Features

* **project:** link mail-sourced calendar events to their project ([#2907](https://github.com/choiceoh/Deneb/issues/2907)) ([7d11a5d](https://github.com/choiceoh/Deneb/commit/7d11a5df641834e67986406cb3d6ad96601e7ffe))
* **project:** server-side project↔item matching via miniapp.project.linked ([#2905](https://github.com/choiceoh/Deneb/issues/2905)) ([278c768](https://github.com/choiceoh/Deneb/commit/278c768d55e60672597148b8a5ec3703b96f016e))
* **recall:** scope evidence to a query's temporal frame (RaMem context-collapse slice) ([#2910](https://github.com/choiceoh/Deneb/issues/2910)) ([277360a](https://github.com/choiceoh/Deneb/commit/277360aa0f2cc4cf954194e0b211dff4fa9f369e))
* typed deal records + sandbox compute + deadline alerts (User-as-Code slice) ([#2912](https://github.com/choiceoh/Deneb/issues/2912)) ([5ac6e65](https://github.com/choiceoh/Deneb/commit/5ac6e65129ec4988decf14fddc93f33bc7f981c0))
* **wiki:** lower page-split default toward Infini Memory optimum + sweep instrument ([#2914](https://github.com/choiceoh/Deneb/issues/2914)) ([b22cf72](https://github.com/choiceoh/Deneb/commit/b22cf72fc8803501b5f108ddca1fb97fc0c1586c))

## [4.37.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.36.0...deneb-v4.37.0) (2026-06-26)


### ✨ Features

* **autonomous:** expose proactive intervention threshold as operator config ([#2903](https://github.com/choiceoh/Deneb/issues/2903)) ([810a063](https://github.com/choiceoh/Deneb/commit/810a063d1e12290779275fbf02d8dd5c9f9ac751))
* **filestore:** leverage the file store — archive sent/fetched files + share links ([#2893](https://github.com/choiceoh/Deneb/issues/2893)) ([af2e968](https://github.com/choiceoh/Deneb/commit/af2e968e0359c1613d71b8931aed29c0f54ed040))
* **notebook:** stamp resolved project refs at mail ingestion (각인) ([#2895](https://github.com/choiceoh/Deneb/issues/2895)) ([e4f36a2](https://github.com/choiceoh/Deneb/commit/e4f36a2022b945c8bea9917a32339c49ef2f2801))
* **productivity:** add evening_letter tool + skill (end-of-day counterpart) ([#2902](https://github.com/choiceoh/Deneb/issues/2902)) ([31932ab](https://github.com/choiceoh/Deneb/commit/31932ab90edb4d6b4d3e9d5a33f1273c507244ed))
* **project:** resolve owned pages server-side via the wiki graph (③ 서버측 매칭) ([#2899](https://github.com/choiceoh/Deneb/issues/2899)) ([b626c08](https://github.com/choiceoh/Deneb/commit/b626c0864b832bd2256471787dfaebf9a3a2e2ea))
* **prompt:** surface this session's active goal in the dynamic block ([#2898](https://github.com/choiceoh/Deneb/issues/2898)) ([2c00779](https://github.com/choiceoh/Deneb/commit/2c00779d08258ee90269077477475f8ed57cd134))
* **sweep:** bug fixes + close read-side loops + observability + hygiene ([#2897](https://github.com/choiceoh/Deneb/issues/2897)) ([2bd823e](https://github.com/choiceoh/Deneb/commit/2bd823ecd7458e719ee49c8f44be9c44d7bc2fa6))
* **wiki:** stamp deal pages with their project (deal→project graph edge) ([#2900](https://github.com/choiceoh/Deneb/issues/2900)) ([35be823](https://github.com/choiceoh/Deneb/commit/35be82358a33f7d81ec4268c32d81151a172e00f))


### 🔧 Internal

* **monitoring:** retire vestigial channel health supervisor + dead restart config ([#2901](https://github.com/choiceoh/Deneb/issues/2901)) ([5e5128f](https://github.com/choiceoh/Deneb/commit/5e5128f3258340241e2705a86c915df108cd55a7))

## [4.36.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.35.0...deneb-v4.36.0) (2026-06-26)


### ✨ Features

* **agent:** declarative + singleton-safe BeforeAPICall hook chain (typed-processor algebra borrow) ([#2890](https://github.com/choiceoh/Deneb/issues/2890)) ([d6ef97a](https://github.com/choiceoh/Deneb/commit/d6ef97a05e1a788d8987ade7ac3c90d223bc1819))
* **project:** ship frozen code in digest so 프로젝트 코너 matches items by code ([#2894](https://github.com/choiceoh/Deneb/issues/2894)) ([8e147e4](https://github.com/choiceoh/Deneb/commit/8e147e400037f34240cd837d3554a324a38862d1))
* **wiki:** dreamer 선호 학습 강화 — 행동 추상화 + fact-level 갱신 ([#2892](https://github.com/choiceoh/Deneb/issues/2892)) ([9480493](https://github.com/choiceoh/Deneb/commit/9480493563ae577bd16284d236e631c268305593))

## [4.35.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.34.0...deneb-v4.35.0) (2026-06-26)


### ✨ Features

* **andromeda:** 작업피드 액션 제거·정정 하단 이동으로 본문 와이드화 ([#2875](https://github.com/choiceoh/Deneb/issues/2875)) ([5b8aba4](https://github.com/choiceoh/Deneb/commit/5b8aba411315b872fe9293b624ad53951782ac8c))
* **chat:** validate-then-repair malformed tool-call arguments ([#2877](https://github.com/choiceoh/Deneb/issues/2877)) ([e0641b2](https://github.com/choiceoh/Deneb/commit/e0641b275a83a0ab5046d8c07b8045ba9e567e24))
* **exec:** 파괴적 명령 차단 (rm -rf /·디스크 포맷·fork bomb) + 노트북 set_mode RPC ([#2876](https://github.com/choiceoh/Deneb/issues/2876)) ([8d38778](https://github.com/choiceoh/Deneb/commit/8d38778e198032d37e526b066e20b8f56428f6e8))
* **harness:** genesis evolve-loop fixes + runtime hardening from HarnessX deep-dive ([#2885](https://github.com/choiceoh/Deneb/issues/2885)) ([ce3181c](https://github.com/choiceoh/Deneb/commit/ce3181c6173a938aca4ed50eaf1bda3909cbcb40))
* **skills:** normalize auto-generated skill descriptions ([#2880](https://github.com/choiceoh/Deneb/issues/2880)) ([fbe3aa5](https://github.com/choiceoh/Deneb/commit/fbe3aa54c41da2f9983e1086f18409b64d1a8b28))
* **wiki:** OKF resource URI field on page frontmatter ([#2873](https://github.com/choiceoh/Deneb/issues/2873)) ([2cc71c3](https://github.com/choiceoh/Deneb/commit/2cc71c35de4a2a93d6a0f0453e1beb65b56bd717))
* **wiki:** procedural memory — apply 사용자 preferences into USER.md ([#2878](https://github.com/choiceoh/Deneb/issues/2878)) ([26f8d68](https://github.com/choiceoh/Deneb/commit/26f8d68c3d172d1cf61ce83b35a7767233b877a7))
* 코드-as-harness 리서치 도입 3건 (exec 체크포인트 · research_panel 종합 · autoresearch 규율) ([#2884](https://github.com/choiceoh/Deneb/issues/2884)) ([1671cb4](https://github.com/choiceoh/Deneb/commit/1671cb4ee8180060febaae258936a547fa2433dd))


### 🐛 Bug Fixes

* **skills:** drive skill-review fork with tool-capable coding role ([#2886](https://github.com/choiceoh/Deneb/issues/2886)) ([117019c](https://github.com/choiceoh/Deneb/commit/117019cbeeb921370d13181dc0b86fa8eff0dba6))
* **skills:** pre-load skill_lifecycle active in the self-review preset ([#2888](https://github.com/choiceoh/Deneb/issues/2888)) ([ae10098](https://github.com/choiceoh/Deneb/commit/ae1009892d6016bcf0dd09331cfc751cd041b3f9))

## [4.34.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.33.0...deneb-v4.34.0) (2026-06-24)


### ✨ Features

* **native:** 작업피드 읽음 기기 간 동기화 ([#2870](https://github.com/choiceoh/Deneb/issues/2870)) ([ca27a67](https://github.com/choiceoh/Deneb/commit/ca27a678a05e125f65d7419e76fc8b3c14d60fcb))
* **push:** proactive 알림 딥링크 타깃(kind+ref) + 데스크탑 클릭스루 ([#2869](https://github.com/choiceoh/Deneb/issues/2869)) ([4406432](https://github.com/choiceoh/Deneb/commit/44064326ca3f578503d743da475daf698613dec3))

## [4.33.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.32.0...deneb-v4.33.0) (2026-06-24)


### ✨ Features

* **andromeda:** 능동 알림 패널 개선 + 로드 실패 가시화 ([#2867](https://github.com/choiceoh/Deneb/issues/2867)) ([eb945da](https://github.com/choiceoh/Deneb/commit/eb945dac33fbeec2367c882c595afb4b5fc5a6f3))
* **workfeed:** 작업피드 읽음 상태 — 게이트웨이 read RPC + andromeda 표시 ([#2865](https://github.com/choiceoh/Deneb/issues/2865)) ([4ac67cd](https://github.com/choiceoh/Deneb/commit/4ac67cd0634edef341d26f3efe0fe0835663dbb1))

## [4.32.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.31.0...deneb-v4.32.0) (2026-06-24)


### ✨ Features

* **andromeda:** 작업피드를 날짜별 페이저로 (전날/다음날 이동) ([#2861](https://github.com/choiceoh/Deneb/issues/2861)) ([71f3d27](https://github.com/choiceoh/Deneb/commit/71f3d272f8710988c214735dbe8c2c4beaf3afa2))


### 🐛 Bug Fixes

* **andromeda:** clear mail unread state on open ([#2864](https://github.com/choiceoh/Deneb/issues/2864)) ([006e45c](https://github.com/choiceoh/Deneb/commit/006e45cafe3ec712a6b80c91fd953d31674f78e7))
* **andromeda:** require explicit project links on project home ([c2b3eee](https://github.com/choiceoh/Deneb/commit/c2b3eee0a6425b2b377bae09a89190f6dccfee71))

## [4.31.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.30.0...deneb-v4.31.0) (2026-06-24)


### ✨ Features

* **andromeda:** add project home pane ([#2858](https://github.com/choiceoh/Deneb/issues/2858)) ([5f8e64e](https://github.com/choiceoh/Deneb/commit/5f8e64ee69cefe8f0ab84f71753f1f0305b07f05))
* **andromeda:** complete notebook source management ([c0f58b3](https://github.com/choiceoh/Deneb/commit/c0f58b31debb11ef2a5523655e79a28909843e39))
* **andromeda:** improve fleet usability ([#2859](https://github.com/choiceoh/Deneb/issues/2859)) ([387991e](https://github.com/choiceoh/Deneb/commit/387991e18458d23729b6681b6f0c0b43e25aaad0))
* **andromeda:** 메일 상세 — 발신자 카드 기본 접힘 + AI 분석 본문 위로 ([#2857](https://github.com/choiceoh/Deneb/issues/2857)) ([f045681](https://github.com/choiceoh/Deneb/commit/f04568132552cdd224f6441db9429edb65098d9c))
* **andromeda:** 작업피드를 날짜별 그룹으로 표시 ([#2856](https://github.com/choiceoh/Deneb/issues/2856)) ([b6453a9](https://github.com/choiceoh/Deneb/commit/b6453a9a6578143088f54663157ace7ed4397b48))
* **mail:** 대용량첨부(대용량 파일) 다운로드 링크 아카이브 ([#2860](https://github.com/choiceoh/Deneb/issues/2860)) ([a5dbda4](https://github.com/choiceoh/Deneb/commit/a5dbda489b438e139d2cdb6803121f7f3735b93f))


### 🐛 Bug Fixes

* **chat:** remove agent gmail tool ([#2853](https://github.com/choiceoh/Deneb/issues/2853)) ([049b4bd](https://github.com/choiceoh/Deneb/commit/049b4bd2d5b5db5355a828af81ffc113b7697462))

## [4.30.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.29.0...deneb-v4.30.0) (2026-06-24)


### ✨ Features

* add APEX sampling and prod log guardrails ([#2622](https://github.com/choiceoh/Deneb/issues/2622)) ([428186b](https://github.com/choiceoh/Deneb/commit/428186b6de9eab3eb48a04df37bf425b29905fd4))
* adopt OpenClaw skill patterns ([79bb2a8](https://github.com/choiceoh/Deneb/commit/79bb2a86c1ebdd59f11b91f4add84f0da53d1dc0))
* **agent:** append recovery hints to raw tool errors ([#2783](https://github.com/choiceoh/Deneb/issues/2783)) ([67c5966](https://github.com/choiceoh/Deneb/commit/67c5966845d27664c85c80b163410f63ca3b19e2))
* **agent:** harden wiki memory and skill evolution ([1579440](https://github.com/choiceoh/Deneb/commit/1579440f1f85487f937ca027a8809ab7fefcf948))
* **agent:** harden wiki memory and skill evolution ([baa490d](https://github.com/choiceoh/Deneb/commit/baa490d5ccf00fe824f2279b94d7f1c8c88b7771))
* **agents:** add coding model role ([4a5d51d](https://github.com/choiceoh/Deneb/commit/4a5d51d7ef97f5c3723d8e43386ad55f43a588b8))
* **andromeda:** improve chat attachment rendering ([#2851](https://github.com/choiceoh/Deneb/issues/2851)) ([210c98c](https://github.com/choiceoh/Deneb/commit/210c98c940e20e2e65d7576b281d8a721105febb))
* **backup:** include the knowledge dir in the daily offsite backup ([#2804](https://github.com/choiceoh/Deneb/issues/2804)) ([995282c](https://github.com/choiceoh/Deneb/commit/995282cd11a80e2b6c3df023b080c3ab43aa3e4d))
* **bar:** 메일·달력 탭에 현재 위치 표시 — Action→Screen 전환(하이라이트) ([#2778](https://github.com/choiceoh/Deneb/issues/2778)) ([b024e82](https://github.com/choiceoh/Deneb/commit/b024e821ab0722834bd52adf1e89274585cb4117))
* **browser:** basics (forward·stop·omnibox·copy·⋮) + translate as a bottom-bar button ([#2741](https://github.com/choiceoh/Deneb/issues/2741)) ([f412d00](https://github.com/choiceoh/Deneb/commit/f412d005cb20f76c14cac8246c6523f85579aa7a))
* **browser:** surface translate status as a toast (no-text / translating / failed) ([#2743](https://github.com/choiceoh/Deneb/issues/2743)) ([ddd570a](https://github.com/choiceoh/Deneb/commit/ddd570af9cc5d6de36c2191a09e536fb90c2692b))
* **browser:** swap the translation model from the ⋮ menu (+ fix translation role drift) ([#2751](https://github.com/choiceoh/Deneb/issues/2751)) ([1f5e1c3](https://github.com/choiceoh/Deneb/commit/1f5e1c3b73bcc2d1fc22c9c92764efb96df5c44d))
* cache direct rpc panes ([fc91966](https://github.com/choiceoh/Deneb/commit/fc9196688c78858cd5fbd6ee30d44084168cbed7))
* **calendar:** add a schedule-audit action (time protection) ([f70fe97](https://github.com/choiceoh/Deneb/commit/f70fe972c78b748c944595ef930f5795d41bdfbc))
* **calendar:** add a work-graph timeline action ([c647cd7](https://github.com/choiceoh/Deneb/commit/c647cd76dfeb69a87c812474bcc2fe7f6c125cdc))
* **calendar:** color month-grid events by owner/invite/deadline ([14b47e1](https://github.com/choiceoh/Deneb/commit/14b47e1fc03c51f3003d7bfad6ee0e7bb7dc3644))
* **calendar:** color month-grid events by owner/invite/deadline ([b791427](https://github.com/choiceoh/Deneb/commit/b7914270ad735fa02c11738325cab750ac737029))
* **calendar:** extract meeting times so proposals land at the right hour ([#2588](https://github.com/choiceoh/Deneb/issues/2588)) ([26c7cfd](https://github.com/choiceoh/Deneb/commit/26c7cfdb48f8052b42989b7a675a61d5c407f8c9))
* **calendar:** link local events to their origin (mail, kind) ([#2604](https://github.com/choiceoh/Deneb/issues/2604)) ([958b275](https://github.com/choiceoh/Deneb/commit/958b275c126776c8e08163c1460b9065a0a40368))
* **calendar:** link mail documents to events for meeting prep ([#2609](https://github.com/choiceoh/Deneb/issues/2609)) ([8333e33](https://github.com/choiceoh/Deneb/commit/8333e33e8685dc947c2cfd911faefebe6afaa5d8))
* **calendar:** link-aware brief + meeting prep actions (agentic calendar) ([#2605](https://github.com/choiceoh/Deneb/issues/2605)) ([6e8d55a](https://github.com/choiceoh/Deneb/commit/6e8d55adbacf4cb5bbceeaf2cfc91d44ebf2fb48))
* **calendar:** mail-analysis calendar proposals via the bell (accept-to-add) ([#2582](https://github.com/choiceoh/Deneb/issues/2582)) ([80346e7](https://github.com/choiceoh/Deneb/commit/80346e7625879cb350c5b2602153f0b0a2fa3ccc))
* **calendar:** mark to-do due dates on the month grid ([#2687](https://github.com/choiceoh/Deneb/issues/2687)) ([4a4d201](https://github.com/choiceoh/Deneb/commit/4a4d20149a8d28acaaf12f3508a5e67db2c6080b))
* **calendar:** post-meeting capture action — minutes written back onto the event ([#2650](https://github.com/choiceoh/Deneb/issues/2650)) ([1c7acb0](https://github.com/choiceoh/Deneb/commit/1c7acb09493c43984ad81c9aa16926e355947d84))
* **calendar:** register the audit action in the tool schema ([0cd3498](https://github.com/choiceoh/Deneb/commit/0cd3498869ed54ab20f0de34ae52f8de5be081b7))
* **calendar:** register the timeline action + query param in the schema ([33efb5e](https://github.com/choiceoh/Deneb/commit/33efb5ead5fcdbbf03c853b31811ad7d3c3a763a))
* **calendar:** work-graph timeline action (+ re-land dropped schedule audit) ([8631cdb](https://github.com/choiceoh/Deneb/commit/8631cdb7aeffc732c49691a10e12a4da552a509e))
* **chat:** add chart tool — render data charts as PNG for the agent to send ([#2522](https://github.com/choiceoh/Deneb/issues/2522)) ([dcd81eb](https://github.com/choiceoh/Deneb/commit/dcd81ebfc2edd05c2811bd5790204872ebc2a1da))
* **chat:** add diagram tool — Mermaid flowchart/gantt as PNG; share renderer with chart ([#2529](https://github.com/choiceoh/Deneb/issues/2529)) ([10cc3ad](https://github.com/choiceoh/Deneb/commit/10cc3ad1b14e2556782e7351624f65c38812ded3))
* **chat:** add timeline to diagram tool — 연혁/이력/로드맵 ([#2530](https://github.com/choiceoh/Deneb/issues/2530)) ([d6537db](https://github.com/choiceoh/Deneb/commit/d6537db88d20ba7414e004b4f3fc3207ed522343))
* **chat:** add todo agent tool backed by localtodo store ([#2593](https://github.com/choiceoh/Deneb/issues/2593)) ([eb1e11b](https://github.com/choiceoh/Deneb/commit/eb1e11bdad5c34dd28052bfa1a5a1e41a500ed0c))
* **chat:** copy button on replies — copies the raw markdown source ([#2527](https://github.com/choiceoh/Deneb/issues/2527)) ([cb28196](https://github.com/choiceoh/Deneb/commit/cb281962697eb9b22c9145087d541e066a0d06c9))
* **chat:** GPU 셰이더 기반 생성 배경 오로라 (안드로이드, 슬라이스 폴백) ([#2500](https://github.com/choiceoh/Deneb/issues/2500)) ([7331db2](https://github.com/choiceoh/Deneb/commit/7331db24724bbfe3cb44efc97071792185504eae))
* **chat:** raise agent turn budgets — default 25→50, 챗봇 10→20 ([#2830](https://github.com/choiceoh/Deneb/issues/2830)) ([af73343](https://github.com/choiceoh/Deneb/commit/af733437da9c395213e0ee8d626cf7fc0d89eb43))
* **chat:** read Sino-Korean Hanja in model output as Hangul ([#2557](https://github.com/choiceoh/Deneb/issues/2557)) ([5ce1650](https://github.com/choiceoh/Deneb/commit/5ce1650704897fec23eca048940518ab66ac5c3c))
* **chat:** swipe left/right across the chat to switch 챗봇 ↔ 업무 ([#2543](https://github.com/choiceoh/Deneb/issues/2543)) ([db9166b](https://github.com/choiceoh/Deneb/commit/db9166b7a2bb3563eafadc1d9f11d47b3429c11d))
* **chat:** 환영 화면 보라 오브 → 모노크롬 스파클 (팔레트 일치) ([#2780](https://github.com/choiceoh/Deneb/issues/2780)) ([9a08ef0](https://github.com/choiceoh/Deneb/commit/9a08ef00c870daaf127bfe05fa78758fe6f8dca8))
* **dashboard:** part-based work overview with auto-classification ([#2704](https://github.com/choiceoh/Deneb/issues/2704)) ([8e500c8](https://github.com/choiceoh/Deneb/commit/8e500c8a70ede67e6ffe7d208f2e25cff7b32ff5))
* **deploy:** remote build-and-ship mode for the srv4 gateway topology ([#2802](https://github.com/choiceoh/Deneb/issues/2802)) ([bdcf4a4](https://github.com/choiceoh/Deneb/commit/bdcf4a47ad4f196e387b2f5742343eb74a4ef7b0))
* **eval:** production-fidelity extraction endpoint for model benchmarking ([#2816](https://github.com/choiceoh/Deneb/issues/2816)) ([18546ed](https://github.com/choiceoh/Deneb/commit/18546eddbe0772998e2ef86dedab2d9d26d88fbe))
* expose prompt tuner run RPC ([#2666](https://github.com/choiceoh/Deneb/issues/2666)) ([4ddc9b1](https://github.com/choiceoh/Deneb/commit/4ddc9b17f8007de55d51d2b2eb8abc3e6adb5780))
* **extract:** 문서 추출 대상에 Markdown(.md) 추가 ([#2843](https://github.com/choiceoh/Deneb/issues/2843)) ([251f21b](https://github.com/choiceoh/Deneb/commit/251f21b807ac510ba57c97ec67b1997ba866778e))
* **files:** delete + folder management (mkdir/move/rename) across all layers ([#2705](https://github.com/choiceoh/Deneb/issues/2705)) ([5025a1d](https://github.com/choiceoh/Deneb/commit/5025a1d00adc0e568a474c7f09dfec192885b727))
* **files:** full-text search over extracted document text ([#2707](https://github.com/choiceoh/Deneb/issues/2707)) ([af56744](https://github.com/choiceoh/Deneb/commit/af567443eef2f885ca89153449adfbac9a47925f))
* **files:** hybrid BM25 + semantic search (RRF) — rescue exact-name below cosine floor ([#2728](https://github.com/choiceoh/Deneb/issues/2728)) ([c70095a](https://github.com/choiceoh/Deneb/commit/c70095aed98084f7c05ab067d863d013f34ced4a))
* **files:** in-app preview for images and text/markdown ([#2703](https://github.com/choiceoh/Deneb/issues/2703)) ([291e5d9](https://github.com/choiceoh/Deneb/commit/291e5d93dca65c5ac79c183d794990da7065dc88))
* **files:** integrate file store into recall — agent auto-references relevant files ([#2733](https://github.com/choiceoh/Deneb/issues/2733)) ([6956dfa](https://github.com/choiceoh/Deneb/commit/6956dfa942a7fe48262ed12c9bbf7cff0e173a1d))
* **files:** name/content/semantic 3-mode search selector in native browser ([#2713](https://github.com/choiceoh/Deneb/issues/2713)) ([e297048](https://github.com/choiceoh/Deneb/commit/e2970481e174bfc5cd07413612c5c387c5ffb1d5))
* **files:** replace Dropbox integration with local file store ([#2694](https://github.com/choiceoh/Deneb/issues/2694)) ([b1a735d](https://github.com/choiceoh/Deneb/commit/b1a735d7ca19afce20d6cece30bd9b258341b1d8))
* **files:** semantic (vector) search backend — BGE-M3 index + background reindex ([#2712](https://github.com/choiceoh/Deneb/issues/2712)) ([02dce66](https://github.com/choiceoh/Deneb/commit/02dce6603ee2a791bdd780e0a5465e2269e116ba))
* **fleet:** agent can manage SparkFleet from chat ([#2517](https://github.com/choiceoh/Deneb/issues/2517)) ([a41435e](https://github.com/choiceoh/Deneb/commit/a41435ed1e91cbfe5697dc6e1831594590c90237))
* **fleet:** diagnose & benchmark SparkFleet from the app ([#2514](https://github.com/choiceoh/Deneb/issues/2514)) ([ddbf20d](https://github.com/choiceoh/Deneb/commit/ddbf20d5518166b74d200c1a935399d058f5de68))
* **genesis:** auto-capture replay cases from successful skill use ([#2691](https://github.com/choiceoh/Deneb/issues/2691)) ([94463ae](https://github.com/choiceoh/Deneb/commit/94463ae83e55950b5b0a9f3a0bb8b7863c105bac))
* **genesis:** execution-grounded behavioral replay gate for skill evolution ([#2689](https://github.com/choiceoh/Deneb/issues/2689)) ([07ccc1c](https://github.com/choiceoh/Deneb/commit/07ccc1cdbff478c42c0d5c7886332abce7f2e1fd))
* **genesis:** track skill opportunity signals ([#2621](https://github.com/choiceoh/Deneb/issues/2621)) ([0227da8](https://github.com/choiceoh/Deneb/commit/0227da851d2f700fef730bd7c972efb87df2c3c3))
* **gmailpoll:** feed raw attachment text to the deal extractor ([#2547](https://github.com/choiceoh/Deneb/issues/2547)) ([79b8805](https://github.com/choiceoh/Deneb/commit/79b8805eaa41cdab14734cb22c06500f0827dfa4))
* **gmailpoll:** give mail analysis real tools (agent turn) + leak sanitizer ([#2598](https://github.com/choiceoh/Deneb/issues/2598)) ([5634fa5](https://github.com/choiceoh/Deneb/commit/5634fa57b5d4c58ed9fc409c8b2e05786c6efd05))
* **gmailpoll:** LLM-gated attachment reading for mail analysis ([#2539](https://github.com/choiceoh/Deneb/issues/2539)) ([84ba47f](https://github.com/choiceoh/Deneb/commit/84ba47f49ab3c10e5eba1cff8c982e6d207bd80d))
* **gmailpoll:** source-traceable 금액 검증 게이트 (FinAcumen ③ 차용) ([#2750](https://github.com/choiceoh/Deneb/issues/2750)) ([0720e5f](https://github.com/choiceoh/Deneb/commit/0720e5f9a0ac8304bf6223fa3e32f88ae481a5ff))
* **gmailpoll:** strict json_schema for local mail extractors (enum-safe) ([#2814](https://github.com/choiceoh/Deneb/issues/2814)) ([692ef7a](https://github.com/choiceoh/Deneb/commit/692ef7aa55c8d1660c58ddba3040f2a6ac5b8eb2))
* **jsonschema:** derive mail extractor schemas from Go types (fail-fast) ([#2815](https://github.com/choiceoh/Deneb/issues/2815)) ([144198a](https://github.com/choiceoh/Deneb/commit/144198abf374db11c533b6896ee5bb783189531c))
* **launcher:** app drawer loading state + enter-to-launch ([#2739](https://github.com/choiceoh/Deneb/issues/2739)) ([0a56675](https://github.com/choiceoh/Deneb/commit/0a566759f7f89b142481f008b678838664a139c1))
* **launcher:** haptic detent ticks on the ㄱㄴㄷ scrub index ([#2758](https://github.com/choiceoh/Deneb/issues/2758)) ([bcc071d](https://github.com/choiceoh/Deneb/commit/bcc071da55005151ee1cb3416c23e18fb8a537d2))
* **launcher:** Home button resets to the feed briefing ([#2738](https://github.com/choiceoh/Deneb/issues/2738)) ([7409195](https://github.com/choiceoh/Deneb/commit/7409195890d82463935224d1108389e5b6ab6b54))
* **launcher:** home-launcher mode toggle (opt-in HOME activity-alias) ([#2734](https://github.com/choiceoh/Deneb/issues/2734)) ([500db3c](https://github.com/choiceoh/Deneb/commit/500db3c20d79ef221c3938244bc929bff33e9a1c))
* **launcher:** Niagara-style text app drawer with ㄱㄴㄷ/A-Z scrub ([#2749](https://github.com/choiceoh/Deneb/issues/2749)) ([eaec0df](https://github.com/choiceoh/Deneb/commit/eaec0df4285d7bd66f44bc41011500b3d224834e))
* **launcher:** Phase 0 background — sensing triage/dedup + calendar offline cache ([#2722](https://github.com/choiceoh/Deneb/issues/2722)) ([2ab3434](https://github.com/choiceoh/Deneb/commit/2ab3434ab0e274792cec525bb6b80cb6cf55b943))
* **launcher:** Phase 0 foundation — app drawer, offline feed cache, notification sensing ([#2719](https://github.com/choiceoh/Deneb/issues/2719)) ([a8cc25d](https://github.com/choiceoh/Deneb/commit/a8cc25d1992ef278c6698cb8c54ac123c488a6b0))
* **launcher:** scrub tuning — drop bubble, stronger fisheye, distinct haptic, wider zone ([#2766](https://github.com/choiceoh/Deneb/issues/2766)) ([aa96f9f](https://github.com/choiceoh/Deneb/commit/aa96f9fc21ef3d315d339abae63e81b71fc5451a))
* **launcher:** swap bottom-bar shortcuts to 메일/달력/설정 when launcher mode is off ([#2768](https://github.com/choiceoh/Deneb/issues/2768)) ([7c3db7c](https://github.com/choiceoh/Deneb/commit/7c3db7cc9f29da14ab08d208e2e55573081b3fd5))
* **launcher:** swipe-up from 자체앱 opens the Niagara app drawer ([#2752](https://github.com/choiceoh/Deneb/issues/2752)) ([82c0697](https://github.com/choiceoh/Deneb/commit/82c06979c1669b236d0d4496c86d6c72145402bc))
* **launcher:** UsageStats sensing — forward a throttled work-rhythm digest ([#2732](https://github.com/choiceoh/Deneb/issues/2732)) ([dd334c2](https://github.com/choiceoh/Deneb/commit/dd334c2dc088552fa2af96eeb3e33578c2ffab96))
* **launcher:** warm calendar+mail caches on background sync ([#2726](https://github.com/choiceoh/Deneb/issues/2726)) ([50cbade](https://github.com/choiceoh/Deneb/commit/50cbade4137cddda97690e74467bb05121c34a2b))
* **launcher:** 앱 드로어 최상단 스와이프다운 → 자체앱 복귀 ([#2760](https://github.com/choiceoh/Deneb/issues/2760)) ([00cde6d](https://github.com/choiceoh/Deneb/commit/00cde6d5f281f46471367815dce80ea6792475ba))
* **launcher:** 자체앱 핀고정 폰 앱 섹션 + 한글 음차 인덱스 ([#2765](https://github.com/choiceoh/Deneb/issues/2765)) ([2d2baa8](https://github.com/choiceoh/Deneb/commit/2d2baa8042dcd06ebbbb245e2318c7f4ba11dcc8))
* **launcher:** 전체 연락처 screen + shared SectionedScrubList (R1 reuse) ([#2757](https://github.com/choiceoh/Deneb/issues/2757)) ([2247c0a](https://github.com/choiceoh/Deneb/commit/2247c0a24ba8be5a8cf525387945bf2fa5037dd1))
* **launcher:** 초성 scrub overhaul — magnified bubble + fisheye + wider touch + 15sp (re-land) ([#2762](https://github.com/choiceoh/Deneb/issues/2762)) ([d3bddb6](https://github.com/choiceoh/Deneb/commit/d3bddb6bd9458ae3af0024189e88005e72a7fc3f))
* **launcher:** 하단바를 모든 자체앱 섹션에서 유지 (메일·달력·검색·설정·…) ([#2771](https://github.com/choiceoh/Deneb/issues/2771)) ([6df58f7](https://github.com/choiceoh/Deneb/commit/6df58f757e570ed5f4c1926f2f44df50ecfa1666))
* **lmtp:** read LMTP inline attachments into mail analysis ([#2544](https://github.com/choiceoh/Deneb/issues/2544)) ([1ed9f33](https://github.com/choiceoh/Deneb/commit/1ed9f3376284d20f6f7a572225830458de2062f0))
* **mailarchive:** active on-demand attachment reading via mail_archive ([#2683](https://github.com/choiceoh/Deneb/issues/2683)) ([af83f0d](https://github.com/choiceoh/Deneb/commit/af83f0d750f96cc11dfdb656f306404d2d71c6ab))
* **mail:** clean human mail display bodies ([#2611](https://github.com/choiceoh/Deneb/issues/2611)) ([4a00cd6](https://github.com/choiceoh/Deneb/commit/4a00cd6a65e77e3cd15afd36ad3e2f3ec08bb3e5))
* **mail:** drop stats header below the received-mail list title ([#2663](https://github.com/choiceoh/Deneb/issues/2663)) ([10255f3](https://github.com/choiceoh/Deneb/commit/10255f365fcff4d7ac7408bb697a5b430faa047a))
* **mail:** expose native archive affordances ([f196452](https://github.com/choiceoh/Deneb/commit/f19645269ef7ab14bae2a62cd138e78b0dc3d115))
* **mail:** LMTP systemd socket activation — survive gateway hot-restarts ([#2568](https://github.com/choiceoh/Deneb/issues/2568)) ([9644895](https://github.com/choiceoh/Deneb/commit/964489519f2622e42ffc04c95c7100da2ea21e4f))
* **mail:** LMTP 서버로 푸시 메일 인입 (Docker 메일서버→Deneb, IMAP 폴링 대체) ([#2538](https://github.com/choiceoh/Deneb/issues/2538)) ([7f62242](https://github.com/choiceoh/Deneb/commit/7f6224204952dabcf64d2c46f0c71baee36da9f3))
* **mail:** load native mail from archive repository ([c5ba4cc](https://github.com/choiceoh/Deneb/commit/c5ba4cc0f6179c221f355e13d88f14b3567085bf))
* **mail:** reconstruct LMTP thread context from the on-box archive (drop Gmail dep) ([#2571](https://github.com/choiceoh/Deneb/issues/2571)) ([b8cd42f](https://github.com/choiceoh/Deneb/commit/b8cd42f67c9cd6bead29213ea0b796e717a09c35))
* **mail:** surface feed-missing mail in list row, not fed mail ([0a33413](https://github.com/choiceoh/Deneb/commit/0a334138296ddba081af854f9d294c6472db6cd9))
* **mail:** surface feed-missing mail in list row, not fed mail ([7ac4c2b](https://github.com/choiceoh/Deneb/commit/7ac4c2be198ab520608608633876d4ce689e404a))
* **mail:** surface local archive row metadata ([#2597](https://github.com/choiceoh/Deneb/issues/2597)) ([ba38477](https://github.com/choiceoh/Deneb/commit/ba384779edc7f0d131ef007dd635f0e8dd47b739))
* **markdown:** graceful placeholder for broken/loading images ([#2532](https://github.com/choiceoh/Deneb/issues/2532)) ([a10aa29](https://github.com/choiceoh/Deneb/commit/a10aa292badc91fe6307d346c9dbcd65b0e262a9))
* **markdown:** normalize common block-level HTML to markdown ([#2519](https://github.com/choiceoh/Deneb/issues/2519)) ([10f861c](https://github.com/choiceoh/Deneb/commit/10f861cdb77c1bc2fb845fb4583755fdaab2a772))
* **markdown:** render &lt;details&gt;/&lt;summary&gt; as a collapsible + strip &lt;span&gt; ([#2512](https://github.com/choiceoh/Deneb/issues/2512)) ([ebd30a5](https://github.com/choiceoh/Deneb/commit/ebd30a594a557d46e9f12aa6acd69aa6fe34ccef))
* **media:** native YouTube transcript+metadata extraction with yt-dlp fallback ([#2844](https://github.com/choiceoh/Deneb/issues/2844)) ([6b3b33f](https://github.com/choiceoh/Deneb/commit/6b3b33f5e5a1b01721b49d3a356d0399fd0f9a6e))
* **miniapp:** capture attached documents instead of dropping them ([#2634](https://github.com/choiceoh/Deneb/issues/2634)) ([ce6f95f](https://github.com/choiceoh/Deneb/commit/ce6f95f124c52ed869bbb103dcf1534347e48caa))
* **miniapp:** local file browser — miniapp.files.* RPC + native DenebFilesScreen ([#2700](https://github.com/choiceoh/Deneb/issues/2700)) ([8dadec3](https://github.com/choiceoh/Deneb/commit/8dadec3b127555a2beeb638624c830623ad75bf8))
* **miniapp:** native Dropbox file browser (browse/search/share/upload/analyze) ([#2560](https://github.com/choiceoh/Deneb/issues/2560)) ([3e1d139](https://github.com/choiceoh/Deneb/commit/3e1d1392cdf4aff7285c4f0db9e5f9520018603f))
* **miniapp:** revive topic-background editor in the prompt corner ([#2695](https://github.com/choiceoh/Deneb/issues/2695)) ([2b1c0b7](https://github.com/choiceoh/Deneb/commit/2b1c0b75754623640c5c77fe01dca11b8fa623bc))
* **miniapp:** vision model config — make it settable + show only when main lacks vision ([#2540](https://github.com/choiceoh/Deneb/issues/2540)) ([78dd610](https://github.com/choiceoh/Deneb/commit/78dd610486fc7060de76b63337af026c9c9bdc05))
* **miniapp:** 프로젝트별 최신 진행상황 모아보기 화면 ([#2834](https://github.com/choiceoh/Deneb/issues/2834)) ([ae9a422](https://github.com/choiceoh/Deneb/commit/ae9a422c9d9d6b80924beda2a679454b2d2a0569))
* **models:** add configurable vision model role for image turns ([#2510](https://github.com/choiceoh/Deneb/issues/2510)) ([220ee9d](https://github.com/choiceoh/Deneb/commit/220ee9d52f3d84e4aa75f1844a08ea7490abe69b))
* **models:** route the watch-tool vision call to RoleVision too ([#2515](https://github.com/choiceoh/Deneb/issues/2515)) ([5a5f145](https://github.com/choiceoh/Deneb/commit/5a5f1456f4d1844931297563a586d9f944bbff69))
* **more:** 더보기를 라벨 그룹 3개로 정돈 + 한줄설명 제거 + 일기 제거 ([#2777](https://github.com/choiceoh/Deneb/issues/2777)) ([10ce335](https://github.com/choiceoh/Deneb/commit/10ce3352d2f873be86a5381bbd7d7b62d9d81906))
* **native:** add a luminous glow to the 답변-중 sparkle ([#2497](https://github.com/choiceoh/Deneb/issues/2497)) ([2b21002](https://github.com/choiceoh/Deneb/commit/2b21002ba0873c43db4660de8b0bdaf518032283))
* **native:** add MOSS self-improvement coding filters ([a91e8c2](https://github.com/choiceoh/Deneb/commit/a91e8c248535e696d32d17650df45a9c7cb31bbf))
* **native:** add MOSS self-improvement coding filters ([3eb82bc](https://github.com/choiceoh/Deneb/commit/3eb82bc3934219b7016d49a13a70c597fe899aac))
* **native:** auto-focus the input only on a brand-new (empty) chat ([#2490](https://github.com/choiceoh/Deneb/issues/2490)) ([e7b3e6d](https://github.com/choiceoh/Deneb/commit/e7b3e6d52e14996b2abdbfbbd9c7cdb75b10fae0))
* **native:** browser bottom chrome (Safari-style) + re-land 자체앱 launcher ([#2737](https://github.com/choiceoh/Deneb/issues/2737)) ([6677be3](https://github.com/choiceoh/Deneb/commit/6677be359245beb2f8c273de9b05f6d38915df5f))
* **native:** coalesce notification bursts in the native listener ([#2776](https://github.com/choiceoh/Deneb/issues/2776)) ([ecd5327](https://github.com/choiceoh/Deneb/commit/ecd53272dfd2f9036d954b4174765cbcba603413))
* **native:** declutter work-feed rows — full-width summary, actions on expand ([#2554](https://github.com/choiceoh/Deneb/issues/2554)) ([3d7245f](https://github.com/choiceoh/Deneb/commit/3d7245f80eaedbef4b1285fee47d2c38159d2e1a))
* **native:** give the 답변-중 sparkle a live hue-flowing gradient ([#2492](https://github.com/choiceoh/Deneb/issues/2492)) ([a3d003b](https://github.com/choiceoh/Deneb/commit/a3d003bbd58619ef743c1a0bd874414d6c4ccfb3))
* **native:** high-priority UX fixes — back affordance, chat trust, feed/form states ([#2502](https://github.com/choiceoh/Deneb/issues/2502)) ([0ec0fe2](https://github.com/choiceoh/Deneb/commit/0ec0fe294c495d9a6257a90b72a8118b5ab9c656))
* **native:** immersive chat input bar — messages scroll under the floating input ([#2618](https://github.com/choiceoh/Deneb/issues/2618)) ([c3a270b](https://github.com/choiceoh/Deneb/commit/c3a270bd4c5c212c930c901bf08d09dbd8a91b8c))
* **native:** immersive chat top bar over a full-height conversation, edge-to-edge status bar ([#2617](https://github.com/choiceoh/Deneb/issues/2617)) ([770dd93](https://github.com/choiceoh/Deneb/commit/770dd93fa48d1051f406d9d21b5332a256c2f774))
* **native:** inline free-text input in kb-interview choice chips ([#2826](https://github.com/choiceoh/Deneb/issues/2826)) ([cc47469](https://github.com/choiceoh/Deneb/commit/cc47469be1a46111a2b6d005aeb56d669d580886))
* **native:** launcher icon → Deneb sparkle (blue gradient) ([#2788](https://github.com/choiceoh/Deneb/issues/2788)) ([022c25c](https://github.com/choiceoh/Deneb/commit/022c25cb123fbe04d5b42632359a2f42d0d671a3))
* **native:** launcher icon → faceted 8-point Deneb star on white ([#2791](https://github.com/choiceoh/Deneb/issues/2791)) ([0dd629a](https://github.com/choiceoh/Deneb/commit/0dd629a278e6185a12e71f065511a51f3aac7fdc))
* **native:** launcher icon → glowing Deneb sparkle (white halo) ([#2789](https://github.com/choiceoh/Deneb/issues/2789)) ([f2b0a0e](https://github.com/choiceoh/Deneb/commit/f2b0a0e69727978379dc32515b1add1bf1d3a1af))
* **native:** meeting prep + 회의록 buttons on the calendar event detail ([ab9fbf0](https://github.com/choiceoh/Deneb/commit/ab9fbf092ba203d4f03e78c40c73b86354c44781))
* **native:** meeting prep + 회의록 buttons on the event detail ([963fc5e](https://github.com/choiceoh/Deneb/commit/963fc5e18a1b42036919d58b119739e2267ec67f))
* **native:** move mail body switch + ask to a bottom action bar ([#2639](https://github.com/choiceoh/Deneb/issues/2639)) ([47f9ce5](https://github.com/choiceoh/Deneb/commit/47f9ce5068a7fc1b062a2eada703be698239e26c))
* **native:** on-demand phone location via FusedLocationProvider (gateway cache for phone_read) ([#2782](https://github.com/choiceoh/Deneb/issues/2782)) ([36bf705](https://github.com/choiceoh/Deneb/commit/36bf7054524d42f2dd86b84965e7d00d9ab3c8d3))
* **native:** richer HuggingFace model search in the fleet screen ([#2511](https://github.com/choiceoh/Deneb/issues/2511)) ([2130946](https://github.com/choiceoh/Deneb/commit/2130946f106d29dba301da5618db02933dbcab87))
* **native:** show self-improvement coding candidates ([082f9bd](https://github.com/choiceoh/Deneb/commit/082f9bd908a2f51bbaa7db364474ed55b11ea27f))
* **native:** show self-improvement coding candidates ([668ace8](https://github.com/choiceoh/Deneb/commit/668ace8ffac518cdff0afba14fd7a0f718408a19))
* **native:** tappable choice chips for kb-interview grill questions ([#2823](https://github.com/choiceoh/Deneb/issues/2823)) ([f37ded3](https://github.com/choiceoh/Deneb/commit/f37ded39266bc6f9e1a9aaf7176ea0f6d4975042))
* **native:** tighten work-feed gutters for denser info display ([#2552](https://github.com/choiceoh/Deneb/issues/2552)) ([3084d16](https://github.com/choiceoh/Deneb/commit/3084d16dbeec0167eed123a6bbdd909cf765c27e))
* **native:** 검색 화면 재디자인 — 밑줄 검색창 + 설명 제거 ([#2795](https://github.com/choiceoh/Deneb/issues/2795)) ([aad7866](https://github.com/choiceoh/Deneb/commit/aad7866d5fd5bbc56f5dc262aaa5b11b62148e18))
* **native:** 더보기 항목 숨김 설정 (미완성 기능 숨기기) ([#2794](https://github.com/choiceoh/Deneb/issues/2794)) ([f660d38](https://github.com/choiceoh/Deneb/commit/f660d38c70c45f89636e8732f2a534e32336ee01))
* **native:** 런처식 슈퍼앱 — 하단 5탭(자체앱 센터) + 자체앱 그리드 ([#2761](https://github.com/choiceoh/Deneb/issues/2761)) ([d39886b](https://github.com/choiceoh/Deneb/commit/d39886b570d5f95ddcf5fb60133f46af8b932c94))
* **native:** 인터넷 탭을 삼성 인터넷 외부앱으로 + 자체 번역 브라우저는 그리드 타일 ([#2764](https://github.com/choiceoh/Deneb/issues/2764)) ([aadcf29](https://github.com/choiceoh/Deneb/commit/aadcf290ee04e0de1d880f4d177a41809b33dbf9))
* **native:** 집/직장 geofence arrival alerts (GeofencingClient → ingestEvent) ([#2784](https://github.com/choiceoh/Deneb/issues/2784)) ([6f7df6e](https://github.com/choiceoh/Deneb/commit/6f7df6ebc3b26d175d81a6de1371f4be1ceaa159))
* **native:** 채팅 탑바 축소 + 업무 Aurora + 메시지 상단 여백 축소 ([#2612](https://github.com/choiceoh/Deneb/issues/2612)) ([30fe349](https://github.com/choiceoh/Deneb/commit/30fe349de4c0504052d60c195c09fe9ca0cc03af))
* **native:** 피드를 날짜별로 보기 — 상단 날짜(요일) + 좌우 화살표 ([#2537](https://github.com/choiceoh/Deneb/issues/2537)) ([25b3e1b](https://github.com/choiceoh/Deneb/commit/25b3e1befd070f0a8e4dfcee10eee9ef904d5082))
* **native:** 하단 바에 채팅 복원(가운데) + 자체앱 그리드를 더보기 리스트로 환원 ([#2773](https://github.com/choiceoh/Deneb/issues/2773)) ([541f1b6](https://github.com/choiceoh/Deneb/commit/541f1b606653332b6d91c4fb9596d0a395c723b2))
* **native:** 할일을 달력에 통합 — 별도 할일 화면 제거 ([#2812](https://github.com/choiceoh/Deneb/issues/2812)) ([97faab7](https://github.com/choiceoh/Deneb/commit/97faab7c4f4263e876fab93b19c53b5e822f7ecd))
* **notebook:** + delete/remove_source 관리 RPC + 페이로드 헬퍼 정리 ([#2846](https://github.com/choiceoh/Deneb/issues/2846)) ([64518f1](https://github.com/choiceoh/Deneb/commit/64518f105b4c30a2ebca82a6cd6e2f50b3c512b0))
* **notebook:** anchor notebooks to deals (EnsureForDeal + pin_to_deal/for_deal) ([#2688](https://github.com/choiceoh/Deneb/issues/2688)) ([aed851e](https://github.com/choiceoh/Deneb/commit/aed851e3718e96feb493d9dbfec08c3b0abfa676))
* **notebook:** auto-pin deal email evidence to its deal notebook ([#2690](https://github.com/choiceoh/Deneb/issues/2690)) ([ba60976](https://github.com/choiceoh/Deneb/commit/ba609764a169c1234e5bd5a4bfcf91ebe092569b))
* **notebook:** link a project wiki page to its deal notebook ([#2811](https://github.com/choiceoh/Deneb/issues/2811)) ([505946a](https://github.com/choiceoh/Deneb/commit/505946ad27a88b84700172c45a8a2fece6aaf099))
* **notebook:** miniapp create/add_source 쓰기 RPC — 데스크톱 노트북 핀 ([#2845](https://github.com/choiceoh/Deneb/issues/2845)) ([d0382e3](https://github.com/choiceoh/Deneb/commit/d0382e3bf6eb0e0b4b30a001ad397e66397b4161))
* **notebook:** miniapp.notebook.* RPC + native notebook viewer ([#2692](https://github.com/choiceoh/Deneb/issues/2692)) ([33a7877](https://github.com/choiceoh/Deneb/commit/33a787716cd00ce668315dba5f0898ade6c9e391))
* **notebook:** NotebookLM식 자료 노트북 + 근거 기반 인용 브리핑 (Phase 1) ([#2685](https://github.com/choiceoh/Deneb/issues/2685)) ([f7914ec](https://github.com/choiceoh/Deneb/commit/f7914eca8fc59210c7b6a687909be7e9877c6edd))
* **notebook:** 문서 그라운딩 세션 모드 + 다중 소스 인입 ([#2840](https://github.com/choiceoh/Deneb/issues/2840)) ([cb7d2d8](https://github.com/choiceoh/Deneb/commit/cb7d2d88e3463d8e9f363de991e50b01fb20420a))
* **org:** edit a 파트 node's classification keywords + 거래처 in the editor ([#2808](https://github.com/choiceoh/Deneb/issues/2808)) ([94fbd17](https://github.com/choiceoh/Deneb/commit/94fbd17fc7732362f06ee817557f1c9bcfe59450))
* **org:** group org chart as master data with auto-derived part classification ([#2715](https://github.com/choiceoh/Deneb/issues/2715)) ([8455a28](https://github.com/choiceoh/Deneb/commit/8455a28d7c8722ca4d76ec62e617458f4449282b))
* **org:** open the org chart read-only, edit behind a 편집 toggle ([#2809](https://github.com/choiceoh/Deneb/issues/2809)) ([fefa150](https://github.com/choiceoh/Deneb/commit/fefa150244741689910cf24a0bc0c76e8f9307fe))
* **org:** org chart back to an indented list (from the box diagram) ([#2806](https://github.com/choiceoh/Deneb/issues/2806)) ([1660e8c](https://github.com/choiceoh/Deneb/commit/1660e8c21de9f8f6566ecfb84315a0f0561e062d))
* **org:** visual org chart + people search + contact links (v2) ([#2731](https://github.com/choiceoh/Deneb/issues/2731)) ([e8c027f](https://github.com/choiceoh/Deneb/commit/e8c027f3d6c56c4b43043f6273957b992b76567b))
* **phone:** cache native location pushes so phone_read skips the SSH round-trip ([#2781](https://github.com/choiceoh/Deneb/issues/2781)) ([3314727](https://github.com/choiceoh/Deneb/commit/33147279631c6e740a8433ad4310528e62f31966))
* **phone:** read call log + contacts, quieter ambient state-change feeds ([#2619](https://github.com/choiceoh/Deneb/issues/2619)) ([acfabfd](https://github.com/choiceoh/Deneb/commit/acfabfdc0ca6ba6d787ef639453b0dcb73130753))
* **prompt:** advertise capability CLIs, drop uninstalled tool recs ([#2559](https://github.com/choiceoh/Deneb/issues/2559)) ([ff4888b](https://github.com/choiceoh/Deneb/commit/ff4888b00ba931d09baecab6c9f58369e2e43712))
* **prompt:** ask user for missing work context instead of guessing ([887f181](https://github.com/choiceoh/Deneb/commit/887f1818de9512fd3610f55e36f7f035df40bfdb))
* **prompt:** ask user for missing work context instead of guessing ([51ec60b](https://github.com/choiceoh/Deneb/commit/51ec60b1c94175782dcb0e1d3049bf023cbb2cf2))
* **prompt:** make the system persona editable in the Settings prompt corner ([#2614](https://github.com/choiceoh/Deneb/issues/2614)) ([aa474ec](https://github.com/choiceoh/Deneb/commit/aa474ec0124d744582d43925a5df57fb86854da5))
* **prompts:** add native prompt corner ([#2580](https://github.com/choiceoh/Deneb/issues/2580)) ([4ae0275](https://github.com/choiceoh/Deneb/commit/4ae02755b359bbb1d376ce1f255ca3e62f7d7636))
* **recall:** provenance penalty — raw diary wins a numeric conflict with a drifted wiki ([#2754](https://github.com/choiceoh/Deneb/issues/2754)) ([2c72591](https://github.com/choiceoh/Deneb/commit/2c725914be8cb7f024629aaf90232200ebeced4d))
* **recall:** wiki BM25 rarity floor — semantic floor의 대칭 leak 게이트 ([#2744](https://github.com/choiceoh/Deneb/issues/2744)) ([20fc7a0](https://github.com/choiceoh/Deneb/commit/20fc7a0d198f3818a0f4db25347295890c08935c))
* **recall:** wiki semantic-only admission floor (FinAcumen ② 차용) ([#2740](https://github.com/choiceoh/Deneb/issues/2740)) ([1627428](https://github.com/choiceoh/Deneb/commit/1627428aa6ae9d23500abaa9e94d1e85cd578e49))
* **regression-watch:** observe-only telemetry regression detector ([44623a7](https://github.com/choiceoh/Deneb/commit/44623a743a45d8e91a4c0f82cb8afd9181f1f1cd))
* **regression-watch:** observe-only telemetry regression detector ([d23ae07](https://github.com/choiceoh/Deneb/commit/d23ae07f6e7665eac8b646c5cbd968f8f8a32f41))
* **research:** parallel multi-model research panel (research_panel + deep-research) ([#2831](https://github.com/choiceoh/Deneb/issues/2831)) ([ac3e732](https://github.com/choiceoh/Deneb/commit/ac3e732f03645d54c6d582476e0a288baa1a869b))
* **skills:** add kb-interview elicitation skill + proprietary-knowledge guard ([#2822](https://github.com/choiceoh/Deneb/issues/2822)) ([df7c6d3](https://github.com/choiceoh/Deneb/commit/df7c6d38ef0985dc6c132f1834120fab576714eb))
* **skills:** add retrieval-plan skill for multi-hop retrieval ([#2824](https://github.com/choiceoh/Deneb/issues/2824)) ([c44eafa](https://github.com/choiceoh/Deneb/commit/c44eafac2633c523539f8c6340b23d827b18d04d))
* **skills:** improve genesis evolution pipeline ([#2573](https://github.com/choiceoh/Deneb/issues/2573)) ([cc15eee](https://github.com/choiceoh/Deneb/commit/cc15eee32d64342a980ad8c6f134b9fcbe322d60))
* **skills:** paper-grounded analysis skills + proactive/pilot follow-ups ([#2820](https://github.com/choiceoh/Deneb/issues/2820)) ([a99b048](https://github.com/choiceoh/Deneb/commit/a99b048c223fb9c0bc9c2055927fe2c2df894565))
* **skills:** surface repo bundled skills/ in prompt and Settings tab ([#2679](https://github.com/choiceoh/Deneb/issues/2679)) ([d01466f](https://github.com/choiceoh/Deneb/commit/d01466f91e432a54e8941e9dcea18d3ed4c0f5a7))
* **skills:** teach email-analysis to read attachments via mail_archive ([#2684](https://github.com/choiceoh/Deneb/issues/2684)) ([ba3a38d](https://github.com/choiceoh/Deneb/commit/ba3a38d844acb90b55f6c7b3959e963aae860703))
* **tooling:** parse make ci failures into clean offender lists ([#2535](https://github.com/choiceoh/Deneb/issues/2535)) ([11d1d03](https://github.com/choiceoh/Deneb/commit/11d1d033ca66cb7e4f061796f762a7c631931a1e))
* **translate:** char-based batching + active-model visibility in browser ([#2763](https://github.com/choiceoh/Deneb/issues/2763)) ([627d575](https://github.com/choiceoh/Deneb/commit/627d575ff17d23004a64c36bb0e47561a6558cf1))
* **translate:** in-app browser with in-place en/ru→ko translation (native) ([#2708](https://github.com/choiceoh/Deneb/issues/2708)) ([cfcf46b](https://github.com/choiceoh/Deneb/commit/cfcf46b10fe7289730ed5c124a1bf071611fde1e))
* **translate:** route in-app web link taps to the in-app browser ([#2709](https://github.com/choiceoh/Deneb/issues/2709)) ([5e94c5d](https://github.com/choiceoh/Deneb/commit/5e94c5dcfa71af4eb74544f89fd343f5127ae910))
* **translate:** translation model role + miniapp.web.translate RPC for in-app browser ([#2706](https://github.com/choiceoh/Deneb/issues/2706)) ([ecc988a](https://github.com/choiceoh/Deneb/commit/ecc988a50b0b559529e60351b337d01372ba25f0))
* **web:** Jina fallback + bot-block escalation + yt-dlp health probe ([#2736](https://github.com/choiceoh/Deneb/issues/2736)) ([7311eb9](https://github.com/choiceoh/Deneb/commit/7311eb950e6943a60aec193f29f0e427b1ce7640))
* **wiki:** autonomous 6h deep-research refresh of project wiki pages ([#2838](https://github.com/choiceoh/Deneb/issues/2838)) ([4caf6bc](https://github.com/choiceoh/Deneb/commit/4caf6bc34af95b59bcea3b41ab97a6a673a8cc53))
* **wiki:** frozen project codes with move-stable ref resolution ([#2801](https://github.com/choiceoh/Deneb/issues/2801)) ([1827d79](https://github.com/choiceoh/Deneb/commit/1827d79a024ac74a59e5debadacda8f2f45e7003))
* **wiki:** 프로젝트 대표페이지 도입 — 근황을 페이지 섹션으로 정본화 ([#2841](https://github.com/choiceoh/Deneb/issues/2841)) ([4c010b5](https://github.com/choiceoh/Deneb/commit/4c010b5aa0f6259a695f9958179750b9a13eb4a2))
* **workfeed:** answer the agent's feed questions inline (chips + reply) ([#2833](https://github.com/choiceoh/Deneb/issues/2833)) ([3929534](https://github.com/choiceoh/Deneb/commit/3929534b8a8ab7d859e027f4c63a386f8778d58e))
* **workfeed:** ask which team owns a new deal, record the answer ([#2813](https://github.com/choiceoh/Deneb/issues/2813)) ([c55e9c7](https://github.com/choiceoh/Deneb/commit/c55e9c70526863a31d6efc39481fe404ab8cc19b))
* **workfeed:** cap mail-report card titles at 16 Korean chars ([#2551](https://github.com/choiceoh/Deneb/issues/2551)) ([a839dee](https://github.com/choiceoh/Deneb/commit/a839dee4de5a632864eec8d8209dbdb179f949f6))
* **workfeed:** LLM-generate the card summary alongside the title ([#2561](https://github.com/choiceoh/Deneb/issues/2561)) ([5d3de46](https://github.com/choiceoh/Deneb/commit/5d3de46f9fea607b83b80950e50509489139033f))
* **workfeed:** long-press a feed card to correct/teach the agent ([#2682](https://github.com/choiceoh/Deneb/issues/2682)) ([e6d171d](https://github.com/choiceoh/Deneb/commit/e6d171ddffac64960f36b215578a866e01e2d7e4))
* **workfeed:** stamp work model at the foot of proactive feed cards ([73a6593](https://github.com/choiceoh/Deneb/commit/73a6593b4a085e9eabafb75400b6078dab8268ce))
* **workfeed:** stamp work model at the foot of proactive feed cards ([a293aff](https://github.com/choiceoh/Deneb/commit/a293aff9f45a3c1b6b9cab8628e3777f6ece6f76))
* **workfeed:** tighten the heuristic mail-subject card title ([#2555](https://github.com/choiceoh/Deneb/issues/2555)) ([3133a3a](https://github.com/choiceoh/Deneb/commit/3133a3aabf8a8d637707c01aff5f2d6b151c190d))
* **workfeed:** trim feed long-press to AI actions, add 다시 작성 + 해당 피드 질문 ([#2686](https://github.com/choiceoh/Deneb/issues/2686)) ([22d03ef](https://github.com/choiceoh/Deneb/commit/22d03ef10ef7eded18fe66d8dad94d399592beb5))
* **wormhole:** load + hot-watch a secrets.env for live key rotation ([#2818](https://github.com/choiceoh/Deneb/issues/2818)) ([e3ce893](https://github.com/choiceoh/Deneb/commit/e3ce893825e9c5fd544937bd520867cf1b6b8ed0))
* **wormhole:** native key rotation — keyHealth badges + rotate dialog in the Wormhole tab ([#2821](https://github.com/choiceoh/Deneb/issues/2821)) ([a27f885](https://github.com/choiceoh/Deneb/commit/a27f885419e5dfbfbfb6c3a5a252420d7bb53652))
* **wormhole:** per-model cloud reasoning profile (glm — off/high, never max) ([#2805](https://github.com/choiceoh/Deneb/issues/2805)) ([ebd571a](https://github.com/choiceoh/Deneb/commit/ebd571a382400491f2520031aa506aeb6b5797be))
* **wormhole:** probe cloud-key health, surface dead keys in /status ([#2817](https://github.com/choiceoh/Deneb/issues/2817)) ([11cf9ee](https://github.com/choiceoh/Deneb/commit/11cf9eee3f173eeabcbf3a82004879eccefe36e0))
* **wormhole:** surface keyHealth + set_key RPC for in-app key rotation (B1) ([#2819](https://github.com/choiceoh/Deneb/issues/2819)) ([21923a1](https://github.com/choiceoh/Deneb/commit/21923a10b08587b5cf5a18ae1e243f8b8a25faa4))


### 🐛 Bug Fixes

* **agents:** address coding model review feedback ([#2633](https://github.com/choiceoh/Deneb/issues/2633)) ([5efb653](https://github.com/choiceoh/Deneb/commit/5efb653326a13831673782bb38492826d9fd8e6a))
* **browser:** lift bottom chrome above the soft keyboard (ime inset) ([#2747](https://github.com/choiceoh/Deneb/issues/2747)) ([2314f9c](https://github.com/choiceoh/Deneb/commit/2314f9cc3842114b2fd385b731c35b9f46590bac))
* **browser:** route downloads to system handler + add explicit browser entry ([#2727](https://github.com/choiceoh/Deneb/issues/2727)) ([62b04a0](https://github.com/choiceoh/Deneb/commit/62b04a077750041b58120bc7bc8a3158a01c30e7))
* **calendar:** accurate brief directive + skip all-day for next-meeting prep ([#2607](https://github.com/choiceoh/Deneb/issues/2607)) ([2a31339](https://github.com/choiceoh/Deneb/commit/2a3133975f0e9283153c06205094122d5d2214f8))
* **calendar:** address Codex review on timeline + audit ([a9f7cf9](https://github.com/choiceoh/Deneb/commit/a9f7cf9722a4b5f4a64069629c912ae36758ebb8))
* **calendar:** address Codex review round 2 on audit + timeline ([2973a29](https://github.com/choiceoh/Deneb/commit/2973a29a5fa877918780106e326928fb90f5027c))
* **calendar:** cap proposal-bell panel height + prune old decided proposals ([#2585](https://github.com/choiceoh/Deneb/issues/2585)) ([0ffe7d2](https://github.com/choiceoh/Deneb/commit/0ffe7d270f6c49c3721690ef0f4fa5f5ce14caca))
* **calendar:** dedup proposal accept + surface accept/reject errors ([#2592](https://github.com/choiceoh/Deneb/issues/2592)) ([4a1b56a](https://github.com/choiceoh/Deneb/commit/4a1b56a91ed338de05199b07f3045fc9ff1111b1))
* **calendar:** match the add control to the bell (size, style, alignment) ([#2596](https://github.com/choiceoh/Deneb/issues/2596)) ([5fc7b52](https://github.com/choiceoh/Deneb/commit/5fc7b52ccadf7ec425b8a479a7f292c4f5c0eeb4))
* **calendar:** preserve event origin link across edits ([#2606](https://github.com/choiceoh/Deneb/issues/2606)) ([28e7624](https://github.com/choiceoh/Deneb/commit/28e76245a80e634ffd61acf3b294d460a35a26db))
* **calendar:** re-land orphaned round-2 review fixes (audit range clamp · timeline docs · partial-data verdict) ([83ed9ed](https://github.com/choiceoh/Deneb/commit/83ed9eda30c80a9c7d06d56acdbc3da3795b8cb4))
* **calendar:** replace truncating "추가" button with a "+" icon, drop "오늘" ([#2590](https://github.com/choiceoh/Deneb/issues/2590)) ([7fd5b8d](https://github.com/choiceoh/Deneb/commit/7fd5b8da6f871a5cb053c8464342c3a4c40dfe51))
* **chat:** escape dsv4 thinking runaways + send high-only reasoning_effort ([#2572](https://github.com/choiceoh/Deneb/issues/2572)) ([a7bd658](https://github.com/choiceoh/Deneb/commit/a7bd6583fafa9ce2b2aa5e055fa342b19d14d479))
* **chat:** smooth the 챗봇 ↔ 업무 mode-switch transition ([#2549](https://github.com/choiceoh/Deneb/issues/2549)) ([231342b](https://github.com/choiceoh/Deneb/commit/231342b2999df11f28988fffe57bcba26d738aee))
* **chat:** wire interactive /weekly + /주간보고 to the deterministic weekly report ([#2825](https://github.com/choiceoh/Deneb/issues/2825)) ([9f61805](https://github.com/choiceoh/Deneb/commit/9f61805eef0f97f6edc8c0841aa9b8737892da07))
* **chat:** 생성 배경 오로라를 수평 다색 + 세로 높낮이 출렁임으로 (커튼) ([#2491](https://github.com/choiceoh/Deneb/issues/2491)) ([8f31226](https://github.com/choiceoh/Deneb/commit/8f312261edaf40a49e8573406fb8e0812a4046b6))
* **chat:** 생성 배경을 슬라이스 렌더로 되돌리고 색분산 낮춤 ([#2506](https://github.com/choiceoh/Deneb/issues/2506)) ([989f46a](https://github.com/choiceoh/Deneb/commit/989f46a3c89547c88532ebef5354edb1b70a9e92))
* **chat:** 오로라 생성 배경의 색 계단 제거 — 연속 hue 그라디언트 + 알파 마스크 ([#2518](https://github.com/choiceoh/Deneb/issues/2518)) ([1d4c2dd](https://github.com/choiceoh/Deneb/commit/1d4c2dd9ca370e9be6c9b2763ef9b5bbdd6b76cc))
* **chat:** 입력창 자동 포커스 제거 — 키보드는 직접 탭할 때만 열림 ([#2521](https://github.com/choiceoh/Deneb/issues/2521)) ([6f7ab8f](https://github.com/choiceoh/Deneb/commit/6f7ab8fd1871244fcc920368f6656aa767d7b1bd))
* **chat:** 키보드 follow-scroll 프레임 단위로 부드럽게 ([#2800](https://github.com/choiceoh/Deneb/issues/2800)) ([a53a794](https://github.com/choiceoh/Deneb/commit/a53a794dda7ef3b6bb25aa41f61efff4653d30b4))
* **chat:** 키보드 열릴 때 채팅 메시지도 함께 스크롤 ([#2799](https://github.com/choiceoh/Deneb/issues/2799)) ([1e0be1e](https://github.com/choiceoh/Deneb/commit/1e0be1ea02b9aa901db2fce20c97e0770d181c6e))
* **ci:** srv4 APK sync uses mapfile to avoid ls|head SIGPIPE under pipefail ([#2699](https://github.com/choiceoh/Deneb/issues/2699)) ([27cbb39](https://github.com/choiceoh/Deneb/commit/27cbb39ef6fb078b2d77352018da70c7bd59aa62))
* **dashboard:** 미분류 레인 muted 키 unsorted→unclassified (추가 코드리뷰) ([#2721](https://github.com/choiceoh/Deneb/issues/2721)) ([9baa4cd](https://github.com/choiceoh/Deneb/commit/9baa4cd2d4f26b3192ec62af1616279a93491473))
* **deploy:** make LMTP socket cutover work on the RefuseManualStop gateway ([#2579](https://github.com/choiceoh/Deneb/issues/2579)) ([bfaa047](https://github.com/choiceoh/Deneb/commit/bfaa047068acc205ebcd865fadec9426fda7bb5b))
* **deploy:** sync skills/ to the remote gateway host, not just the binary ([#2827](https://github.com/choiceoh/Deneb/issues/2827)) ([eab4e41](https://github.com/choiceoh/Deneb/commit/eab4e41922884ece58823484b741e4d8bfe0e957))
* **dropbox:** drop DropboxAuthBridge refs from androidApp (publish build) ([#2702](https://github.com/choiceoh/Deneb/issues/2702)) ([323d3c8](https://github.com/choiceoh/Deneb/commit/323d3c89995f11f7480080e70ef232f9764f4850))
* **embedding:** pool BGE-M3 contexts for parallel, crash-free embedding ([#2494](https://github.com/choiceoh/Deneb/issues/2494)) ([5504d9c](https://github.com/choiceoh/Deneb/commit/5504d9c30d77caeebd95db389ec5bb7bf0f9bb93))
* **feed:** load selected day before limiting workfeed ([#2591](https://github.com/choiceoh/Deneb/issues/2591)) ([2e63fc0](https://github.com/choiceoh/Deneb/commit/2e63fc0232c693eceadaaa181e811610b9d55b79))
* **files:** review fixes — native pathDisplay for I/O + semantic score floor ([#2718](https://github.com/choiceoh/Deneb/issues/2718)) ([ab07e56](https://github.com/choiceoh/Deneb/commit/ab07e5679bc43e1345122d3ee46c629df8d9a727))
* **files:** semantic index P2 — lock-free save/search, move/delete sync, content-hash, error code, Korean floor 0.73 ([#2725](https://github.com/choiceoh/Deneb/issues/2725)) ([b936a60](https://github.com/choiceoh/Deneb/commit/b936a604cc64f890d1902bae16eabd5546c4f932))
* **genesis:** bound evolution evidence to recent failures ([#2620](https://github.com/choiceoh/Deneb/issues/2620)) ([a6ea310](https://github.com/choiceoh/Deneb/commit/a6ea310a359686e2fa8441f6e20d9b9eb5930ea0))
* **gmailpoll:** disable dsv4 thinking in mail analysis via chat_template_kwargs ([#2564](https://github.com/choiceoh/Deneb/issues/2564)) ([09050c8](https://github.com/choiceoh/Deneb/commit/09050c8356751c9ffc00786efce5c0e698ce383c))
* **gmailpoll:** force Korean section labels in mail analysis (no 'Primary Analysis') ([#2493](https://github.com/choiceoh/Deneb/issues/2493)) ([8f3a215](https://github.com/choiceoh/Deneb/commit/8f3a2154bc76e6ae12be0b2e1cfb786f98ef8e0f))
* **gmailpoll:** read mail attachments deeply in autonomous analysis ([#2680](https://github.com/choiceoh/Deneb/issues/2680)) ([d97e850](https://github.com/choiceoh/Deneb/commit/d97e8501aace10debc3928e38e789174a7dfd940))
* **gmailpoll:** scope mail action extraction to the executive's own work ([#2644](https://github.com/choiceoh/Deneb/issues/2644)) ([8769fb8](https://github.com/choiceoh/Deneb/commit/8769fb8b96de90dd5a42b74eab8f012b905fa79d))
* **gmailpoll:** stop dropping mail + harden LMTP parsing (audit) ([#2569](https://github.com/choiceoh/Deneb/issues/2569)) ([49a010f](https://github.com/choiceoh/Deneb/commit/49a010f775c002780877f51a893c2d6cbf74f333))
* **gmailpoll:** 메일 분석·리포트 이모지 남용 억제 — 3개 시스템 프롬프트에 emojiRestraint 제약 ([#2542](https://github.com/choiceoh/Deneb/issues/2542)) ([797aa3b](https://github.com/choiceoh/Deneb/commit/797aa3b7ce85decbcda5663dc020a897cca08e2a))
* **gmail:** 메일 분석에서 소비자 없는 '위키 갱신 제안' 블록 제거 ([#2848](https://github.com/choiceoh/Deneb/issues/2848)) ([0cf466e](https://github.com/choiceoh/Deneb/commit/0cf466e9915b30dda65a2b3c36eec3fb86fb9a1f))
* keep feed date navigation on empty days ([9781b1d](https://github.com/choiceoh/Deneb/commit/9781b1dd02692cf1774496e1b6d1e0b084c27bab))
* **launcher:** code-review fixes — reactive launcher gate, section keys, scrub dedup ([#2753](https://github.com/choiceoh/Deneb/issues/2753)) ([83acac8](https://github.com/choiceoh/Deneb/commit/83acac815b088c46bcdfb4fe0172b45b5fb762ae))
* **launcher:** swipe-up-to-apps fires anywhere on 자체앱 (was a too-narrow edge zone) ([#2759](https://github.com/choiceoh/Deneb/issues/2759)) ([9cdfe48](https://github.com/choiceoh/Deneb/commit/9cdfe48077057002b5265a06cafcf501a9f21776))
* **launcher:** 더보기 탭은 항상 허브로 — 드릴다운한 섹션 복원 안 함 ([#2775](https://github.com/choiceoh/Deneb/issues/2775)) ([4bd57fa](https://github.com/choiceoh/Deneb/commit/4bd57fa822b20feabf2fc0fb9f52ffb247bb9ad4))
* **launcher:** 자체앱 스와이프업 타일 위 발동 + 핀고정 안내문 제거 ([#2769](https://github.com/choiceoh/Deneb/issues/2769)) ([af70490](https://github.com/choiceoh/Deneb/commit/af70490be555ff06ef32b7652a3c44967a8930e2))
* **localai:** authenticate health probe so wormhole-backed lightweight isn't falsely unhealthy ([#2697](https://github.com/choiceoh/Deneb/issues/2697)) ([983395e](https://github.com/choiceoh/Deneb/commit/983395e30dffa25f0e251a6abd7caa60ce63c7c1))
* **mailarchive:** preserve thread context priority ([#2576](https://github.com/choiceoh/Deneb/issues/2576)) ([ee75bf3](https://github.com/choiceoh/Deneb/commit/ee75bf30a17aaec5408dc783212cce01e69be383))
* **mail:** harden LMTP archive ingest ([#2577](https://github.com/choiceoh/Deneb/issues/2577)) ([a438c3a](https://github.com/choiceoh/Deneb/commit/a438c3a4963ea8cd7714ac841e1d0d5c5fbcb100))
* **mail:** harden native list cache refresh ([#2595](https://github.com/choiceoh/Deneb/issues/2595)) ([b683699](https://github.com/choiceoh/Deneb/commit/b683699b3e227d1f1eb6f5e5887e87b2bc837a5a))
* **mail:** harden native workflow state ([#2599](https://github.com/choiceoh/Deneb/issues/2599)) ([50b32ef](https://github.com/choiceoh/Deneb/commit/50b32ef72f9ddabee762655ee0e20b1980302336))
* **mail:** move analysis status into list timestamp ([#2602](https://github.com/choiceoh/Deneb/issues/2602)) ([8eec02c](https://github.com/choiceoh/Deneb/commit/8eec02c8ebdbb649488f4014139723674342767c))
* **mail:** move search and filters into toolbar icons ([#2594](https://github.com/choiceoh/Deneb/issues/2594)) ([b243023](https://github.com/choiceoh/Deneb/commit/b243023efd21c204133d3cbb1818700cf310d9fe))
* **mail:** preserve forwarded body after attachment metadata ([#2616](https://github.com/choiceoh/Deneb/issues/2616)) ([83b2abc](https://github.com/choiceoh/Deneb/commit/83b2abc57159178dcd1f8b03266a55445c173653))
* **mail:** restore feed title summaries for auto analysis ([#2581](https://github.com/choiceoh/Deneb/issues/2581)) ([21486b0](https://github.com/choiceoh/Deneb/commit/21486b0d1907957b4aa06bfa29e15478bc7182b0))
* **mail:** serialize workflow state writes ([#2600](https://github.com/choiceoh/Deneb/issues/2600)) ([f7de566](https://github.com/choiceoh/Deneb/commit/f7de566aa00d00d393adef236f19945fb594d4ad))
* **mail:** show deeper native mail lists ([#2589](https://github.com/choiceoh/Deneb/issues/2589)) ([2aa1dcb](https://github.com/choiceoh/Deneb/commit/2aa1dcb1caca7bce7e7369ba0f7c8bf0b6c02825))
* **mail:** strengthen clean body noise removal ([#2615](https://github.com/choiceoh/Deneb/issues/2615)) ([0eed9f2](https://github.com/choiceoh/Deneb/commit/0eed9f2a230ef2f5ef1360ede166271b76b45fbe))
* **mail:** strip signature noise before analysis ([#2603](https://github.com/choiceoh/Deneb/issues/2603)) ([f38b803](https://github.com/choiceoh/Deneb/commit/f38b803ddcf9ca46e2d032b4977a354d2d10cddd))
* **mail:** strip trailing signoff noise before analysis ([#2610](https://github.com/choiceoh/Deneb/issues/2610)) ([0a46f1f](https://github.com/choiceoh/Deneb/commit/0a46f1f1cb5ea0a456d115076774142bd145542d))
* **mail:** surface workflow state load errors ([#2601](https://github.com/choiceoh/Deneb/issues/2601)) ([a703cb6](https://github.com/choiceoh/Deneb/commit/a703cb617b5999f8f627c31ed4a333ab8abeb7b0))
* **mail:** sync attachment wire model ([16737a6](https://github.com/choiceoh/Deneb/commit/16737a6a6982788cd06737f59991af61586888d4))
* **mail:** tighten the first section gap under the received-mail title ([#2665](https://github.com/choiceoh/Deneb/issues/2665)) ([3699bc2](https://github.com/choiceoh/Deneb/commit/3699bc234aa888950e12e1e87f7791d90416ff11))
* **mail:** trim trailing visual residue before analysis ([#2608](https://github.com/choiceoh/Deneb/issues/2608)) ([6ec0d71](https://github.com/choiceoh/Deneb/commit/6ec0d71c2f70d89552c4bd8329c3064797333437))
* **markdown:** convert model-fenced box tables + lock the reported multi-line case ([#2496](https://github.com/choiceoh/Deneb/issues/2496)) ([856824d](https://github.com/choiceoh/Deneb/commit/856824dc78643947ec187ed7b63fc5cf1c4727a8))
* **markdown:** decode common symbol + smart-quote HTML entities ([#2507](https://github.com/choiceoh/Deneb/issues/2507)) ([a00f423](https://github.com/choiceoh/Deneb/commit/a00f423ff36f3609b90af86c288f1d19e057115d))
* **markdown:** scroll wide tables by content width, CJK-aware ([#2501](https://github.com/choiceoh/Deneb/issues/2501)) ([447f5c2](https://github.com/choiceoh/Deneb/commit/447f5c2febc71d2dc0341024801d8ee1afa19f98))
* **markdown:** unwrap markdown tables the model wrapped in a code fence ([#2498](https://github.com/choiceoh/Deneb/issues/2498)) ([c43c15b](https://github.com/choiceoh/Deneb/commit/c43c15bd819431d8c0cc3460e875b6f81f3d7add))
* **miniapp:** correct Dropbox browser path case, load races, and list key ([#2562](https://github.com/choiceoh/Deneb/issues/2562)) ([6a3802f](https://github.com/choiceoh/Deneb/commit/6a3802f082abcf17f9fc605abcd5854f5c4b7b4e))
* **miniapp:** re-analyze mail reads the on-box archive, not the Gmail API ([#2828](https://github.com/choiceoh/Deneb/issues/2828)) ([a65e6b9](https://github.com/choiceoh/Deneb/commit/a65e6b9a2035851bb87b2ed27cc23c91fe6993d3))
* **miniapp:** sanitize mail analysis cache filename instead of rejecting dotted IDs ([#2553](https://github.com/choiceoh/Deneb/issues/2553)) ([23b0e3f](https://github.com/choiceoh/Deneb/commit/23b0e3f67432fa65bce4c86b4660a67867462dc1))
* minSemanticScore=0.4 floor (matches wiki semSupportThreshold). ([ab07e56](https://github.com/choiceoh/Deneb/commit/ab07e5679bc43e1345122d3ee46c629df8d9a727))
* **modeltuner:** drop proactive notification, surface recs in model picker ([#2565](https://github.com/choiceoh/Deneb/issues/2565)) ([b6d0375](https://github.com/choiceoh/Deneb/commit/b6d03758ef1de05c161084908576d57dd9fd4ca7))
* **native:** always offer the vision model role (drop unreliable auto-hide) ([#2613](https://github.com/choiceoh/Deneb/issues/2613)) ([f2fab61](https://github.com/choiceoh/Deneb/commit/f2fab61b65d65f9c9ec94d703d981f531e96affc))
* **native:** follow-the-finger 챗봇↔업무 swipe transition ([#2567](https://github.com/choiceoh/Deneb/issues/2567)) ([6c6fe94](https://github.com/choiceoh/Deneb/commit/6c6fe9416dbed7983aa5a079ec0c904f442d230a))
* **native:** merge 음성 입력 into the 더보기 group card ([#2536](https://github.com/choiceoh/Deneb/issues/2536)) ([1e27cc7](https://github.com/choiceoh/Deneb/commit/1e27cc7fce06033f8c620cb4d6a5558003b47db1))
* **native:** notification icon = Deneb 4-point sparkle (was the old Kai mark) ([#2786](https://github.com/choiceoh/Deneb/issues/2786)) ([91a9c6f](https://github.com/choiceoh/Deneb/commit/91a9c6f009e5fa6a44967f51f65524460c3ddad9))
* **native:** slim the chat top and bottom bars ([#2520](https://github.com/choiceoh/Deneb/issues/2520)) ([13ce4f0](https://github.com/choiceoh/Deneb/commit/13ce4f09ed3edcb8610356eab4bb26f42ad0b99f))
* **native:** slim the phone bottom bar a touch more (58→52dp) ([#2546](https://github.com/choiceoh/Deneb/issues/2546)) ([0ca47db](https://github.com/choiceoh/Deneb/commit/0ca47db763354050e31c6ed90642efad2fa9e8b4))
* **native:** surface feed-card feedback report (was discarded) ([#2832](https://github.com/choiceoh/Deneb/issues/2832)) ([8e69270](https://github.com/choiceoh/Deneb/commit/8e69270f1abdb5cc542a4aae038b13bf26c9afa2))
* **native:** tighten the feed date bar height ([#2548](https://github.com/choiceoh/Deneb/issues/2548)) ([c5ecd28](https://github.com/choiceoh/Deneb/commit/c5ecd28b263a18a82b69f11bc03404900a0961fa))
* **native:** 더보기 tab returns to the More list, not the last-opened section ([#2550](https://github.com/choiceoh/Deneb/issues/2550)) ([4f55948](https://github.com/choiceoh/Deneb/commit/4f5594882ae24746cc43fb263925b765eee1ecf8))
* **native:** 앱 아이콘 8각 별 뾰족함 중간값으로 (골 0.50→0.38) ([#2798](https://github.com/choiceoh/Deneb/issues/2798)) ([52ffe3e](https://github.com/choiceoh/Deneb/commit/52ffe3ebcd1a363c658affe8a38aca9e73884c98))
* **native:** 앱 아이콘 8각 별 크기·뾰족함 완화 ([#2793](https://github.com/choiceoh/Deneb/issues/2793)) ([8d2bc28](https://github.com/choiceoh/Deneb/commit/8d2bc28aa65c68f844f7e51933a92bdffbbfd1ef))
* **native:** 하단 탭바 높이 축소 (80→64dp) ([#2516](https://github.com/choiceoh/Deneb/issues/2516)) ([511e578](https://github.com/choiceoh/Deneb/commit/511e578a8e4cddc04f741fffbd670dbc44a59f64))
* **notebook:** add missing DenebNotebooksScreen import so android compiles ([#2698](https://github.com/choiceoh/Deneb/issues/2698)) ([e75ac68](https://github.com/choiceoh/Deneb/commit/e75ac6850cb6398dd96ce34af6dadc23e0d4c4d7))
* **notebook:** anchor deal notebooks by frozen project code ([#2803](https://github.com/choiceoh/Deneb/issues/2803)) ([f63e6ad](https://github.com/choiceoh/Deneb/commit/f63e6ad792f1a425eb93b37ac58609956f0dbaa9))
* **org:** dashboard lane-key mismatch + 겸직 소실 + 3 low-sev (코드리뷰 후속) ([#2716](https://github.com/choiceoh/Deneb/issues/2716)) ([3c059b9](https://github.com/choiceoh/Deneb/commit/3c059b959c89748585f0287027a248ab78d7ed54))
* **server:** CORS 추가 — 브라우저 워크스테이션(Andromeda) 게이트웨이 연결 ([#2839](https://github.com/choiceoh/Deneb/issues/2839)) ([a8201a6](https://github.com/choiceoh/Deneb/commit/a8201a66ccd7cb2b5292de7eb1a63eeadad86679))
* **server:** skip phone Gmail notifications already covered by gmail-poll ([#2837](https://github.com/choiceoh/Deneb/issues/2837)) ([225fb96](https://github.com/choiceoh/Deneb/commit/225fb96982d53325b73e75834bcd475d7b01cb93))
* **skills:** apply thrash guard + recency gate to all skill-evolution paths ([7424d57](https://github.com/choiceoh/Deneb/commit/7424d57e857c677286830f7f38601a10a6acdde6))
* **skills:** gate review-path skill evolution (thrash guard + recency gate) ([3da92ee](https://github.com/choiceoh/Deneb/commit/3da92eed9b488177a2340aead8949de094c447ec))
* **skills:** generalize replay validation traces ([#2574](https://github.com/choiceoh/Deneb/issues/2574)) ([f841d85](https://github.com/choiceoh/Deneb/commit/f841d85f5a0d68906bd51fae5545f43e76118a5f))
* **skills:** make bundled skills readable and immutable to the agent ([#2681](https://github.com/choiceoh/Deneb/issues/2681)) ([40db858](https://github.com/choiceoh/Deneb/commit/40db858304b0186632a72e504fd96f84a1370960))
* **skills:** point email-analysis read path at mail_archive, not legacy gmail ([a3c03db](https://github.com/choiceoh/Deneb/commit/a3c03dbb34432f9e4586625e4ae44ef5e38acdba))
* **skills:** point email-analysis read path at mail_archive, not legacy gmail ([2bc4bb4](https://github.com/choiceoh/Deneb/commit/2bc4bb4ce3e864c9716cf7a857da0425473a5b4d))
* **skills:** read bundled SKILL.md at any catalog root; revert unmeasured tool-error hints ([#2790](https://github.com/choiceoh/Deneb/issues/2790)) ([658b2ff](https://github.com/choiceoh/Deneb/commit/658b2ff042ca21b7a520828b45a3acb1325e4224))
* **state:** derive wiki/diary/poll dirs from ResolveStateDir so DENEB_STATE_DIR isolates everything ([#2558](https://github.com/choiceoh/Deneb/issues/2558)) ([49af47a](https://github.com/choiceoh/Deneb/commit/49af47ac832554e627e9783905fa3123e0e44f27))
* **todo:** 메일·open-loop 자동 할일 생성 중단 (승인 우선) ([#2810](https://github.com/choiceoh/Deneb/issues/2810)) ([8b4d10b](https://github.com/choiceoh/Deneb/commit/8b4d10b1a8ec5bddac21f13b788fe12f127c1c49))
* **translate:** in-app browser translate toggle (off-by-default + re-enable re-translates) ([#2717](https://github.com/choiceoh/Deneb/issues/2717)) ([447b969](https://github.com/choiceoh/Deneb/commit/447b96999c4a2c9c73bfc61684ee19a2bb736c20))
* **translate:** small batches + split-retry so real pages don't come back untranslated ([#2746](https://github.com/choiceoh/Deneb/issues/2746)) ([c6797f2](https://github.com/choiceoh/Deneb/commit/c6797f28d474c6f9d127f5f447d5cf828b1ac3b1))
* **wiki:** hold writeMu across index rebuild to prevent dropped entries ([#2836](https://github.com/choiceoh/Deneb/issues/2836)) ([e4dfc72](https://github.com/choiceoh/Deneb/commit/e4dfc72f0286ca154e82fd96f70ee88ba0fc6ee9))
* **wiki:** serialize page writes to prevent lost updates ([#2835](https://github.com/choiceoh/Deneb/issues/2835)) ([2e22221](https://github.com/choiceoh/Deneb/commit/2e222210b5519f741d5de25b9efba2bfe75781a6))
* **wiki:** serialize semantic refresh startup against shutdown; harden gain corpus ([#2745](https://github.com/choiceoh/Deneb/issues/2745)) ([6d2982d](https://github.com/choiceoh/Deneb/commit/6d2982d7df4a7b20a7f51135915ec8df9765729e))
* **workfeed:** honest dream headline + suppress narration-only proactive cards ([#2566](https://github.com/choiceoh/Deneb/issues/2566)) ([15ea151](https://github.com/choiceoh/Deneb/commit/15ea1510d0b8987efc447d83275d2ce4fa6cb4e0))
* **wormhole:** classify tailnet (CGNAT 100.64/10) upstreams as local, not cloud egress ([#2779](https://github.com/choiceoh/Deneb/issues/2779)) ([1d13f59](https://github.com/choiceoh/Deneb/commit/1d13f5926738d7cbd4bd60e1e895c1702091f987))


### ⚡ Performance

* **calendar:** make Google Calendar read opt-in (default local-only) ([#2646](https://github.com/choiceoh/Deneb/issues/2646)) ([8be55de](https://github.com/choiceoh/Deneb/commit/8be55de321edd02f8f9e56991309b3a6bb76b299))
* **chat:** 마크다운 파싱을 백그라운드 코어로 — 히스토리 프리컴퓨트로 스크롤 프레임 메인 스레드 파싱 제거 ([#2523](https://github.com/choiceoh/Deneb/issues/2523)) ([dd70bb4](https://github.com/choiceoh/Deneb/commit/dd70bb4a645c1d4f6f3d11edaf832c1b6243f18a))
* **chat:** 스트리밍 history emission coalescing — 토큰마다 돌던 combine을 ~15fps로 샘플링 ([#2534](https://github.com/choiceoh/Deneb/issues/2534)) ([55feef4](https://github.com/choiceoh/Deneb/commit/55feef482429acba7455d8d4b2202cb5e995bc8c))
* **chat:** 스트리밍 답변 마크다운 파싱을 백그라운드 코어로 (produceState 더블버퍼) ([#2528](https://github.com/choiceoh/Deneb/issues/2528)) ([2ce5fd1](https://github.com/choiceoh/Deneb/commit/2ce5fd195255ccd2e8fea3f079b13a4f4701a23c))
* **chat:** 이미지 첨부 디코딩을 백그라운드 코어로 + 유저 메시지도 캐시 적용 ([#2526](https://github.com/choiceoh/Deneb/issues/2526)) ([de991a1](https://github.com/choiceoh/Deneb/commit/de991a1be62e8ad10bf5b417cf5bd0e1bbe9f6e0))
* **compaction:** 동기 압축 예산 2m→3m — GPU 경합 시 청크 요약 타임아웃 완화 ([#2489](https://github.com/choiceoh/Deneb/issues/2489)) ([e15c9e3](https://github.com/choiceoh/Deneb/commit/e15c9e3fc8949fa173001d9c02b8951c31bd1df1))
* **compaction:** 요약 모델을 analysis(클라우드 glm-5.2)→lightweight(로컬 qwen)로 ([#2508](https://github.com/choiceoh/Deneb/issues/2508)) ([113fcfb](https://github.com/choiceoh/Deneb/commit/113fcfb3887ea72bd7f714f546827bffaba6691b))
* **native:** avoid duplicate native_status fetch on mail open ([#2630](https://github.com/choiceoh/Deneb/issues/2630)) ([dba5862](https://github.com/choiceoh/Deneb/commit/dba586245406195f15760a83961ee9c4d734ec5c))
* **native:** cache calendar months client-side for instant re-open ([#2642](https://github.com/choiceoh/Deneb/issues/2642)) ([5081c6f](https://github.com/choiceoh/Deneb/commit/5081c6f145b7c2c5dc1e550fd630255a8ca20954))
* **native:** coalesce chat follow-scroll during streaming ([#2625](https://github.com/choiceoh/Deneb/issues/2625)) ([00d2d62](https://github.com/choiceoh/Deneb/commit/00d2d6208bcd81423838b8210c6199141a3611d5))
* **native:** downsample inbound images to display scale ([#2626](https://github.com/choiceoh/Deneb/issues/2626)) ([f35f161](https://github.com/choiceoh/Deneb/commit/f35f161fbcc2a54ee0a73233a5031510d2bb1650))
* **native:** lazy diary list, memoize derived list derivations ([#2629](https://github.com/choiceoh/Deneb/issues/2629)) ([000bccc](https://github.com/choiceoh/Deneb/commit/000bcccd17f932f6b7d1be6516cf0dac6d90261b))
* **native:** mark UI DTOs and generated wire types @Immutable ([#2628](https://github.com/choiceoh/Deneb/issues/2628)) ([5da5c02](https://github.com/choiceoh/Deneb/commit/5da5c021c601dfde216de6247bc621c59789499b))
* **native:** 비채팅 화면 마크다운(위키·일기·인물·스킬·크론·메일) 큰 본문을 백그라운드 코어에서 파싱 ([#2531](https://github.com/choiceoh/Deneb/issues/2531)) ([17c6c9d](https://github.com/choiceoh/Deneb/commit/17c6c9d0b935829ac6132991f0e898cd3dc7cbca))
* **prompt:** promote code_action (CodeAct) to eager ([#2570](https://github.com/choiceoh/Deneb/issues/2570)) ([0f891c9](https://github.com/choiceoh/Deneb/commit/0f891c91ac9a33639b026e6f8840451455cb50a9))
* **youtube:** 자막 요약을 analysis(클라우드)→lightweight(로컬 qwen)로 원복 ([#2509](https://github.com/choiceoh/Deneb/issues/2509)) ([1423566](https://github.com/choiceoh/Deneb/commit/14235668ee680a9208e92bf56e887ab661680158))


### 🔧 Internal

* **calendar:** drop the month-grid color legend ([f3dafe4](https://github.com/choiceoh/Deneb/commit/f3dafe488e0b2f02b9df9e58e38a6129706ba9b7))
* **calendar:** drop the month-grid color legend ([4701754](https://github.com/choiceoh/Deneb/commit/470175424683002f83d5220eb1001d7aa0e2c318))
* **chat:** remove interactiveTurnSem — the unified-memory OOM premise was false ([#2829](https://github.com/choiceoh/Deneb/issues/2829)) ([b35b943](https://github.com/choiceoh/Deneb/commit/b35b943babf94c0e33845a955d1dac94badc6779))
* **dropbox:** remove Dropbox integration; local file store replaces it ([#2701](https://github.com/choiceoh/Deneb/issues/2701)) ([a7b437e](https://github.com/choiceoh/Deneb/commit/a7b437eb3b13677e038e97225f356db9b02dc49f))
* **gmailpoll:** reconstruct mail thread from threadId, not subject search ([#2545](https://github.com/choiceoh/Deneb/issues/2545)) ([77b9441](https://github.com/choiceoh/Deneb/commit/77b94418a283abc7682e0c384344c769a90bbcaf))
* **launcher:** consolidate launcher code into ai.deneb.ui.launcher ([#2770](https://github.com/choiceoh/Deneb/issues/2770)) ([379f448](https://github.com/choiceoh/Deneb/commit/379f4484af4331e35f946e547e0a12cfe72de028))
* **launcher:** drop dead LauncherAppEntry.icon field ([#2755](https://github.com/choiceoh/Deneb/issues/2755)) ([43693c6](https://github.com/choiceoh/Deneb/commit/43693c66bbe694653bc907fa00d7ab401199385a))
* **launcher:** drop the phone home-launcher surface, focus the assistant app ([#2772](https://github.com/choiceoh/Deneb/issues/2772)) ([5c869e2](https://github.com/choiceoh/Deneb/commit/5c869e2ef3aa55aa1d515cee932fdf72d7546061))
* **markdown:** one renderer for all surfaces, delete DenebMarkdown ([#2524](https://github.com/choiceoh/Deneb/issues/2524)) ([06618e5](https://github.com/choiceoh/Deneb/commit/06618e5db422f213f74dd03b8dca13c97a60b414))
* **native:** drop dead home-launcher reset wiring ([#2774](https://github.com/choiceoh/Deneb/issues/2774)) ([68e8a77](https://github.com/choiceoh/Deneb/commit/68e8a77d16c8a206218e5afe1ed2557a86d5b7a3))
* **native:** move Dropbox 연결됨 blurb into a tooltip ([#2563](https://github.com/choiceoh/Deneb/issues/2563)) ([a3c8b6c](https://github.com/choiceoh/Deneb/commit/a3c8b6c6d880e8fd66da6787f84945d78c382be0))
* **native:** remove the desktop product UI (Andromeda owns desktop) ([#2710](https://github.com/choiceoh/Deneb/issues/2710)) ([a2a7825](https://github.com/choiceoh/Deneb/commit/a2a782583c9344f8b028a17b1316acf006cc9f23))
* **native:** unify admin/settings typography to DenebType tokens ([#2792](https://github.com/choiceoh/Deneb/issues/2792)) ([6081102](https://github.com/choiceoh/Deneb/commit/60811023970855bca26e3d2217d531d80797c3df))
* **native:** 검색 입력 idiom 공용화 + 플릿 HF 검색창 밑줄화 ([#2796](https://github.com/choiceoh/Deneb/issues/2796)) ([a5f0bd6](https://github.com/choiceoh/Deneb/commit/a5f0bd6d26d76dcc44609170e9cf39b49ece1524))
* **native:** 검색창 전부 밑줄로 통일 + 알약 컴포넌트 제거 ([#2797](https://github.com/choiceoh/Deneb/issues/2797)) ([b7bb91a](https://github.com/choiceoh/Deneb/commit/b7bb91a15361b051efca8f05f5f87e53eb1b1a56))
* **native:** 나이아가라 앱 드로어에서 앱 검색 제거 ([#2767](https://github.com/choiceoh/Deneb/issues/2767)) ([aa9c9d6](https://github.com/choiceoh/Deneb/commit/aa9c9d621d79ef182bbacff722520235f31223d0))
* **translate:** unify in-app browser chrome with the Deneb design system ([#2711](https://github.com/choiceoh/Deneb/issues/2711)) ([35f2b50](https://github.com/choiceoh/Deneb/commit/35f2b50586629ff2b978a305edbd3e5fb71b040f))
* **workfeed:** 카드 제목 생성을 lightweight→tiny 역할로 (diffusiongemma) ([#2499](https://github.com/choiceoh/Deneb/issues/2499)) ([0c630ae](https://github.com/choiceoh/Deneb/commit/0c630aecb30d3c4695b9a776cfd4ffa40b582718))

## [4.29.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.28.0...deneb-v4.29.0) (2026-06-15)


### ✨ Features

* **chat:** clean general-assistant system prompt for 챗봇 workspace ([#2441](https://github.com/choiceoh/Deneb/issues/2441)) ([ed2a976](https://github.com/choiceoh/Deneb/commit/ed2a97601f8742fb2486011c857edc539bbe09fa))
* **chat:** differentiate 챗봇 tone via a cache-safe tail directive ([#2437](https://github.com/choiceoh/Deneb/issues/2437)) ([3290e72](https://github.com/choiceoh/Deneb/commit/3290e727a6d6d1fd410679bedbf3d6e55a5654f6))
* **chat:** forbid ASCII box-drawing tables in agent output ([#2443](https://github.com/choiceoh/Deneb/issues/2443)) ([c3defcb](https://github.com/choiceoh/Deneb/commit/c3defcb627fe821960b0b6a7be0038bd4b237f12))
* **chat:** inject the day's work-feed digest as 업무 chat context ([#2442](https://github.com/choiceoh/Deneb/issues/2442)) ([450493a](https://github.com/choiceoh/Deneb/commit/450493a18a96cb83b4c3dda6fa7e1dd988545f20))
* **chat:** 사용자 메시지 말풍선을 테두리 없는 회색 배경으로 ([#2421](https://github.com/choiceoh/Deneb/issues/2421)) ([efb5701](https://github.com/choiceoh/Deneb/commit/efb5701ad04fe5a6facd3092d0c178620ef32802))
* **chat:** 응답 생성 중 화면 상단 hue 순환 글로우 배경 (제미나이식) ([#2419](https://github.com/choiceoh/Deneb/issues/2419)) ([76df62b](https://github.com/choiceoh/Deneb/commit/76df62be9e8eab99f041442225b80108fbb393db))
* **chat:** 응답 중 메시지 영역 오로라 글로우 (제미나이식) ([#2415](https://github.com/choiceoh/Deneb/issues/2415)) ([94dd348](https://github.com/choiceoh/Deneb/commit/94dd3489c3de230be709f5753673c79522032669))
* **markdown:** recover pipe tables missing the delimiter row ([#2450](https://github.com/choiceoh/Deneb/issues/2450)) ([2a8946f](https://github.com/choiceoh/Deneb/commit/2a8946f9f53832e48057adbdbbb1201206ef26ca))
* **markdown:** render box-drawing (ASCII-art) tables as real tables ([#2446](https://github.com/choiceoh/Deneb/issues/2446)) ([bcf242d](https://github.com/choiceoh/Deneb/commit/bcf242d60ab6d3e934992ea818dab3f2a5eb7d16))
* **markdown:** render GFM footnotes as superscripts with a notes section ([#2452](https://github.com/choiceoh/Deneb/issues/2452)) ([fcfb9d6](https://github.com/choiceoh/Deneb/commit/fcfb9d6ae78e75db4ec7d109b6966821c5977422))
* **markdown:** render HTML u, mark, sup/sub, and anchor inline tags ([#2459](https://github.com/choiceoh/Deneb/issues/2459)) ([18842f7](https://github.com/choiceoh/Deneb/commit/18842f78569b1aebd56c68de44459e2e05c110dc))
* **markdown:** tap a markdown image to open the fullscreen zoom viewer ([#2454](https://github.com/choiceoh/Deneb/issues/2454)) ([5a4711c](https://github.com/choiceoh/Deneb/commit/5a4711c37bd8d122fe989cf22a68a8c87fc1f824))
* **media:** transcribe YouTube audio via ASR when captions are unavailable ([#2471](https://github.com/choiceoh/Deneb/issues/2471)) ([dfd78d2](https://github.com/choiceoh/Deneb/commit/dfd78d2e38b20c97150ecf9fe480c67223f3678a))
* **miniapp:** add native Dropbox connect (PKCE OAuth) from Settings &gt; 연동 ([#2423](https://github.com/choiceoh/Deneb/issues/2423)) ([e57b50e](https://github.com/choiceoh/Deneb/commit/e57b50e85915a74c62555f4a0fc110b7c185a996))
* **models:** add a separate chatbot-workspace model role ([#2406](https://github.com/choiceoh/Deneb/issues/2406)) ([bba53df](https://github.com/choiceoh/Deneb/commit/bba53df0325966de5bde39592becf04590db1bd0))
* **native:** apply 2026-06 design refresh to content + settings screens ([#2396](https://github.com/choiceoh/Deneb/issues/2396)) ([ff4ff9b](https://github.com/choiceoh/Deneb/commit/ff4ff9b671b156fec044e1fceacb84124e76d043))
* **native:** client cache-then-network for transcript + mail (instant reopen) ([#2465](https://github.com/choiceoh/Deneb/issues/2465)) ([742d075](https://github.com/choiceoh/Deneb/commit/742d075b0f9c97e18f9ec366442ab77073817542))
* **native:** cute sky-blue slime (squash & stretch) for the 답변-중 status ([#2458](https://github.com/choiceoh/Deneb/issues/2458)) ([2a92686](https://github.com/choiceoh/Deneb/commit/2a9268660ddc254fea79f7d6c182f9646fb4c86b))
* **native:** design-system refresh foundation — grouped cards + 2-accent ([#2384](https://github.com/choiceoh/Deneb/issues/2384)) ([969277e](https://github.com/choiceoh/Deneb/commit/969277ea427e202d0710da0c9d0a30e1d561390c))
* **native:** fade out scroll-to-bottom button while the list scrolls ([#2422](https://github.com/choiceoh/Deneb/issues/2422)) ([9bf377a](https://github.com/choiceoh/Deneb/commit/9bf377a978246990cfbe74e663c8ef461e1c0fd1))
* **native:** FCM push for closed-app proactive delivery ([#2375](https://github.com/choiceoh/Deneb/issues/2375)) ([eb14135](https://github.com/choiceoh/Deneb/commit/eb1413568b037397e4d504978a4e612adaded679))
* **native:** flat 답변-중 status with staggered three-dot typing motion ([#2453](https://github.com/choiceoh/Deneb/issues/2453)) ([cf22495](https://github.com/choiceoh/Deneb/commit/cf22495ac2b3fa71cac4bb55f145d4fabd400a5f))
* **native:** flatten chatbot sessions, drop the chat:main home ([#2409](https://github.com/choiceoh/Deneb/issues/2409)) ([1d8dd76](https://github.com/choiceoh/Deneb/commit/1d8dd76e154f07a4125ee6d2fd67767420113d72))
* **native:** four-point sparkle (✦) burst for the 답변-중 status ([#2461](https://github.com/choiceoh/Deneb/issues/2461)) ([95e356a](https://github.com/choiceoh/Deneb/commit/95e356a410b11fbf3912e0cbaf4bc1fde54c4d9c))
* **native:** GPT/Claude-style left session drawer + captures in input ([#2374](https://github.com/choiceoh/Deneb/issues/2374)) ([2ea68d3](https://github.com/choiceoh/Deneb/commit/2ea68d3f3987520f1b69edc89cea9deb8f6cb49c))
* **native:** hide 업무 데이터 sections from navigation in 챗봇 mode ([#2414](https://github.com/choiceoh/Deneb/issues/2414)) ([44be827](https://github.com/choiceoh/Deneb/commit/44be827c8985baf4f22045381cf681fcbef1af96))
* **native:** larger chat font + line spacing in 챗봇 mode ([#2433](https://github.com/choiceoh/Deneb/issues/2433)) ([4741baf](https://github.com/choiceoh/Deneb/commit/4741baf035b08027e94268922feaf47f7c419a24))
* **native:** migrate settings to the grouped-card aesthetic ([#2388](https://github.com/choiceoh/Deneb/issues/2388)) ([9abf1a2](https://github.com/choiceoh/Deneb/commit/9abf1a28d9f249dbf8f4d44cdac2d3e4ca69c9c6))
* **native:** refine chat input placeholder and split empty-state greeting by mode ([#2402](https://github.com/choiceoh/Deneb/issues/2402)) ([313d1bc](https://github.com/choiceoh/Deneb/commit/313d1bcbec450144da4014ab01e3f4b6ed93a09e))
* **native:** remove bottom bar entirely in 챗봇 mode (focus chat) ([#2420](https://github.com/choiceoh/Deneb/issues/2420)) ([121b4a9](https://github.com/choiceoh/Deneb/commit/121b4a9636c2889730dbc9e178d5247afebe9997))
* **native:** restyle 더보기 to match 설정 grouped-card idiom ([#2418](https://github.com/choiceoh/Deneb/issues/2418)) ([9b537af](https://github.com/choiceoh/Deneb/commit/9b537af6778db17a30bff472591eb1f0a208e4c6))
* **native:** save/share actions for inline image attachments ([#2435](https://github.com/choiceoh/Deneb/issues/2435)) ([49a940d](https://github.com/choiceoh/Deneb/commit/49a940d2e38666612ca9e6790de46bbb1336bb92))
* **native:** scope notification feed to workspace, mute 업무 리포트 in 챗봇 mode ([#2400](https://github.com/choiceoh/Deneb/issues/2400)) ([8b237a6](https://github.com/choiceoh/Deneb/commit/8b237a6907f617f550f3cc6b26a604415eeddecc))
* **native:** Toss-style bottom tab bar + super-app icon nav ([#2366](https://github.com/choiceoh/Deneb/issues/2366)) ([a44d469](https://github.com/choiceoh/Deneb/commit/a44d4694bf82affea7b8f884e2afc320e65b760f))
* **native:** 첨부 + 단일 피커 — 파일 종류 자동 라우팅, 메뉴 제거, 음성입력 더보기 이동 ([#2466](https://github.com/choiceoh/Deneb/issues/2466)) ([f273a53](https://github.com/choiceoh/Deneb/commit/f273a53eaf138a8237b6c6c9bf50aa83c705631a))
* **native:** 폰 내비에서 카테↔설정 위치 교체, 더보기서 중복 플릿 제거 ([#2398](https://github.com/choiceoh/Deneb/issues/2398)) ([2ffff66](https://github.com/choiceoh/Deneb/commit/2ffff6609903a058cbf0108db65347d3fc4ada27))
* **native:** 피드 main screen for 업무 mode (feed-first home) ([#2448](https://github.com/choiceoh/Deneb/issues/2448)) ([4ad3cff](https://github.com/choiceoh/Deneb/commit/4ad3cff12f87cd3e8f6a3fdeddc0684bfe72173b))
* **native:** 피드 탭/레일에 안읽음 배지 + 상단 종 제거 ([#2455](https://github.com/choiceoh/Deneb/issues/2455)) ([63be38c](https://github.com/choiceoh/Deneb/commit/63be38c7d6ac6e4b4231d7764840650a2d4bc838))
* **proactive:** 업무 피드 전용 배달 — client:main 채팅 트랜스크립트 미러링 중단 ([#2449](https://github.com/choiceoh/Deneb/issues/2449)) ([d490590](https://github.com/choiceoh/Deneb/commit/d490590bd87de7c053d2cc73bb0257c8c8cea1d1))
* **push:** FCM proactive delivery fallback (gateway scaffolding, dormant) ([#2365](https://github.com/choiceoh/Deneb/issues/2365)) ([7a76e75](https://github.com/choiceoh/Deneb/commit/7a76e75b7f3e17d4a44e6bdf9a8cd138d2bfcbeb))
* **recall:** 하인드사이트 stale 사실 마커 (1C 게이트웨이측 저비용 대안) ([#2444](https://github.com/choiceoh/Deneb/issues/2444)) ([8871d85](https://github.com/choiceoh/Deneb/commit/8871d85d566f000de84f8cec1f522692691e268a))
* **security:** harden internet-exposed gateway (body caps, turn limit, untrusted-tool gate) ([#2385](https://github.com/choiceoh/Deneb/issues/2385)) ([fef4339](https://github.com/choiceoh/Deneb/commit/fef4339f85e03db9549310426e3de9cab5505983))
* **weekly:** render the report image at 2x device scale ([#2436](https://github.com/choiceoh/Deneb/issues/2436)) ([067a27d](https://github.com/choiceoh/Deneb/commit/067a27d7476f4689d685fc9fc2d41cc2b4505d48))
* **workfeed:** 메일 카드 제목 — 경량 LLM 우선 + 휴리스틱 폴백 ([#2460](https://github.com/choiceoh/Deneb/issues/2460)) ([12ba75e](https://github.com/choiceoh/Deneb/commit/12ba75e539b32a1fb28846cd917825e3e9b0ef48))
* **workfeed:** 메일 카드에 편지(envelope) 아이콘 — 우체통 대체 ([#2463](https://github.com/choiceoh/Deneb/issues/2463)) ([945ebe7](https://github.com/choiceoh/Deneb/commit/945ebe7948316d9230b576b28c6de3b493c6f5c5))
* **workfeed:** 보관/휴지통 feed actions + better proactive card titles ([#2479](https://github.com/choiceoh/Deneb/issues/2479)) ([06a2cf3](https://github.com/choiceoh/Deneb/commit/06a2cf36a259a2c5549bea620ffe6199ca3bf417))
* **wormhole:** /metrics observability + load-time config validation ([#2381](https://github.com/choiceoh/Deneb/issues/2381)) ([671a261](https://github.com/choiceoh/Deneb/commit/671a261cbe8a48c39ca16c64a333967fcb4f9511))
* **wormhole:** auto-discover local models from SparkFleet ([#2361](https://github.com/choiceoh/Deneb/issues/2361)) ([9a224dd](https://github.com/choiceoh/Deneb/commit/9a224dde7dfa6df97340eb1b61abee44327dd85a))
* **wormhole:** feed multi-turn history to the effort classifier ([#2413](https://github.com/choiceoh/Deneb/issues/2413)) ([42e8959](https://github.com/choiceoh/Deneb/commit/42e8959d55569b413910446534bfe05a8847067e))
* **wormhole:** fleet-backed explicit entries — url from SparkFleet discovery ([#2429](https://github.com/choiceoh/Deneb/issues/2429)) ([a05e475](https://github.com/choiceoh/Deneb/commit/a05e47552ba0ea2347d55c31eace7d327f34151c))
* **wormhole:** identify the calling client (foundation for per-client output shaping) ([#2407](https://github.com/choiceoh/Deneb/issues/2407)) ([de1a31c](https://github.com/choiceoh/Deneb/commit/de1a31c173e4fb5eea265a855cbfe128f3dc2eee))
* **wormhole:** openai-only /v1/models + re-land dropped external-ready ([#2383](https://github.com/choiceoh/Deneb/issues/2383)) ([#2403](https://github.com/choiceoh/Deneb/issues/2403)) ([2d16947](https://github.com/choiceoh/Deneb/commit/2d1694739da748f9fbe23cf332032444038d9665))
* **wormhole:** per-client response shaping framework ([#2408](https://github.com/choiceoh/Deneb/issues/2408)) ([8f553aa](https://github.com/choiceoh/Deneb/commit/8f553aae9e54fde0a1f9361ad61433ee60331580))
* **wormhole:** re-land status-tab fleet surfacing dropped from main ([#2364](https://github.com/choiceoh/Deneb/issues/2364)) ([#2370](https://github.com/choiceoh/Deneb/issues/2370)) ([9c219d7](https://github.com/choiceoh/Deneb/commit/9c219d76edec09601a108a91011445b9cb9ef651))
* **wormhole:** report each local model's max_model_len; gateway discovers it from the proxy ([#2440](https://github.com/choiceoh/Deneb/issues/2440)) ([977660f](https://github.com/choiceoh/Deneb/commit/977660fa1959129feb2a837b761015fa2612f674))
* **wormhole:** wire cloud upstream keys (EnvironmentFile) + document cloud routing ([#2378](https://github.com/choiceoh/Deneb/issues/2378)) ([a85cb5d](https://github.com/choiceoh/Deneb/commit/a85cb5d8aa19b8907d171cf5f7427cbb3030ac42))


### 🐛 Bug Fixes

* **chat:** auto-title flat 챗봇 (chat:&lt;uuid&gt;) conversations ([#2451](https://github.com/choiceoh/Deneb/issues/2451)) ([3375e6e](https://github.com/choiceoh/Deneb/commit/3375e6e6c66f28136bd7301ad5f2e6ac2dc61d62))
* **chat:** skip hindsight retain for 챗봇 sessions (session-key gate) ([#2456](https://github.com/choiceoh/Deneb/issues/2456)) ([359eeff](https://github.com/choiceoh/Deneb/commit/359eeffe2856f55beb3b973544427c9759b4475c))
* **chat:** skip recall for 챗봇 sessions so the clean prompt stays clean ([#2445](https://github.com/choiceoh/Deneb/issues/2445)) ([6eea49b](https://github.com/choiceoh/Deneb/commit/6eea49bd0c5ef9cb50fe19693fad57be96acf95d))
* **chat:** 사용자 말풍선 폭 제한(반응형) — 긴 메시지가 왼쪽 끝까지 늘어나지 않게 ([#2426](https://github.com/choiceoh/Deneb/issues/2426)) ([ba047b1](https://github.com/choiceoh/Deneb/commit/ba047b121ca4fb4e0af6721fa95fd75df034b7d4))
* **chat:** 응답 오로라 글로우를 흐르는 빛으로 (네 면 균일 → 한 점에서 흐름) ([#2417](https://github.com/choiceoh/Deneb/issues/2417)) ([26ee059](https://github.com/choiceoh/Deneb/commit/26ee0594491bcb45df64c98ba706175616c0bccf))
* **chat:** 채팅 입력창 컴팩트화 + 맨아래 버튼 동작 교정 ([#2410](https://github.com/choiceoh/Deneb/issues/2410)) ([9429c73](https://github.com/choiceoh/Deneb/commit/9429c73c12ff4c640d7f58cb39b1e5efbc4b6920))
* **chat:** 챗봇 모드(chat:main:) 대화도 자동 제목 생성 ([#2404](https://github.com/choiceoh/Deneb/issues/2404)) ([c0b2753](https://github.com/choiceoh/Deneb/commit/c0b2753decc4ffa4441692f21588ef3ae7858374))
* **cron:** emit deterministic weekly-report 양식 instead of drifting LLM synthesis ([#2474](https://github.com/choiceoh/Deneb/issues/2474)) ([806b15c](https://github.com/choiceoh/Deneb/commit/806b15c60be7ec88b7e28c34f94a884b4af0955c))
* **effort:** arm the effort router for the main model routed via wormhole ([#2431](https://github.com/choiceoh/Deneb/issues/2431)) ([cf38e17](https://github.com/choiceoh/Deneb/commit/cf38e17e7885fd0a7b791e6edb8084e1c498e3f6))
* **fleet:** dedup 플릿 알림 — 상시 조건 폰 스팸(285회/일) 차단 ([#2477](https://github.com/choiceoh/Deneb/issues/2477)) ([31a9a0e](https://github.com/choiceoh/Deneb/commit/31a9a0ec4cb624303bbe6f8b4b6444cff0502a74))
* **gmail:** flatten HTML that leaks into text/plain mail bodies ([#2464](https://github.com/choiceoh/Deneb/issues/2464)) ([52289ea](https://github.com/choiceoh/Deneb/commit/52289ea0b9c26e02617bfbee78fd3e1a6a4648c5))
* **gmail:** read by query so the model stops guessing opaque message IDs ([#2481](https://github.com/choiceoh/Deneb/issues/2481)) ([17204bb](https://github.com/choiceoh/Deneb/commit/17204bb8400753188ff37239a16b6e625cabf3ab))
* **jsonutil:** LLM JSON 파싱 견고화 — 배열·제어문자·절단 복구 ([#2480](https://github.com/choiceoh/Deneb/issues/2480)) ([f9e407b](https://github.com/choiceoh/Deneb/commit/f9e407b9b3a6d4ad76ca03c881e057f63e58dc16))
* **miniapp:** don't show a false Dropbox connect error on duplicate auth code ([#2439](https://github.com/choiceoh/Deneb/issues/2439)) ([179930b](https://github.com/choiceoh/Deneb/commit/179930b071bd28c6c3799a881dc808a5eee4787a))
* **modelrole:** restore the real context window for the wormhole-fronted main model ([#2438](https://github.com/choiceoh/Deneb/issues/2438)) ([7139567](https://github.com/choiceoh/Deneb/commit/7139567d40d3eae04598913d4869807ad6cc14c0))
* **models:** send provider apiKey on the picker /models probe so token-gated providers (wormhole) show online ([#2399](https://github.com/choiceoh/Deneb/issues/2399)) ([ede53ad](https://github.com/choiceoh/Deneb/commit/ede53adbc785ccbe74f606aca1dcc40fea5d8e01))
* **native:** bind the chat model switcher to the chatbot role in 챗봇 mode ([#2428](https://github.com/choiceoh/Deneb/issues/2428)) ([6485e35](https://github.com/choiceoh/Deneb/commit/6485e3540aec5294ca88f5d3a2641a9122df332b))
* **native:** correct transcript-cache edge cases (switch, delete, stale) ([#2469](https://github.com/choiceoh/Deneb/issues/2469)) ([ea9ca60](https://github.com/choiceoh/Deneb/commit/ea9ca602de1940d479f0b8b999babe2b0b49b543))
* **native:** make credential switch atomic + serialize native-sync reset ([#2473](https://github.com/choiceoh/Deneb/issues/2473)) ([be17915](https://github.com/choiceoh/Deneb/commit/be1791564a88eb92c9e49c1034559177298aa8d5))
* **native:** ordering-immune credential fence + reset all account state on switch ([#2472](https://github.com/choiceoh/Deneb/issues/2472)) ([be24f67](https://github.com/choiceoh/Deneb/commit/be24f671de84cf09620ffe1ea067ae55987174da))
* **native:** purge content caches on credential change + keep mail cache consistent ([#2470](https://github.com/choiceoh/Deneb/issues/2470)) ([967efb0](https://github.com/choiceoh/Deneb/commit/967efb03cfe191d0cd759a5b45ed08443a159052))
* **native:** shorten the generating-backdrop glow so it doesn't reach so far down ([#2424](https://github.com/choiceoh/Deneb/issues/2424)) ([a61f441](https://github.com/choiceoh/Deneb/commit/a61f4413c97aed876a580312682d612cf8d12074))
* **native:** show the generating aurora backdrop only in 챗봇 mode ([#2425](https://github.com/choiceoh/Deneb/issues/2425)) ([24bef69](https://github.com/choiceoh/Deneb/commit/24bef69cf24d0a432335604e3d548eddab8312ba))
* **native:** soft edge-fade so chat flows under the bars (uncovered feel) ([#2478](https://github.com/choiceoh/Deneb/issues/2478)) ([b5ee306](https://github.com/choiceoh/Deneb/commit/b5ee306cb1a5aad25b5cb5ed153d9141778a6c25))
* **native:** tighten user message bubble padding (was oversized for the font) ([#2434](https://github.com/choiceoh/Deneb/issues/2434)) ([6f038bc](https://github.com/choiceoh/Deneb/commit/6f038bc823236c86a4b44bf9b23a89c1465f08a7))
* **native:** 피드 본문을 마크다운 렌더러로 — 깨진 표 수정 ([#2457](https://github.com/choiceoh/Deneb/issues/2457)) ([1586207](https://github.com/choiceoh/Deneb/commit/15862070199b5a9bc912ed4072edd631be74b21c))
* **recall:** demote single-term broadening hits + normalize ㄴ-adnominal verbs ([#2468](https://github.com/choiceoh/Deneb/issues/2468)) ([b666b56](https://github.com/choiceoh/Deneb/commit/b666b56c86dd5e98775e40cd09ba6ab1635e1082))
* **recall:** normalize diary BM25 so curated wiki pages stop being buried ([#2475](https://github.com/choiceoh/Deneb/issues/2475)) ([9b3b202](https://github.com/choiceoh/Deneb/commit/9b3b2029ac19c6ed8adc91405bf3de181ea2fe26))
* **recall:** 하인드사이트 사실을 OccurredAt 기준으로 age (벌크-retain MentionedAt 아님) ([#2447](https://github.com/choiceoh/Deneb/issues/2447)) ([228a478](https://github.com/choiceoh/Deneb/commit/228a4786e62765d26285a41790ce6de4d578cd25))
* **sessions:** keep 챗봇 (chat:) conversations in the drawer across restarts ([#2432](https://github.com/choiceoh/Deneb/issues/2432)) ([6aff435](https://github.com/choiceoh/Deneb/commit/6aff435cb92c811f05274759d78317bf64347c2d))
* **tools:** accept string-encoded numeric/bool tool params (LLM quirk) ([#2476](https://github.com/choiceoh/Deneb/issues/2476)) ([d56d14b](https://github.com/choiceoh/Deneb/commit/d56d14b133a8533dff76346872bdec264c6db9ad))
* **wiki:** semantic-aware blend — demote BM25 hits with weak cosine ([#2485](https://github.com/choiceoh/Deneb/issues/2485)) ([d536886](https://github.com/choiceoh/Deneb/commit/d53688649e4680ed1e7dc82ca69fe49f400568e1))
* **wiki:** warm the semantic vector index at startup ([#2482](https://github.com/choiceoh/Deneb/issues/2482)) ([234c745](https://github.com/choiceoh/Deneb/commit/234c745a1b2f3f0fd882af375571bd165280d3cd))
* **wiki:** 임베딩 재계산을 백그라운드로 — recall 핫패스 타임아웃 제거 ([#2483](https://github.com/choiceoh/Deneb/issues/2483)) ([128a8ca](https://github.com/choiceoh/Deneb/commit/128a8caac89af2b7759fe917e74c4f3ac19e8a9f))
* **workfeed:** strip leading emoji from feed card titles ([#2484](https://github.com/choiceoh/Deneb/issues/2484)) ([1782d4f](https://github.com/choiceoh/Deneb/commit/1782d4f7b1369527307fd4194d38982552d54ec6))
* **wormhole:** correct systemd unit for --user scope (drop User=, default.target) ([#2372](https://github.com/choiceoh/Deneb/issues/2372)) ([5f65320](https://github.com/choiceoh/Deneb/commit/5f6532001e77dcf10ab8eebdaa04b623d9ab1890))


### ⚡ Performance

* **chat:** persist frozen prompt snapshots across restarts ([#2368](https://github.com/choiceoh/Deneb/issues/2368)) ([82dd0fc](https://github.com/choiceoh/Deneb/commit/82dd0fcf48e572013ca32ac7bf4550bba624fb8b))
* **compaction:** defer LLM compaction off the interactive critical path ([#2369](https://github.com/choiceoh/Deneb/issues/2369)) ([ebb8579](https://github.com/choiceoh/Deneb/commit/ebb8579048240e9a0456a4686054ac6801882c4e))
* **router:** word-start matching for Latin hard signals (kill substring false-hards) ([#2411](https://github.com/choiceoh/Deneb/issues/2411)) ([c7b9b79](https://github.com/choiceoh/Deneb/commit/c7b9b7961fdc0fde6d01a90261586a63baf3612c))
* **server:** negotiated gzip for miniapp RPC JSON responses ([#2467](https://github.com/choiceoh/Deneb/issues/2467)) ([b125ece](https://github.com/choiceoh/Deneb/commit/b125ece79929eb68832072690460aad3837d5ba9))


### 🔧 Internal

* **chat:** simplify chatbot model routing guard and a safe-call ([#2412](https://github.com/choiceoh/Deneb/issues/2412)) ([e2c0ab0](https://github.com/choiceoh/Deneb/commit/e2c0ab068d8cf5ddc699d39301444044376e1a87))
* **cleanup:** remove retired Telegram channel vestiges ([#1922](https://github.com/choiceoh/Deneb/issues/1922) follow-up) ([#2376](https://github.com/choiceoh/Deneb/issues/2376)) ([b5ac271](https://github.com/choiceoh/Deneb/commit/b5ac271371d9a51d7ff7d7033c424948500c3b82))
* **config:** remove dead TypeScript-ported config never wired in Go ([#2395](https://github.com/choiceoh/Deneb/issues/2395)) ([f4c15fa](https://github.com/choiceoh/Deneb/commit/f4c15fa395b76e663125a603948f771b32bf82c3))
* **modeltuner:** 부팅 재적용 로그 Info→Debug (튜너가 매 부팅 도는 착시 제거) ([#2430](https://github.com/choiceoh/Deneb/issues/2430)) ([da05f7a](https://github.com/choiceoh/Deneb/commit/da05f7a4ccc145d990c40a11277c6d09e0c7c4b2))
* **native:** delete orphaned cloud-direct provider plumbing ([#2387](https://github.com/choiceoh/Deneb/issues/2387)) ([5277ea5](https://github.com/choiceoh/Deneb/commit/5277ea515d9968dc71eafab9cb3ed7164403c976))
* **native:** re-wire chat repo off dead cloud-direct path ([#2379](https://github.com/choiceoh/Deneb/issues/2379)) ([f6f7744](https://github.com/choiceoh/Deneb/commit/f6f7744040ac849938c2e5df29807e0f867db761))
* **native:** remove on-device Service provider model and service-config persistence ([#2397](https://github.com/choiceoh/Deneb/issues/2397)) ([bb7dc61](https://github.com/choiceoh/Deneb/commit/bb7dc61478d7fdf7263671d511a76799a138fdb5))
* **native:** remove the retired left nav menu + dead nav callbacks ([#2377](https://github.com/choiceoh/Deneb/issues/2377)) ([cc54dab](https://github.com/choiceoh/Deneb/commit/cc54dab7bb45ac9e702213f734bf3f81e2dae65f))
* **native:** unify settings-tab cards on the grouped-card surface ([#2390](https://github.com/choiceoh/Deneb/issues/2390)) ([751fdef](https://github.com/choiceoh/Deneb/commit/751fdef2412e4ba05ea8584ea1e21cb85696ead3))
* **recall:** retire Hindsight service, sharpen wiki/diary precision ([#2462](https://github.com/choiceoh/Deneb/issues/2462)) ([21c24a0](https://github.com/choiceoh/Deneb/commit/21c24a03cbadbe0038ea036c6a31de264ad9458d))
* **tools:** split document parsers into docparse.go, unify extraction dispatch ([#2363](https://github.com/choiceoh/Deneb/issues/2363)) ([690359d](https://github.com/choiceoh/Deneb/commit/690359ddd5f9186ea94aa92f209402dde7a7d899))

## [4.28.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.27.0...deneb-v4.28.0) (2026-06-14)


### ✨ Features

* **chat:** add code_action (CodeAct) — sandboxed Python to batch read-only tools in one turn ([#2326](https://github.com/choiceoh/Deneb/issues/2326)) ([420468b](https://github.com/choiceoh/Deneb/commit/420468b6fa7ccee0d489ecf174863e0c99d5b044))
* **chat:** extract documents as markdown tables, drop external lit dependency ([#2316](https://github.com/choiceoh/Deneb/issues/2316)) ([ddfd4f8](https://github.com/choiceoh/Deneb/commit/ddfd4f8049ff78ad1ff1930ef7087e8528cfdba7))
* **chat:** re-land code_action follow-ups dropped from main ([#2329](https://github.com/choiceoh/Deneb/issues/2329)/[#2330](https://github.com/choiceoh/Deneb/issues/2330)/[#2331](https://github.com/choiceoh/Deneb/issues/2331)/[#2333](https://github.com/choiceoh/Deneb/issues/2333)) ([#2334](https://github.com/choiceoh/Deneb/issues/2334)) ([2910b0e](https://github.com/choiceoh/Deneb/commit/2910b0ec769e936539e28fdb1597f4581edaba87))
* **chat:** skipRecall on chat send/stream (focused-chat memory toggle — server side) ([#2340](https://github.com/choiceoh/Deneb/issues/2340)) ([a6ad1a5](https://github.com/choiceoh/Deneb/commit/a6ad1a5463c35c27ecaf971138150e1d30ee3bda))
* **chat:** upgrade digital PDF tables to markdown via PaddleOCR ([#2317](https://github.com/choiceoh/Deneb/issues/2317)) ([83c44f9](https://github.com/choiceoh/Deneb/commit/83c44f9e445903301c6a3477967ac32180dab493))
* **genesis:** honest evolve signal + thrash visibility ([#2336](https://github.com/choiceoh/Deneb/issues/2336)) ([7befc53](https://github.com/choiceoh/Deneb/commit/7befc53f514970b812a60b0ee4b50f69fa8bf89f))
* **genesis:** quality-gate generated skills + measure library value ([#2338](https://github.com/choiceoh/Deneb/issues/2338)) ([6db2c19](https://github.com/choiceoh/Deneb/commit/6db2c198f4c653e5c48122f5cc631d5bd92abead))
* **genesis:** reconcile orphan curator entries against catalog at startup ([#2344](https://github.com/choiceoh/Deneb/issues/2344)) ([c51c31f](https://github.com/choiceoh/Deneb/commit/c51c31f38a19f6bd6c936490886c2c3b4c900fe8))
* **goal:** agent-driven standing goals (Ralph loop) with subgoals + idempotency ledger ([#2339](https://github.com/choiceoh/Deneb/issues/2339)) ([4fb2256](https://github.com/choiceoh/Deneb/commit/4fb2256f9aea790cca7989e1c65923c39b2df5ef))
* **memory:** add miniapp.memory.move_page to reclassify wiki pages ([#2342](https://github.com/choiceoh/Deneb/issues/2342)) ([a3462d8](https://github.com/choiceoh/Deneb/commit/a3462d8218c49b14dab409cd0d031d3c10563dd7))
* **memory:** nest mail analyses under their related project ([#2343](https://github.com/choiceoh/Deneb/issues/2343)) ([c1b6b08](https://github.com/choiceoh/Deneb/commit/c1b6b0863a32dbd9fa9a1e18a2eed9de3956b50d))
* **miniapp:** make cloud-catalog models deletable from the native picker ([#2322](https://github.com/choiceoh/Deneb/issues/2322)) ([b90c88c](https://github.com/choiceoh/Deneb/commit/b90c88cbe6ec48181266c714833162ea5da18fa5))
* **miniapp:** settings as section list, push into detail ([#2335](https://github.com/choiceoh/Deneb/issues/2335)) ([924c4f5](https://github.com/choiceoh/Deneb/commit/924c4f55063cb618dc2f0823e0d4b97c3922c60e))
* **miniapp:** wormhole router status + feature toggles RPC ([#2358](https://github.com/choiceoh/Deneb/issues/2358)) ([e2f0ef4](https://github.com/choiceoh/Deneb/commit/e2f0ef4160d64a4faafeb7fd669845d7355f425c))
* **models:** drop GLM-5 Turbo from z.ai picker (superseded by GLM-5.2) ([#2320](https://github.com/choiceoh/Deneb/issues/2320)) ([b0b057c](https://github.com/choiceoh/Deneb/commit/b0b057c816da21b416c6bb81a3a204c0f9f04761))
* **models:** offer GLM-5.2 (replace GLM-5.1 in z.ai picker, add catalog entry) ([#2319](https://github.com/choiceoh/Deneb/issues/2319)) ([18bd511](https://github.com/choiceoh/Deneb/commit/18bd511ce87d5076b0559d735060b8b1394c820b))
* **native:** focused-chat memory toggle in the chat top bar ([#2346](https://github.com/choiceoh/Deneb/issues/2346)) ([4bbce5f](https://github.com/choiceoh/Deneb/commit/4bbce5f579423345b1dde027f1164a21739d0d41))
* **native:** reclassify wiki pages from the category screen ([#2347](https://github.com/choiceoh/Deneb/issues/2347)) ([4211403](https://github.com/choiceoh/Deneb/commit/4211403206aefe715a905349a75089b27da22cf9))
* **native:** separate 챗봇/업무 session lists (workspace switch via the mode pill) ([#2351](https://github.com/choiceoh/Deneb/issues/2351)) ([16cb7e6](https://github.com/choiceoh/Deneb/commit/16cb7e6352ce1e40c823b45e6e4b0b2c38ae448f))
* **native:** split fleet and version into their own settings sections ([#2359](https://github.com/choiceoh/Deneb/issues/2359)) ([6da5afe](https://github.com/choiceoh/Deneb/commit/6da5afe7189fed86f5023dee4afb0077182603f1))
* **native:** Wormhole settings tab — router status + feature toggles ([#2360](https://github.com/choiceoh/Deneb/issues/2360)) ([5db4ca4](https://github.com/choiceoh/Deneb/commit/5db4ca4da683b2596173d69048a6d82c6c56502c))
* **observe:** add 1일/7일 period switcher to the 관찰 tab ([#2324](https://github.com/choiceoh/Deneb/issues/2324)) ([999b84b](https://github.com/choiceoh/Deneb/commit/999b84bc2ed2af6b04fe68b632f1a97e5630d96d))
* **wiki:** 6-category taxonomy (프로젝트·인물·시스템·업무·사용자·기타) ([#2337](https://github.com/choiceoh/Deneb/issues/2337)) ([0bd5567](https://github.com/choiceoh/Deneb/commit/0bd5567e05dadbc0154820d5ed7aab841d221d53))
* **wiki:** auto-apply high-confidence verify findings in the dream cycle ([#2345](https://github.com/choiceoh/Deneb/issues/2345)) ([ab95883](https://github.com/choiceoh/Deneb/commit/ab9588330fd8e4df9494ef11d93ff0d7faac8ce0))
* **wiki:** clean up the category browser — w: normalize + path hierarchy ([#2325](https://github.com/choiceoh/Deneb/issues/2325)) ([ce9eeea](https://github.com/choiceoh/Deneb/commit/ce9eeea4130441b3c388d44bae39d74a37f7cd1f))
* **wormhole:** auto model selection on the same endpoint ([#2353](https://github.com/choiceoh/Deneb/issues/2353)) ([8f191ce](https://github.com/choiceoh/Deneb/commit/8f191cef01f14674113deda8d5bf388b72e7c8ce))
* **wormhole:** live config hot-reload + global effortRouting switch ([#2357](https://github.com/choiceoh/Deneb/issues/2357)) ([af1665f](https://github.com/choiceoh/Deneb/commit/af1665f4be29049e79ad3c47f5d2fe9e5fdf45f4))
* **wormhole:** local-first egress guard ([#2350](https://github.com/choiceoh/Deneb/issues/2350)) ([e3883f4](https://github.com/choiceoh/Deneb/commit/e3883f47a4d69f2f81691ccf6c0c7dc92fb0ff52))
* **wormhole:** native Anthropic via /v1/messages pass-through ([#2352](https://github.com/choiceoh/Deneb/issues/2352)) ([6d31a7e](https://github.com/choiceoh/Deneb/commit/6d31a7e140776d4e515e6009ac12085e15f910e0))
* **wormhole:** OpenAI-compatible model router — first slice ([#2349](https://github.com/choiceoh/Deneb/issues/2349)) ([68b5a3a](https://github.com/choiceoh/Deneb/commit/68b5a3a717e701c5e021051e48b97a9467cf408d))
* **wormhole:** thinking/non-thinking routing at the proxy ([#2354](https://github.com/choiceoh/Deneb/issues/2354)) ([a5ba610](https://github.com/choiceoh/Deneb/commit/a5ba6100700c2d607b602e8e15056f1d8461a526))


### 🐛 Bug Fixes

* **chat:** raise ASR transcription timeout 4m→10m for on-demand cold-load ([#2321](https://github.com/choiceoh/Deneb/issues/2321)) ([d012f30](https://github.com/choiceoh/Deneb/commit/d012f309b02b3c202439f7e998085641a076a71e))
* **chat:** wire StripThink recovery for Anthropic thinking-signature 400s ([#2327](https://github.com/choiceoh/Deneb/issues/2327)) ([cfaad37](https://github.com/choiceoh/Deneb/commit/cfaad3786b023fe52753ecad940e28ff2bd3a56d))
* **genesis:** stop evolve thrashing — drop infra-failure usage signal, disable judge thinking on dsv4 ([#2328](https://github.com/choiceoh/Deneb/issues/2328)) ([2d5910d](https://github.com/choiceoh/Deneb/commit/2d5910d14a8c6a80a71a0e4605ab961c6cfd3cbc))
* **modelrole:** vLLM /models discovery wins over deneb.json contextWindow ([#2318](https://github.com/choiceoh/Deneb/issues/2318)) ([a52878a](https://github.com/choiceoh/Deneb/commit/a52878ac21a02a464415d1ca51c512d292996019))
* **native:** center the 챗봇/업무 mode pill in the chat top bar ([#2356](https://github.com/choiceoh/Deneb/issues/2356)) ([cc5add1](https://github.com/choiceoh/Deneb/commit/cc5add1d93a8e28e802be4bcf7fc64340552ab16))
* **sparkfleet:** move GPU-backend status to a dedicated log + log only on transitions ([#2355](https://github.com/choiceoh/Deneb/issues/2355)) ([8d0d27d](https://github.com/choiceoh/Deneb/commit/8d0d27dc7c9b92d0880fe85cb2052b95e22a93ce))
* **wiki:** accept array or string for dream supersedes (parse crash) ([#2341](https://github.com/choiceoh/Deneb/issues/2341)) ([bae27e9](https://github.com/choiceoh/Deneb/commit/bae27e9f40710b38a51a3e495e8d99add2cf6350))
* **wiki:** parse dream synthesis items individually (one bad item no longer sinks the cycle) ([#2348](https://github.com/choiceoh/Deneb/issues/2348)) ([9cba4f2](https://github.com/choiceoh/Deneb/commit/9cba4f22050d25704bff67dffc82b98ff6bfabb5))
* **wiki:** strip w: namespace from dreamer page paths (category misfiling) ([#2332](https://github.com/choiceoh/Deneb/issues/2332)) ([6b29e9f](https://github.com/choiceoh/Deneb/commit/6b29e9fa066a9dbed2b7913b2aa6008e46c6d52b))


### ⚡ Performance

* **prompt:** drop the deneb-ui instruction block — 0.02% model use, server cards keep working ([#2315](https://github.com/choiceoh/Deneb/issues/2315)) ([cbf4197](https://github.com/choiceoh/Deneb/commit/cbf419738e8c74dd7d5db359898d408c62fc6928))


### 🔧 Internal

* **native:** fold deneb-ui into chat, drop standalone interactive mode ([#2313](https://github.com/choiceoh/Deneb/issues/2313)) ([aa1dbb9](https://github.com/choiceoh/Deneb/commit/aa1dbb99cd3df3ecf6fc308a8aa918eec821e493))
* **router:** generalize effort router into per-model config-driven layer ([#2323](https://github.com/choiceoh/Deneb/issues/2323)) ([5b27286](https://github.com/choiceoh/Deneb/commit/5b2728654bc826ab79f8ee63b51f7dff46b0d6cc))

## [4.27.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.26.0...deneb-v4.27.0) (2026-06-13)


### ✨ Features

* **agentlog:** log per-step effort decision and observation size ([#2305](https://github.com/choiceoh/Deneb/issues/2305)) ([bf189e5](https://github.com/choiceoh/Deneb/commit/bf189e52904718069386bb4072cc5b42bb2709b7))
* **compaction:** ACON-style learned summarizer guidelines (opt-in) ([#2303](https://github.com/choiceoh/Deneb/issues/2303)) ([b05f34b](https://github.com/choiceoh/Deneb/commit/b05f34ba52f666fe08649294d28eb82500bdb396))
* **native:** 스킬 진화 내역 탭하면 펼쳐서 자세히 보기 ([#2297](https://github.com/choiceoh/Deneb/issues/2297)) ([ad54f8d](https://github.com/choiceoh/Deneb/commit/ad54f8d3f32b94e1b034303376a43accc9e2d286))
* research-grounded recall/skill correctness + effort & proactive observability ([#2299](https://github.com/choiceoh/Deneb/issues/2299)) ([8cf3c98](https://github.com/choiceoh/Deneb/commit/8cf3c9851d949d7bd2c04725464a5ad1c71df8c6))


### 🐛 Bug Fixes

* **chat:** gateway 사용자 표면 한국어 누출 봉합 + Telegram 화석 정리 ([#2301](https://github.com/choiceoh/Deneb/issues/2301)) ([f42b308](https://github.com/choiceoh/Deneb/commit/f42b30806a8990edafbc2c8296b63d34e1f360e4))
* **chat:** stop labeling interactive native chats as scheduled auto-runs ([#2312](https://github.com/choiceoh/Deneb/issues/2312)) ([d480e9c](https://github.com/choiceoh/Deneb/commit/d480e9c3a09d341ccb50ce7a7528a667e98c1dd2))
* **chat:** 깊이 생각 중 칩을 추론 토막 대신 완결 문장으로 정제 ([#2298](https://github.com/choiceoh/Deneb/issues/2298)) ([c1485f7](https://github.com/choiceoh/Deneb/commit/c1485f7ce884493fa566c078cf9823dbc11ce472))
* **compaction:** rune-safe tool-result truncation + ctx-guard in emergency tier ([#2307](https://github.com/choiceoh/Deneb/issues/2307)) ([1b5118b](https://github.com/choiceoh/Deneb/commit/1b5118bc644e1b53b5417e34708c21d0a0464b25))
* **gmailpoll:** retry on total batch-analysis failure instead of dropping mail ([#2310](https://github.com/choiceoh/Deneb/issues/2310)) ([877fcc1](https://github.com/choiceoh/Deneb/commit/877fcc1f569f9dcd5f79db2878fb36568f64ec2e))
* **native:** drop the outer card around self-chromed deneb-ui roots ([#2293](https://github.com/choiceoh/Deneb/issues/2293)) ([3171299](https://github.com/choiceoh/Deneb/commit/3171299544a0bb884e2195a543aeba6591c971db))
* **native:** quiet per-message action buttons to hint tone ([#2290](https://github.com/choiceoh/Deneb/issues/2290)) ([20c81f3](https://github.com/choiceoh/Deneb/commit/20c81f3fb2bf9c9e66a1d14655c4da0653a231f1))


### ⚡ Performance

* **chat:** widen effort-router non-thinking window ([#2302](https://github.com/choiceoh/Deneb/issues/2302)) ([09f435b](https://github.com/choiceoh/Deneb/commit/09f435bb95b1879cd047d9fd964333c5c5404d27))


### 🔧 Internal

* **chat:** dead channel-silent 억제 메커니즘 통째 제거 ([#2309](https://github.com/choiceoh/Deneb/issues/2309)) ([97a5d10](https://github.com/choiceoh/Deneb/commit/97a5d10fdb7d4f8c76bc9fa43c5975fc0c124502))
* **chat:** dead clarify 도구 통째 제거 + message channel 예시 정리 ([#2308](https://github.com/choiceoh/Deneb/issues/2308)) ([cdfe57c](https://github.com/choiceoh/Deneb/commit/cdfe57cd206860674afde8dc5a787f2eb53d45c7))
* **chat:** LLM 프롬프트의 텔레그램 잔재 제거 + 놓친 주석 정리 ([#2311](https://github.com/choiceoh/Deneb/issues/2311)) ([87bbcde](https://github.com/choiceoh/Deneb/commit/87bbcde3f88164bc71b3341c8f168ba3cb0155dd))
* **chat:** 잔여 Telegram 화석 전수 정리 (주석·식별자·dead 분기) ([#2306](https://github.com/choiceoh/Deneb/issues/2306)) ([aa1e9cc](https://github.com/choiceoh/Deneb/commit/aa1e9ccb7a13fae123dd64385ab2db740a1090c9))
* **native:** drop per-message copy button ([#2292](https://github.com/choiceoh/Deneb/issues/2292)) ([f2ad065](https://github.com/choiceoh/Deneb/commit/f2ad065a30794c5bb39fc65de8b946fb15cf3430))
* **native:** 한국어 표면 폴리싱 — 영어 누출·톤 통일·검색 디자인 이식 ([#2304](https://github.com/choiceoh/Deneb/issues/2304)) ([0c85f34](https://github.com/choiceoh/Deneb/commit/0c85f343903eb129b9aa3bf4e49328aa42ebe2c7))

## [4.26.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.25.0...deneb-v4.26.0) (2026-06-12)


### ✨ Features

* **chat:** adaptive reasoning-effort router for dual-mode models (v1) ([#2259](https://github.com/choiceoh/Deneb/issues/2259)) ([20ca8cb](https://github.com/choiceoh/Deneb/commit/20ca8cbbbef16ef40cb993d1f2410e06a940f02a))
* **chat:** effort-router telemetry — durable agentlog decisions + run-scoped o_t feed ([#2274](https://github.com/choiceoh/Deneb/issues/2274)) ([5295237](https://github.com/choiceoh/Deneb/commit/529523759f2b0084ef318bd19c18760357f53414))
* **chat:** enforce sessions_spawn tool presets (researcher/implementer/verifier) ([#2285](https://github.com/choiceoh/Deneb/issues/2285)) ([d788c4f](https://github.com/choiceoh/Deneb/commit/d788c4fe4cfd6038b99f9c3dc0983a56df43be69))
* **chat:** stream live reasoning preview + step-boundary continuity in the waiting chip ([#2286](https://github.com/choiceoh/Deneb/issues/2286)) ([e7e417b](https://github.com/choiceoh/Deneb/commit/e7e417b78320857ac8d0899a0ab9a9ad453dcc04))
* **dev:** puppet seat — coding agent drives the gateway as its LLM (빙의 모드) ([#2268](https://github.com/choiceoh/Deneb/issues/2268)) ([d0f75ec](https://github.com/choiceoh/Deneb/commit/d0f75ec49cf63069f7063bc34c13515c120c8784))
* **llm:** Anthropic-path hardening — Complete guards, fallback cache-marker reconcile, wire fixes ([#2282](https://github.com/choiceoh/Deneb/issues/2282)) ([4b0c7f2](https://github.com/choiceoh/Deneb/commit/4b0c7f200105ba0ae6ff5c8d7b9b197f7bbf4bf0))
* **native:** skill detail screen — tap a skill for full meta, SKILL.md doc, and its evolution history ([#2279](https://github.com/choiceoh/Deneb/issues/2279)) ([1047547](https://github.com/choiceoh/Deneb/commit/104754794bd17d14c0077f4d56a1496f36cd0519))
* **observe:** surface vLLM prefix-cache hit rate from engine /metrics ([#2273](https://github.com/choiceoh/Deneb/issues/2273)) ([86c0d3c](https://github.com/choiceoh/Deneb/commit/86c0d3cac5e8f3b0e966e458597948d0ba7ac88f))
* **skill:** observe skill evolution from the native app — origin badges + lifecycle timeline ([#2271](https://github.com/choiceoh/Deneb/issues/2271)) ([14e1221](https://github.com/choiceoh/Deneb/commit/14e12210507940f5321cd2f065f4154fef143e3c))


### 🐛 Bug Fixes

* **chat:** strip baked RFC3339 timestamp prefix from user bubbles on history reload ([#2278](https://github.com/choiceoh/Deneb/issues/2278)) ([c158234](https://github.com/choiceoh/Deneb/commit/c158234020a4e57dcba4cea48be42c8b2aa1ba87))
* **llm:** surface mid-stream clean EOF as stream error instead of silent empty success ([#2275](https://github.com/choiceoh/Deneb/issues/2275)) ([4c9f5f6](https://github.com/choiceoh/Deneb/commit/4c9f5f6e3a27704b4584ac82479aa18669f4b399))
* **skills:** make advertised SKILL.md locations actually readable + compact the index ([#2284](https://github.com/choiceoh/Deneb/issues/2284)) ([1653658](https://github.com/choiceoh/Deneb/commit/16536585fb68df272add1133c0d4b4ab54bab91a))


### ⚡ Performance

* **chat:** defer 7 cold eager tools off the wire (-~2.4K tok/turn) ([#2280](https://github.com/choiceoh/Deneb/issues/2280)) ([13ce5cb](https://github.com/choiceoh/Deneb/commit/13ce5cbedea4d0d16fa7f1fb9dd36c2069e4a18e))
* **chat:** dsv4 gateway optimization pass — sampling, APC stability, thinking control ([#2287](https://github.com/choiceoh/Deneb/issues/2287)) ([eba22df](https://github.com/choiceoh/Deneb/commit/eba22df23d2f4f7979a8e716fbe587d7ab61f1e8))
* **chat:** keep vLLM APC prefix stable across turns — tail-inject recall/auto-delivery, freeze tier-1, engine cache telemetry ([#2288](https://github.com/choiceoh/Deneb/issues/2288)) ([a324542](https://github.com/choiceoh/Deneb/commit/a324542314d20ba2dc838624f4293f6b05ea8d15))
* **toolreg:** defer graphify off the eager wire (-1.2K tok/turn) + prompt-mass audit harness ([#2276](https://github.com/choiceoh/Deneb/issues/2276)) ([341a20b](https://github.com/choiceoh/Deneb/commit/341a20b05f7fe7012deca0308a4435ed71676a47))


### 🔧 Internal

* **client-android:** unify androidMain logging on DenebLog ([#2269](https://github.com/choiceoh/Deneb/issues/2269)) ([f70824a](https://github.com/choiceoh/Deneb/commit/f70824a2aea9cbbc5527a6013174174232df40a5))
* **native:** flatten skills tab view switcher — ink/hint text over hairline instead of segmented pill ([#2281](https://github.com/choiceoh/Deneb/issues/2281)) ([97c6e22](https://github.com/choiceoh/Deneb/commit/97c6e224b29417a0fbb173a80a28344b0e3fccc0))

## [4.25.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.24.0...deneb-v4.25.0) (2026-06-12)


### ✨ Features

* **llm:** OpenAI-path hardening — cached-token usage, premature-end tool flush, Complete guard, mid-stream retry ([#2270](https://github.com/choiceoh/Deneb/issues/2270)) ([061b097](https://github.com/choiceoh/Deneb/commit/061b097bfa2a36672872d928ad2f781ff2b25086))
* **native:** open proactive 업무 cards at their mirrored client:main message ([#2266](https://github.com/choiceoh/Deneb/issues/2266)) ([7723447](https://github.com/choiceoh/Deneb/commit/7723447692bd73489b0a9b0d71374d093f8965ac))


### 🐛 Bug Fixes

* **client-android:** route common-code failures through a real multiplatform logger ([#2265](https://github.com/choiceoh/Deneb/issues/2265)) ([9c95e51](https://github.com/choiceoh/Deneb/commit/9c95e516655fa778cf8a69c76ed9a59df4546b5d))


### 🔧 Internal

* **native:** complete the DenebScreenScaffold design-system rollout (phases 2–4) ([#2261](https://github.com/choiceoh/Deneb/issues/2261)) ([66c222f](https://github.com/choiceoh/Deneb/commit/66c222f254af21c28cd83cb9be77e96057137217))

## [4.24.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.23.0...deneb-v4.24.0) (2026-06-12)


### ✨ Features

* **android:** remove Kai vestiges (sponsor promo, branding links, Splinterlands) ([#1887](https://github.com/choiceoh/Deneb/issues/1887)) ([415d852](https://github.com/choiceoh/Deneb/commit/415d8525128b547366a18216589c6243c7dc2b2b))
* **android:** remove report (flag) button from bot chat messages ([#1876](https://github.com/choiceoh/Deneb/issues/1876)) ([f3824b0](https://github.com/choiceoh/Deneb/commit/f3824b09cdcf2f974896bced9579d3b859b9fe5c))
* **asr:** bias audio transcription with wiki proper nouns (people, companies, deals) ([#1848](https://github.com/choiceoh/Deneb/issues/1848)) ([dbe81a6](https://github.com/choiceoh/Deneb/commit/dbe81a64311005f459ad5a4103a40d507d1d3909))
* **calendar:** drop the video-conference link from event briefings ([#1913](https://github.com/choiceoh/Deneb/issues/1913)) ([cac441c](https://github.com/choiceoh/Deneb/commit/cac441cd0ad5bd74d49c2191325125e1643e77ed))
* **calendar:** hybrid local calendar with month view and add/edit/delete ([#2066](https://github.com/choiceoh/Deneb/issues/2066)) ([79ed0c7](https://github.com/choiceoh/Deneb/commit/79ed0c78004a3a77f8891b67920303d685bd0ac2))
* **calendar:** polish month grid, entry form, detail span (2.9.60) ([#2080](https://github.com/choiceoh/Deneb/issues/2080)) ([2e5bf20](https://github.com/choiceoh/Deneb/commit/2e5bf208c6edecce22612ce0ae854124b02911f0))
* **calendar:** support multi-day events end-to-end ([#2074](https://github.com/choiceoh/Deneb/issues/2074)) ([9c49fbf](https://github.com/choiceoh/Deneb/commit/9c49fbfa4080975d5f1e3d9b14eb01e96e47a677))
* **calendar:** swipe month nav and add-from-empty-day (2.9.64) ([#2092](https://github.com/choiceoh/Deneb/issues/2092)) ([c413fbd](https://github.com/choiceoh/Deneb/commit/c413fbd669bccc36fbd756a376f697d319e64be2))
* **capture:** audio capture — share a recording to Deneb, transcribed via VibeVoice-ASR ([#1847](https://github.com/choiceoh/Deneb/issues/1847)) ([59ce627](https://github.com/choiceoh/Deneb/commit/59ce6273e19df00f35d96daa7541133f32215382))
* **capture:** image capture — share a photo/screenshot to Deneb, gateway OCRs it ([#1842](https://github.com/choiceoh/Deneb/issues/1842)) ([71d68f7](https://github.com/choiceoh/Deneb/commit/71d68f70fdf701411236ebc0886ab269415fd04e))
* **chat:** add /help slash command discovery ([c1b32d7](https://github.com/choiceoh/Deneb/commit/c1b32d71656677b4eb2fb79bb58fce161bdde070))
* **chat:** add content-hash anchored edits to read/edit tools ([#1820](https://github.com/choiceoh/Deneb/issues/1820)) ([d268e78](https://github.com/choiceoh/Deneb/commit/d268e78a1aa62655204892958451efa2d662c3a7))
* **chat:** add evidence-citation and per-project memory discipline to analysis ([acf53cd](https://github.com/choiceoh/Deneb/commit/acf53cd1eda0cfc44cda9b37d6bb7fe477796109))
* **chat:** add pinned facts slash commands ([#2063](https://github.com/choiceoh/Deneb/issues/2063)) ([ad042f4](https://github.com/choiceoh/Deneb/commit/ad042f41c4e0b879947f1cbfb2f720491592b272))
* **chat:** auto-title native chat sessions via lightweight LLM ([#1917](https://github.com/choiceoh/Deneb/issues/1917)) ([6203b3b](https://github.com/choiceoh/Deneb/commit/6203b3b90549fbea637e74b6c01422d0dfe03435))
* **chat:** bootstrap reusable workflows ([#1940](https://github.com/choiceoh/Deneb/issues/1940)) ([ec9abfa](https://github.com/choiceoh/Deneb/commit/ec9abfaebc1da382fd3ac8fd2fad16f091ce5858))
* **chat:** calendar agent tool — schedule, ambient awareness, meeting prep, free slots ([#2117](https://github.com/choiceoh/Deneb/issues/2117)) ([0e0f125](https://github.com/choiceoh/Deneb/commit/0e0f1251a8ada450c826ef3678fc916a8dd6bc47))
* **chat:** deliver mail analyses as collapsed tap-to-expand cards in 업무 chat ([#2238](https://github.com/choiceoh/Deneb/issues/2238)) ([bcfb9f2](https://github.com/choiceoh/Deneb/commit/bcfb9f2429a166e9bf745e7827682b4b65c6d672))
* **chat:** improve answer readability and bump to 2.9.57 ([#2070](https://github.com/choiceoh/Deneb/issues/2070)) ([f6ab876](https://github.com/choiceoh/Deneb/commit/f6ab876b2dadd0d81c2b60f37119b38c001b4d98))
* **chat:** narrate tool targets, failures, and a post-turn footprint in the waiting chip ([#2231](https://github.com/choiceoh/Deneb/issues/2231)) ([c24efa8](https://github.com/choiceoh/Deneb/commit/c24efa80f84ec936ddc5f55a01c6e8cff0078f51))
* **chat:** observe tool — the agent inspects its own runtime ([#2138](https://github.com/choiceoh/Deneb/issues/2138)) ([370e711](https://github.com/choiceoh/Deneb/commit/370e7110994fc399e0ae78e646744cfd75eee3df))
* **chat:** per-source recall attribution — bench table + per-turn source/latency logging ([#2215](https://github.com/choiceoh/Deneb/issues/2215)) ([fddd784](https://github.com/choiceoh/Deneb/commit/fddd7842cb8d60ffb103162075aaa6497ba62817))
* **chat:** per-turn reasoning sandwich (planning-turn thinking boost) ([dda8d4b](https://github.com/choiceoh/Deneb/commit/dda8d4bb649f681f20dbdfda9f4a19a02773a0ab))
* **chat:** rank fetch_tools deferred-tool search with BM25 ([#1811](https://github.com/choiceoh/Deneb/issues/1811)) ([d6a4bac](https://github.com/choiceoh/Deneb/commit/d6a4bacbee36ded40ba7a2b4c3b1aa9dce30b8ff))
* **chat:** re-register the morning_letter tool — re-wire the orphaned collector ([#2201](https://github.com/choiceoh/Deneb/issues/2201)) ([20b8667](https://github.com/choiceoh/Deneb/commit/20b86679d5faa0a96819f89eddf2d78109c26cb0))
* **chat:** recall quality benchmark + two recall bugs it caught ([#2205](https://github.com/choiceoh/Deneb/issues/2205)) ([d3f5671](https://github.com/choiceoh/Deneb/commit/d3f5671a01ebfe8ec22540c167d8073e5d8dde95))
* **chat:** rewire inbound link enrichment to the native client send paths ([#2222](https://github.com/choiceoh/Deneb/issues/2222)) ([224ba48](https://github.com/choiceoh/Deneb/commit/224ba480c027b594c32a063d7a4893241891364e))
* **chat:** stream live tool/thinking progress to the native waiting chip ([#2225](https://github.com/choiceoh/Deneb/issues/2225)) ([88a314f](https://github.com/choiceoh/Deneb/commit/88a314f3b8814eedef3d355743714dce4c1335a3))
* **chat:** surface model fallback badge and fall back on model stalls ([#1830](https://github.com/choiceoh/Deneb/issues/1830)) ([3aa06ee](https://github.com/choiceoh/Deneb/commit/3aa06eed8f92396572c51da75d60a181c2ad5c95))
* **chat:** surface prompt-cache hit ratio in /status ([97c0121](https://github.com/choiceoh/Deneb/commit/97c01216e624d9e970425eef9b47fc4de52a5f9d))
* **chat:** unify contacts (인물) output into the shared recall format ([03a925d](https://github.com/choiceoh/Deneb/commit/03a925d5f58d9a55e662bbb6d18451d8f137f203))
* **chat:** unify memory-search result format and ref scheme ([dc16b88](https://github.com/choiceoh/Deneb/commit/dc16b8864df13f5e605953f28326f43a63e6a4bc))
* **chat:** verification gate — mutating runs must verify before finishing ([#2223](https://github.com/choiceoh/Deneb/issues/2223)) ([4bad93f](https://github.com/choiceoh/Deneb/commit/4bad93f4be7e1a137d39a6b45dc4937462571c07))
* **client-android:** add a 캡처 group to the drawer (image OCR, audio transcribe, voice) ([#1853](https://github.com/choiceoh/Deneb/issues/1853)) ([d45bfc3](https://github.com/choiceoh/Deneb/commit/d45bfc32af5bd8781e1bbcdafae9aa3af085becb))
* **client-android:** add Gemma icon and real MiniMax logo ([#1890](https://github.com/choiceoh/Deneb/issues/1890)) ([df2cc0d](https://github.com/choiceoh/Deneb/commit/df2cc0d4573cdab2125f875455dc1c050ed01f4b))
* **client-android:** add OpenAI-compatible models from settings ([#1939](https://github.com/choiceoh/Deneb/issues/1939)) ([33d71c6](https://github.com/choiceoh/Deneb/commit/33d71c6b61627c6fa7c15ea828de42bd4531eb5c))
* **client-android:** add Qwen, StepFun, and Xiaomi MiMo model icons ([#1888](https://github.com/choiceoh/Deneb/issues/1888)) ([99fa728](https://github.com/choiceoh/Deneb/commit/99fa7284c0667d35ac964fa3f2f120304bdb5b52))
* **client-android:** calendar depth + wiki page meta-edit (Tier 2) ([#1824](https://github.com/choiceoh/Deneb/issues/1824)) ([8de54a4](https://github.com/choiceoh/Deneb/commit/8de54a4bcaab9e8de7b7e9c2a3c8aec584fa6be3))
* **client-android:** choose which apps Deneb captures notifications from (134/2.9.11) ([#1870](https://github.com/choiceoh/Deneb/issues/1870)) ([19741a2](https://github.com/choiceoh/Deneb/commit/19741a20653b586b5c3aacc865294dec63ca911a))
* **client-android:** clearer loading/error/empty states across native screens (146/2.9.23) ([#1904](https://github.com/choiceoh/Deneb/issues/1904)) ([f36210c](https://github.com/choiceoh/Deneb/commit/f36210c9018e7c814fc6a4ab324efd10e410cc3e))
* **client-android:** color-coded response status in the config model tab ([#1883](https://github.com/choiceoh/Deneb/issues/1883)) ([b139e51](https://github.com/choiceoh/Deneb/commit/b139e5122d9e9736ebf949a9e43a443577e4090b))
* **client-android:** copy-confirmation on code blocks ([6869f53](https://github.com/choiceoh/Deneb/commit/6869f536eb329922acd6d926b8b65987756815c6))
* **client-android:** cron detail screen — schedule, instruction, delivery, state; enable/run/delete ([#1828](https://github.com/choiceoh/Deneb/issues/1828)) ([1f90917](https://github.com/choiceoh/Deneb/commit/1f909178d84148f46a570f07c173ef1579ee4d61))
* **client-android:** deepen native client — mail/calendar/search/people/wiki/settings + markdown ([#1815](https://github.com/choiceoh/Deneb/issues/1815)) ([11223d1](https://github.com/choiceoh/Deneb/commit/11223d14d30fac675c6322622ee24a7a0fa14cbc))
* **client-android:** deliver proactive reports (모닝레터·메일분석) to native app (151/2.9.28) ([#1912](https://github.com/choiceoh/Deneb/issues/1912)) ([c5c89d7](https://github.com/choiceoh/Deneb/commit/c5c89d74d46dcf25f7567a2466685770abc803c6))
* **client-android:** dismiss keyboard when a drawer opens ([#1905](https://github.com/choiceoh/Deneb/issues/1905)) ([94b4297](https://github.com/choiceoh/Deneb/commit/94b429793a0e6c8e28020f46bf7a7e2ee82eb394))
* **client-android:** drop home-screen chips and pin UI font to Pretendard ([#1825](https://github.com/choiceoh/Deneb/issues/1825)) ([c22689e](https://github.com/choiceoh/Deneb/commit/c22689e2fd58014effed98ee5ef91922e35893bc))
* **client-android:** fold cron/system sessions into a drawer group ([a804a43](https://github.com/choiceoh/Deneb/commit/a804a430b2759b428db468dc8ce8d99ca9d13358))
* **client-android:** forward a captured notification's image with its text context ([#1874](https://github.com/choiceoh/Deneb/issues/1874)) ([6fd74ee](https://github.com/choiceoh/Deneb/commit/6fd74ee17524c1a130ba811202f1f6b0de6c79d0))
* **client-android:** high-grade polish of the chat markdown renderer ([64961af](https://github.com/choiceoh/Deneb/commit/64961afe0b1a21a0e9ca1a19d0314677adc28a46))
* **client-android:** home-screen widget — next meeting + unread mail at a glance ([#1851](https://github.com/choiceoh/Deneb/issues/1851)) ([60f77e7](https://github.com/choiceoh/Deneb/commit/60f77e74576109bf686195bde93ae1891e1f5882))
* **client-android:** hybrid design-system foundation + calendar-event pilot (148/2.9.25) ([#1908](https://github.com/choiceoh/Deneb/issues/1908)) ([c59e976](https://github.com/choiceoh/Deneb/commit/c59e9760c144b1636620ada2ed0bd4f53bf4b83e))
* **client-android:** kai-ui robustness and a chart node for interactive mode ([#1929](https://github.com/choiceoh/Deneb/issues/1929)) ([25db806](https://github.com/choiceoh/Deneb/commit/25db8069981ea11d5c7d092ba68e598d0f6132d7))
* **client-android:** mail-detail depth, person dossier, pull-to-refresh + haptics ([#1821](https://github.com/choiceoh/Deneb/issues/1821)) ([19c3b60](https://github.com/choiceoh/Deneb/commit/19c3b604cd52389da50f131e02cef8460b75cec9))
* **client-android:** mask client token + card the model-add form ([#1956](https://github.com/choiceoh/Deneb/issues/1956)) ([9409223](https://github.com/choiceoh/Deneb/commit/9409223de41a971a8e03743f80579aab8e9d8c12))
* **client-android:** move settings + history into the left drawer, redesign topic tabs ([#1852](https://github.com/choiceoh/Deneb/issues/1852)) ([6650477](https://github.com/choiceoh/Deneb/commit/6650477900d781102ff7818121bab23d3aeb6b5d))
* **client-android:** move topic switcher to a right-side drawer ([#1877](https://github.com/choiceoh/Deneb/issues/1877)) ([474db07](https://github.com/choiceoh/Deneb/commit/474db07f405b07b125498bf18cb31a93534f2ed8))
* **client-android:** move work feed into a notification sheet ([#1959](https://github.com/choiceoh/Deneb/issues/1959)) ([2bbebaf](https://github.com/choiceoh/Deneb/commit/2bbebaf1a1d19b98721aa7fe9ac71738603197f5))
* **client-android:** mute struck text and round markdown images ([7e83196](https://github.com/choiceoh/Deneb/commit/7e8319697b1aaec1d8f88a4ebaf7f3b4b51c4175))
* **client-android:** name cron/system/boot sessions from their key ([889d3b1](https://github.com/choiceoh/Deneb/commit/889d3b119ce21b15ef962523c3b52fdac59d2648))
* **client-android:** native Kai-based client (Android/iOS/Mac) + gateway support ([#1808](https://github.com/choiceoh/Deneb/issues/1808)) ([16f92e4](https://github.com/choiceoh/Deneb/commit/16f92e4cfe60cc22596ec2518f90efe5600161b0))
* **client-android:** notification capture tab — read other apps' notifications, tap to triage ([#1840](https://github.com/choiceoh/Deneb/issues/1840)) ([ee8cfd6](https://github.com/choiceoh/Deneb/commit/ee8cfd6ccea0d7dd0cc8f83dee2547a50b56c23c))
* **client-android:** OTA in-app APK download + install ([eaa3ecd](https://github.com/choiceoh/Deneb/commit/eaa3ecdd5be2c2ed25be13e433484789895e25f0))
* **client-android:** patch notes in settings version card ([#1849](https://github.com/choiceoh/Deneb/issues/1849)) ([9fee986](https://github.com/choiceoh/Deneb/commit/9fee98698b085ac45baf76eb5373126b927f11c9))
* **client-android:** per-model brand icons in the gateway model switcher ([#1879](https://github.com/choiceoh/Deneb/issues/1879)) ([c246c24](https://github.com/choiceoh/Deneb/commit/c246c245a38e7aa3bb444045481775ce57fd94b8))
* **client-android:** per-role model pickers (main/lightweight/fallback) ([#1834](https://github.com/choiceoh/Deneb/issues/1834)) ([2746753](https://github.com/choiceoh/Deneb/commit/274675383da45c7446c7a3fb10ec74b8244ed74c))
* **client-android:** pill-style config tabs (no underline) ([#1829](https://github.com/choiceoh/Deneb/issues/1829)) ([905f71b](https://github.com/choiceoh/Deneb/commit/905f71ba9e0bfaaeb0c3fde2ccf377e661f0782a))
* **client-android:** polish chat streaming scroll, caret, and spacing ([ca5827f](https://github.com/choiceoh/Deneb/commit/ca5827f5f6a9f7851dccf2509e5b89be15127f93))
* **client-android:** polish finish — input-bar a11y + config list motion (129/2.9.6) ([#1860](https://github.com/choiceoh/Deneb/issues/1860)) ([f657002](https://github.com/choiceoh/Deneb/commit/f657002a9d62612cef6efe091d8c36d240542492))
* **client-android:** polish v2 — streaming cursor, haptics, list motion (127/2.9.4) ([#1858](https://github.com/choiceoh/Deneb/issues/1858)) ([4916d75](https://github.com/choiceoh/Deneb/commit/4916d75c104c516d08bf0b33642fb335e2fc0fe1))
* **client-android:** real model icon, Kai-style card settings, in-app update check ([#1817](https://github.com/choiceoh/Deneb/issues/1817)) ([880a173](https://github.com/choiceoh/Deneb/commit/880a17305a8a945a8c4dee486f62f3fef345d9c2))
* **client-android:** rebrand + calendar, Gmail triage, and gateway surfaces ([#1813](https://github.com/choiceoh/Deneb/issues/1813)) ([6421ba1](https://github.com/choiceoh/Deneb/commit/6421ba12a5238e232d8d58ef235c1869228b497d))
* **client-android:** recent-diary timeline screen ([a1651a8](https://github.com/choiceoh/Deneb/commit/a1651a8cb1c9d9091900e240e7e0b7fb484318be))
* **client-android:** recoverable errors, write-action feedback and a11y across native screens (147/2.9.24) ([#1907](https://github.com/choiceoh/Deneb/issues/1907)) ([55d2b19](https://github.com/choiceoh/Deneb/commit/55d2b194016212e21e2c00e5f54c0c1d83901a9c))
* **client-android:** remove unused video-conference (Meet) join from calendar (149/2.9.26) ([#1911](https://github.com/choiceoh/Deneb/issues/1911)) ([cb2b428](https://github.com/choiceoh/Deneb/commit/cb2b4287174dcd2fa948d2be551e0eefac665885))
* **client-android:** render GFM task lists as checkboxes ([b251bf1](https://github.com/choiceoh/Deneb/commit/b251bf152976d97c2a7431e3a35260df0a268676))
* **client-android:** request the panel's fastest display mode for 120Hz scrolling ([d2ee5b0](https://github.com/choiceoh/Deneb/commit/d2ee5b0e0053232cdae2fec3feabdad829928db3))
* **client-android:** richer code syntax highlighting ([e23398b](https://github.com/choiceoh/Deneb/commit/e23398b210f1b3bea171bf60eec040e9f6bd5373))
* **client-android:** right session drawer edge-swipe + typographic redesign ([#1903](https://github.com/choiceoh/Deneb/issues/1903)) ([03a9911](https://github.com/choiceoh/Deneb/commit/03a9911a8e5568945e115a2d633168a607c55bc9))
* **client-android:** right-side session drawer replacing the history bottom sheet ([#1896](https://github.com/choiceoh/Deneb/issues/1896)) ([e82d8f2](https://github.com/choiceoh/Deneb/commit/e82d8f2f901a2cea543126a79325ad5be6a1359d))
* **client-android:** share-sheet capture — shared text into the Deneb chat ([#1837](https://github.com/choiceoh/Deneb/issues/1837)) ([4961b3d](https://github.com/choiceoh/Deneb/commit/4961b3d137350bc636cd4b35598ef56ca9f0ee38))
* **client-android:** share-sheet capture — shared text into the Deneb chat ([#1839](https://github.com/choiceoh/Deneb/issues/1839)) ([8fa8158](https://github.com/choiceoh/Deneb/commit/8fa815850d0930e6c8e06a3dd6f4d6117628e70a))
* **client-android:** slightly reduce body text size and line height (156/2.9.33) ([#1966](https://github.com/choiceoh/Deneb/issues/1966)) ([49fe4b5](https://github.com/choiceoh/Deneb/commit/49fe4b5fefc6adc24eb2566f0d3fb01994f31317))
* **client-android:** stream native chat responses token-by-token ([#1841](https://github.com/choiceoh/Deneb/issues/1841)) ([62ad230](https://github.com/choiceoh/Deneb/commit/62ad230033a265e8815a5a5671e659556130d70c))
* **client-android:** swipe between settings tabs ([#1943](https://github.com/choiceoh/Deneb/issues/1943)) ([2887e2f](https://github.com/choiceoh/Deneb/commit/2887e2fe5f37e9770f2b74d13f4875bfb98584a6))
* **client-android:** toggle between auto-inject and manual injection of captured notifications ([#1875](https://github.com/choiceoh/Deneb/issues/1875)) ([07086b2](https://github.com/choiceoh/Deneb/commit/07086b2f576bda96ab4e0cf146d5ef295484b11f))
* **client-android:** triage captured notifications instantly (136/2.9.13) ([#1873](https://github.com/choiceoh/Deneb/issues/1873)) ([315691e](https://github.com/choiceoh/Deneb/commit/315691e6910e67f4e1811e1bb63105d6002aee23))
* **client-android:** UI polish — color scheme, typography, skeletons, haptics ([#1854](https://github.com/choiceoh/Deneb/issues/1854)) ([37b4b2f](https://github.com/choiceoh/Deneb/commit/37b4b2f08e8603ccdf255daa6adbb9c6e2e37b85))
* **client-android:** unify brand borders into a slow iridescent aurora sweep ([#1855](https://github.com/choiceoh/Deneb/issues/1855)) ([25fa08c](https://github.com/choiceoh/Deneb/commit/25fa08c935f6496017ba61b567bf43b88106e3d5))
* **client-android:** voice capture app shortcut — speak to capture into Deneb ([#1843](https://github.com/choiceoh/Deneb/issues/1843)) ([0bacf17](https://github.com/choiceoh/Deneb/commit/0bacf170cb2b532e50bcd56d639696e238bcf1ef))
* **client-android:** widen session-drawer open swipe to the right half (153/2.9.30) ([#1916](https://github.com/choiceoh/Deneb/issues/1916)) ([af0cf8d](https://github.com/choiceoh/Deneb/commit/af0cf8dad32172d384142638f0d646d4a97e855e))
* **client-android:** wiki category browser (categories + per-category pages) ([#1831](https://github.com/choiceoh/Deneb/issues/1831)) ([fdcd5bd](https://github.com/choiceoh/Deneb/commit/fdcd5bdfaf07568bfc7d5bb789afa87f32fc6ddd))
* **client:** mini-app type-menu drawer and thin typography ([ed4d815](https://github.com/choiceoh/Deneb/commit/ed4d815ed543e8b3d541d5e6f6c0457eec7f8d81))
* **contacts:** address-book DB with phone lookup, search, and ASR hotwords ([#1897](https://github.com/choiceoh/Deneb/issues/1897)) ([4c17b48](https://github.com/choiceoh/Deneb/commit/4c17b48c3fa1983919c0946a83cfd0ac9c52aa10))
* **contacts:** enrich existing wiki people from the android address book ([#1892](https://github.com/choiceoh/Deneb/issues/1892)) ([9a457fc](https://github.com/choiceoh/Deneb/commit/9a457fcfef6f27f190fbfcaf6eff21e4bb7f599b))
* **cron:** pin 주간업무보고 form for /weekly cron, keep LLM as writer ([#1938](https://github.com/choiceoh/Deneb/issues/1938)) ([06a0817](https://github.com/choiceoh/Deneb/commit/06a081767a80b1f24e97f1961cd65be6513df2e1))
* deliver weekly 주간업무보고 form image to native 업무 chat ([#1944](https://github.com/choiceoh/Deneb/issues/1944)) ([96262e2](https://github.com/choiceoh/Deneb/commit/96262e2d3d88460397f91bcddbe01b42c2a35166))
* **denebui:** date/time picker inputs + calm placeholder for streaming UI fences ([#2192](https://github.com/choiceoh/Deneb/issues/2192)) ([a9763b1](https://github.com/choiceoh/Deneb/commit/a9763b19dd65ab5997d32611313a51440d750780))
* **denebui:** required-field validation, keyboard hints, select placeholder ([#2189](https://github.com/choiceoh/Deneb/issues/2189)) ([f369d2b](https://github.com/choiceoh/Deneb/commit/f369d2b02222089a2a53367dd060cc3787e923dd))
* **dev:** add headless live native-app harness for agent verification ([#1950](https://github.com/choiceoh/Deneb/issues/1950)) ([a0f6730](https://github.com/choiceoh/Deneb/commit/a0f67304a9cbe6a257ef7d0018ae5ae269cf2b0b))
* **dev:** DGX-aware dev env — GO_PAR OOM guard, DENEB_INSTANCE isolation, data-gen CI check ([#1857](https://github.com/choiceoh/Deneb/issues/1857)) ([6bcd7f1](https://github.com/choiceoh/Deneb/commit/6bcd7f1f63f74bfbe8c1a69f6ead7d377e583436))
* **dev:** OCR-driven find/assert/taptext for the native live harness ([6726cbe](https://github.com/choiceoh/Deneb/commit/6726cbe3a90d86f2035a00f856c378f18f439f43))
* **dropbox:** Dropbox integration with proactive automation ([#2091](https://github.com/choiceoh/Deneb/issues/2091)) ([b080fab](https://github.com/choiceoh/Deneb/commit/b080fab51b4c6f8f50d0dddebcfd85f0352883ec))
* **fleet:** manage SparkFleet from the native app — gateway passthrough + 플릿 tab ([#2256](https://github.com/choiceoh/Deneb/issues/2256)) ([23da3ec](https://github.com/choiceoh/Deneb/commit/23da3ec9c22a3dbbfb301f656b4171ca6f156639))
* **fleet:** phone alerts, 모델 tab, launch overrides, job cancel ([#2258](https://github.com/choiceoh/Deneb/issues/2258)) ([f8c762c](https://github.com/choiceoh/Deneb/commit/f8c762cd70e257e41a9c06d44bedcacc835bead6))
* **genesis:** record skill usage in the chat turn loop ([#2151](https://github.com/choiceoh/Deneb/issues/2151)) ([0dc065c](https://github.com/choiceoh/Deneb/commit/0dc065cc891abe12199f3c396a5a5938a67a7a01))
* **genesis:** revive the self-evolution loop (skill review fix + every-turn recall) ([#1932](https://github.com/choiceoh/Deneb/issues/1932)) ([21d8950](https://github.com/choiceoh/Deneb/commit/21d8950c4a804da0db2c02762c70f0fda4b8de6a))
* **gmail:** accept client token in attachment download query ([#1819](https://github.com/choiceoh/Deneb/issues/1819)) ([76b30dc](https://github.com/choiceoh/Deneb/commit/76b30dc515651d874d984d97bce960aebce24578))
* **gmail:** analysis verdict layers over the heuristic row priority ([#2232](https://github.com/choiceoh/Deneb/issues/2232)) ([73158b7](https://github.com/choiceoh/Deneb/commit/73158b7f38b5f8cdebd1effa895b2a323a1e988e))
* **gmail:** heuristic mail priority tiers + native inbox dot ([#2221](https://github.com/choiceoh/Deneb/issues/2221)) ([f78f44f](https://github.com/choiceoh/Deneb/commit/f78f44fadb77fa1a88179e1c144a934db407247a))
* **gmail:** PaddleOCR-VL attachment OCR + sidecar-model docs ([#1838](https://github.com/choiceoh/Deneb/issues/1838)) ([ce3f6b5](https://github.com/choiceoh/Deneb/commit/ce3f6b5f2fa7abca7d946cd26363d2af92175cde))
* **gmailpoll:** silent mode — pre-warm mail-detail cache without chat delivery ([#2149](https://github.com/choiceoh/Deneb/issues/2149)) ([0a5e8f0](https://github.com/choiceoh/Deneb/commit/0a5e8f0491ceea3216f59e0a0ec534a3ca2a0a62))
* **gmail:** tappable links, copyable body, full-body view, richer HTML-to-text ([#2183](https://github.com/choiceoh/Deneb/issues/2183)) ([0cea155](https://github.com/choiceoh/Deneb/commit/0cea155e2df489d56920ee64e4578df131572b80))
* **infra:** SparkFleet control-plane readiness for GPU backends ([#2121](https://github.com/choiceoh/Deneb/issues/2121)) ([e098bb5](https://github.com/choiceoh/Deneb/commit/e098bb542b2925d93eed6b8624a95b77a3b01167))
* instant push of proactive 업무 reports to native client (133/2.9.10) ([#1867](https://github.com/choiceoh/Deneb/issues/1867)) ([fc0dc6e](https://github.com/choiceoh/Deneb/commit/fc0dc6e4f4d5ae1c6c5f6f18266535f8962075d3))
* **markdown:** autolink, emphasis flanking, task-list AST, html br (2.9.60) ([#2079](https://github.com/choiceoh/Deneb/issues/2079)) ([401b5f7](https://github.com/choiceoh/Deneb/commit/401b5f747768e599a096d931fe27559918338dea))
* **markdown:** unicode bullets, box-drawing rules, circled numbers (2.9.63) ([#2084](https://github.com/choiceoh/Deneb/issues/2084)) ([4c55c8c](https://github.com/choiceoh/Deneb/commit/4c55c8ccc9c56d8eb30879507107254a4b4c06cb))
* **media:** add watch tool — let the agent see and hear videos ([#1826](https://github.com/choiceoh/Deneb/issues/1826)) ([898fee2](https://github.com/choiceoh/Deneb/commit/898fee22dbd8fa6b239b4259ef48cd94b36543d1))
* **meeting-minutes:** auto-archive minutes to the tracked Gmail (+ file/email delivery) ([#1885](https://github.com/choiceoh/Deneb/issues/1885)) ([01e1224](https://github.com/choiceoh/Deneb/commit/01e1224c55679941aa12958dd8ea22327330c74e))
* **meeting-minutes:** let the user correct a wrong speaker mapping ([#1884](https://github.com/choiceoh/Deneb/issues/1884)) ([38c897d](https://github.com/choiceoh/Deneb/commit/38c897d2c855c1bd5f90099ef9f1a072bb4b2e66))
* **meeting-minutes:** map speakers, link the meeting series, update deals ([#1882](https://github.com/choiceoh/Deneb/issues/1882)) ([9210469](https://github.com/choiceoh/Deneb/commit/92104691938ee1d7e0ee6ac82d8ec9cbb2726e6c))
* **meeting-minutes:** turn shared recordings into meeting minutes + analysis ([#1880](https://github.com/choiceoh/Deneb/issues/1880)) ([46b2b3e](https://github.com/choiceoh/Deneb/commit/46b2b3e2f0b249c4348fc7e088dd97ce6474cd7d))
* **memory:** daily offsite backup + wiki git versioning ([#2191](https://github.com/choiceoh/Deneb/issues/2191)) ([90f0838](https://github.com/choiceoh/Deneb/commit/90f08380905113b71ed4d2838b8bb7a3b4aeaf57))
* **memory:** multi-select delete on category pages ([#1924](https://github.com/choiceoh/Deneb/issues/1924)) ([73ede5f](https://github.com/choiceoh/Deneb/commit/73ede5fea40925ac5e232a8d1bb7307fead271c4))
* **miniapp:** add Appearance and Agent settings tabs ([#2161](https://github.com/choiceoh/Deneb/issues/2161)) ([3c4b113](https://github.com/choiceoh/Deneb/commit/3c4b113ca0c292f8a80766447e7d23063497d9d0))
* **miniapp:** add calendar to-do feature (store, RPC, native UI) ([#2111](https://github.com/choiceoh/Deneb/issues/2111)) ([c462a34](https://github.com/choiceoh/Deneb/commit/c462a34a9a84bb8a81f80ed6f452d5e7764f2bb9))
* **miniapp:** add read-only Skills tab to settings ([#2118](https://github.com/choiceoh/Deneb/issues/2118)) ([603917a](https://github.com/choiceoh/Deneb/commit/603917acd3c3aee2a89564fb5f14989c2735e059))
* **miniapp:** explain model roles via a "?" tooltip in settings ([#2135](https://github.com/choiceoh/Deneb/issues/2135)) ([a61bf64](https://github.com/choiceoh/Deneb/commit/a61bf64ba68e24517d062f43914f9816e160ccf5))
* **miniapp:** generate Kotlin types for analysis + sender-context ([84a9450](https://github.com/choiceoh/Deneb/commit/84a94502c76dc0a82a5204c0c7b2676ef0a63a94))
* **miniapp:** generate Kotlin wire types for gmail mail shapes ([e7b738f](https://github.com/choiceoh/Deneb/commit/e7b738f53e91a393c117c59d755a8aa6f5a2c8e2))
* **miniapp:** generate Kotlin wire types for session shapes ([9653182](https://github.com/choiceoh/Deneb/commit/96531822871a9ab078429135b62cd9cabc8f1148))
* **miniapp:** generate Kotlin wire types from Go //deneb:wire structs ([7be82ea](https://github.com/choiceoh/Deneb/commit/7be82ea2a5e0737b084cd59d07f80ff8cb584f87))
* **miniapp:** remove topic-docs settings tab — knowledge docs are edited via chat ([#2179](https://github.com/choiceoh/Deneb/issues/2179)) ([540ba30](https://github.com/choiceoh/Deneb/commit/540ba3049a2265a6beb7360185873877315685c8))
* **miniapp:** role-assignment summary + provider grouping in model tab ([#2140](https://github.com/choiceoh/Deneb/issues/2140)) ([38e9272](https://github.com/choiceoh/Deneb/commit/38e92728765d97e164e0b3bfa30472d4a38db23b))
* **miniapp:** run full 2-stage analysis pipeline for native mail analyze ([15e8387](https://github.com/choiceoh/Deneb/commit/15e8387126fc5995943637baa2f0fda75bc6dfdb))
* **modelrole:** model capability registry, context-budget clamping, fallback circuit breaker ([#2163](https://github.com/choiceoh/Deneb/issues/2163)) ([667bd36](https://github.com/choiceoh/Deneb/commit/667bd36da0f4562e733976a7c3795dc693fb5824))
* **modelrole:** model profiles — fix step3p7 reasoning misclassification ([2f65e79](https://github.com/choiceoh/Deneb/commit/2f65e7941f59ee4553b38c121dc0536f20aad973))
* **modelrole:** split local AI into tiny/light/analysis quality tiers ([d74ede3](https://github.com/choiceoh/Deneb/commit/d74ede3ac8a71e8df74b9617c1fa0120530ff75d))
* **models:** expose tiny/analysis tiers in the per-role model picker ([#2065](https://github.com/choiceoh/Deneb/issues/2065)) ([a7cdf4f](https://github.com/choiceoh/Deneb/commit/a7cdf4f8ab59ff81910a04013c58fe4e70fdf93f))
* **modeltuner:** close measurement gaps + surface tuner data in the native model picker ([#2190](https://github.com/choiceoh/Deneb/issues/2190)) ([f5891b5](https://github.com/choiceoh/Deneb/commit/f5891b558fa944554c82bb12a904f45aede97be4))
* **modeltuner:** per-model auto-tuning loop + sampling profiles (follow-up to [#2163](https://github.com/choiceoh/Deneb/issues/2163)) ([#2167](https://github.com/choiceoh/Deneb/issues/2167)) ([f8ba8dc](https://github.com/choiceoh/Deneb/commit/f8ba8dc02e04efd04dfa13cea888cab97c8ac19b))
* native client topic switcher + left navigation drawer ([#1845](https://github.com/choiceoh/Deneb/issues/1845)) ([ae9e891](https://github.com/choiceoh/Deneb/commit/ae9e891ec088d8697db6cb5b010f5e2dafbd6072))
* **native:** add haptic feedback to calendar and wiki edit screens ([#2073](https://github.com/choiceoh/Deneb/issues/2073)) ([b62f1b0](https://github.com/choiceoh/Deneb/commit/b62f1b02802f5087d0f0bb73c40628d094331edf))
* **native:** add slider notch and refresh-button haptics ([#2086](https://github.com/choiceoh/Deneb/issues/2086)) ([3ab7482](https://github.com/choiceoh/Deneb/commit/3ab748230e7df74d06fa4e29ec30a45d352ab664))
* **native:** body-line analysis teaser + auth-expired model health state ([#2193](https://github.com/choiceoh/Deneb/issues/2193)) ([ffe9666](https://github.com/choiceoh/Deneb/commit/ffe9666d233945a2cee20c35e5ec72c28cb1e518))
* **native:** center Deneb content at a max width on desktop ([#2090](https://github.com/choiceoh/Deneb/issues/2090)) ([0ba1583](https://github.com/choiceoh/Deneb/commit/0ba1583f161b6d0b9b03dafec7210a54043fa865))
* **native:** collapse mail AI analysis card by default with expand toggle ([#2177](https://github.com/choiceoh/Deneb/issues/2177)) ([065751f](https://github.com/choiceoh/Deneb/commit/065751f3d6a1df174499d60928034f9304a324af))
* **native:** cron edit screen with friendly schedule picker ([#2095](https://github.com/choiceoh/Deneb/issues/2095)) ([e2f64a1](https://github.com/choiceoh/Deneb/commit/e2f64a1ae663079ddbd079a6280b6726b00fe037))
* **native:** desktop fixed left sidebar ([#2122](https://github.com/choiceoh/Deneb/issues/2122)) ([a30ccfc](https://github.com/choiceoh/Deneb/commit/a30ccfc8cebb0fd06eeca86785c1aadf99912f86))
* **native:** desktop mail split-view (list + detail side by side) ([#2133](https://github.com/choiceoh/Deneb/issues/2133)) ([b11e550](https://github.com/choiceoh/Deneb/commit/b11e550dd0ce9e8d5cc200d12ff6d92252d5e36d))
* **native:** desktop UI polish — chat width cap, sidebar hover/full-row, shortcuts ([#2178](https://github.com/choiceoh/Deneb/issues/2178)) ([5e5e04a](https://github.com/choiceoh/Deneb/commit/5e5e04a7f20a1dbde89c2c74cb9f57a6f5017fdf))
* **native:** differentiate haptic feedback by interaction type ([#2078](https://github.com/choiceoh/Deneb/issues/2078)) ([54e3050](https://github.com/choiceoh/Deneb/commit/54e3050c3fe1b89c67ed5adddde2a54f29b18705))
* **native:** extend haptic feedback across chat and home surfaces ([#2076](https://github.com/choiceoh/Deneb/issues/2076)) ([9dcc18d](https://github.com/choiceoh/Deneb/commit/9dcc18d74f85d77d6dd9b95a86ff8dd4da0b3f7c))
* **native:** finger-tracking month pager for the calendar ([#2113](https://github.com/choiceoh/Deneb/issues/2113)) ([61aa202](https://github.com/choiceoh/Deneb/commit/61aa202a1fbaf9d6f81ad5b0387d847c83404f1f))
* **native:** hide the mail search field until the list is pulled down ([#2217](https://github.com/choiceoh/Deneb/issues/2217)) ([6aea742](https://github.com/choiceoh/Deneb/commit/6aea74295a0d0159d2685c9c59b27508f475275b))
* **native:** improve markdown parser and renderer for LLM output ([#2158](https://github.com/choiceoh/Deneb/issues/2158)) ([e3c9a4a](https://github.com/choiceoh/Deneb/commit/e3c9a4afb3f836629db115de5b31910b4c903e53))
* **native:** industry-grade motion system with spring physics ([#2106](https://github.com/choiceoh/Deneb/issues/2106)) ([20806af](https://github.com/choiceoh/Deneb/commit/20806af8109bae09eb84257dec1233a635e62f18))
* **native:** mail search, inline image attachment previews, simplified detail actions ([#2180](https://github.com/choiceoh/Deneb/issues/2180)) ([4ebea39](https://github.com/choiceoh/Deneb/commit/4ebea3989e81272ec3c6193466cd8d80909f13ad))
* **native:** merge 사람 and 인물 into one people surface under categories ([#2195](https://github.com/choiceoh/Deneb/issues/2195)) ([b29ea08](https://github.com/choiceoh/Deneb/commit/b29ea08b90594ac642cd4bd9c730b24518935733))
* **native:** migrate inbox to Deneb idiom, aurora chat polish ([#2175](https://github.com/choiceoh/Deneb/issues/2175)) ([fa21e78](https://github.com/choiceoh/Deneb/commit/fa21e78f1fd6bb596dd24fa988f27f75022793fd))
* **native:** observe dashboard — settings 관찰 tab over miniapp.observe.* ([#2141](https://github.com/choiceoh/Deneb/issues/2141)) ([def23fa](https://github.com/choiceoh/Deneb/commit/def23fa2471679b1a3c29c581d74312345c47325))
* **native:** open 업무 cards in a dedicated side-conversation, not client:main ([#2110](https://github.com/choiceoh/Deneb/issues/2110)) ([f70c53c](https://github.com/choiceoh/Deneb/commit/f70c53cfe6f8d1bdd478f1bbcedd658db7b964ab))
* **native:** redesign search screen in the Deneb idiom ([#2098](https://github.com/choiceoh/Deneb/issues/2098)) ([56d8f8a](https://github.com/choiceoh/Deneb/commit/56d8f8aa6f007b82cdab894705efa6208bfbabc4))
* **native:** redesign work-feed rows — Deneb type, source icons, inline actions ([#2101](https://github.com/choiceoh/Deneb/issues/2101)) ([a7d0ada](https://github.com/choiceoh/Deneb/commit/a7d0ada582c3c1e7e7e1b0d4f3628202c184f0c4))
* **native:** remove in-app notification capture in favor of Termux [#2148](https://github.com/choiceoh/Deneb/issues/2148) ([#2156](https://github.com/choiceoh/Deneb/issues/2156)) ([3f94f63](https://github.com/choiceoh/Deneb/commit/3f94f63a15aba23e0bff0eb193e295307161a2bb))
* **native:** wide-table scroll, inline HTML, reference links, plain-text task state ([#2160](https://github.com/choiceoh/Deneb/issues/2160)) ([0848551](https://github.com/choiceoh/Deneb/commit/08485516b8c92f5de8888fc5b7b65ba5a4555510))
* **native:** Windows desktop support — MSI build and release pipeline ([#2072](https://github.com/choiceoh/Deneb/issues/2072)) ([4905223](https://github.com/choiceoh/Deneb/commit/49052230edec9b0e4cad8ffa6d7ccf9ac792ef53))
* **observability:** record agent-turn shape, proactive/background events, and tool-usage stats ([#2108](https://github.com/choiceoh/Deneb/issues/2108)) ([987e448](https://github.com/choiceoh/Deneb/commit/987e4485af496b82036bbf8d69be33d986e78956))
* **observe:** unified observation plane for coding agents ([#2123](https://github.com/choiceoh/Deneb/issues/2123)) ([69451b0](https://github.com/choiceoh/Deneb/commit/69451b0e1cc9c7a6f182b4b02f239854521dba86))
* proactive meeting briefs, mail→to-do, and deal-doc extraction ([#2144](https://github.com/choiceoh/Deneb/issues/2144)) ([6b003ab](https://github.com/choiceoh/Deneb/commit/6b003abc068f9eedc100d12f80d589ffbbf42040))
* **proactive:** batch notification bursts in deneb-notification-watch (one triage turn, not N) ([#2234](https://github.com/choiceoh/Deneb/issues/2234)) ([6ec09d5](https://github.com/choiceoh/Deneb/commit/6ec09d5273d52ca6487c238d45feb44fb5ea1492))
* **proactive:** deliver cron and proactive reports to the native client only ([#1902](https://github.com/choiceoh/Deneb/issues/1902)) ([e9d3d90](https://github.com/choiceoh/Deneb/commit/e9d3d90d018d41f97e6ef5ac48f2a7ce2fc0a369))
* **proactive:** deneb-location-watch — GPS place transitions for WiFi-less spots ([#2131](https://github.com/choiceoh/Deneb/issues/2131)) ([0f0a935](https://github.com/choiceoh/Deneb/commit/0f0a93585cbfccb5c6fdf68a031fcb0131262c72))
* **proactive:** deneb-notification-watch — durable Termux notification source for Samsung ([#2148](https://github.com/choiceoh/Deneb/issues/2148)) ([af0db90](https://github.com/choiceoh/Deneb/commit/af0db90e988c80742471d12a486c02df54aaecf8))
* **proactive:** deneb-supervisor — one Termux:Boot entry to start+watch phone agents ([#2129](https://github.com/choiceoh/Deneb/issues/2129)) ([e3dbce3](https://github.com/choiceoh/Deneb/commit/e3dbce35dc1ea62718ccf143b0ddc43dd73a16d0))
* **proactive:** offline event queue in deneb-emit (retry on reconnect) ([#2124](https://github.com/choiceoh/Deneb/issues/2124)) ([f9abc9f](https://github.com/choiceoh/Deneb/commit/f9abc9ff408481dabd188416eb57b0dc35b52daa))
* **proactive:** per-type phone-event judgment (context/clipboard) + WiFi context watcher ([#2120](https://github.com/choiceoh/Deneb/issues/2120)) ([838958e](https://github.com/choiceoh/Deneb/commit/838958e3b4d6abb032d4ec4f5188cc1748f4d49a))
* **proactive:** phone event ingest over SSH (deneb-emit → /api/event/ingest) ([#2116](https://github.com/choiceoh/Deneb/issues/2116)) ([37916a6](https://github.com/choiceoh/Deneb/commit/37916a630d9934d8db2e0bdc62a9fba7f4f346fa))
* **proactive:** phone link health watchdog — heartbeat ping + host timer warns on silent tunnel death ([#2236](https://github.com/choiceoh/Deneb/issues/2236)) ([e18735b](https://github.com/choiceoh/Deneb/commit/e18735bce682537c11ef365b8980e3bdc64704e3))
* **proactive:** phone_read/phone_write tools — close the SSH loop (gateway ↔ phone) ([#2143](https://github.com/choiceoh/Deneb/issues/2143)) ([5a98328](https://github.com/choiceoh/Deneb/commit/5a983280b96a04fbf49a26498f0c34de9553946a))
* **security,memory:** adopt three Hermes Agent v0.15 elements ([#1806](https://github.com/choiceoh/Deneb/issues/1806)) ([c93a4e5](https://github.com/choiceoh/Deneb/commit/c93a4e59945171e3be1c668054d6cd353d47ab3e))
* **server:** dedicated dream sub-session routing + tolerant dream-synthesis parsing ([#2185](https://github.com/choiceoh/Deneb/issues/2185)) ([116c155](https://github.com/choiceoh/Deneb/commit/116c155fe40b36cbe172a64520ca53962983c310))
* **server:** retire Telegram bot — native client is the sole surface ([#1922](https://github.com/choiceoh/Deneb/issues/1922)) ([452a8ec](https://github.com/choiceoh/Deneb/commit/452a8eca306b5ced65d53d1e3b21fd3ab0fef7b0))
* **server:** role-provider health watch — dead keys surfaced, mail-analysis roles unified ([#2197](https://github.com/choiceoh/Deneb/issues/2197)) ([034d378](https://github.com/choiceoh/Deneb/commit/034d37833783d13aaba3f2e89636ac3cd9783d2b))
* **session:** harden session GC and subagent lifecycle ([#2168](https://github.com/choiceoh/Deneb/issues/2168)) ([c8ac262](https://github.com/choiceoh/Deneb/commit/c8ac2624e132231f52d1aa3f4a63baba450b9acb))
* **skills:** harden self-evolution loop (self-test, teacher, liveness, dedup) ([#2096](https://github.com/choiceoh/Deneb/issues/2096)) ([d9a9acf](https://github.com/choiceoh/Deneb/commit/d9a9acf2305d9ca2316b88cb5aa4d969693172d9))
* **tools:** wiki status action becomes the memory-system panel ([#2214](https://github.com/choiceoh/Deneb/issues/2214)) ([509c7f8](https://github.com/choiceoh/Deneb/commit/509c7f87a590587fbc177f07d13da62d5d6b204a))
* **ui:** rework DenebType — editorial/functional split, scale, cardTitle, theory ([#2169](https://github.com/choiceoh/Deneb/issues/2169)) ([8a51b00](https://github.com/choiceoh/Deneb/commit/8a51b00497bfba101b5ee48e13b6266bbfe8541e))
* **web:** summarize YouTube transcripts in isolated local-LLM call ([#1809](https://github.com/choiceoh/Deneb/issues/1809)) ([5c881f0](https://github.com/choiceoh/Deneb/commit/5c881f058ce3ef9ae589254bb06ccdee989e10ab))
* **weekly-report:** generate 주간업무보고 from wiki and render PDF ([#1898](https://github.com/choiceoh/Deneb/issues/1898)) ([240e3b5](https://github.com/choiceoh/Deneb/commit/240e3b57b596b3d23cfa6921697a973fead32789))
* **widget:** add calendar/mail glyphs to the home widget ([c10f599](https://github.com/choiceoh/Deneb/commit/c10f59972514eda43ae71d98943a0ae1f2319f76))
* **widget:** align home widget to Deneb design system ([#2068](https://github.com/choiceoh/Deneb/issues/2068)) ([4c42292](https://github.com/choiceoh/Deneb/commit/4c42292e04d21ec0b63fc97350f0efbf6d81dbf8))
* **widget:** show the most recent mail on the home widget ([7aa631c](https://github.com/choiceoh/Deneb/commit/7aa631c2ac22ce81f14a4e8e093180dff4e8fcc2))
* **wiki:** auto-record address-book contacts on wiki write ([8866edb](https://github.com/choiceoh/Deneb/commit/8866edbd24628615d09b50d187cd705214180368))
* **wiki:** collapse categories to 프로젝트/인물/운영시스템 ([#1925](https://github.com/choiceoh/Deneb/issues/1925)) ([15fa390](https://github.com/choiceoh/Deneb/commit/15fa3909e11a76fa95e7cc482e3938f0057b7d5c))
* **wiki:** document inline wiki-link authoring in the wiki tool schema ([2c52e42](https://github.com/choiceoh/Deneb/commit/2c52e42fcb5f058e93538d534adfa7835194d46f))
* **wiki:** dream cycle change report — pages, git snapshot, diffstat, rollback hint ([#2203](https://github.com/choiceoh/Deneb/issues/2203)) ([78f319f](https://github.com/choiceoh/Deneb/commit/78f319f129f9ac8df0482bb3932266444f66f041))
* **wiki:** dream cycles distill and curate workspace MEMORY.md ([#2196](https://github.com/choiceoh/Deneb/issues/2196)) ([d339caf](https://github.com/choiceoh/Deneb/commit/d339caf7cd279d8f6658d00faf964076a5e9f214))
* **wiki:** fact validity — superseded_by marking + staleness-demoted search ranking ([#2207](https://github.com/choiceoh/Deneb/issues/2207)) ([4dc4089](https://github.com/choiceoh/Deneb/commit/4dc4089d9ff0272cf43b56a2f465a2be8ce8ae30))
* **wiki:** flag stale (overdue) deadlines in dreamer verification ([5096895](https://github.com/choiceoh/Deneb/commit/5096895739736541f803eee73cc39600d3ca8ef7))
* **wiki:** hybrid semantic search + auto-suggested related links ([c1597d1](https://github.com/choiceoh/Deneb/commit/c1597d15a6b3d110174fe6bc4305b1e0bc3aa026))
* **wiki:** in-process graph query for sender context (no graphify dependency) ([0eb8d85](https://github.com/choiceoh/Deneb/commit/0eb8d857fb9c9a8547417e99a991eeb12c301a76))
* **wiki:** mention-driven person page seeding from the contacts mirror ([#2212](https://github.com/choiceoh/Deneb/issues/2212)) ([21db347](https://github.com/choiceoh/Deneb/commit/21db347bc831ab16972d96a4784a742b00b31809))
* **wiki:** parse inline [[wiki-link]] links as knowledge-graph edges ([6bd1d89](https://github.com/choiceoh/Deneb/commit/6bd1d8972e2fb478ec0d3f1ad5f853cbd2f4b808))
* **wiki:** persist raw capture originals (ASR/OCR) with searchable diary breadcrumbs ([#2211](https://github.com/choiceoh/Deneb/issues/2211)) ([8836f2a](https://github.com/choiceoh/Deneb/commit/8836f2aa82c923c4ea137fa07115cadb5daec635))
* **wiki:** prospective memory — dream cycles extract open loops into the to-do store ([#2200](https://github.com/choiceoh/Deneb/issues/2200)) ([680362e](https://github.com/choiceoh/Deneb/commit/680362e85e19c0105d38a529f11f9e60ffcc0873))
* **wiki:** rerank graph neighbors by embedding similarity ([cc42059](https://github.com/choiceoh/Deneb/commit/cc42059cbb7b17b1e93d328561359937227b6ec3))
* **wiki:** show connected-item footer when a wiki page is read ([4b7a4fa](https://github.com/choiceoh/Deneb/commit/4b7a4fabac5d6877dacdc97f8adba27ce2b0e67e))
* **workfeed:** declutter the feed with icon-only quick actions ([ed2e343](https://github.com/choiceoh/Deneb/commit/ed2e34354ac915e5acf99e425b1ed13a9ffaf09b))
* **workfeed:** derive card titles/summaries from body; cut noise ([#2094](https://github.com/choiceoh/Deneb/issues/2094)) ([e5408b4](https://github.com/choiceoh/Deneb/commit/e5408b4bfa88c97726636ceefcfdfd9da905a0c3))
* **workfeed:** make snooze re-surface later instead of hiding forever ([66fab4f](https://github.com/choiceoh/Deneb/commit/66fab4ffc8cc204148460251be592a99bb5c9178))
* **workfeed:** prioritize the feed and bound its growth ([94ec104](https://github.com/choiceoh/Deneb/commit/94ec104aabd67d67638abc615327fef1f2e4f9fa))
* **workfeed:** sheet UX/actions + filter contentless proactive reports ([#1971](https://github.com/choiceoh/Deneb/issues/1971)) ([1593c63](https://github.com/choiceoh/Deneb/commit/1593c63ca631ce45777151fd35efb04225192fd7))


### 🐛 Bug Fixes

* **agent:** don't let a trailing usage-only message_delta clobber stop_reason ([dd142ce](https://github.com/choiceoh/Deneb/commit/dd142ce850c449170bdf071fbeb674110c3023b3))
* **agent:** join the tool heartbeat goroutine before returning the tool result ([#2248](https://github.com/choiceoh/Deneb/issues/2248)) ([b708708](https://github.com/choiceoh/Deneb/commit/b708708de6eb6e74d1db60f607895f550297d633))
* **agent:** re-check heartbeat stop before firing a racing tick ([#2251](https://github.com/choiceoh/Deneb/issues/2251)) ([fec8a0d](https://github.com/choiceoh/Deneb/commit/fec8a0dcd5e73467965ee969e7d068d74495595c))
* **agent:** store streamed thinking reasoning in the block, not Text ([d522e6e](https://github.com/choiceoh/Deneb/commit/d522e6ea23ebd3218e4f6253da5b17b201cf7c5c))
* **autonomous:** persist task last-run times so intervals survive restarts ([c68f18f](https://github.com/choiceoh/Deneb/commit/c68f18ff0bcec576a111f55a0cf7fa46468ec828))
* **boot:** make the startup boot turn ephemeral (stop unbounded session growth) ([9e4be97](https://github.com/choiceoh/Deneb/commit/9e4be97617f8a89e5be8e4c163db458453cae45b))
* **bootstrap:** join the shutdown supervisor before RunWithSignals returns ([#2208](https://github.com/choiceoh/Deneb/issues/2208)) ([53fc19f](https://github.com/choiceoh/Deneb/commit/53fc19fe87f4cd10e0c7402696e904a4c70b9b2e))
* **calendar:** keep the month grid pinned while the day list scrolls ([#2081](https://github.com/choiceoh/Deneb/issues/2081)) ([b6f3475](https://github.com/choiceoh/Deneb/commit/b6f3475686d1a2cb8f1ab42d5d09954ff5effd97))
* **chat:** chat tool system audit fixes (P0+P1) ([#1918](https://github.com/choiceoh/Deneb/issues/1918)) ([81ff2d7](https://github.com/choiceoh/Deneb/commit/81ff2d78f9dd14726481bd7b976fd73c930f7350))
* **chat:** clamp boosted thinking budget under max_tokens + tests ([7aa95a1](https://github.com/choiceoh/Deneb/commit/7aa95a1535d9b97e20ea7fc75611b0eadfffc15d))
* **chat:** close every agent run in its session log — run.end/run.error move into executeAgentRun ([#2230](https://github.com/choiceoh/Deneb/issues/2230)) ([8903d47](https://github.com/choiceoh/Deneb/commit/8903d47d341517f95db3698b5e898f71f7873440))
* **chat:** ensure subagent results reach the parent (reclaim race + result query) ([#2056](https://github.com/choiceoh/Deneb/issues/2056)) ([399fd16](https://github.com/choiceoh/Deneb/commit/399fd16298077b63e21ca84aa2b73d32c8317051))
* **chat:** gate cache-hit metric to Anthropic runs + add recent EWMA ([e004b12](https://github.com/choiceoh/Deneb/commit/e004b12bb13adf18543b66e330aef442e52d253e))
* **chat:** hide tool_result blocks from client chat history ([#2243](https://github.com/choiceoh/Deneb/issues/2243)) ([6057bc6](https://github.com/choiceoh/Deneb/commit/6057bc60b477fc23562d7d89706e235338508a7d))
* **chat:** hindsight recall died on long Korean turns — token-aware query cap + too-long retry ([#2216](https://github.com/choiceoh/Deneb/issues/2216)) ([6b42874](https://github.com/choiceoh/Deneb/commit/6b428741967001097f75c3672cb9564f37c732a1))
* **chat:** keep deadline-truncated recall snapshots out of the frozen cache ([#2250](https://github.com/choiceoh/Deneb/issues/2250)) ([59be6c4](https://github.com/choiceoh/Deneb/commit/59be6c46fd61f678ffa39b589b95b42a2938f443))
* **chat:** log dropped-reply failures at Error, not Warn ([84510f4](https://github.com/choiceoh/Deneb/commit/84510f45d2ab671de8d245f7f3ac69be52b99eb7))
* **chat:** route prompt timezone through dentime to fix UTC mismatch ([#1868](https://github.com/choiceoh/Deneb/issues/1868)) ([0e9ec41](https://github.com/choiceoh/Deneb/commit/0e9ec418b3f586d9d4c5a2a3ee665c60379e37b5))
* **chat:** rune-safe truncation in tool/result previews to stop UTF-8 corruption ([8d02d18](https://github.com/choiceoh/Deneb/commit/8d02d18ee703a23e37fa1e6fc3b986ee6922f34b))
* **chat:** skip cache-hit recording on provider fallback ([2349cf9](https://github.com/choiceoh/Deneb/commit/2349cf939807d6536e477b0ebcb25292a2ffc7e4))
* **chat:** skip polaris compaction for sub-floor history budgets ([#2210](https://github.com/choiceoh/Deneb/issues/2210)) ([08a8404](https://github.com/choiceoh/Deneb/commit/08a840411e5e863c3f813e4413fff203687cf084))
* **chat:** stop false reply_func_nil delivery alarm on subagent runs ([#2060](https://github.com/choiceoh/Deneb/issues/2060)) ([c247da5](https://github.com/choiceoh/Deneb/commit/c247da5d37fa2eaa065a0283b4b9694f6e81cbd4))
* **chat:** stop MaxHistoryTokens from collapsing the compaction budget ([272df39](https://github.com/choiceoh/Deneb/commit/272df39e3d22bb962302e5e0f50c02e257ae386f))
* **chat:** strip leaked reasoning markers from sync answers ([54490dc](https://github.com/choiceoh/Deneb/commit/54490dc4703f15c2a5f881e16c051a626bc1f99c))
* **chat:** strip self-narration head from proactive deliverables ([#2199](https://github.com/choiceoh/Deneb/issues/2199)) ([93a859b](https://github.com/choiceoh/Deneb/commit/93a859b2b109e9e50924ea655fea1424da59763f))
* **chat:** tighten chat body line-height to messenger density (2.9.58) ([#2071](https://github.com/choiceoh/Deneb/issues/2071)) ([b319f5b](https://github.com/choiceoh/Deneb/commit/b319f5bd543b10173c18370d9ded51c2681e916c))
* **chat:** tool hygiene — rename stubs, clarify fallback, empty-response broadcast (P3) ([#1923](https://github.com/choiceoh/Deneb/issues/1923)) ([54990e6](https://github.com/choiceoh/Deneb/commit/54990e6d0702934cf1e65dab2b9abde9ef403c18))
* **chat:** tool schema drift + gateway action cleanup (P2) ([#1919](https://github.com/choiceoh/Deneb/issues/1919)) ([4806065](https://github.com/choiceoh/Deneb/commit/480606534d33fb867916fd64407ae6fb9a68d1c4))
* **chat:** trim over-reaching speculation cues from mail analysis prompts ([#1995](https://github.com/choiceoh/Deneb/issues/1995)) ([b800e66](https://github.com/choiceoh/Deneb/commit/b800e660c16e8b070de809877cb7ca66e61d708e))
* **chat:** wiki-write no longer erases the streamed answer (132/2.9.9) ([#1864](https://github.com/choiceoh/Deneb/issues/1864)) ([f6639c9](https://github.com/choiceoh/Deneb/commit/f6639c93f340cf0242bc83f956b40735828ea6f6))
* **client-android:** always show client:main in the session drawer ([86c2ef9](https://github.com/choiceoh/Deneb/commit/86c2ef934470a84dbaf4e544e7b8b4915b3e3103))
* **client-android:** auto-assign collision-free OTA versionCode ([b32a890](https://github.com/choiceoh/Deneb/commit/b32a89099a90ef92e6c6f4d8a86ab032bc376a22))
* **client-android:** backfill stale patch notes (stuck at 2.9.30) and guard recurrence ([#2061](https://github.com/choiceoh/Deneb/issues/2061)) ([7572a35](https://github.com/choiceoh/Deneb/commit/7572a35e3906f2284dcca20ffea65f12feab70be))
* **client-android:** build release APK from the bundle (AGP 9 assemble is broken) ([82f1739](https://github.com/choiceoh/Deneb/commit/82f173918cd6602320dce582ac3e26a08c2a2990))
* **client-android:** derive in-app version from the build, not a hardcoded constant ([5d2d423](https://github.com/choiceoh/Deneb/commit/5d2d4234f46a9957b3812a50e9626e02f268c665))
* **client-android:** drop redundant 사람 tab from settings hub ([#1869](https://github.com/choiceoh/Deneb/issues/1869)) ([29703a9](https://github.com/choiceoh/Deneb/commit/29703a95bc1dcb102deb9cd94a7b484ba010369d))
* **client-android:** even out chat input border ([#1953](https://github.com/choiceoh/Deneb/issues/1953)) ([3fb4e6e](https://github.com/choiceoh/Deneb/commit/3fb4e6e77eea2ca99ba68480c511e4e3ac049c64))
* **client-android:** fold every machine session in the right drawer, not just cron/system/boot ([234d333](https://github.com/choiceoh/Deneb/commit/234d333ad4140170f2b4275c3aa60e199dbe584e))
* **client-android:** harden notification creation ([534b61e](https://github.com/choiceoh/Deneb/commit/534b61e42ef92a025acbba89e7771d55eccc766e))
* **client-android:** keep deneb native client lean and alive ([#1881](https://github.com/choiceoh/Deneb/issues/1881)) ([0284971](https://github.com/choiceoh/Deneb/commit/0284971b1b46f6bae629d59b339e0212d30c29c7))
* **client-android:** keep session drawer list when sessions.recent fetch fails ([#2058](https://github.com/choiceoh/Deneb/issues/2058)) ([04ed33e](https://github.com/choiceoh/Deneb/commit/04ed33ea3a24966f95971c93fc4318731fb3af81))
* **client-android:** open session drawer via inside-edge left swipe (avoid OS back gesture) (152/2.9.29) ([#1915](https://github.com/choiceoh/Deneb/issues/1915)) ([4f7197e](https://github.com/choiceoh/Deneb/commit/4f7197eaa259764f2d5e6f4f7d0814c086bdedfa))
* **client-android:** proactive push opens the work topic, not heartbeat (135/2.9.12) ([#1871](https://github.com/choiceoh/Deneb/issues/1871)) ([a132757](https://github.com/choiceoh/Deneb/commit/a132757fdcb125fbff091723c43318557d532f6b))
* **client-android:** raise output-token ceiling in interactive UI mode (OpenAI-compatible) ([#1926](https://github.com/choiceoh/Deneb/issues/1926)) ([407028d](https://github.com/choiceoh/Deneb/commit/407028d0b0904f75f7340b46e2ac2a1af46d1d26))
* **client-android:** regen button + cron 업무 mirror, bump 131/2.9.8 ([#1863](https://github.com/choiceoh/Deneb/issues/1863)) ([ea36c6f](https://github.com/choiceoh/Deneb/commit/ea36c6ff5380599e68f91a24cda02a9f6c9c41aa))
* **client-android:** render mail-detail analysis with full markdown (tables) ([911f6aa](https://github.com/choiceoh/Deneb/commit/911f6aaac69e7261b60544096ed40cc135d73bbc))
* **client-android:** right-edge swipe actually opens the session drawer (150/2.9.27) ([#1914](https://github.com/choiceoh/Deneb/issues/1914)) ([81bda56](https://github.com/choiceoh/Deneb/commit/81bda56ec50b4d2da48e8d9fda26ccad77b8942d))
* **client-android:** shared content gets a reply without a second message ([#1872](https://github.com/choiceoh/Deneb/issues/1872)) ([6129187](https://github.com/choiceoh/Deneb/commit/61291870681f6c851de52b60a1e9d1d3dc074697))
* **client-android:** show mail time in device local zone ([#1935](https://github.com/choiceoh/Deneb/issues/1935)) ([303f4b5](https://github.com/choiceoh/Deneb/commit/303f4b59a2f63cc8825995c1e672cb26aa834d2f))
* **client-android:** stop leaking retired topics into the session drawer ([#1942](https://github.com/choiceoh/Deneb/issues/1942)) ([07da0f7](https://github.com/choiceoh/Deneb/commit/07da0f7d84b2d6afd6857af349bf24f1fd177cb7))
* **client:** pick a topic when sharing into Deneb ([866b955](https://github.com/choiceoh/Deneb/commit/866b955397c1fe852a2fa917da49ff77f1710860))
* **compaction:** balance orphaned tool_use/tool_result to prevent a 400 wedge ([5b384ef](https://github.com/choiceoh/Deneb/commit/5b384ef050ee1fc8f51f9ff41bf0998481730d8f))
* **compaction:** stop double-counting the trigger input in EmergencyCompact eviction ([#2246](https://github.com/choiceoh/Deneb/issues/2246)) ([ac65fca](https://github.com/choiceoh/Deneb/commit/ac65fca18400485c061e4d7fbb72b3aa28cebaf6))
* **concurrency:** panic recovery for unguarded goroutines + lock hierarchy docs ([#2252](https://github.com/choiceoh/Deneb/issues/2252)) ([8d3d74f](https://github.com/choiceoh/Deneb/commit/8d3d74f8460e124b85b32b760fe0e4383b075df0))
* **cron:** default delivery to native client in native-only mode ([#1921](https://github.com/choiceoh/Deneb/issues/1921)) ([8b2fcc7](https://github.com/choiceoh/Deneb/commit/8b2fcc7f888a390b1fa1762d1f3738662d34e163))
* **cron:** mark a run as error when delivery handoff fails ([7208760](https://github.com/choiceoh/Deneb/commit/72087604fa944a5460adb5bf86486a8620101575))
* **cron:** requeue runs lost to restarts and overlap-skipped triggers ([#2218](https://github.com/choiceoh/Deneb/issues/2218)) ([4eaa091](https://github.com/choiceoh/Deneb/commit/4eaa0911b96576ca1f46713ce774be602240a70b))
* **cron:** return an independent snapshot from Store.Load to fix a data race ([e91e6d8](https://github.com/choiceoh/Deneb/commit/e91e6d877c5172de390c3052bd1b062d1108cbf5))
* **cron:** stop working narration and send-failure apologies leaking into reports ([#1894](https://github.com/choiceoh/Deneb/issues/1894)) ([e22fa92](https://github.com/choiceoh/Deneb/commit/e22fa926d65e7cff38a4bed14d582c6ea2c9127b))
* **deploy:** make BGE-M3 embedding server actually start (CPU, fixed args) ([#1846](https://github.com/choiceoh/Deneb/issues/1846)) ([d9e3e2f](https://github.com/choiceoh/Deneb/commit/d9e3e2f5e580b04471309d381679741d3fa24bfd))
* **deploy:** quiet-period guard — one deploy per PR stack, not one per merge ([#2219](https://github.com/choiceoh/Deneb/issues/2219)) ([2b96b5b](https://github.com/choiceoh/Deneb/commit/2b96b5b35f2dde89d605098aa192933a9eea2ba3))
* **dev:** native-app smoke no longer false-flags a live app as crashed ([#2255](https://github.com/choiceoh/Deneb/issues/2255)) ([60e4fd1](https://github.com/choiceoh/Deneb/commit/60e4fd1686ccc491e56c68297ae1f4fc86374edf))
* **dev:** smoke harness reads the screen again — OCR preprocessing + desktop-layout nav ([#2260](https://github.com/choiceoh/Deneb/issues/2260)) ([81c56ed](https://github.com/choiceoh/Deneb/commit/81c56edcd18a6aa01dccddf5f33ac9d40bcc5fd7))
* **dynamic-ui:** degrade unknown deneb-ui node types instead of dropping them ([#2254](https://github.com/choiceoh/Deneb/issues/2254)) ([2755926](https://github.com/choiceoh/Deneb/commit/2755926f1fab241c4b14bb8f8d5374c35d785218))
* **embedding:** don't mark server unhealthy on caller-side cancellation ([6413d2b](https://github.com/choiceoh/Deneb/commit/6413d2b085540ff293299c13750498935e78f71a))
* **fleet:** coerce Go's null JSON arrays — 노드 tab stuck on loading ([#2257](https://github.com/choiceoh/Deneb/issues/2257)) ([715006d](https://github.com/choiceoh/Deneb/commit/715006de69c30a37a822e62f7c1fcbf6c652b626))
* **gateway:** address reviewer comments (Kimi cache aliases, UTF-8 truncation, role reset) ([#2105](https://github.com/choiceoh/Deneb/issues/2105)) ([7b13be3](https://github.com/choiceoh/Deneb/commit/7b13be33045096f95ea29ddfc5bac5f577e6477d))
* **gateway:** resolve all 57 lint findings surfaced by the tightened ruleset ([#2174](https://github.com/choiceoh/Deneb/issues/2174)) ([ec2e28f](https://github.com/choiceoh/Deneb/commit/ec2e28f87bb6461efb0b3a3b1983fa078fae3bab))
* **gmail:** decode HTML entities in mail list snippets ([553bd5b](https://github.com/choiceoh/Deneb/commit/553bd5b934e89326b8721d6ca7ba4a5ea16f7e12))
* **gmail:** decode HTML entities leaked into text/plain mail bodies ([#2087](https://github.com/choiceoh/Deneb/issues/2087)) ([98086bc](https://github.com/choiceoh/Deneb/commit/98086bc61831bb313d6cb1adda1e60dc653509ec))
* **gmail:** disable extended thinking for on-demand mail analysis ([#1816](https://github.com/choiceoh/Deneb/issues/1816)) ([3b09d45](https://github.com/choiceoh/Deneb/commit/3b09d453e63bf551f232da999d1d05b47f2aa8f9))
* **gmailpoll:** disable reasoning on mail analysis (GLM + step3p7) ([#1900](https://github.com/choiceoh/Deneb/issues/1900)) ([a8341b1](https://github.com/choiceoh/Deneb/commit/a8341b175eff6c51e0f5c7a897fc4a0192fa22dc))
* **gmailpoll:** filter thinking_delta in the single-call email analyzer ([5c80181](https://github.com/choiceoh/Deneb/commit/5c801815a8762cff338e5bc4ca1213e6d41fc67c))
* **gmailpoll:** keep reasoning on but never surface it in cron analysis ([#1866](https://github.com/choiceoh/Deneb/issues/1866)) ([ea930db](https://github.com/choiceoh/Deneb/commit/ea930db0ea51c38248197115db5a80b89a2e4c7f))
* **gmailpoll:** loosen rigid mail-analysis templates into an analysis stance ([#1865](https://github.com/choiceoh/Deneb/issues/1865)) ([d76ea77](https://github.com/choiceoh/Deneb/commit/d76ea771aa7b9aa9945a32efb5b9a53cfb96df14))
* **gmail:** route on-demand mail analysis to the fallback model ([#1818](https://github.com/choiceoh/Deneb/issues/1818)) ([af67606](https://github.com/choiceoh/Deneb/commit/af67606096dd5ffc5810649c9b3c0039a406ef7c))
* harden self-evolution + dreaming against SIGUSR1 restarts (audit fixes) ([#2132](https://github.com/choiceoh/Deneb/issues/2132)) ([f618ac1](https://github.com/choiceoh/Deneb/commit/f618ac194acfef2e952b874f748bbf6eeb18668f))
* **llm:** assemble parallel tool calls by emitting contiguous blocks ([7f9826c](https://github.com/choiceoh/Deneb/commit/7f9826c4fcbea9e1bc4f0d640d67d5fd8c8dd4b1))
* **llm:** correct step3p7 disabled-thinking to reasoning_effort=low ([#1910](https://github.com/choiceoh/Deneb/issues/1910)) ([dcdc7a0](https://github.com/choiceoh/Deneb/commit/dcdc7a0ae279aace1a67298ecf77301398a5b0a3))
* **llm:** drop empty messages before Anthropic-style requests ([#1833](https://github.com/choiceoh/Deneb/issues/1833)) ([67caec2](https://github.com/choiceoh/Deneb/commit/67caec298304d0d818c6ae1a127c61982bff5df7))
* **llm:** harden openai stream translator — own block index for thinking + surface refusals ([#2154](https://github.com/choiceoh/Deneb/issues/2154)) ([ecd9bef](https://github.com/choiceoh/Deneb/commit/ecd9bef6181c987fc4a23edc607206ca6ec46dec))
* **llm:** keep max_tokens for vLLM when extended thinking is on ([#1810](https://github.com/choiceoh/Deneb/issues/1810)) ([537eb75](https://github.com/choiceoh/Deneb/commit/537eb75534432e2a19b677ae33a89de877780b4f))
* **llm:** map disabled thinking to reasoning_effort=minimal for step3p7 ([#1906](https://github.com/choiceoh/Deneb/issues/1906)) ([d8b9bdb](https://github.com/choiceoh/Deneb/commit/d8b9bdbc9c71277ecf9a2101e71fd6a3ae6491b8))
* **llm:** never emit empty-Content messages on malformed tool args ([#2202](https://github.com/choiceoh/Deneb/issues/2202)) ([46ab718](https://github.com/choiceoh/Deneb/commit/46ab7188b11eb3df39631ca14ba57eba66508350))
* **llm:** replace invalid UTF-8 with U+FFFD when encoding JSON strings ([04e0f57](https://github.com/choiceoh/Deneb/commit/04e0f5708fdb5d1bc627428c382ed7b5ba2668a0))
* **llm:** rescue truncated-stream content, surface non-stream refusals, synthesize missing tool ids ([#2239](https://github.com/choiceoh/Deneb/issues/2239)) ([5453ccb](https://github.com/choiceoh/Deneb/commit/5453ccb3472a695c6420db5912a5646341ac89fa))
* **llm:** surface SSE scan errors instead of silently truncating the turn ([02cfb57](https://github.com/choiceoh/Deneb/commit/02cfb57399ff12f2566407bece78af4e3c2bd090))
* **llm:** surface vLLM reasoning streamed under "reasoning" field ([#1836](https://github.com/choiceoh/Deneb/issues/1836)) ([4e5b3b1](https://github.com/choiceoh/Deneb/commit/4e5b3b1939f515315551226475736c2ea5c360c9))
* **logging:** raise permanent user-affecting failures from Warn to Error ([#2249](https://github.com/choiceoh/Deneb/issues/2249)) ([0040dc1](https://github.com/choiceoh/Deneb/commit/0040dc14ac69a890474343a83eb14a02ca6ab212))
* **mail:** keep read mail read across a list refetch ([#2253](https://github.com/choiceoh/Deneb/issues/2253)) ([ddad3be](https://github.com/choiceoh/Deneb/commit/ddad3be1308578af3f1639d1188e531b2630b54e))
* **memory:** resolve full memory-system audit findings (budget, sweeps, durability, recall) ([#2182](https://github.com/choiceoh/Deneb/issues/2182)) ([de518f2](https://github.com/choiceoh/Deneb/commit/de518f22b483e8516a4e0c806156e1b42e6eff5c))
* **miniapp:** bound people search so it can't hang unified search ([7418cc6](https://github.com/choiceoh/Deneb/commit/7418cc6d8dc1cf9a0750762545a0990c7d0a3f3d))
* **miniapp:** delete sessions server-side so dismissed ones don't resurrect ([#2069](https://github.com/choiceoh/Deneb/issues/2069)) ([b265632](https://github.com/choiceoh/Deneb/commit/b265632952d1d0fbba9002a01de1af502ef233dc))
* **miniapp:** reattach MemoryCategoryRow deneb:wire directive lost in the file split ([#2244](https://github.com/choiceoh/Deneb/issues/2244)) ([cb8d062](https://github.com/choiceoh/Deneb/commit/cb8d062b9a725d4699632291b9e90fb058139569))
* **miniapp:** remove Agent settings tab (no-op against the gateway) ([#2165](https://github.com/choiceoh/Deneb/issues/2165)) ([035a46a](https://github.com/choiceoh/Deneb/commit/035a46a3120e25b98ea7e96776b843e397aa1534))
* **miniapp:** route gmail.analyze to analysis/tiny roles like the poller ([#2181](https://github.com/choiceoh/Deneb/issues/2181)) ([0efdbec](https://github.com/choiceoh/Deneb/commit/0efdbec625933b7d040c14cb2cdef51f72de0593))
* **miniapp:** shrink bot message action icons (copy/regen) ([e3a5b26](https://github.com/choiceoh/Deneb/commit/e3a5b265a8208d79384991114b3abb34f01dd396))
* **models:** allow tiny/analysis role switching in the model picker ([#2088](https://github.com/choiceoh/Deneb/issues/2088)) ([d1290f7](https://github.com/choiceoh/Deneb/commit/d1290f73b1b06c02db1b94bc8c76e353c654ce75))
* **native-app:** isolate the live-app harness per worktree instance ([#1969](https://github.com/choiceoh/Deneb/issues/1969)) ([2ccba88](https://github.com/choiceoh/Deneb/commit/2ccba88a7bd414ec059d7ebf23c91c664205f1cc))
* **native:** correct per-role model tooltips to actual call sites ([#2157](https://github.com/choiceoh/Deneb/issues/2157)) ([2707fb8](https://github.com/choiceoh/Deneb/commit/2707fb8346dfa0b96632dcf4892397d5b50a3d4b))
* **native:** deliver work-feed notifications via durable sync, not live SSE alone ([#2115](https://github.com/choiceoh/Deneb/issues/2115)) ([2ba4870](https://github.com/choiceoh/Deneb/commit/2ba4870e0b148cd8509b5be212b00bec5eab6d29))
* **native:** desktop MSI version tracks the build, not the static floor ([#2139](https://github.com/choiceoh/Deneb/issues/2139)) ([09fc1c0](https://github.com/choiceoh/Deneb/commit/09fc1c001e6ba1e26968327b67ae55661353e80d))
* **native:** keep gateway URL+token across app updates (durable mirror) ([#2150](https://github.com/choiceoh/Deneb/issues/2150)) ([fdd5510](https://github.com/choiceoh/Deneb/commit/fdd55108d92a14355b049798abdb8247bedd14f0))
* **native:** keep model-role tooltip on-screen on narrow phones ([#2155](https://github.com/choiceoh/Deneb/issues/2155)) ([022440c](https://github.com/choiceoh/Deneb/commit/022440c5f398719e63f67896df16073fc8648454))
* **native:** lift list/form bottoms above the soft keyboard ([#2153](https://github.com/choiceoh/Deneb/issues/2153)) ([1ab9872](https://github.com/choiceoh/Deneb/commit/1ab98724d527590e6dfaa66a4ac0737e9cdb8a1a))
* **native:** make the desktop content-width cap actually apply ([#2114](https://github.com/choiceoh/Deneb/issues/2114)) ([aeb953c](https://github.com/choiceoh/Deneb/commit/aeb953c875af18b1260e38ae8b42ad12ff040803))
* **native:** match Android launcher icon to the rounded-star mark ([#2085](https://github.com/choiceoh/Deneb/issues/2085)) ([7498263](https://github.com/choiceoh/Deneb/commit/74982630abfb5e79f6a6013c375a703e92c44b98))
* **native:** rebrand desktop app from Kai to Deneb ([#2077](https://github.com/choiceoh/Deneb/issues/2077)) ([1488dd8](https://github.com/choiceoh/Deneb/commit/1488dd851a89f7b44bdfea1ca572a1c446eb5ef3))
* **native:** refresh stale patch notes and gate native feature PRs ([#2147](https://github.com/choiceoh/Deneb/issues/2147)) ([0e52ff8](https://github.com/choiceoh/Deneb/commit/0e52ff8c94e0bb2c0c1d6bc89f93f401058d8f9a))
* **native:** use rounded-star artifact icon (replaces sharp star) ([#2083](https://github.com/choiceoh/Deneb/issues/2083)) ([17f515d](https://github.com/choiceoh/Deneb/commit/17f515d3b32cde9454b5f96a97e00f9e119e74e9))
* **pilot:** surface top-level and unparseable stream errors instead of swallowing ([2893385](https://github.com/choiceoh/Deneb/commit/289338541d76101d812019c2a02f848098b29dfb))
* **polaris:** unblock LLM summarization tier wedged by injected summary fences ([#2227](https://github.com/choiceoh/Deneb/issues/2227)) ([af70f32](https://github.com/choiceoh/Deneb/commit/af70f32b9fd4e6e723c267aaf3dfe01fdcf9651f))
* **proactive:** drop "nothing to report" pings before the work feed ([#1952](https://github.com/choiceoh/Deneb/issues/1952)) ([bdb9329](https://github.com/choiceoh/Deneb/commit/bdb93295eac437a09d89b1d619325123caa18cca))
* **proactive:** strip leading working-narration preamble from work-feed cards ([#2097](https://github.com/choiceoh/Deneb/issues/2097)) ([d527cbe](https://github.com/choiceoh/Deneb/commit/d527cbe52f253210600cec665f5a723352ec6e0b))
* **proactive:** suppress NO_REPLY in the native relay instead of delivering it ([#1968](https://github.com/choiceoh/Deneb/issues/1968)) ([c6b680b](https://github.com/choiceoh/Deneb/commit/c6b680b0dba1a60ff0fbd2b40cefed0b43694098))
* **process:** stop losing subprocess stdout to the Wait/drain race ([#2164](https://github.com/choiceoh/Deneb/issues/2164)) ([ac86df5](https://github.com/choiceoh/Deneb/commit/ac86df58562b7c0a4943d206303b7bf44b226bb9))
* **prompt:** unify search-tool routing guide (knowledge/wiki/polaris/graphify) ([#1920](https://github.com/choiceoh/Deneb/issues/1920)) ([5485aaa](https://github.com/choiceoh/Deneb/commit/5485aaa54012961165ed84a0e72a68af138b64cd))
* **provider:** strip Anthropic cache_control for Kimi (rejects it with HTTP 400) ([#2102](https://github.com/choiceoh/Deneb/issues/2102)) ([9cd4a74](https://github.com/choiceoh/Deneb/commit/9cd4a743134d117dfac2c533ec0a9a9e65d32b79))
* **server:** register proactive delivery sessions in the session manager ([#2213](https://github.com/choiceoh/Deneb/issues/2213)) ([57e083f](https://github.com/choiceoh/Deneb/commit/57e083fa81e18699421df887d32934df77a42352))
* **server:** serve in-app update over the gateway port ([#1937](https://github.com/choiceoh/Deneb/issues/1937)) ([46487d5](https://github.com/choiceoh/Deneb/commit/46487d55d455dd1f634b5a00727c09ee5a80e296))
* **skill:** re-land lost [#2119](https://github.com/choiceoh/Deneb/issues/2119)/[#2125](https://github.com/choiceoh/Deneb/issues/2125)/[#2126](https://github.com/choiceoh/Deneb/issues/2126) — catalog path resolution, review-loop waste, evolve revival ([#2235](https://github.com/choiceoh/Deneb/issues/2235)) ([45777f8](https://github.com/choiceoh/Deneb/commit/45777f83a5a5b9c7abcefd2ec80e4fcf461c7e54))
* **skill:** reach flat-layout skills and exempt no-op proposals from candidate ([#2112](https://github.com/choiceoh/Deneb/issues/2112)) ([12d7be3](https://github.com/choiceoh/Deneb/commit/12d7be349905dd11fdd0f50503af3c6435f04879))
* **skill:** strip LLM-echoed frontmatter in evolve rewrites + force version bump ([#2262](https://github.com/choiceoh/Deneb/issues/2262)) ([37164f7](https://github.com/choiceoh/Deneb/commit/37164f7dceec5255b3ada0b1e11fcbc637ef3e9e))
* **step3p7:** client-android build/APK + stream idle timeout stabilization ([#1901](https://github.com/choiceoh/Deneb/issues/1901)) ([542e443](https://github.com/choiceoh/Deneb/commit/542e443cbe8ed3b792772d757f4954cc7989668b))
* stop zombie session resurrection + guard mislabeled APK publishes ([#2082](https://github.com/choiceoh/Deneb/issues/2082)) ([cbf91b7](https://github.com/choiceoh/Deneb/commit/cbf91b7f20426aa0df73767fb6691bf6a414bb79))
* **text:** truncate Korean strings on rune boundaries to stop mojibake ([62d8d7c](https://github.com/choiceoh/Deneb/commit/62d8d7c10f9223a22bc2897ac06fa47cfc1df1a8))
* **tools:** clear error when edit/write target a directory ([#2134](https://github.com/choiceoh/Deneb/issues/2134)) ([80f750e](https://github.com/choiceoh/Deneb/commit/80f750e149f35a7307eb67ccf45b043f66ff2e3c))
* **tools:** give a clear hint when read targets a directory ([cb8b8c4](https://github.com/choiceoh/Deneb/commit/cb8b8c4b8bce006bf4082cd950d4ce9e26b15d5e))
* **tools:** read a directory as a listing instead of a hard error ([#2127](https://github.com/choiceoh/Deneb/issues/2127)) ([5f5506e](https://github.com/choiceoh/Deneb/commit/5f5506e235032d16bcab221e565e86bc9ea0b525))
* **tools:** tolerate string numeric params + surface grep's real error ([d76c940](https://github.com/choiceoh/Deneb/commit/d76c94036f4ee9c6aeaee8656c7d3de3b946c5a8))
* **topics:** warn when topics.map lacks the native home key ([940ad53](https://github.com/choiceoh/Deneb/commit/940ad53d984f333761011dca88b75ae7ffc964a6))
* **web:** make singleflight panic-safe so a fetch panic can't poison the URL key ([087ddab](https://github.com/choiceoh/Deneb/commit/087ddab5a5b4b79b619e76b4efff3cf73a1af12f))
* **weekly-report:** render PDF on real disk instead of full /tmp tmpfs ([#1909](https://github.com/choiceoh/Deneb/issues/1909)) ([b3726d5](https://github.com/choiceoh/Deneb/commit/b3726d5b3abe76942b972aaf7d7ab01f0d79e685))
* **wiki:** dream cycle backs off a full interval on synthesis failure + LLM sub-timeouts ([#2226](https://github.com/choiceoh/Deneb/issues/2226)) ([b144622](https://github.com/choiceoh/Deneb/commit/b1446222be3c719fe22e4c78391537c3b703acef))
* **wiki:** strip embedded frontmatter from page bodies to stop duplicate blocks ([#1805](https://github.com/choiceoh/Deneb/issues/1805)) ([b521899](https://github.com/choiceoh/Deneb/commit/b521899f68a2eca8c765eb0a620630202189c23c))
* **workfeed:** keep item ids unique across restarts ([#1961](https://github.com/choiceoh/Deneb/issues/1961)) ([854a6a9](https://github.com/choiceoh/Deneb/commit/854a6a9eefb6cc1a1d2eeadbdcf9b8510640f55a))
* **workfeed:** settle every item sharing an id on ack/snooze ([#2067](https://github.com/choiceoh/Deneb/issues/2067)) ([842cca3](https://github.com/choiceoh/Deneb/commit/842cca3e3a0d465a3b53fb29779f4a9bc03db88d))


### ⚡ Performance

* **chat:** cache image decode + AnnotatedString across scroll disposal ([cc36738](https://github.com/choiceoh/Deneb/commit/cc36738885502435e5a192613112eea5d6940f0b))
* **chat:** cache markdown parse across scroll to smooth pure scrolling ([835211a](https://github.com/choiceoh/Deneb/commit/835211a65e62c46933cc934dd1003aa5fa89e0af))
* **chat:** coalesce streaming token updates to ~30/s ([26d7bf4](https://github.com/choiceoh/Deneb/commit/26d7bf4da087a4d99c6d82086285bb351ce1b461))
* **chat:** compact tool output before context injection (ANSI + adjacent dedup) ([#2100](https://github.com/choiceoh/Deneb/issues/2100)) ([d85eb78](https://github.com/choiceoh/Deneb/commit/d85eb78fe9f815720e037cde4eccf6422f40b85b))
* **chat:** cross-source recall dedup + cue-adaptive evidence budget ([#2206](https://github.com/choiceoh/Deneb/issues/2206)) ([f826ce4](https://github.com/choiceoh/Deneb/commit/f826ce461153bc3a3d0956fb653a70a6cf3312f5))
* **chat:** DENEB_MEMORY_TOKEN_BUDGET override for latency-bound local serving ([#2229](https://github.com/choiceoh/Deneb/issues/2229)) ([0f32fb4](https://github.com/choiceoh/Deneb/commit/0f32fb4f06241d357f910f8e21a7528a4ab918f8))
* **chat:** let unchanged messages skip recomposition while a reply streams ([2931486](https://github.com/choiceoh/Deneb/commit/29314862c7c38c006d65a570626c9f4999530d55))
* **chat:** prefetch the message list with a cache window (pausable composition) ([#2064](https://github.com/choiceoh/Deneb/issues/2064)) ([0402362](https://github.com/choiceoh/Deneb/commit/0402362864f771f6d8526d6c9b58f0be69a77fe1))
* **chat:** throttle streaming markdown re-parse to ~16fps ([a065043](https://github.com/choiceoh/Deneb/commit/a0650437fde52ef57aa3773ea0af3b50809f03f0))
* **chat:** tune streaming throttles toward smoothness over liveness ([c01875e](https://github.com/choiceoh/Deneb/commit/c01875ed4029dc965a7a70001ddf471324403454))
* **client-android:** prefix-caching for kai-ui (vLLM catalog + Anthropic system) ([#1931](https://github.com/choiceoh/Deneb/issues/1931)) ([f2c12b6](https://github.com/choiceoh/Deneb/commit/f2c12b6ac6960da4dab6d111a404635efeab6b7f))
* **client-android:** streaming/RPC hot-path optimizations + locale cleanup ([#1951](https://github.com/choiceoh/Deneb/issues/1951)) ([1c78018](https://github.com/choiceoh/Deneb/commit/1c78018e27edd2190c21725eb6211163ff5a49bc))
* **compaction:** incremental recompaction (update prior summary, not re-summarize) ([#2109](https://github.com/choiceoh/Deneb/issues/2109)) ([b1ce0cc](https://github.com/choiceoh/Deneb/commit/b1ce0ccb363f2d553966344e715a986f618a9428))
* **compaction:** prevent tool_use/tool_result orphans at the cut, not after ([#2103](https://github.com/choiceoh/Deneb/issues/2103)) ([7dbaaff](https://github.com/choiceoh/Deneb/commit/7dbaaffd579c6d4d51ac15b017ec314ad3432037))


### 🔧 Internal

* **chat:** address bm25 fetch_tools review feedback ([#1812](https://github.com/choiceoh/Deneb/issues/1812)) ([db71c3a](https://github.com/choiceoh/Deneb/commit/db71c3a8c23b59bcfa512afc8546dc5b1d1ef945))
* **chat:** slash layer scoped to ops commands + sync-path fix + tuner follow-ups ([#2171](https://github.com/choiceoh/Deneb/issues/2171)) ([d77c674](https://github.com/choiceoh/Deneb/commit/d77c674952d4b1184298dafc3eaa18c5ca259b10))
* **chat:** split run_exec.go into stage-cohesive files under the 700-LOC rule ([#2233](https://github.com/choiceoh/Deneb/issues/2233)) ([489f516](https://github.com/choiceoh/Deneb/commit/489f516a2b5b22cea7568dec3f135c9e2ad7c34c))
* **client-android:** adopt generated analysis + sender types ([37319ac](https://github.com/choiceoh/Deneb/commit/37319acae390d653193cb4193cd9d84875905525))
* **client-android:** adopt generated gmail mail wire types ([3ef8413](https://github.com/choiceoh/Deneb/commit/3ef8413b6588b9ea334b062c2165f5939f07510d))
* **client-android:** adopt generated miniapp wire types ([4be863a](https://github.com/choiceoh/Deneb/commit/4be863adf7a43cbd60406df030118a544ee7a2d3))
* **client-android:** adopt generated session wire types ([a32355b](https://github.com/choiceoh/Deneb/commit/a32355b9c3c010e93377d66bf8d555c6f6bfa45c))
* **client-android:** drop chat topic switcher and share topic picker ([#1889](https://github.com/choiceoh/Deneb/issues/1889)) ([4009a54](https://github.com/choiceoh/Deneb/commit/4009a54dc14a33f4d2cc7d7c1f11aa70a01f7207))
* **client-android:** extract domain models out of DenebGatewayClient ([83f252e](https://github.com/choiceoh/Deneb/commit/83f252e53b9f8db216eda971ff8c5baca19ccc11))
* **client-android:** extract RPC wire payloads out of DenebGatewayClient ([daaa031](https://github.com/choiceoh/Deneb/commit/daaa03117e3f0fa6c753458025c4985c978543b6))
* **core:** drop dead Telegram-era markdown IR pipeline and unused insights accessor ([#2220](https://github.com/choiceoh/Deneb/issues/2220)) ([35e8b09](https://github.com/choiceoh/Deneb/commit/35e8b0932330fc662e064cae12fe7c6b8fc3a2b9))
* **gateway:** drop dead miniapp topic wiring after client topic removal ([#1895](https://github.com/choiceoh/Deneb/issues/1895)) ([0512470](https://github.com/choiceoh/Deneb/commit/05124708cf5f411094d22cb19540f13667c2047e))
* **miniapp:** move topic docs into settings, not home ([#1807](https://github.com/choiceoh/Deneb/issues/1807)) ([f082fc5](https://github.com/choiceoh/Deneb/commit/f082fc53fd757a7b7d5bf11086be5e41f6eea01c))
* **miniapp:** remove Telegram Mini App web surface, native client only ([#1891](https://github.com/choiceoh/Deneb/issues/1891)) ([754645c](https://github.com/choiceoh/Deneb/commit/754645c805f6c91106f362d9c9f2d7a76d022146))
* **native:** de-Kai client — ai.deneb package, deneb-ui fence, drop on-device inference ([#2128](https://github.com/choiceoh/Deneb/issues/2128)) ([a82341b](https://github.com/choiceoh/Deneb/commit/a82341bac024bc8b4eba923d550f6c8d47b44d0e))
* **native:** extract the local chat pipeline from RemoteDataRepository ([#2166](https://github.com/choiceoh/Deneb/issues/2166)) ([fbb8d3f](https://github.com/choiceoh/Deneb/commit/fbb8d3f2db0b6fb7c8c861c76ff56e85868b0048))
* **native:** migrate screen typography to the reworked DenebType roles ([#2176](https://github.com/choiceoh/Deneb/issues/2176)) ([25f5586](https://github.com/choiceoh/Deneb/commit/25f5586b318eaecde109aef3b5d531a3038a0cdf))
* **native:** remove unreachable Kai settings tree + sandbox UI (~10k LOC) ([#2137](https://github.com/choiceoh/Deneb/issues/2137)) ([2949140](https://github.com/choiceoh/Deneb/commit/29491400d4d6bc7c6642bda0ecdc90c364119f48))
* **native:** split deneb-ui renderer, chat screen modes, calendar month math ([#2162](https://github.com/choiceoh/Deneb/issues/2162)) ([5beec03](https://github.com/choiceoh/Deneb/commit/5beec034cf95985709021403a38307360d5b31b6))
* **native:** split DenebGatewayClient + settings hub into domain files ([#2159](https://github.com/choiceoh/Deneb/issues/2159)) ([b4546c4](https://github.com/choiceoh/Deneb/commit/b4546c45acaf7f0c3c284e8a739ce9bfa67ccf91))
* per-package dead-code cleanup from the static audit (deadcode ./cmd/...) ([#2224](https://github.com/choiceoh/Deneb/issues/2224)) ([035141c](https://github.com/choiceoh/Deneb/commit/035141c0367efd94329c553597ab5e3b2efcb404))
* **release:** versionCode-only build identity, align OTA workflow ([#2099](https://github.com/choiceoh/Deneb/issues/2099)) ([a6b52af](https://github.com/choiceoh/Deneb/commit/a6b52aff1af27898ec6de8257aa4ebc2f28649c0))
* **server:** retire the dormant background-task control plane (domain/tasks) ([#2240](https://github.com/choiceoh/Deneb/issues/2240)) ([8ccd02e](https://github.com/choiceoh/Deneb/commit/8ccd02ebb267a35f8d71d10f134e102ddf02c5bc))
* **session:** collapse to one main session, retire topic-as-session and Telegram session paths ([9ccefc2](https://github.com/choiceoh/Deneb/commit/9ccefc2093cb1975533881f7543641741a98a85b))
* split gmailpoll pipeline, wiki dreamer, and agent executor under the 700-LOC rule ([#2237](https://github.com/choiceoh/Deneb/issues/2237)) ([752da77](https://github.com/choiceoh/Deneb/commit/752da7776485c52787e5746f78535f289800c3db))
* split the remaining eight 700+ LOC files (final 700-LOC-rule pass) ([#2242](https://github.com/choiceoh/Deneb/issues/2242)) ([9159543](https://github.com/choiceoh/Deneb/commit/9159543911ce01cb303ce8747b935db573cc1a84))
* split wiki store, miniapp models, and miniapp memory handler under the 700-LOC rule ([#2241](https://github.com/choiceoh/Deneb/issues/2241)) ([9030247](https://github.com/choiceoh/Deneb/commit/9030247b4e65854b990c93df5801460e0afb97bb))
* **subagent:** retire the Telegram-only /focus and /unfocus commands ([#2209](https://github.com/choiceoh/Deneb/issues/2209)) ([f704823](https://github.com/choiceoh/Deneb/commit/f704823ed816c452c8673d480cd1bacf4aedc74d))
* **ui:** codify color/spacing/component doctrine and close two drifts ([#2186](https://github.com/choiceoh/Deneb/issues/2186)) ([f8fd41c](https://github.com/choiceoh/Deneb/commit/f8fd41c4f09b41c6e3e223857743badaecd2b46a))
* **ui:** ride chat markdown headings and dynamic-ui stats on the DenebType scale ([#2187](https://github.com/choiceoh/Deneb/issues/2187)) ([178d9c3](https://github.com/choiceoh/Deneb/commit/178d9c38d49295f6fe5bdce1846708a1aa317512))

## [4.23.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.22.3...deneb-v4.23.0) (2026-05-30)


### ✨ Features

* 2.5-layer 메모리 통합 (wiki/polaris/hindsight) ([#1669](https://github.com/choiceoh/Deneb/issues/1669)) ([6bc2047](https://github.com/choiceoh/Deneb/commit/6bc20476a700dd7b3050d2b43f855e6af7e4e5fa))
* **agent:** line ranker for CompactPriorToolResults ([#1514](https://github.com/choiceoh/Deneb/issues/1514)) ([445adaf](https://github.com/choiceoh/Deneb/commit/445adaf0c9b967319cc131fdcc932581fda4be35))
* **chat:** /restart restarts immediately (no confirm gate) ([#1682](https://github.com/choiceoh/Deneb/issues/1682)) ([3dbae3e](https://github.com/choiceoh/Deneb/commit/3dbae3ef5e447f5dd81758c9d17c88a66cd9350b))
* **chat:** /update auto-stashes untracked files ([#1690](https://github.com/choiceoh/Deneb/issues/1690)) ([587c09f](https://github.com/choiceoh/Deneb/commit/587c09ff57186f0a91b5932a2490545be31be94a))
* **chat:** add graphify tool + build initial knowledge graph ([#1531](https://github.com/choiceoh/Deneb/issues/1531)) ([23ca5b9](https://github.com/choiceoh/Deneb/commit/23ca5b938ba7d2b2c731835d47b7e68aa34634e1))
* **chat:** add Hindsight memory provider integration ([#1640](https://github.com/choiceoh/Deneb/issues/1640)) ([073c481](https://github.com/choiceoh/Deneb/commit/073c4819b6b0e1281f10c444778ab2f850f4f2c0))
* **chat:** add STW compaction feedback with typing keepalive ([#1517](https://github.com/choiceoh/Deneb/issues/1517)) ([771b3e5](https://github.com/choiceoh/Deneb/commit/771b3e5aa2e3c6f28e39475cedfdc76f86733fa2))
* **chat:** ban markdown tables in Telegram replies ([9777e4c](https://github.com/choiceoh/Deneb/commit/9777e4c8c11aab59192d70b430b314638bc7c236))
* **chat:** ban markdown tables in Telegram replies ([fd0b877](https://github.com/choiceoh/Deneb/commit/fd0b877e6aca135aa3515bfc0289294f13b1635b))
* **chat:** chief-of-staff persona and gmail analyze in system prompt ([#1784](https://github.com/choiceoh/Deneb/issues/1784)) ([771311a](https://github.com/choiceoh/Deneb/commit/771311a56218284e1c53679c01d3cca07eca046d))
* **chat:** deal tracking — 거래 category, gmail thread, deadline watch ([#1642](https://github.com/choiceoh/Deneb/issues/1642)) ([ef010a4](https://github.com/choiceoh/Deneb/commit/ef010a4b5c84e6b8b5a6164b24c944400ba9efb5))
* **chat:** Hermes-inspired prompt cache improvements ([a2a455c](https://github.com/choiceoh/Deneb/commit/a2a455c8c55964fdbbef6159ec5c588ed44573d9))
* **chat:** inject per-topic knowledge into system prompt ([#1800](https://github.com/choiceoh/Deneb/issues/1800)) ([c0ad704](https://github.com/choiceoh/Deneb/commit/c0ad7047d45ea42e5e9b2e5c974529fde68cdb3b))
* **chat:** introduce Anthropic interleaved thinking ([#1600](https://github.com/choiceoh/Deneb/issues/1600)) ([48dc44b](https://github.com/choiceoh/Deneb/commit/48dc44b4323a101a82d87596804f6b27c7e60de6))
* **chat:** merge consecutive messages within 3s by cancelling active run ([#1528](https://github.com/choiceoh/Deneb/issues/1528)) ([a51ed03](https://github.com/choiceoh/Deneb/commit/a51ed03c9c7fda914f3a1ea43e780197a8841861))
* **chat:** promote polaris to default toolset with active-recall guide ([#1656](https://github.com/choiceoh/Deneb/issues/1656)) ([150d8d0](https://github.com/choiceoh/Deneb/commit/150d8d0ff3c8addcefd6c6a084de460f61e462fa))
* **chat:** remove work mode and continuous run ([088f871](https://github.com/choiceoh/Deneb/commit/088f871f89172dedcc0f98efc1e260e9753b8230))
* **chat:** remove work mode and continuous run ([2b887d9](https://github.com/choiceoh/Deneb/commit/2b887d984a96f05bbf85187bd11dc53df51abd4c))
* **chat:** require replyFunc — fail-fast instead of silent drop ([#1545](https://github.com/choiceoh/Deneb/issues/1545)) ([ff40794](https://github.com/choiceoh/Deneb/commit/ff407946017c9f0915d206f16f60974278131662))
* **chat:** restore fetch_tools meta-tool for deferred tool activation ([bf6f5d9](https://github.com/choiceoh/Deneb/commit/bf6f5d9fe1cb4f38083c4822deff226451eb6d1d))
* **chat:** restore read_spillover + fetch_tools, tighten tool output budgets ([d36ead1](https://github.com/choiceoh/Deneb/commit/d36ead1117556b49143bcf5d456e7d19562ba167))
* **chat:** restore read_spillover tool (eager registration) ([9e3bdaf](https://github.com/choiceoh/Deneb/commit/9e3bdaf6845649f91b89a42e1a50cd3bff5e8b11))
* **chat:** specialize agent toward business email analysis ([#1639](https://github.com/choiceoh/Deneb/issues/1639)) ([d848c05](https://github.com/choiceoh/Deneb/commit/d848c05ecc395129e192779b0d56f6635c186282))
* **chat:** sticky compaction reminder + threshold rationale (P3+P4) ([e985642](https://github.com/choiceoh/Deneb/commit/e985642b076eaec5e25ef9e3d1d7e7f781bb4815))
* **chat:** strengthen skill prompt for higher utilization ([8a49550](https://github.com/choiceoh/Deneb/commit/8a4955056fed0c44160ac524b6ee84198d59bf87))
* **chat:** strengthen skill prompt to Korean imperative for higher utilization ([8675427](https://github.com/choiceoh/Deneb/commit/867542799e92c78d804bcb8e902aa4efcc269167))
* **chat:** strengthen sub-agent delegation prompt ([49784a0](https://github.com/choiceoh/Deneb/commit/49784a0f3b2c06222a88ce079248070b50969aa8))
* **chat:** strengthen sub-agent delegation prompt for higher spawn rate ([6ba9b54](https://github.com/choiceoh/Deneb/commit/6ba9b54786fbb1c8a092415c9f87713670325deb))
* **chat:** suggest similar tool names on unknown-tool errors ([faaa59d](https://github.com/choiceoh/Deneb/commit/faaa59d8ebfd1696b0f4769e356fb8f677f79a68))
* **chat:** surface extended thinking in Telegram replies ([#1606](https://github.com/choiceoh/Deneb/issues/1606)) ([f307d9a](https://github.com/choiceoh/Deneb/commit/f307d9ae6abec34a2354547a443a40a6254baa03))
* **chat:** surface preparing/recalling phase signals ([2616e5c](https://github.com/choiceoh/Deneb/commit/2616e5c74df235feef78a22e83569a743d35e444))
* **chat:** wiki-writeback policy + attachment forwarding chain guide ([#1661](https://github.com/choiceoh/Deneb/issues/1661)) ([833aa8f](https://github.com/choiceoh/Deneb/commit/833aa8fdf01c3ec36c14c20b6b4df018c0d42105))
* **compaction:** add BGE-M3 embedding + MMR compaction fallback ([#1518](https://github.com/choiceoh/Deneb/issues/1518)) ([7f6899d](https://github.com/choiceoh/Deneb/commit/7f6899dbd1c88df4f4a9fd1e7a27737327c37f4a))
* **cron:** add POST /api/cron/run REST endpoint ([42a0cb9](https://github.com/choiceoh/Deneb/commit/42a0cb9b15981dc649f162f44e259b3c2d3cf527))
* **deploy:** add auto-deploy.sh for unattended main-branch redeploys ([#1569](https://github.com/choiceoh/Deneb/issues/1569)) ([9992db4](https://github.com/choiceoh/Deneb/commit/9992db44e1e7ef3b43d1b7ddce93f48c3b8e27d8))
* flood-control strikes for draft stream + time-based compaction cooldown ([#1558](https://github.com/choiceoh/Deneb/issues/1558)) ([521eec6](https://github.com/choiceoh/Deneb/commit/521eec65cbf3947d7b7e50eb4e4968436d5ca360))
* **genesis:** wire Catalog + session context loader ([8a7b40d](https://github.com/choiceoh/Deneb/commit/8a7b40d441282d24f423085446196892eb8a7925))
* **genesis:** wire Catalog + session context loader — fix two critical genesis defects ([5497283](https://github.com/choiceoh/Deneb/commit/5497283b09c7d35ccc45e553a43d262ba3165c74))
* **gmail:** cite related projects in mail detail and persist per-email analysis on poll ([#1772](https://github.com/choiceoh/Deneb/issues/1772)) ([74d1461](https://github.com/choiceoh/Deneb/commit/74d14610ff846e8da45f638f16b082efba5a6808))
* **gmail:** configurable proactive delivery target for poll summaries ([#1776](https://github.com/choiceoh/Deneb/issues/1776)) ([3fac174](https://github.com/choiceoh/Deneb/commit/3fac17477fe6d0470c98ef0d166e37bf98f9bcda))
* **gmail:** Excel/OCR/download/analyze (the [#1652](https://github.com/choiceoh/Deneb/issues/1652) follow-ups that didn't merge) ([#1658](https://github.com/choiceoh/Deneb/issues/1658)) ([72be85a](https://github.com/choiceoh/Deneb/commit/72be85a1a2f9fee7b7a1fdba823d0e33c66498d6))
* **gmail:** expand header/footer filters and strip reply-quote tails ([#1747](https://github.com/choiceoh/Deneb/issues/1747)) ([925cfb1](https://github.com/choiceoh/Deneb/commit/925cfb1c27d2466f8e1cc64ec8e95aca1619f797))
* **gmail:** extract Word (.docx) and PowerPoint (.pptx) attachments ([#1659](https://github.com/choiceoh/Deneb/issues/1659)) ([fbaa11a](https://github.com/choiceoh/Deneb/commit/fbaa11ae1fff6feed7c554de2ce2e6b9e2846c21))
* **gmail:** inline follow-up Q&A on mail analysis (ephemeral, context-grounded) ([#1779](https://github.com/choiceoh/Deneb/issues/1779)) ([0ae8693](https://github.com/choiceoh/Deneb/commit/0ae8693704cf470c8a0f2615f8c36cdc66b7d481))
* **gmail:** make read/unread status visually distinct in mail list ([#1737](https://github.com/choiceoh/Deneb/issues/1737)) ([539c5e6](https://github.com/choiceoh/Deneb/commit/539c5e6d48e8be5e3102c9067ac1a68860d3c39a))
* **gmailpoll:** local-AI fact extraction appended to analyze output ([#1663](https://github.com/choiceoh/Deneb/issues/1663)) ([c7c1451](https://github.com/choiceoh/Deneb/commit/c7c14518a7df867b8c576fc592d32d920223091e))
* **gmailpoll:** populate MemoryContext via wiki-graph query for sender ([#1662](https://github.com/choiceoh/Deneb/issues/1662)) ([5c526c8](https://github.com/choiceoh/Deneb/commit/5c526c8518144a4cda33d656a700904711474b3b))
* **gmail:** read email attachments (PDF text) and fix base64 decoding ([#1652](https://github.com/choiceoh/Deneb/issues/1652)) ([0c7a8e8](https://github.com/choiceoh/Deneb/commit/0c7a8e89776d14d6a880fb75012873c50299e624))
* **heartbeat:** 5m cadence + active hours; ship missed qwen3 tuning ([#1540](https://github.com/choiceoh/Deneb/issues/1540)) ([1fc519e](https://github.com/choiceoh/Deneb/commit/1fc519e98da5217814ba1878e60dbcb7e72f4832))
* **heartbeat:** join active telegram session, drop context isolation ([#1587](https://github.com/choiceoh/Deneb/issues/1587)) ([90d031e](https://github.com/choiceoh/Deneb/commit/90d031ef9a19eb355b39fb4a649d63e5dc9634cf))
* **heartbeat:** make trigger turn ephemeral, add system-prompt rule ([#1588](https://github.com/choiceoh/Deneb/issues/1588)) ([09794b9](https://github.com/choiceoh/Deneb/commit/09794b9455c837391c7f7693af175fb2dbbc6660))
* **hindsight:** wire self-memory reflex across SOUL.md, system prompt, and a skill ([#1667](https://github.com/choiceoh/Deneb/issues/1667)) ([8e8351c](https://github.com/choiceoh/Deneb/commit/8e8351cc6007d317388746ce68436403c3e8c326))
* JSON-safe tool-args truncation + compaction handoff framing ([#1557](https://github.com/choiceoh/Deneb/issues/1557)) ([3161d02](https://github.com/choiceoh/Deneb/commit/3161d02f5c55ef05c87a602750d52676a39d92e7))
* **knowledge:** unify wiki + hindsight under one agent tool ([#1670](https://github.com/choiceoh/Deneb/issues/1670)) ([fd1737e](https://github.com/choiceoh/Deneb/commit/fd1737eda1e52e752a2fc2c670ba8ecde29231d1))
* **llm:** route z.ai through Anthropic Messages API ([#1602](https://github.com/choiceoh/Deneb/issues/1602)) ([f487831](https://github.com/choiceoh/Deneb/commit/f487831690b29908d56054e15a2f9a31fc76b10f))
* **media:** add ZIP archive support for inbound Telegram documents ([#1525](https://github.com/choiceoh/Deneb/issues/1525)) ([55c2ac9](https://github.com/choiceoh/Deneb/commit/55c2ac973c4c57ae284b2a5ac53f9c96b7dda84d))
* **miniapp:** "🔍 분석" button — LLM email analysis on demand ([#1680](https://github.com/choiceoh/Deneb/issues/1680)) ([2cc5a87](https://github.com/choiceoh/Deneb/commit/2cc5a87c26461363c245f0af32021fea43e61e3f))
* **miniapp:** add bulk mail selection ([12bb584](https://github.com/choiceoh/Deneb/commit/12bb584a3f74d52dde9a1db94771456e5a2355c2))
* **miniapp:** add new forum topic creation from topics view ([#1748](https://github.com/choiceoh/Deneb/issues/1748)) ([945b009](https://github.com/choiceoh/Deneb/commit/945b0091480a73c5d835f27eea4751ac646b4196))
* **miniapp:** add project page merge ([#1797](https://github.com/choiceoh/Deneb/issues/1797)) ([d99c3b8](https://github.com/choiceoh/Deneb/commit/d99c3b8453afb14a7e42d5ed2a82766b02d35a04))
* **miniapp:** add settings tab ([7b24cbf](https://github.com/choiceoh/Deneb/commit/7b24cbfdfcec3de4edb08b36dfbd479eb97ff2df))
* **miniapp:** auto-fill context window and name for custom models ([#1794](https://github.com/choiceoh/Deneb/issues/1794)) ([7ce316d](https://github.com/choiceoh/Deneb/commit/7ce316d9f84fc43c690ddeb1d30a9c6f485f1ed8))
* **miniapp:** backend RPC route + initData auth middleware ([#1673](https://github.com/choiceoh/Deneb/issues/1673)) ([ba9829f](https://github.com/choiceoh/Deneb/commit/ba9829fe92eff96c8e55e5fc3f85094a10e79c6c))
* **miniapp:** cache mail analyses + write to wiki ([#1696](https://github.com/choiceoh/Deneb/issues/1696)) ([064ad91](https://github.com/choiceoh/Deneb/commit/064ad9131e25e1879dddc410c617295ada3cc8c5))
* **miniapp:** chat surface — backend RPC + UI ([#1688](https://github.com/choiceoh/Deneb/issues/1688)) ([23c0e20](https://github.com/choiceoh/Deneb/commit/23c0e2087dda67c4aae23fc20471f544d59092a7))
* **miniapp:** clickable sender-context wiki chips + wiki page inline edit ([#1716](https://github.com/choiceoh/Deneb/issues/1716)) ([2f69dcc](https://github.com/choiceoh/Deneb/commit/2f69dccecbe168807a1854f5ded782c4eba74255))
* **miniapp:** collapse home status to current model only ([#1681](https://github.com/choiceoh/Deneb/issues/1681)) ([9cf0c0a](https://github.com/choiceoh/Deneb/commit/9cf0c0a79e3e155d6eea336ac6974086cb26ca8c))
* **miniapp:** context-attach chat — open mail/wiki/session in chat ([#1697](https://github.com/choiceoh/Deneb/issues/1697)) ([6bb754e](https://github.com/choiceoh/Deneb/commit/6bb754ed735114c6418bb980f984f8728d539cd2))
* **miniapp:** cron detail view with edit, run, and delete ([#1791](https://github.com/choiceoh/Deneb/issues/1791)) ([78d8c40](https://github.com/choiceoh/Deneb/commit/78d8c40b164d8d6113430b1f680d4626e76567d7))
* **miniapp:** custom model deletion and cloud health probing ([#1792](https://github.com/choiceoh/Deneb/issues/1792)) ([726d6ec](https://github.com/choiceoh/Deneb/commit/726d6ecb22ee93d83dd32e43d7648c7ca9d66d98))
* **miniapp:** deeper polish — focal hover, body stagger, skeleton, dot pulse ([#1733](https://github.com/choiceoh/Deneb/issues/1733)) ([fa5fffa](https://github.com/choiceoh/Deneb/commit/fa5fffac6d19b3332cd4ca1a11c93b5771914b0f))
* **miniapp:** desktop optimization — platform class, viewport scaling, kbd nav ([#1765](https://github.com/choiceoh/Deneb/issues/1765)) ([5116bd6](https://github.com/choiceoh/Deneb/commit/5116bd69b7970e7f86594a34da8cc43112332841))
* **miniapp:** drop the more tab; everything lives on home ([#1729](https://github.com/choiceoh/Deneb/issues/1729)) ([8aec790](https://github.com/choiceoh/Deneb/commit/8aec790d840e5b9af9fed57f548d2470fc6f4340))
* **miniapp:** edit topic knowledge files from the Mini App ([#1803](https://github.com/choiceoh/Deneb/issues/1803)) ([27710e8](https://github.com/choiceoh/Deneb/commit/27710e84c3127bbeab65eff603ef00291a2c7b82))
* **miniapp:** elite polish — direction-aware transitions, letter cascade, scroll-hide ([#1731](https://github.com/choiceoh/Deneb/issues/1731)) ([0eb9d68](https://github.com/choiceoh/Deneb/commit/0eb9d682c9ebfe2dc131e282bdeb199816ab4ae2))
* **miniapp:** expose 'new topic' on home menu ([#1760](https://github.com/choiceoh/Deneb/issues/1760)) ([9926cb9](https://github.com/choiceoh/Deneb/commit/9926cb954ee86b9c94d4723d8162581bff36bf9f))
* **miniapp:** final polish — cards, motion, markdown, dead code ([#1728](https://github.com/choiceoh/Deneb/issues/1728)) ([34e18bb](https://github.com/choiceoh/Deneb/commit/34e18bb6e9e09807cd7a3f9f014604b9a10b01db))
* **miniapp:** final typography sweep — strip detail SVGs, flatten globals ([#1725](https://github.com/choiceoh/Deneb/issues/1725)) ([3f9aa96](https://github.com/choiceoh/Deneb/commit/3f9aa96e864c5f45c1c2782fcfce6d7068f65214))
* **miniapp:** finish typography pass — list, more, settings, detail, Inter ([#1722](https://github.com/choiceoh/Deneb/issues/1722)) ([135073c](https://github.com/choiceoh/Deneb/commit/135073cc7db441f977fd321bdb50eba332008373))
* **miniapp:** global toast/flash system — pill stack, slide-up, variants ([#1761](https://github.com/choiceoh/Deneb/issues/1761)) ([e873532](https://github.com/choiceoh/Deneb/commit/e873532547f744fab946ed43969438b9e1e8ce8a))
* **miniapp:** Gmail triage — backend RPC + frontend UI ([#1676](https://github.com/choiceoh/Deneb/issues/1676)) ([8bd8292](https://github.com/choiceoh/Deneb/commit/8bd8292ab676a1cc0f7928c3b36fdce91040272f))
* **miniapp:** Google Calendar integration + D-15min briefing push ([#1687](https://github.com/choiceoh/Deneb/issues/1687)) ([80d8b0e](https://github.com/choiceoh/Deneb/commit/80d8b0eaf9f757719464aabeaf755795890480e5))
* **miniapp:** home → pure black-and-white typography ([#1715](https://github.com/choiceoh/Deneb/issues/1715)) ([cf54e47](https://github.com/choiceoh/Deneb/commit/cf54e476a2c79cbb9d465c5a19eaa62956a482d5))
* **miniapp:** home → Zune HD / Metro typography redesign ([#1714](https://github.com/choiceoh/Deneb/issues/1714)) ([485fa66](https://github.com/choiceoh/Deneb/commit/485fa660b4c4df6d171bdbe5fc8c2886d9fffc25))
* **miniapp:** inline 🗑 + 더 보기 pagination on inbox (with review fixes) ([#1691](https://github.com/choiceoh/Deneb/issues/1691)) ([6350755](https://github.com/choiceoh/Deneb/commit/63507557015f3721e3df95bb09fa218868e13101))
* **miniapp:** inline read/archive on list + graphify in sender_context + test cleanup ([#1694](https://github.com/choiceoh/Deneb/issues/1694)) ([5b1da02](https://github.com/choiceoh/Deneb/commit/5b1da02d593a47b9c9217569efb3d5316dc002b2))
* **miniapp:** memory.search + sessions.recent (backend + UI) ([#1678](https://github.com/choiceoh/Deneb/issues/1678)) ([c7452aa](https://github.com/choiceoh/Deneb/commit/c7452aab090c269213479cbd3fced9583cb11f0e))
* **miniapp:** Mira-inspired bottom tab bar + 더보기 hub page ([#1693](https://github.com/choiceoh/Deneb/issues/1693)) ([5010749](https://github.com/choiceoh/Deneb/commit/5010749ae42393e9abbaab8268e315b38143431d))
* **miniapp:** move 'new topic' to a chip on the topics view ([#1764](https://github.com/choiceoh/Deneb/issues/1764)) ([6b9a6d2](https://github.com/choiceoh/Deneb/commit/6b9a6d22f3ff20d8153920a2f65ef7a8f1c290a0))
* **miniapp:** optimistic mail actions + full haptic wiring ([#1758](https://github.com/choiceoh/Deneb/issues/1758)) ([0201a2f](https://github.com/choiceoh/Deneb/commit/0201a2fbcab99b698ec601eb89dd2ff47b18f792))
* **miniapp:** person detail view — tap a person card to drill in ([#1711](https://github.com/choiceoh/Deneb/issues/1711)) ([2f8926f](https://github.com/choiceoh/Deneb/commit/2f8926f597ef717654215a748e39911a5883ecab))
* **miniapp:** Pretendard font + settings pared to the model picker ([#1773](https://github.com/choiceoh/Deneb/issues/1773)) ([7609636](https://github.com/choiceoh/Deneb/commit/7609636bb49e114ec4db6b926670208a5a965f94))
* **miniapp:** real-black bg, more = home idiom, Zune panorama top tabs ([#1724](https://github.com/choiceoh/Deneb/issues/1724)) ([435cfc3](https://github.com/choiceoh/Deneb/commit/435cfc30ed83cb72281d0c173999d6cdf26a7011))
* **miniapp:** real-black dark bg + Zune-style page transitions ([#1720](https://github.com/choiceoh/Deneb/issues/1720)) ([7926a38](https://github.com/choiceoh/Deneb/commit/7926a38d63a9cd29a2a8bb041f0a7b1ac7b10f3d))
* **miniapp:** relabel sessions view to topics ([#1727](https://github.com/choiceoh/Deneb/issues/1727)) ([770e2ed](https://github.com/choiceoh/Deneb/commit/770e2ed0486542df8d0ee593e6e77dac75cf20ef))
* **miniapp:** replace emoji UI with Lucide SVG icons + refresh hero copy ([#1710](https://github.com/choiceoh/Deneb/issues/1710)) ([fee1a40](https://github.com/choiceoh/Deneb/commit/fee1a40fa237a799654b7375c83bc7a13379eddf))
* **miniapp:** replace refresh button with pull-to-refresh ([#1734](https://github.com/choiceoh/Deneb/issues/1734)) ([c4f7057](https://github.com/choiceoh/Deneb/commit/c4f7057d6bd7c31914f0f5c05818af249ac30e5f))
* **miniapp:** sender context card — backend RPC + detail UI ([#1679](https://github.com/choiceoh/Deneb/issues/1679)) ([85b421b](https://github.com/choiceoh/Deneb/commit/85b421b3b3a437b5257af72c418962423368c928))
* **miniapp:** show last 7 days of inbox + add trash button ([#1686](https://github.com/choiceoh/Deneb/issues/1686)) ([75c4622](https://github.com/choiceoh/Deneb/commit/75c4622852442021706ac17301d22c5fa8531242))
* **miniapp:** strip marketing chrome from mail bodies ([#1698](https://github.com/choiceoh/Deneb/issues/1698)) ([215319b](https://github.com/choiceoh/Deneb/commit/215319bfec668766b3399437d589783eb9bc9f0c))
* **miniapp:** surface most-recent important mail on home ([#1736](https://github.com/choiceoh/Deneb/issues/1736)) ([21827e9](https://github.com/choiceoh/Deneb/commit/21827e9de19765fee84b464df2dda5a08bfa93af))
* **miniapp:** tone down icon tiles — Linear/Notion-style muted tints ([#1712](https://github.com/choiceoh/Deneb/issues/1712)) ([8e21751](https://github.com/choiceoh/Deneb/commit/8e21751eac0d324e74653613f9548868a2369b1b))
* **miniapp:** unify memory/diary/people behind single search entry ([#1751](https://github.com/choiceoh/Deneb/issues/1751)) ([1dd9118](https://github.com/choiceoh/Deneb/commit/1dd911843d690593ef30439d36f02df83232fb59))
* **miniapp:** unify view headers in pure typography idiom ([#1719](https://github.com/choiceoh/Deneb/issues/1719)) ([6b8b8db](https://github.com/choiceoh/Deneb/commit/6b8b8db7e6579bbf39e08d25c38ccd96262f8d65))
* **miniapp:** visual polish — depth tokens, hero, action hierarchy ([#1707](https://github.com/choiceoh/Deneb/issues/1707)) ([f3967ed](https://github.com/choiceoh/Deneb/commit/f3967ed2df71c6e54d22ecc886327ace921683c6))
* **miniapp:** Vite frontend + embed.FS + Cloudflare Tunnel runbook ([#1674](https://github.com/choiceoh/Deneb/issues/1674)) ([28d21ba](https://github.com/choiceoh/Deneb/commit/28d21ba91fab7ad144b1343f663907ca438ee8ba))
* **miniapp:** wiki page + session transcript detail views ([#1683](https://github.com/choiceoh/Deneb/issues/1683)) ([390ec88](https://github.com/choiceoh/Deneb/commit/390ec88aea91b6797cf38ad1bb81c8bab4c9003d))
* **miniapp:** wiki page creation + frontmatter editing ([#1721](https://github.com/choiceoh/Deneb/issues/1721)) ([b495834](https://github.com/choiceoh/Deneb/commit/b495834db298680768657656e2f6de44c9f7ab9d))
* **miniapp:** 더보기 탐색 — 카테고리 / 다이어리 진입점 추가 ([#1706](https://github.com/choiceoh/Deneb/issues/1706)) ([4fc111f](https://github.com/choiceoh/Deneb/commit/4fc111f3a0f2094cd22e85fcf0b8261832cab706))
* **miniapp:** 사람들 — Gmail 발신자 빈도 디렉토리 ([#1709](https://github.com/choiceoh/Deneb/issues/1709)) ([f0cba10](https://github.com/choiceoh/Deneb/commit/f0cba10698c0982d1ddce6b53f015087c229cdfa))
* **miniapp:** 자동 작업 일람 — 더보기 &gt; ⚡ 자동 작업 ([#1708](https://github.com/choiceoh/Deneb/issues/1708)) ([01db82b](https://github.com/choiceoh/Deneb/commit/01db82b0d30ee17b3bb3e40446181e96ae86f8f3))
* **modelrole:** auto-discover served vLLM model name ([#1604](https://github.com/choiceoh/Deneb/issues/1604)) ([935cebf](https://github.com/choiceoh/Deneb/commit/935cebf2b17e1ed1f09b51165a46b59b2a4a0b40))
* **modelrole:** register openrouter as known provider ([#1608](https://github.com/choiceoh/Deneb/issues/1608)) ([ece8b07](https://github.com/choiceoh/Deneb/commit/ece8b07ec96bdfaee51b8751b75d70c0004fe476))
* **models:** configurable lightweight/fallback roles + provider catalog ([#1781](https://github.com/choiceoh/Deneb/issues/1781)) ([411aeb2](https://github.com/choiceoh/Deneb/commit/411aeb22008f06ad71154dba117cd64092dbd36f))
* **models:** per-role model config (lightweight/fallback) + miniapp picker ([#1782](https://github.com/choiceoh/Deneb/issues/1782)) ([d1b5575](https://github.com/choiceoh/Deneb/commit/d1b5575a05e63f90ad02bc44a3e8818b3ff0081c))
* **obs:** depth-layer diagnostics — text preview, tool outcomes, scheduler trail, mem pressure ([#1583](https://github.com/choiceoh/Deneb/issues/1583)) ([2c9f8de](https://github.com/choiceoh/Deneb/commit/2c9f8de7cf2b6b9fc848f742262bd4464272cd72))
* **obs:** stronger diagnostic logging for cron/agent delivery postmortems ([#1582](https://github.com/choiceoh/Deneb/issues/1582)) ([c713717](https://github.com/choiceoh/Deneb/commit/c713717574651f95a4a1eec50b7fabcc0b943f6e))
* Phase 2 — redact, hostmatch, grace call, dentime, llmerr migration ([#1575](https://github.com/choiceoh/Deneb/issues/1575)) ([6e3408a](https://github.com/choiceoh/Deneb/commit/6e3408a23e8d0f480ec284d651453f6c2b4da2bb))
* Phase 3 — redaction depth + checkpoint lifecycle + llmerr coverage ([#1581](https://github.com/choiceoh/Deneb/issues/1581)) ([43c0186](https://github.com/choiceoh/Deneb/commit/43c0186ea897f5c41f21c8d1a96647214fbb5f1e))
* Phase 4 — rollback UX, spillover lifecycle, cron TZ, steer registry ([#1584](https://github.com/choiceoh/Deneb/issues/1584)) ([31db923](https://github.com/choiceoh/Deneb/commit/31db923447938eb3f72a46754b6d8dd85f4fa2c9))
* Phase 5 — skill nudge/manage, clarify, gateway self-mgmt, auto-resume, heartbeats, compaction anti-thrash ([#1585](https://github.com/choiceoh/Deneb/issues/1585)) ([ca14d2c](https://github.com/choiceoh/Deneb/commit/ca14d2c3d8c5a3fc849ab2a2cfb9a58742817809))
* port Hermes Agent runtime strengths (steer/checkpoint/insights/llmerr) ([#1571](https://github.com/choiceoh/Deneb/issues/1571)) ([3ad63ce](https://github.com/choiceoh/Deneb/commit/3ad63ce2521f66378dc0243ef53cc9d214a8f41f))
* **provider:** add Xiaomi MiMo Token Plan and Kimi Code providers ([#1638](https://github.com/choiceoh/Deneb/issues/1638)) ([27c10eb](https://github.com/choiceoh/Deneb/commit/27c10eb6d32a49facbda9fd546737f5d0a3ad802))
* **server:** expose /debug/pprof for live goroutine dumps ([#1550](https://github.com/choiceoh/Deneb/issues/1550)) ([1bfc692](https://github.com/choiceoh/Deneb/commit/1bfc69277882c50d5d23efe5b045c5842cc79d42))
* **server:** expose ShutdownCtx; use it for background Condense ([#1546](https://github.com/choiceoh/Deneb/issues/1546)) ([ac1e7fd](https://github.com/choiceoh/Deneb/commit/ac1e7fd90ae75eb8923e332b28f0c7fcde271cab))
* **server:** introduce DefaultTurnDeadline for end-to-end request budget ([#1548](https://github.com/choiceoh/Deneb/issues/1548)) ([860155f](https://github.com/choiceoh/Deneb/commit/860155f960aeb61a9c99660ba67913d09d6f9231))
* **session:** seed new sessions with agents.defaults.thinking ([#1603](https://github.com/choiceoh/Deneb/issues/1603)) ([1367757](https://github.com/choiceoh/Deneb/commit/13677576013f75a85dd1ca40217a855751c26527))
* **telegram:** /app slash command — open Mini App from any topic ([#1763](https://github.com/choiceoh/Deneb/issues/1763)) ([6d67d13](https://github.com/choiceoh/Deneb/commit/6d67d1389cb5bafa4220979374fc90398040c84b))
* **telegram:** add /restart slash command for in-app gateway restart ([#1644](https://github.com/choiceoh/Deneb/issues/1644)) ([1445e78](https://github.com/choiceoh/Deneb/commit/1445e7891be9ba355c0a2eaea68c00fc07c98848))
* **telegram:** add /update slash command for in-app updates ([#1643](https://github.com/choiceoh/Deneb/issues/1643)) ([c940d7e](https://github.com/choiceoh/Deneb/commit/c940d7eb5d3c1af3af4c6eb35f24c78b4eb1314a))
* **telegram:** add WebApp init data verification and menu button wiring ([#1672](https://github.com/choiceoh/Deneb/issues/1672)) ([0bd0887](https://github.com/choiceoh/Deneb/commit/0bd08871b29eb88486b96f5da157236df89e6e16))
* **telegram:** expand /models quick-change with provider-grouped keyboard ([#1641](https://github.com/choiceoh/Deneb/issues/1641)) ([3cac75d](https://github.com/choiceoh/Deneb/commit/3cac75d42964ef6a18355e0cb3c0b6307bfe0377))
* **telegram:** expose /btw side-question slash command ([e1b1ee4](https://github.com/choiceoh/Deneb/commit/e1b1ee4b1c3c64cead8af70134cb60683399d93c))
* **telegram:** forum supergroup support — thread_id plumbing, /use-forum, cron topic routing ([#1726](https://github.com/choiceoh/Deneb/issues/1726)) ([cea9329](https://github.com/choiceoh/Deneb/commit/cea9329f3cf16df587a0caa5dbabcfadee3efed3))
* **telegram:** greet on admin promotion + warn on Manage Topics loss ([#1730](https://github.com/choiceoh/Deneb/issues/1730)) ([559e754](https://github.com/choiceoh/Deneb/commit/559e75495d2f26f3bec88af6bd1211bdf5562930))
* **telegram:** mirror status + delivery errors to a secondary monitoring chat ([#1593](https://github.com/choiceoh/Deneb/issues/1593)) ([7fcd7a7](https://github.com/choiceoh/Deneb/commit/7fcd7a7ff55ba93296550cbc425d96f6a0e38035))
* **telegram:** normalize HTML/entities + flatten markdown tables in output ([#1597](https://github.com/choiceoh/Deneb/issues/1597)) ([3dfb3fe](https://github.com/choiceoh/Deneb/commit/3dfb3fe5871b94601181bb07840428e10f4d88cd))
* **telegram:** register slash command menu and drop dead commands ([#1651](https://github.com/choiceoh/Deneb/issues/1651)) ([03a5d29](https://github.com/choiceoh/Deneb/commit/03a5d2960de97afd9bf0913278e8293b87785bb1))
* **telegram:** show applied PR in /status and hand /update dirty worktree to the agent ([#1649](https://github.com/choiceoh/Deneb/issues/1649)) ([7b6b931](https://github.com/choiceoh/Deneb/commit/7b6b93106fd711c878ce565be1f035e4b8068da4))
* **telegram:** surface Kimi Code and MiMo in the /models quick-change ([#1645](https://github.com/choiceoh/Deneb/issues/1645)) ([e34ec5b](https://github.com/choiceoh/Deneb/commit/e34ec5bac58add5f63cf9e3e37cacf04ff7dc571))
* **web:** add Serper news/scholar/autocomplete search types ([#1613](https://github.com/choiceoh/Deneb/issues/1613)) ([05f3af9](https://github.com/choiceoh/Deneb/commit/05f3af92d632aaac286fdeb15fb2e4009d7f09d6))
* **web:** wire Serper scrape endpoint as dedicated web fetch ([6401a3c](https://github.com/choiceoh/Deneb/commit/6401a3cfdb5d05336b5c8ea8e8791ac143d7be1a))
* **wiki:** BM25 + recency diary search index ([b493bc6](https://github.com/choiceoh/Deneb/commit/b493bc6c2222b5cb0a6d4688570807e09fc93d38))
* **wiki:** re-introduce graphify with wiki+code knowledge graph fusion ([#1598](https://github.com/choiceoh/Deneb/issues/1598)) ([5b42dc6](https://github.com/choiceoh/Deneb/commit/5b42dc6bdbf0367bbeabbdc76d15b1dcaf9b2222))


### 🐛 Bug Fixes

* 3 functional bugs from audit (compaction recency, telegram dedup, prompt UTF-8) ([#1533](https://github.com/choiceoh/Deneb/issues/1533)) ([79e068f](https://github.com/choiceoh/Deneb/commit/79e068f84dbdb595fa9d5c6b34a33de52e7c5af6))
* address P1 review feedback from recent miniapp/calendar PRs ([#1702](https://github.com/choiceoh/Deneb/issues/1702)) ([3eb9b87](https://github.com/choiceoh/Deneb/commit/3eb9b8795d3cacc60e7fc86c81b619a6ec3ac219))
* **build:** wire frontend embed into gateway-prod ([#1695](https://github.com/choiceoh/Deneb/issues/1695)) ([fce7e07](https://github.com/choiceoh/Deneb/commit/fce7e07af6447167f896c48f2603577bc5fc546a))
* **chat:** address Copilot review on PR [#1626](https://github.com/choiceoh/Deneb/issues/1626) ([8ad7451](https://github.com/choiceoh/Deneb/commit/8ad7451bb303164511a486193efab4a4a1241f81))
* **chat:** broadcast context overflow + compaction degrade; improve draft edit logs ([#1554](https://github.com/choiceoh/Deneb/issues/1554)) ([a086fac](https://github.com/choiceoh/Deneb/commit/a086fac3a05499d87995a4eaf4799f4163c7ab24))
* **chat:** clean up orphaned draft when dedup suppresses Telegram reply ([8889a76](https://github.com/choiceoh/Deneb/commit/8889a7637ec85eadb945eae650b18d0627fba387))
* **chat:** deliver media even when NO_REPLY; fallback text on abnormal stop ([#1553](https://github.com/choiceoh/Deneb/issues/1553)) ([1908bf0](https://github.com/choiceoh/Deneb/commit/1908bf0e3ad1eb16899f271f2ba4210aea34fa87))
* **chat:** derive Delivery in sessions.send/steer so replies reach their channel ([#1579](https://github.com/choiceoh/Deneb/issues/1579)) ([c5fbe79](https://github.com/choiceoh/Deneb/commit/c5fbe7903fde2df80fb96b459d5b8b71e5aad0f2))
* **chat:** enforce answer-before-save, strip NO_REPLY from transcript ([#1537](https://github.com/choiceoh/Deneb/issues/1537)) ([3919209](https://github.com/choiceoh/Deneb/commit/3919209bc898f3d72d87012d57b1dfadf14aeaac))
* **chat:** enforce freshTailCount limit in Polaris context assembly ([bc10c4b](https://github.com/choiceoh/Deneb/commit/bc10c4bf7770a995822dba2c49e9d42cb00dbc29))
* **chat:** enforce freshTailCount limit in Polaris context assembly ([ce6bd07](https://github.com/choiceoh/Deneb/commit/ce6bd07777e33479f2c14ceb340c5883d9c0dc2d))
* **chat:** preserve streaming draft on NO_REPLY and error paths ([#1526](https://github.com/choiceoh/Deneb/issues/1526)) ([4cd429a](https://github.com/choiceoh/Deneb/commit/4cd429a86f1bd92823aa87f813420e141201ef9e))
* **chat:** prevent fake external delivery confirmations ([#1538](https://github.com/choiceoh/Deneb/issues/1538)) ([3ebf45f](https://github.com/choiceoh/Deneb/commit/3ebf45fca20347e9d83a24a8942f7fbe54172920))
* **chat:** record delivery failure in transcript; block 'channel disconnected' hallucination ([#1560](https://github.com/choiceoh/Deneb/issues/1560)) ([4de0bed](https://github.com/choiceoh/Deneb/commit/4de0bed33df797311917c88d44e6cf8279e0c69c))
* **chat:** reword in-loop send errors so cron LLM does not invent channel-down report ([f26d9e8](https://github.com/choiceoh/Deneb/commit/f26d9e8bd4e159361e82eef9cb7dfae2476386cb))
* **chat:** rewrite HandleBtw to use synchronous SendSync path ([#1520](https://github.com/choiceoh/Deneb/issues/1520)) ([01f1412](https://github.com/choiceoh/Deneb/commit/01f1412f94e3f2a349c84e48f890c17a26841a70))
* **chat:** scope recall cache per-cue + load MEMORY.md ([d57ed54](https://github.com/choiceoh/Deneb/commit/d57ed549ddf0e27d54db1d0d3f6651299a33c632))
* **chat:** suppress compaction-degraded warn when contextBudget is 0 ([#1565](https://github.com/choiceoh/Deneb/issues/1565)) ([a70890d](https://github.com/choiceoh/Deneb/commit/a70890dd0a76b961f2df7b97bc78e5dfe7c82e85))
* **compaction:** prevent garbled responses from consecutive same-role messages ([8889a76](https://github.com/choiceoh/Deneb/commit/8889a7637ec85eadb945eae650b18d0627fba387))
* **compaction:** remove context-notice prepend that orphaned fresh-tail boundary msg after first compaction ([#1527](https://github.com/choiceoh/Deneb/issues/1527)) ([30828c2](https://github.com/choiceoh/Deneb/commit/30828c29bd379376628dce3cf115f736d55360d8))
* **config:** accept 0.0.0.0 / 127.0.0.1 / localhost as bind mode aliases ([#1591](https://github.com/choiceoh/Deneb/issues/1591)) ([daf5ffd](https://github.com/choiceoh/Deneb/commit/daf5ffd002fd22e9754909be8bf3055a9e1fed1a))
* **cron:** address PR [#1628](https://github.com/choiceoh/Deneb/issues/1628) review — lazy load, shutdown ctx, dead branch ([067cd1b](https://github.com/choiceoh/Deneb/commit/067cd1be038f66fa3d67297a0bf40fa4ff591f6b))
* **cron:** break service mutex re-entry deadlock in emit ([#1541](https://github.com/choiceoh/Deneb/issues/1541)) ([bc2705f](https://github.com/choiceoh/Deneb/commit/bc2705f3eb2ecf74663f51f395eb0374c709e2e5))
* **cron:** bump cron MaxTurns to 50 now that progress-reporting keeps users informed ([#1573](https://github.com/choiceoh/Deneb/issues/1573)) ([f94b970](https://github.com/choiceoh/Deneb/commit/f94b9707b4a5ba25440933d91129bca2723f2ad4))
* **cron:** gate DefaultTo seeding on snap.Valid ([613fbeb](https://github.com/choiceoh/Deneb/commit/613fbeb033dad1776859e7d252a54d96be144146))
* **cron:** gate DefaultTo seeding on usable Telegram token + skip secret resolve ([2957fc4](https://github.com/choiceoh/Deneb/commit/2957fc482db13877dcf020102fa0ec8906937358))
* **cron:** graceful shutdown waits for in-flight executors ([6719154](https://github.com/choiceoh/Deneb/commit/67191541a03d646d07c43d5eb0791c3df7270adb))
* **cron:** prefer AllText when final turn is a short wrap-up + boot-time job status log ([#1580](https://github.com/choiceoh/Deneb/issues/1580)) ([6b3d4ca](https://github.com/choiceoh/Deneb/commit/6b3d4cae4b69c5312135bde6a28a39838868cbf7))
* **cron:** rebuild scheduler as always-on worker to stop chain death + preserve manual-run NextRunAtMs ([#1594](https://github.com/choiceoh/Deneb/issues/1594)) ([4a0a960](https://github.com/choiceoh/Deneb/commit/4a0a960d28b175f347b641db7da825c8ef0a879e))
* **cron:** route proactive cron output through main session so follow-ups keep context ([#1576](https://github.com/choiceoh/Deneb/issues/1576)) ([da01b51](https://github.com/choiceoh/Deneb/commit/da01b512b2f774e12323f033b726776303d5b614))
* **cron:** seed DefaultTo from Telegram chat ID at service construction ([c56c4d5](https://github.com/choiceoh/Deneb/commit/c56c4d5c6cf184921b48f7b718e8be08ae3913c2))
* **cron:** stop scheduled runs from misreporting channel as disconnected ([0812b61](https://github.com/choiceoh/Deneb/commit/0812b619bf062f2773ecd5868b28a5b4f11adbc6))
* **cron:** stop truncated-run planning text from masquerading as the delivery + bump MaxTurns for cron ([#1570](https://github.com/choiceoh/Deneb/issues/1570)) ([6d2e514](https://github.com/choiceoh/Deneb/commit/6d2e514449ad0d1347002b46267c5d4ac3838eaf))
* **cron:** surface JobByName load errors instead of returning nil ([4d2db7a](https://github.com/choiceoh/Deneb/commit/4d2db7a1c4a40b9ba08955db93cb73165a10dabb))
* **cron:** wire delivery context and guard double-fire so morning-letter stops hallucinating disconnection ([#1568](https://github.com/choiceoh/Deneb/issues/1568)) ([935b07c](https://github.com/choiceoh/Deneb/commit/935b07c472ba7ae8dedd10113fb9961da684b975))
* **deploy:** auto-deploy self-heals on a dirty worktree via stash+pop ([#1723](https://github.com/choiceoh/Deneb/issues/1723)) ([8cbf396](https://github.com/choiceoh/Deneb/commit/8cbf3966ac79c08dcf617f0ecccf988cf5094f64))
* **deploy:** auto-deploy.sh always exits 0 to keep watchdog green ([#1718](https://github.com/choiceoh/Deneb/issues/1718)) ([5126b6e](https://github.com/choiceoh/Deneb/commit/5126b6e3eaa8f19e246ad8c2ad6f18a625f8de07))
* **deploy:** detect gateway by listening port so go-run instances are caught ([#1561](https://github.com/choiceoh/Deneb/issues/1561)) ([5026945](https://github.com/choiceoh/Deneb/commit/50269451935ac9c6854d7238618456a5b8e021e7))
* **deploy:** probe actual listen address in health_ok ([#1703](https://github.com/choiceoh/Deneb/issues/1703)) ([76d8fed](https://github.com/choiceoh/Deneb/commit/76d8fedcd3431e81803eb159afd212ba3511c9e9))
* eliminate silent drops, hangs, and panic-cascade risks across gateway ([#1543](https://github.com/choiceoh/Deneb/issues/1543)) ([f1aaf08](https://github.com/choiceoh/Deneb/commit/f1aaf08550e7f05a1664345facbc29a59175ef68))
* **gateway:** bound every shutdown drain so a stalled subsystem can't block teardown ([#1777](https://github.com/choiceoh/Deneb/issues/1777)) ([4d771eb](https://github.com/choiceoh/Deneb/commit/4d771ebeaa46b8217efeb0b01c6fd0316715c77f))
* **gateway:** force-exit on hung shutdown so a stalled drain can't wedge the gateway ([#1775](https://github.com/choiceoh/Deneb/issues/1775)) ([ddfe767](https://github.com/choiceoh/Deneb/commit/ddfe767e1ad7093f31b768e53806e12d50298981))
* **gateway:** prevent 7 latent bugs — data races, nil panics, slice aliasing, goroutine leaks ([2335e35](https://github.com/choiceoh/Deneb/commit/2335e35278ce4e422132694b182559c0a5586077))
* **gateway:** prevent 7 latent bugs — races, nil panics, slice aliasing, leaks ([fd32392](https://github.com/choiceoh/Deneb/commit/fd323922edcf211eefba256493807f491fb815f0))
* **gateway:** restore isContextOverflow and fix gofmt ([3e1c06f](https://github.com/choiceoh/Deneb/commit/3e1c06fc7748a831ab22fe3f85e12428d059b362))
* **gateway:** restore isContextOverflow and fix gofmt across 138 files ([fc45a89](https://github.com/choiceoh/Deneb/commit/fc45a89ca0309143d525991c3fcccdc4affe5b83))
* **gateway:** restore lost compaction features and clean up stale references ([1fdb822](https://github.com/choiceoh/Deneb/commit/1fdb822a84e1fcf6002284ec5661e5fc2193c001))
* **gateway:** restore lost compaction features and clean up stale references ([ca95904](https://github.com/choiceoh/Deneb/commit/ca95904f4491d4b1722c40ffb29f842b2ed3c9a6))
* **gmailpoll:** raise stage2 analysis timeout 60s → 240s ([#1500](https://github.com/choiceoh/Deneb/issues/1500)) ([379d7ae](https://github.com/choiceoh/Deneb/commit/379d7aed232db7524d9eee20d3312cb1e85abcab))
* **gmail:** surface token persist failures to prevent silent refresh-token loss ([#1589](https://github.com/choiceoh/Deneb/issues/1589)) ([090b8a9](https://github.com/choiceoh/Deneb/commit/090b8a92862ea75949ab151495cd77291eb7fb5c))
* graceful SIGTERM deploy + sensitive-path guard in exec safety ([#1559](https://github.com/choiceoh/Deneb/issues/1559)) ([405bc90](https://github.com/choiceoh/Deneb/commit/405bc90aded1d9553a4ab7fc337b7d9846d735a9))
* harden typing close race, process drain panics, and media silent failure ([#1544](https://github.com/choiceoh/Deneb/issues/1544)) ([6b9e795](https://github.com/choiceoh/Deneb/commit/6b9e795fd801da330442bd5b72a334633c3fe533))
* **heartbeat:** break repeat-loop with asymmetric ephemeral + self-edit tool ([#1609](https://github.com/choiceoh/Deneb/issues/1609)) ([fb56aef](https://github.com/choiceoh/Deneb/commit/fb56aefe7d2539ad8d18640a5daa7d23bbaf9ceb))
* increase default max_tokens from 8192 to 32768 ([55e1538](https://github.com/choiceoh/Deneb/commit/55e15383fc0640c7ea758ee7ec46d299456d79c4))
* **localai:** handle reasoning models in hub requests and fallback ([#1646](https://github.com/choiceoh/Deneb/issues/1646)) ([0c6860a](https://github.com/choiceoh/Deneb/commit/0c6860a6e336b06fe70eff938cdc44473dd71183))
* media failure transcript marker; cron status reflects delivery ([#1562](https://github.com/choiceoh/Deneb/issues/1562)) ([deff0c2](https://github.com/choiceoh/Deneb/commit/deff0c2214eba3992021d22b5b600280db8e5b73))
* **memory:** deduplicate MEMORY.md on case-insensitive filesystems ([#1512](https://github.com/choiceoh/Deneb/issues/1512)) ([9109a9a](https://github.com/choiceoh/Deneb/commit/9109a9a0ceb1712daed1a9e0e49231966e723821))
* **miniapp:** align mail Q&A styling with the monochrome analysis-card idiom ([#1780](https://github.com/choiceoh/Deneb/issues/1780)) ([0543a4f](https://github.com/choiceoh/Deneb/commit/0543a4fd784d9af32cad67d5cb8781bc30200971))
* **miniapp:** align memory item detail metadata with canonical row idiom ([#1742](https://github.com/choiceoh/Deneb/issues/1742)) ([fa8979c](https://github.com/choiceoh/Deneb/commit/fa8979ce9af3d70ada927a4719cbd020819205cd))
* **miniapp:** apply desktop layout at real 384px panel width ([#1801](https://github.com/choiceoh/Deneb/issues/1801)) ([99aacec](https://github.com/choiceoh/Deneb/commit/99aacec2cd513cd6b70aeb29c399f4bc24475c66))
* **miniapp:** carry prod hotfix — fullscreen safe-area + WebView cache bust ([#1767](https://github.com/choiceoh/Deneb/issues/1767)) ([deae414](https://github.com/choiceoh/Deneb/commit/deae4142de4bbc04a43a3b4291cfe23e245f50a2))
* **miniapp:** disable Vite modulePreload to stop __VITE_PRELOAD__ leak ([#1757](https://github.com/choiceoh/Deneb/issues/1757)) ([b9ec5a3](https://github.com/choiceoh/Deneb/commit/b9ec5a3abe0634360734e026ad9b257e92537c3f))
* **miniapp:** flatten HTML-only mail body to plain text ([#1684](https://github.com/choiceoh/Deneb/issues/1684)) ([c57f093](https://github.com/choiceoh/Deneb/commit/c57f0934a7d69cd262170d581ab9b48021fd1404))
* **miniapp:** fullscreen + Vite preload guard (carry hotfix from prod) ([#1762](https://github.com/choiceoh/Deneb/issues/1762)) ([bd239b8](https://github.com/choiceoh/Deneb/commit/bd239b8e0de0e000292956f480fa68333cd596b0))
* **miniapp:** gate home mail notice behind pull-to-refresh; always show latest ([#1738](https://github.com/choiceoh/Deneb/issues/1738)) ([7b99af2](https://github.com/choiceoh/Deneb/commit/7b99af2bc23d0e8de0557d27332737ddafa4e611))
* **miniapp:** guard prefetch helpers against nullish row fields ([#1755](https://github.com/choiceoh/Deneb/issues/1755)) ([870b21b](https://github.com/choiceoh/Deneb/commit/870b21b6c093654463cda1a5af32045b804fd541))
* **miniapp:** keep desktop windowed — skip fullscreen request on PC ([#1768](https://github.com/choiceoh/Deneb/issues/1768)) ([d500000](https://github.com/choiceoh/Deneb/commit/d500000d8b938db8aa08f979ed5d8021af3b48fe))
* **miniapp:** mail tap not opening — drop optimistic shell, guard prefetch ([#1754](https://github.com/choiceoh/Deneb/issues/1754)) ([e0050e1](https://github.com/choiceoh/Deneb/commit/e0050e1be2b91017ec09e95cd0dab96dec61f628))
* **miniapp:** preserve link URLs and image alt in mail body ([#1689](https://github.com/choiceoh/Deneb/issues/1689)) ([6e40238](https://github.com/choiceoh/Deneb/commit/6e40238e1fda9b98e755db84ff539d54f691860b))
* **miniapp:** recover from stale-chunk import failure after redeploy ([#1769](https://github.com/choiceoh/Deneb/issues/1769)) ([a5e916a](https://github.com/choiceoh/Deneb/commit/a5e916a3e85bd7f1a51501445af1699a415a695f))
* **miniapp:** remove default tap highlight on type-menu items ([#1752](https://github.com/choiceoh/Deneb/issues/1752)) ([53e21df](https://github.com/choiceoh/Deneb/commit/53e21dfb20c4a00636d6335697d616e6b17202b0))
* **miniapp:** remove view-chunk prefetch poisoning the module cache ([#1756](https://github.com/choiceoh/Deneb/issues/1756)) ([fc3439b](https://github.com/choiceoh/Deneb/commit/fc3439b6e77a196825b75035cf71382727129aba))
* **miniapp:** resolve PR review findings in mail + boot flows ([13d0b1c](https://github.com/choiceoh/Deneb/commit/13d0b1ce69bf7fbf555733a8778844c21560cf94))
* **miniapp:** restore dark-mode --tg-secondary-bg so category rows stay legible ([#1743](https://github.com/choiceoh/Deneb/issues/1743)) ([031d10b](https://github.com/choiceoh/Deneb/commit/031d10b325322d22beb6831f4dd274eeea5835fb))
* **miniapp:** run project merge in background with completion notice ([#1802](https://github.com/choiceoh/Deneb/issues/1802)) ([09f483d](https://github.com/choiceoh/Deneb/commit/09f483da9365ea4cb4af798e3b6189709e0185dd))
* **miniapp:** sync 역할 section main row with live model switch ([#1790](https://github.com/choiceoh/Deneb/issues/1790)) ([108a795](https://github.com/choiceoh/Deneb/commit/108a795d87a92e6eead5abf0a58828024afacb2c))
* **miniapp:** wiki page back returns to entry screen, not search ([#1778](https://github.com/choiceoh/Deneb/issues/1778)) ([a06d6cc](https://github.com/choiceoh/Deneb/commit/a06d6ccf3f9baa88f010778f7691635ae4dabe8a))
* **polaris:** bootstrap context recovery when no DAG summaries exist ([9788a12](https://github.com/choiceoh/Deneb/commit/9788a129970eaae79b73d31cc6a37dc38c3a5ab0))
* **polaris:** bootstrap context recovery when no DAG summaries exist ([e8148e5](https://github.com/choiceoh/Deneb/commit/e8148e58782be7ab8d597a49d690fdf4c18f92f4))
* **polaris:** defer raw-message truncation until after compaction ([#1509](https://github.com/choiceoh/Deneb/issues/1509)) ([6a2e065](https://github.com/choiceoh/Deneb/commit/6a2e06596520a48077cf7aa1b86e0b155ed313cc))
* **polaris:** serialize capturingSummarizer writes with a mutex ([#1660](https://github.com/choiceoh/Deneb/issues/1660)) ([8e00c18](https://github.com/choiceoh/Deneb/commit/8e00c18dda0b6b57e5d7e8dcc76ad8894b468874))
* prevent failures upstream instead of just reporting them ([#1556](https://github.com/choiceoh/Deneb/issues/1556)) ([1f04633](https://github.com/choiceoh/Deneb/commit/1f0463365b5fab10353be4b24dd960fa6bf460e5))
* **prompt:** clarify wiki saves are not user responses ([#1592](https://github.com/choiceoh/Deneb/issues/1592)) ([7265e85](https://github.com/choiceoh/Deneb/commit/7265e852e9be5eb04c7515c5320d1a7afb790273))
* repair broken deploy script and gemini/gemma fallback chain ([#1539](https://github.com/choiceoh/Deneb/issues/1539)) ([1a049c4](https://github.com/choiceoh/Deneb/commit/1a049c4c28e94860020316112667aafbf8344eb4))
* **server:** autoreply executor skips empty message instead of hitting RPC validation ([#1564](https://github.com/choiceoh/Deneb/issues/1564)) ([777e2c0](https://github.com/choiceoh/Deneb/commit/777e2c0ffe9aadbbc7bda4ed0608c4f84399a8b5))
* **session:** suppress no-op EventStatusChanged in ApplyLifecycleEvent ([#1563](https://github.com/choiceoh/Deneb/issues/1563)) ([af09b17](https://github.com/choiceoh/Deneb/commit/af09b174bb81acfce7de85722cffb7bad6a23a1b))
* **skills:** lower genesis activation thresholds for typical sessions ([db25a53](https://github.com/choiceoh/Deneb/commit/db25a53dcbb4a275dbb5c43f610d14887cdc7542))
* **skills:** repair frontmatter and dangling skill references ([#1654](https://github.com/choiceoh/Deneb/issues/1654)) ([e4e7b0f](https://github.com/choiceoh/Deneb/commit/e4e7b0fcd6daa96de11dfc0db542e14964bccc3b))
* **streaming:** retreat to UTF-8 rune boundary in truncateForBroadcast ([#1534](https://github.com/choiceoh/Deneb/issues/1534)) ([83dbec5](https://github.com/choiceoh/Deneb/commit/83dbec5f3b4c7f07d66235a1fde4ff6d127c193c))
* surface silent persistence failures in cron, agentlog, gmailpoll ([#1555](https://github.com/choiceoh/Deneb/issues/1555)) ([736335e](https://github.com/choiceoh/Deneb/commit/736335e7b60e7bd1c9236418fd86ad5829f3f479))
* **telegram:** /app reply uses URL button, not web_app inline button ([#1766](https://github.com/choiceoh/Deneb/issues/1766)) ([890ac78](https://github.com/choiceoh/Deneb/commit/890ac78d68b8df28f2ecdcc1dd4dd1f1cca8ef5b))
* **telegram:** address review feedback on chunked reply finalization ([6bf8916](https://github.com/choiceoh/Deneb/commit/6bf8916edc4e1af6a3ce0f8b6698bbecca83f3fe))
* **telegram:** chunk long replies in-place instead of delete-and-resend ([ce608e6](https://github.com/choiceoh/Deneb/commit/ce608e6afb71c5d5760456a3277175732022de60))
* **telegram:** decode HTML entities before tag rewriting in normalizer ([#1599](https://github.com/choiceoh/Deneb/issues/1599)) ([3660ce0](https://github.com/choiceoh/Deneb/commit/3660ce0bfdbc5a51ae82600affb059f72c22cd6b))
* **telegram:** graft built-in models when deneb.json declares empty list ([#1655](https://github.com/choiceoh/Deneb/issues/1655)) ([5fb869d](https://github.com/choiceoh/Deneb/commit/5fb869dfd3a9ebb9dbfddfa06022e9fd3f43bc71))
* **telegram:** keep zai and core providers in /models when config omits them ([#1650](https://github.com/choiceoh/Deneb/issues/1650)) ([6cb243f](https://github.com/choiceoh/Deneb/commit/6cb243ff32e1f8e28ae85d5529d551eaacfcb2a7))
* **telegram:** route proactive delivery to active home, not stale config chatID ([#1774](https://github.com/choiceoh/Deneb/issues/1774)) ([69fae25](https://github.com/choiceoh/Deneb/commit/69fae25932034ffde7c71aa0e6985d9a9c821903))
* **telegram:** stack-based HTML normalizer + deterministic span rendering ([#1607](https://github.com/choiceoh/Deneb/issues/1607)) ([6881e45](https://github.com/choiceoh/Deneb/commit/6881e4533cf60628c0b51868e9d555e7c04e07c0))
* **telegram:** use setChatMenuButton (official Bot API method) ([#1675](https://github.com/choiceoh/Deneb/issues/1675)) ([19abedc](https://github.com/choiceoh/Deneb/commit/19abedcb49810500929f079f8dedee8c4af53b98))
* **test-harness:** detect tool usage from reaction emoji ([#1567](https://github.com/choiceoh/Deneb/issues/1567)) ([c82d560](https://github.com/choiceoh/Deneb/commit/c82d560be8650c69627335948891606b4db78aa3))
* **test-harness:** wait for reply-worthy events, not reactions alone ([#1566](https://github.com/choiceoh/Deneb/issues/1566)) ([9bdc45e](https://github.com/choiceoh/Deneb/commit/9bdc45ea3a2116012ee4772c9fd38603b0db0acb))
* txt/md 파일 인식, 압축 역할 충돌, 유튜브 답변 씹힘 수정 ([#1524](https://github.com/choiceoh/Deneb/issues/1524)) ([8889a76](https://github.com/choiceoh/Deneb/commit/8889a7637ec85eadb945eae650b18d0627fba387))
* **web:** add missing type param to web tool schema source ([2051149](https://github.com/choiceoh/Deneb/commit/2051149f460a935176b81d12c2978595d3ab2bf4))
* **wiki:** normalize page paths to .md in Store to prevent duplicate pages ([#1799](https://github.com/choiceoh/Deneb/issues/1799)) ([7a6e5cc](https://github.com/choiceoh/Deneb/commit/7a6e5ccea80e055d40ab1a9c2ec072461eade525))


### ⚡ Performance

* **chat:** bake ISO 8601 timestamp into transcript user messages (P6) ([aa43c30](https://github.com/choiceoh/Deneb/commit/aa43c30d2470e5124017998382e4aa3ab82c90f4))
* **chat:** compact skills index in semi-static block (P5) ([c9075ad](https://github.com/choiceoh/Deneb/commit/c9075ade1f11b87e945afc05b9f3d38c11371a8a))
* **chat:** compress Sub-Agents system prompt section ([4353e89](https://github.com/choiceoh/Deneb/commit/4353e896bfa877f2398de92e214740d05c95c9d3))
* **chat:** defer gateway and process tools (P1 partial) ([1463ab3](https://github.com/choiceoh/Deneb/commit/1463ab3ee08dfe68937008de90b15a05ed3e1b1f))
* **chat:** defer rare tools + widen memory budget to reach 200K window ([763ef18](https://github.com/choiceoh/Deneb/commit/763ef1899128f0ec9e93470847f6197d1adb1e3f))
* **chat:** limit context file ancestor walk to 6 levels ([ddd8eac](https://github.com/choiceoh/Deneb/commit/ddd8eacbcd253c77bf7d69b4614edc92958ac6b6))
* **chat:** limit context file ancestor walk to 6 levels ([e856c7e](https://github.com/choiceoh/Deneb/commit/e856c7e6a8792c5a716bb759b2efc2bff68ba3f3))
* **chat:** pass FinalMessages to continuation runs to skip transcript reload ([aab8043](https://github.com/choiceoh/Deneb/commit/aab8043967f83b34c5d5c86334812c26c9776332))
* **chat:** raise memory budget + compaction threshold to utilize 200K window ([8c2dfdf](https://github.com/choiceoh/Deneb/commit/8c2dfdfa9d150082a72c26637f81bc063b5fed8e))
* **chat:** shrink context file char budgets (150K-&gt;40K total, 20K-&gt;8K per file) ([e6057ac](https://github.com/choiceoh/Deneb/commit/e6057ac84b6012ee56f66f347a22c1573099f1c7))
* **chat:** skip transcript reload on continuation runs ([95b63bd](https://github.com/choiceoh/Deneb/commit/95b63bd07b69870c24d89e997ae0bf9f1a920d1f))
* **chat:** tighten tool output char budgets (32K-&gt;24K default, 51K-&gt;32K exec, 30K-&gt;20K wiki) ([6a688cb](https://github.com/choiceoh/Deneb/commit/6a688cbb0923f3081d86b4b489e0570869360660))
* **compaction:** remove 4096 output cap and add chunked LLM summarization ([#1507](https://github.com/choiceoh/Deneb/issues/1507)) ([8443b1d](https://github.com/choiceoh/Deneb/commit/8443b1d4c079adb14151ea5053d29ec5492e48fd))
* **miniapp:** code-split views, prefetch chunks during idle time ([#1746](https://github.com/choiceoh/Deneb/issues/1746)) ([45f9893](https://github.com/choiceoh/Deneb/commit/45f98934a5282981039a589133f07a8d2f1e6d27))
* **miniapp:** defer Telegram SDK, preload latin font, progressive paint home ([#1745](https://github.com/choiceoh/Deneb/issues/1745)) ([d7cc47b](https://github.com/choiceoh/Deneb/commit/d7cc47bda24b924e152b9390b8396ef0ca1c5429))
* **miniapp:** faster mail loading — single-round fan-out + inbox cache ([#1783](https://github.com/choiceoh/Deneb/issues/1783)) ([7f614a2](https://github.com/choiceoh/Deneb/commit/7f614a2cbe0ee52812f2981ecb5b994039653d68))
* **miniapp:** optimistic mail detail paint + pointerdown prefetch ([#1750](https://github.com/choiceoh/Deneb/issues/1750)) ([953ad28](https://github.com/choiceoh/Deneb/commit/953ad2868c1dc593b5fbcd807c4836757561af5a))
* **miniapp:** parallelize + cache sender_context, prefetch on row tap ([#1753](https://github.com/choiceoh/Deneb/issues/1753)) ([fc0de1e](https://github.com/choiceoh/Deneb/commit/fc0de1e277b79af33193f8f2d66e75aa2ed9b315))
* **miniapp:** pre-compress text assets + cache whoami per session ([#1749](https://github.com/choiceoh/Deneb/issues/1749)) ([4bdd4d0](https://github.com/choiceoh/Deneb/commit/4bdd4d08a6f5362e2d146a035ce9007bf7d91fbf))
* **polaris:** raise engine compaction thresholds (soft 0.75-&gt;0.80, hard 0.90-&gt;0.92) ([f581692](https://github.com/choiceoh/Deneb/commit/f581692e4faebef34f45134989e2ccade24dc700))
* **polaris:** raise engine compaction thresholds (soft 0.75→0.80, hard 0.90→0.92) ([05536b8](https://github.com/choiceoh/Deneb/commit/05536b87a6dc4df4155a8448a5dba96cb3722305))


### 🔧 Internal

* **agent:** simplify line ranker ([#1516](https://github.com/choiceoh/Deneb/issues/1516)) ([f593c01](https://github.com/choiceoh/Deneb/commit/f593c01be897afa8b9185de91ab11d5e5e473261))
* **agentsys:** remove dead agent CRUD Store (~610 LOC) ([89d3038](https://github.com/choiceoh/Deneb/commit/89d30382612dfc7183a5db6ab7bd4967af88ff71))
* **agentsys:** remove dead agent CRUD Store and 8 orphan RPC methods (~610 LOC) ([39ff384](https://github.com/choiceoh/Deneb/commit/39ff3843e4223511b6533add534504290b83775e))
* **autoreply:** remove dead code ([cc5c027](https://github.com/choiceoh/Deneb/commit/cc5c0271bf4d76d0696eed2301734428bd55b1d1))
* **autoreply:** remove dead code — unused reply templates, facade shim, AudioAsVoiceBuffer ([18a01ae](https://github.com/choiceoh/Deneb/commit/18a01ae8e4d7d850556b7b8810c6f65228e4704a))
* **autoreply:** remove dead code and simplify over-engineered subsystems ([9b1730d](https://github.com/choiceoh/Deneb/commit/9b1730d5b8383430f61856f30bf10cf6a5f7d43a))
* **autoreply:** remove dead code and simplify over-engineered subsystems ([051ea1b](https://github.com/choiceoh/Deneb/commit/051ea1b2a683240b9c51a1a74e7a9a7e5cfe6eac))
* **autoreply:** remove dead code, wire orphan dedup, fix double directive parsing ([94746de](https://github.com/choiceoh/Deneb/commit/94746de18dd760a05e360ec3063566f02ad97acc))
* **autoreply:** remove dead code, wire orphan dedup, fix double directive parsing (~830 LOC) ([3c2ff70](https://github.com/choiceoh/Deneb/commit/3c2ff70f056c1120982b5b1bc158d63d3a5f390f))
* **autoreply:** remove dead post-RunTurn pipeline and trim AgentTurnResult (~250 LOC) ([8b01580](https://github.com/choiceoh/Deneb/commit/8b015802614fbf14d8686c566db516d5ff6246d2))
* **autoreply:** remove dead thinking level abstraction ([#1506](https://github.com/choiceoh/Deneb/issues/1506)) ([12b23c9](https://github.com/choiceoh/Deneb/commit/12b23c9e807cc000ec1275d80915d11951b926ae))
* **autoreply:** rewrite module — remove dead code, consolidate entry point (~1,020 LOC) ([a7c3da0](https://github.com/choiceoh/Deneb/commit/a7c3da095075d19ef67a6010f5bd70ae45338a0f))
* **chat:** consolidate agent tools — 43 to 33 ([155cdf8](https://github.com/choiceoh/Deneb/commit/155cdf8783610205a876e847ec87d4e10088d27c))
* **chat:** consolidate agent tools — 43 to 33 ([5dfd603](https://github.com/choiceoh/Deneb/commit/5dfd6039a18cca51f81e5e08a10e5293898eee9b))
* **chat:** decompose Handler god object and executeAgentRun mega-function ([3ffc99f](https://github.com/choiceoh/Deneb/commit/3ffc99f50017f02a658bf3b3d3a983b033ee345e))
* **chat:** decompose Handler god object and pipeline mega-function ([2d65f66](https://github.com/choiceoh/Deneb/commit/2d65f6669c646ee6439098f869fa564bdb452c35))
* **chat:** extract pilot/ and compaction/ to pipeline/, consolidate prompt caches ([2612fc1](https://github.com/choiceoh/Deneb/commit/2612fc13962ff6f16e2759852ac245ece37af787))
* **chat:** extract pilot/ and compaction/ to pipeline/, consolidate prompt caches ([bbca6a3](https://github.com/choiceoh/Deneb/commit/bbca6a305fa8bb0fa30eeab6a0b64f58d88fcd01))
* **chat:** make email analysis a reasoning lens, not a procedure ([#1648](https://github.com/choiceoh/Deneb/issues/1648)) ([81a6f6d](https://github.com/choiceoh/Deneb/commit/81a6f6d59ad388651c020366f28594778cbdc163))
* **chat:** remove coordinator mode ([#1505](https://github.com/choiceoh/Deneb/issues/1505)) ([3aac576](https://github.com/choiceoh/Deneb/commit/3aac57646a53b74ec4fcbce10ce63f3d7183183f))
* **chat:** remove dead adapter code and unused callbacks ([#1504](https://github.com/choiceoh/Deneb/issues/1504)) ([5fef129](https://github.com/choiceoh/Deneb/commit/5fef12992202c421d17d59e1f5e81559de5d9f6a))
* **chat:** remove dead code — unused types, duplicate functions, unwired abstractions (~1,050 LOC) ([9f3af33](https://github.com/choiceoh/Deneb/commit/9f3af3379744b1999d969b1ec3b92ec8946b609c))
* **chat:** remove dead code (~1,050 LOC) ([8433a25](https://github.com/choiceoh/Deneb/commit/8433a259fab54aa53e50d28bc58d8f3cfd3657a7))
* **chat:** remove deprecated agent tools ([a47118d](https://github.com/choiceoh/Deneb/commit/a47118d6e5dc5b09bd72a616c08f63a73582c8b8))
* **chat:** remove deprecated agent tools ([12ab0cd](https://github.com/choiceoh/Deneb/commit/12ab0cd131ec6e004d0672c92f16c76641690a92))
* **chat:** remove diff and find tools ([#1501](https://github.com/choiceoh/Deneb/issues/1501)) ([9db47d9](https://github.com/choiceoh/Deneb/commit/9db47d9ff62c2f3ec08c688449781f34eb8dec13))
* **chat:** remove kv agent tool ([e642e0b](https://github.com/choiceoh/Deneb/commit/e642e0b23be3001c7950d5d00cfd89e104926d52))
* **chat:** remove parallel tool execution ([0ff3571](https://github.com/choiceoh/Deneb/commit/0ff35715f56da480aa293bb92ed1b2bc32d18d34))
* **chat:** remove phantom tool names from toolCategories ([#1536](https://github.com/choiceoh/Deneb/issues/1536)) ([b017795](https://github.com/choiceoh/Deneb/commit/b017795c704f19e50dd48cd4a0a37432e2b4d716))
* **chat:** remove redundant agent tools ([eb4e532](https://github.com/choiceoh/Deneb/commit/eb4e53295e0405bbccad00349fa9ad1f6598f710))
* **chat:** remove redundant agent tools (projects_*, memory_*, update_plan) ([c877fac](https://github.com/choiceoh/Deneb/commit/c877facb5c73e6eb5b7941393fb6b0a11f04273d))
* **chat:** restructure chat module — fix broken build, remove dead code, deduplicate, extract functions (~290 LOC net reduction) ([d1df6db](https://github.com/choiceoh/Deneb/commit/d1df6dbd630b03ceed225935fcaf3bf8a1bd1631))
* **chat:** restructure chat module — fix build, remove dead code, deduplicate ([c3c579d](https://github.com/choiceoh/Deneb/commit/c3c579d77c99988042ec55755ba32bd81a22ea88))
* **compaction:** unify bootstrap summarization with LLMCompact path ([#1521](https://github.com/choiceoh/Deneb/issues/1521)) ([9e9ca86](https://github.com/choiceoh/Deneb/commit/9e9ca86b01dcef56eec5f01abf735776d8d4a7a7))
* **core:** wire SanitizeHTML/DetectFences and remove dead exports ([#1513](https://github.com/choiceoh/Deneb/issues/1513)) ([27de18a](https://github.com/choiceoh/Deneb/commit/27de18abb908fd4d8f0d06cdab3f28eb641bc9a7))
* **cron:** eliminate dual scheduling, wire dead code, remove ~1,240 LOC ([86ea8a6](https://github.com/choiceoh/Deneb/commit/86ea8a615b519f9a6bb7b68985ff27d1767ed692))
* **cron:** eliminate dual scheduling, wire dead code, remove ~1,240 LOC ([226f218](https://github.com/choiceoh/Deneb/commit/226f218d303b74fadb3318f1d275f78e3ad54437))
* **cron:** extract shared helpers to reduce copy-paste ([#1502](https://github.com/choiceoh/Deneb/issues/1502)) ([286c442](https://github.com/choiceoh/Deneb/commit/286c44287c64358967cb3b5d837b1fbee6df35fd))
* **frontend:** remove home greeting ([#1744](https://github.com/choiceoh/Deneb/issues/1744)) ([f05fbdf](https://github.com/choiceoh/Deneb/commit/f05fbdf94b9d225df5774dfad1d5bef334628c94))
* **gateway:** remove 2 low-utility agent tools ([83f6e0b](https://github.com/choiceoh/Deneb/commit/83f6e0b24000e2f380419fd9159b92d7fef08a93))
* **gateway:** remove 2 low-utility agent tools (youtube_transcript, health_check) ([a91025c](https://github.com/choiceoh/Deneb/commit/a91025c6cfa2b1816a7b0f0b074622fdb0bd3fbb))
* **gateway:** remove 3 orphan packages ([0782ed4](https://github.com/choiceoh/Deneb/commit/0782ed4ec4e22e027b85abe523a5d4fc797863a4))
* **gateway:** remove 3 orphan packages — FFI delegation layer, FFI RPC handlers, cron migration ([0b23a6c](https://github.com/choiceoh/Deneb/commit/0b23a6c8e4788fd841debc96d0f4ca664b2abdf8))
* **gateway:** remove 3 orphan packages — FFI layers, cron migration ([d80674f](https://github.com/choiceoh/Deneb/commit/d80674f2eeb34ec96a492bbf2a026300ab681d40))
* **gateway:** remove 3 orphan packages with zero inbound references ([88d8a03](https://github.com/choiceoh/Deneb/commit/88d8a032e1d6f5cff73448f45a12293e82b7a927))
* **gateway:** remove autoresearch subsystem ([5543c57](https://github.com/choiceoh/Deneb/commit/5543c5782d9d75a24fcc9f495c3053edc4c5d34a))
* **gateway:** remove autoresearch subsystem ([911de12](https://github.com/choiceoh/Deneb/commit/911de1282ebaa87e448d2d53f2e50ed4a2d34a0b))
* **gateway:** wire orphan coremarkdown/coremedia, remove dead coreprotocol/base64util ([d88fb4c](https://github.com/choiceoh/Deneb/commit/d88fb4ccda56c05eeaad96b20184f67e924f1022))
* **gateway:** wire orphan coremarkdown/coremedia, remove dead coreprotocol/base64util (~2,900 LOC) ([99c400d](https://github.com/choiceoh/Deneb/commit/99c400dbf18bc05c19ead4fb8bd3c0313c3beea0))
* **gmail:** mark unread mail with a single ○ instead of blockquote read ([#1741](https://github.com/choiceoh/Deneb/issues/1741)) ([a9a019a](https://github.com/choiceoh/Deneb/commit/a9a019a69637b331af6859f72110e2cf477a3869))
* **gmail:** use blockquote for read mail instead of icon markers ([#1739](https://github.com/choiceoh/Deneb/issues/1739)) ([f5b700f](https://github.com/choiceoh/Deneb/commit/f5b700fa44546fa8ba8e84d8200ce68a73e1b62b))
* **live-test:** revert to mock Telegram environment for live validation ([43946cf](https://github.com/choiceoh/Deneb/commit/43946cfe0ffd0f126054088d979fa03713c2e829))
* **live-test:** revert to mock Telegram environment for live validation ([b24fcb8](https://github.com/choiceoh/Deneb/commit/b24fcb8df1db66aa149ac65bd41043eaca21f688))
* **metrics:** remove Prometheus infrastructure, keep RPC counter only ([#1508](https://github.com/choiceoh/Deneb/issues/1508)) ([6f8684a](https://github.com/choiceoh/Deneb/commit/6f8684a71be3eb41570999f24a67dc5e4e98b8f3))
* **miniapp:** declutter mail detail view ([#1789](https://github.com/choiceoh/Deneb/issues/1789)) ([588f25f](https://github.com/choiceoh/Deneb/commit/588f25fd4b55b354ae8d2b881f951dd5e356cf24))
* **miniapp:** declutter model picker header and current-model row ([#1795](https://github.com/choiceoh/Deneb/issues/1795)) ([63cd489](https://github.com/choiceoh/Deneb/commit/63cd4891d233527d0872b9d04b714c5389e35e29))
* **miniapp:** drop panorama tab strip; home is the single index ([#1740](https://github.com/choiceoh/Deneb/issues/1740)) ([de8a341](https://github.com/choiceoh/Deneb/commit/de8a3418cbda6afe58451e4f1cba4e92920b6a26))
* **miniapp:** extract shared view helpers — formatRpcError, buildViewHeader ([#1699](https://github.com/choiceoh/Deneb/issues/1699)) ([0c5c0f9](https://github.com/choiceoh/Deneb/commit/0c5c0f965128219e2da10229224fe0f2d585a852))
* **miniapp:** remove chat surface — Telegram is the chat ([#1704](https://github.com/choiceoh/Deneb/issues/1704)) ([6244856](https://github.com/choiceoh/Deneb/commit/6244856ae039b991d8f9954ec696c071eb0466d7))
* **miniapp:** remove stale settings styles ([3a8cc5b](https://github.com/choiceoh/Deneb/commit/3a8cc5b39e0dd4274b4064ed897a8fff6733d186))
* **miniapp:** unify search screen UI with the app idiom ([#1770](https://github.com/choiceoh/Deneb/issues/1770)) ([1ff9fc7](https://github.com/choiceoh/Deneb/commit/1ff9fc764698094964620dae9820f293f9df8f4f))
* **modelrole:** read local vLLM model from deneb.json instead of hardcoded const ([#1574](https://github.com/choiceoh/Deneb/issues/1574)) ([8944eaa](https://github.com/choiceoh/Deneb/commit/8944eaa84a1ca6f66d396a650361c5e371646b16))
* **proactive:** bypass LLM for cron/gmail/dreaming delivery via ProactiveRelay ([#1577](https://github.com/choiceoh/Deneb/issues/1577)) ([6141880](https://github.com/choiceoh/Deneb/commit/6141880b857e7474a254b197709330c7c53dfbf0))
* remove 6 fully unreachable packages (~2,900 LOC) ([08a34cc](https://github.com/choiceoh/Deneb/commit/08a34cc94b504d1770ed746af3e9e43ffe68864b))
* remove 6 fully unreachable packages (~2,900 LOC) ([96ccbac](https://github.com/choiceoh/Deneb/commit/96ccbac5c2a8ec5bec122a6952324e174b665ac9))
* remove dead DefaultAgentRunner subsystem and media package (~2,800 LOC) ([85401c0](https://github.com/choiceoh/Deneb/commit/85401c026a712be8426c949bf0a4889b1c4fa8be))
* **rpc:** move builtin handlers to domain packages ([893ed2f](https://github.com/choiceoh/Deneb/commit/893ed2fa85fdf81b3eebd82e379a40872d6c95b4))
* **rpc:** move builtin handlers to domain packages and remove dead code ([5f6abba](https://github.com/choiceoh/Deneb/commit/5f6abbadeaba45ff9908e183c3cac60ce8ee54e9))
* **rpc:** remove bridge and platform handlers ([#1510](https://github.com/choiceoh/Deneb/issues/1510)) ([febecb8](https://github.com/choiceoh/Deneb/commit/febecb86f8c894d1ef1dd9315191ee222e08d62b))
* **rpc:** remove dead RPC methods (approval, inject, preview, resolve, doctor, monitoring.activity) ([#1519](https://github.com/choiceoh/Deneb/issues/1519)) ([d3d498d](https://github.com/choiceoh/Deneb/commit/d3d498d03ddcbee1586c13fe4736da45e40bd00d))
* **runtime:** remove dead HeartbeatEvent and InternalRegistry infrastructure ([#1511](https://github.com/choiceoh/Deneb/issues/1511)) ([58e8cb0](https://github.com/choiceoh/Deneb/commit/58e8cb06196fa6170a1759b7bf374155bdcd8100))
* **scripts:** consolidate dev scripts and remove indirection layers ([cea4dd7](https://github.com/choiceoh/Deneb/commit/cea4dd7253add145ac2c69e24e02e5a0d7de4d36))
* **scripts:** consolidate dev scripts and remove indirection layers ([82fb41b](https://github.com/choiceoh/Deneb/commit/82fb41bf00c6b1fbdfcc5af2b1f421283c17c5f2))
* **server:** remove dead WebSocket, HTTP RPC, and dedupe code ([f01b4b1](https://github.com/choiceoh/Deneb/commit/f01b4b13cfd35bb35802c4ebc4de946abec42221))
* **server:** remove dead WebSocket, HTTP RPC, and dedupe code ([88d377e](https://github.com/choiceoh/Deneb/commit/88d377ee4415edb9e031104ed412720d243a1568))
* **session,cron:** harden emit re-entry and document lock hierarchy ([#1542](https://github.com/choiceoh/Deneb/issues/1542)) ([97d3c7f](https://github.com/choiceoh/Deneb/commit/97d3c7fb645e79b799a2a65ebf63d2bdcbcf10f9))
* **session:** async per-subscriber mailboxes in EventBus ([#1547](https://github.com/choiceoh/Deneb/issues/1547)) ([f81654d](https://github.com/choiceoh/Deneb/commit/f81654dd99c68ed16982810f09d647f7e9dd53a1))
* **skills:** consolidate domain module — merge skill/ into skills/, remove dead code (~820 LOC) ([5989d7e](https://github.com/choiceoh/Deneb/commit/5989d7e7c5fd637aa20008df41aa4a9a33fed6ac))
* **skills:** consolidate domain module, remove dead code (~820 LOC) ([2d0a181](https://github.com/choiceoh/Deneb/commit/2d0a18101a591ecea4dfb00e07518fdbabac6fd0))
* strip coding-agent surface (git, /dashboard, graphify code, stale doc) ([#1664](https://github.com/choiceoh/Deneb/issues/1664)) ([a3d8bf6](https://github.com/choiceoh/Deneb/commit/a3d8bf6f2a1d2e664beccc9b4a953c6fbdbb3cbb))
* **web:** replace Perplexity with Serper for web search ([7ddc382](https://github.com/choiceoh/Deneb/commit/7ddc38221d380edc775bb1fe2fc05e33de78c190))
* **web:** replace Perplexity with Serper for web search ([3bf0f1c](https://github.com/choiceoh/Deneb/commit/3bf0f1c89e9087b1d2ef9abb6bab0b8dd529f6cc))
* **web:** replace Tavily with Perplexity Sonar ([005cb19](https://github.com/choiceoh/Deneb/commit/005cb194557e4e92143253796499ad099907cf25))
* **web:** replace Tavily with Perplexity Sonar for web search ([14fa01b](https://github.com/choiceoh/Deneb/commit/14fa01b0780481dc52d26dbb95f15938138c2125))

## [4.22.3](https://github.com/choiceoh/Deneb/compare/deneb-v4.22.2...deneb-v4.22.3) (2026-04-08)


### 🔧 Internal

* **server:** remove single-user unnecessary infrastructure ([d865132](https://github.com/choiceoh/Deneb/commit/d865132a3483f3ed60bfeef56b3b72105ffc88e9))

## [4.22.2](https://github.com/choiceoh/Deneb/compare/deneb-v4.22.1...deneb-v4.22.2) (2026-04-08)


### 🐛 Bug Fixes

* **chat:** lower default memory token budget from 200k to 150k ([07cd76a](https://github.com/choiceoh/Deneb/commit/07cd76aae1e0ab631e942379b1ca5934bb895142))
* **chat:** lower default memory token budget to 150k ([3a7cc52](https://github.com/choiceoh/Deneb/commit/3a7cc52b00151451407fb1452c818909b7e2fc90))
* **chat:** unify token budgets and prevent silent context truncation ([dd68a91](https://github.com/choiceoh/Deneb/commit/dd68a9167da2d7c254942a186931a937c4dc4ca7))
* **chat:** unify token budgets and prevent silent context truncation ([b0529e0](https://github.com/choiceoh/Deneb/commit/b0529e0835f41ae745b8af4cb7702939352dfe89))


### ⚡ Performance

* **chat:** reduce multi-turn token cost with prior-turn compaction ([4e5222c](https://github.com/choiceoh/Deneb/commit/4e5222c5ee4945a80219f1228f9d9287e2f5e41c))
* **chat:** reduce multi-turn token cost with prior-turn compaction and tighter web defaults ([fe9653f](https://github.com/choiceoh/Deneb/commit/fe9653f7cad3dd3913d40c438d8f218cf70993f7))


### 🔧 Internal

* **chat:** remove legacy context assembly, require Polaris Bridge ([c843347](https://github.com/choiceoh/Deneb/commit/c84334702ddafbd55c7506fbd1cba62300c6a386))
* **chat:** remove legacy context assembly, require Polaris Bridge ([906c92d](https://github.com/choiceoh/Deneb/commit/906c92dc5a372f9adab76192bdda7e33bb29fa7d))
* **chat:** show compaction tier in polaris log message ([2db17fb](https://github.com/choiceoh/Deneb/commit/2db17fb3d7646e78059f1ff88389f2d8bd0cc3fe))
* **chat:** show compaction tier in polaris log message ([add255c](https://github.com/choiceoh/Deneb/commit/add255c42a34f4408d616ff43f9b7d0c1ca6945b))
* remove dead code, simplify compaction config ([b08fe38](https://github.com/choiceoh/Deneb/commit/b08fe388b607287ee1235edd9fc669956d05e60d))
* remove dead code, simplify compaction config ([26f3421](https://github.com/choiceoh/Deneb/commit/26f34210084259af8ef8628bf0c0a8f80c9a5719))
* **rpc:** remove dead validators and orphaned methods ([c740743](https://github.com/choiceoh/Deneb/commit/c7407434b76fc714a6ddc1993ec4a6247fb531db))
* **rpc:** remove dead validators, fix name mismatches, delete send/poll/wake methods ([19a2489](https://github.com/choiceoh/Deneb/commit/19a248941c6e971c6b39dae98f6cdee47c468e1b))
* **telegram:** remove multi-user access control and auth for single-user deployment ([1a57ad9](https://github.com/choiceoh/Deneb/commit/1a57ad9b8cc4d011a114020c808a026cfab71f03))
* **telegram:** remove multi-user access control and auth for single-user deployment ([f8706de](https://github.com/choiceoh/Deneb/commit/f8706de30534aad6015cabaf24affe38bb413e54))
* **telegram:** remove orphan ThreadNamer code ([1c4bb2c](https://github.com/choiceoh/Deneb/commit/1c4bb2cc108283ec4f50a8c28ff38c414a85609d))
* **telegram:** remove orphan ThreadNamer code ([acd8f94](https://github.com/choiceoh/Deneb/commit/acd8f94fc2cc4b4f48d2969e7aa6f302e3562410))
* **tools:** rename rlm_projects.go to wiki_tools.go ([36278da](https://github.com/choiceoh/Deneb/commit/36278da03b199a4218ef97106c936f86757fd1d4))
* **tools:** rename rlm_projects.go to wiki_tools.go and fix PATH ([22f5684](https://github.com/choiceoh/Deneb/commit/22f56842dabce61a845ccd6f5d4c6ec69a1b532c))

## [4.22.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.22.0...deneb-v4.22.1) (2026-04-08)


### 🔧 Internal

* **chat,autoreply:** extract helpers to reduce nesting and parameterize dispatcher ctx ([8c127f3](https://github.com/choiceoh/Deneb/commit/8c127f3d92525e0a80d6acc0fc78e811f0ba0a03))
* **chat,autoreply:** reduce nesting and parameterize dispatcher ctx ([0bcf6df](https://github.com/choiceoh/Deneb/commit/0bcf6dfe2ec4a011769e4127b2306f715e461ab8))
* improve code hygiene — remove unused suppressions, add defer, flatten nesting ([83229af](https://github.com/choiceoh/Deneb/commit/83229af68edd0f168c0e1530b0fa04b82eb0c035))
* improve code hygiene (audit VIII [#37](https://github.com/choiceoh/Deneb/issues/37)-40) ([1dfa559](https://github.com/choiceoh/Deneb/commit/1dfa559cb2d8f2319cf14d35bbdb1e4e3c5bcda0))
* **rpc:** adopt Bind[P] generics across 74 RPC handlers ([b19df87](https://github.com/choiceoh/Deneb/commit/b19df874414508387d77029817c51282379c4c41))
* **rpc:** adopt Bind[P] generics across 74 RPC handlers ([0dc63c5](https://github.com/choiceoh/Deneb/commit/0dc63c5f538591816ceb1150a39a2079e4c5253e))
* **test:** standardize assertion messages to Go `got, want` convention ([42cc04a](https://github.com/choiceoh/Deneb/commit/42cc04a514e1375ac9bf39522e48b98ceba8a9a8))
* **test:** standardize assertion messages to Go got/want convention ([8acda61](https://github.com/choiceoh/Deneb/commit/8acda616de8964523429d3eb000b8f532f0b2e1a))

## [4.22.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.21.0...deneb-v4.22.0) (2026-04-08)


### ✨ Features

* **chat:** add Polaris compaction system ([2375f7f](https://github.com/choiceoh/Deneb/commit/2375f7febe2452994d09800b785f8fcc5f02e2ce))
* **chat:** replace compaction with Polaris system ([d1255a0](https://github.com/choiceoh/Deneb/commit/d1255a086426eaf4544b81dc472e8850c026d05e))
* **devtools:** add model hot-swap for live testing ([6d4e923](https://github.com/choiceoh/Deneb/commit/6d4e923000328e8dde51854ebdace599a526d888))
* **devtools:** add model hot-swap for live testing ([2eca841](https://github.com/choiceoh/Deneb/commit/2eca841cc60b27b0445b238a7197a1768bb8678d))
* **lcm:** add immutable store + dual-write bridge (Phase 1) ([4ffd2d5](https://github.com/choiceoh/Deneb/commit/4ffd2d5f99c2a69fa123fbbfa0a120bc52ccd87d))
* **polaris:** Polaris context management system ([e5a8ebf](https://github.com/choiceoh/Deneb/commit/e5a8ebf4a30c971f1df3013fe9dfdc3688a61c90))
* **polaris:** unify LCM into Polaris with condensation and full rebranding ([b4777f1](https://github.com/choiceoh/Deneb/commit/b4777f16c20722f5b5ee77c4d333eb480cac3393))
* **rlm:** add root and worker LLM observation traces ([bd45e34](https://github.com/choiceoh/Deneb/commit/bd45e3455c53a9960fc748950795508358a28411))
* **rlm:** add root and worker LLM observation traces ([82bbb7e](https://github.com/choiceoh/Deneb/commit/82bbb7e836df2328f16e9a32bfc20752768a024f))
* **tokenest:** add script-aware token estimation engine with self-calibration ([8b2e2af](https://github.com/choiceoh/Deneb/commit/8b2e2af3e4d5c86850f5e6766c7fe37638d82af1))
* **tokenest:** script-aware token estimation with self-calibration ([fd738a9](https://github.com/choiceoh/Deneb/commit/fd738a94f37e038f70270ca493ed3736e954bd48))
* **web:** add parallel queries and deep_research tool ([61b0546](https://github.com/choiceoh/Deneb/commit/61b05461575249d85990bf430fa66e5cebe83e9c))
* **web:** add parallel queries and deep_research tool ([658a257](https://github.com/choiceoh/Deneb/commit/658a257dabd036ab6a13b25d9417059bcc87e301))
* **web:** replace Perplexity with Tavily Search API ([0cb208a](https://github.com/choiceoh/Deneb/commit/0cb208a531105b3f3e32027c5e200fcfbb3d6cd6))
* **web:** replace Perplexity with Tavily Search API ([17958b2](https://github.com/choiceoh/Deneb/commit/17958b270edffb8c00ab92fa4ed89e47a3fa976e))
* **wiki:** add RLM-based auto-recording after every chat response ([b6c4325](https://github.com/choiceoh/Deneb/commit/b6c4325c04e5f8570970c6139ea80b7ce8402c43))
* **wiki:** add verification phase to dreamer ([9619c94](https://github.com/choiceoh/Deneb/commit/9619c9431520e33299c51e92b12b0c5bbc29a61a))
* **wiki:** add verification phase to dreamer (duplicate + misclassification detection) ([f2f2e47](https://github.com/choiceoh/Deneb/commit/f2f2e47c4935cce041d90a1a906438e0cdfd8cb2))
* **wiki:** align with Karpathy wiki concept ([5182f12](https://github.com/choiceoh/Deneb/commit/5182f120764336e159e4359061036ce2d276fbe6))
* **wiki:** enable wiki by default ([fb358a1](https://github.com/choiceoh/Deneb/commit/fb358a12251a104a7155f02f0f43440a574665d7))
* **wiki:** enable wiki by default ([2742d4a](https://github.com/choiceoh/Deneb/commit/2742d4a280857210185f502596e265a44a221cb8))
* **wiki:** RLM-based auto-recording after every chat response ([5e68c10](https://github.com/choiceoh/Deneb/commit/5e68c101624eb062f6ccde7691e79e6b0aee5e6f))


### 🐛 Bug Fixes

* **chat:** address Polaris compaction review issues [#6](https://github.com/choiceoh/Deneb/issues/6) [#7](https://github.com/choiceoh/Deneb/issues/7) [#8](https://github.com/choiceoh/Deneb/issues/8) ([584e331](https://github.com/choiceoh/Deneb/commit/584e33100aedee9ae34c0498452f4a356ef20817))
* **cron:** skip running jobs in timer scheduling to prevent 2s refire loop ([88640b1](https://github.com/choiceoh/Deneb/commit/88640b18016274ca79b489e4d06502e4f48fd716))
* **cron:** skip running jobs in timer scheduling to prevent 2s refire loop ([08b497b](https://github.com/choiceoh/Deneb/commit/08b497b60556d16c51d837b16b92e09a52b5e39e))
* **devtools:** isolate dev/iterate server state from production memory ([afbea3c](https://github.com/choiceoh/Deneb/commit/afbea3c7af827204bf2f6ed5fac63bf2e7ee6709))
* **gateway:** address code review findings in file store migration ([1e8db1b](https://github.com/choiceoh/Deneb/commit/1e8db1be010121f47db450e048d392cb03b7d41e))
* **gateway:** lowercase error prefixes and annotate best-effort error handling ([b5ea1e5](https://github.com/choiceoh/Deneb/commit/b5ea1e549433b4265c493546ac8f883cb2d19437))
* **gateway:** lowercase error prefixes and annotate best-effort error handling ([eb145d5](https://github.com/choiceoh/Deneb/commit/eb145d5fbea2cc6ae351894c450eca1934531747))
* **gateway:** second-pass review fixes for file store migration ([f935e99](https://github.com/choiceoh/Deneb/commit/f935e99b9d3ed58a23e52dc9a0a2dadcfe19ce96))
* **http:** add TLSHandshakeTimeout to SSRF transports, probe immediately in WaitForHealth ([03d2393](https://github.com/choiceoh/Deneb/commit/03d2393ea443c41945ed826007b29ce56327f1cb))
* **http:** propagate context to WaitForHealth HTTP requests ([e123a03](https://github.com/choiceoh/Deneb/commit/e123a03be4fd61d3ed6075568bff2afeb25fe827))
* **http:** review fixes — defer CloseIdle, drop redundant timeout param, avoid req.Clone ([47be000](https://github.com/choiceoh/Deneb/commit/47be00025a3ada96516960c6dadd64eb74a747b3))
* **rlm:** add premature FINAL guard to prevent plan-as-answer ([8a8e9be](https://github.com/choiceoh/Deneb/commit/8a8e9bec140dbc9279d9df2b3f0465db4d5f403c))
* **rlm:** add premature FINAL guard to prevent plan-as-answer ([a5403b9](https://github.com/choiceoh/Deneb/commit/a5403b909de17e059250f7e0f9a111a38503fea7))
* **wiki:** harden wiki subsystem — validation, dedup, split, noise filter ([b34e710](https://github.com/choiceoh/Deneb/commit/b34e7101bae7bce60d35eeea27597cc01ba0848f))
* **wiki:** harden wiki subsystem — validation, dedup, split, noise filter ([0a3beba](https://github.com/choiceoh/Deneb/commit/0a3bebace2c4e7c16262b948af8f5823687b680a))


### ⚡ Performance

* **tasks:** coalesce snapshot writes and prune orphaned events ([a5eaeb0](https://github.com/choiceoh/Deneb/commit/a5eaeb025c010e4034278ff5160b08ff53bc1349))


### 🔧 Internal

* add channel direction annotations for type safety ([d080b2e](https://github.com/choiceoh/Deneb/commit/d080b2e6b2c2865f69814c871b43ebf62e2872ed))
* add channel direction annotations for type safety ([b9cca7a](https://github.com/choiceoh/Deneb/commit/b9cca7a138be344fbe99de57314cac55efd51c3c))
* **auth:** simplify RBAC to single-user model ([47ade16](https://github.com/choiceoh/Deneb/commit/47ade165976fcfda337d024a684092b2bd1a1531))
* **auth:** simplify RBAC to single-user model ([0be2b81](https://github.com/choiceoh/Deneb/commit/0be2b81b239ce48b951805e71d99a854af944411))
* **autoresearch,chat:** split large files into focused modules ([9fa8ea0](https://github.com/choiceoh/Deneb/commit/9fa8ea0c3f27cc4c8044d79a2a381dc8cd9e9a8d))
* **autoresearch:** split executor.go into focused modules ([b960fa2](https://github.com/choiceoh/Deneb/commit/b960fa2f3a99db22a20c51f866f5ddb21219d4d0))
* **chat:** extract slash command + status into dispatch_slash.go ([0735198](https://github.com/choiceoh/Deneb/commit/0735198778322447c2b23e79b0b4886f14fd2f9f))
* **chat:** extract slash+status from dispatch.go ([f6775c3](https://github.com/choiceoh/Deneb/commit/f6775c39e90b25d8d03f1b099dcee48649050e09))
* **chat:** remove compaction system ([7d02339](https://github.com/choiceoh/Deneb/commit/7d023390807ab6ae08232aef9fbb426ffb087e90))
* **chat:** split run_helpers.go into domain-specific files ([73a98e3](https://github.com/choiceoh/Deneb/commit/73a98e3681f536213c122833161f3d8a7f352867))
* **chat:** split run_helpers.go into domain-specific files ([4b93312](https://github.com/choiceoh/Deneb/commit/4b93312510d7d03c8f882480d9c41b2e3f78598d))
* **config:** remove dead config types and over-engineered settings ([494d115](https://github.com/choiceoh/Deneb/commit/494d1154bddd7effe407f694591769257cbc2ddc))
* **config:** remove dead config types, unused middleware, and over-engineered settings ([81545ee](https://github.com/choiceoh/Deneb/commit/81545eee58361893ca2a43e9e71f0e98c55de24e))
* **coremarkdown:** split ir.go into focused modules ([79e9000](https://github.com/choiceoh/Deneb/commit/79e9000ac208b50188631f2d7c0300ec44722969))
* **coremarkdown:** split ir.go into focused modules ([ea6e3c6](https://github.com/choiceoh/Deneb/commit/ea6e3c61161598d09b765ce5b3a1e9cc1112ae9f))
* decompose StreamChat, extract validation helper, struct-ify updateResult ([6dac6b4](https://github.com/choiceoh/Deneb/commit/6dac6b4830c257e19672154d9ae54225bdb0422c))
* decompose StreamChat, extract validation helper, struct-ify updateResult ([7dfd62f](https://github.com/choiceoh/Deneb/commit/7dfd62f6292a26e463db283dc32ac3de7e5c80f8))
* **error:** implement Unwrap across all error types, preserve error chains, expand errors.Join ([5284396](https://github.com/choiceoh/Deneb/commit/52843964cc838b42f86041ab27e11d39932e9447))
* **error:** implement Unwrap, preserve error chains, expand errors.Join ([02cf293](https://github.com/choiceoh/Deneb/commit/02cf293390d8565f86a47f9f30f66d3cc98270af))
* **gateway:** add 20 compile-time interface compliance checks ([d6451af](https://github.com/choiceoh/Deneb/commit/d6451af816886ec9d7adb01f80a35100c8ece46d))
* **gateway:** add 20 compile-time interface compliance checks across 17 files ([c3b7019](https://github.com/choiceoh/Deneb/commit/c3b701969b74f1c99d0dbd03362b8033c2c254ad))
* **gateway:** add compile-time interface compliance checks ([0f4aa23](https://github.com/choiceoh/Deneb/commit/0f4aa23b98b63151714aa9f821ff3d01916fe289))
* **gateway:** add compile-time interface compliance checks ([72dcbb0](https://github.com/choiceoh/Deneb/commit/72dcbb02be045641ff6e24f5d6f9376a617b579b))
* **gateway:** drop redundant Get prefix from 35 methods/functions ([f318ccf](https://github.com/choiceoh/Deneb/commit/f318ccfb72acdce415186be24fb893510c2db7b2))
* **gateway:** drop redundant Get prefix from 35 methods/functions ([96c083b](https://github.com/choiceoh/Deneb/commit/96c083b69777eb1c01cd400c68245ae2e5f78305))
* **gateway:** replace nhooyr.io/websocket with internal ws package ([3f54cb8](https://github.com/choiceoh/Deneb/commit/3f54cb866d61352300bdb6ecdb6f6c7686cbba6d))
* **gateway:** replace nhooyr.io/websocket with internal ws package ([123873e](https://github.com/choiceoh/Deneb/commit/123873e8431f0e72dc14a67c69da947698431584))
* **gateway:** replace SQLite with zero-dependency file stores ([8366a71](https://github.com/choiceoh/Deneb/commit/8366a71ef113a23028cb96a9538221c3fafdf9a9))
* **gateway:** replace SQLite with zero-dependency file stores ([f5b587a](https://github.com/choiceoh/Deneb/commit/f5b587a7f4ba2b5b947df2e575cd396fddd942b5))
* **gateway:** unify data structures — generic LRU cache and map[string]struct{} sets ([8c906e6](https://github.com/choiceoh/Deneb/commit/8c906e68d80cf86350574953dd362b5bf2091e2e))
* **gateway:** unify data structures — generic LRU cache and map[string]struct{} sets ([eceeca0](https://github.com/choiceoh/Deneb/commit/eceeca04a99a8a0270a2f2db8c8ec595597570ba))
* **http:** centralize HTTP transport pool ([e9c259b](https://github.com/choiceoh/Deneb/commit/e9c259b66642e65dd5efcde2f414262b8b93ef35))
* **http:** centralize HTTP transport with shared pool, User-Agent, and health check ([bd02e22](https://github.com/choiceoh/Deneb/commit/bd02e22726c0ec4b7af31c73dddc5f970021c515))
* **httpretry:** unify duplicate APIError types ([2d20b46](https://github.com/choiceoh/Deneb/commit/2d20b46a51746ade2d5b0ba110f4be7731f196b7))
* **httpretry:** unify duplicate APIError types from telegram and llm ([84bbd85](https://github.com/choiceoh/Deneb/commit/84bbd851a270395605ba87123c97c0a544336bd9))
* **llm:** extract OpenAI wire types to openai_types.go ([b36f405](https://github.com/choiceoh/Deneb/commit/b36f405461ac4ffbe44a6151039673bc48ecbe2d))
* **llm:** extract OpenAI wire types to openai_types.go ([5abba27](https://github.com/choiceoh/Deneb/commit/5abba272f81c439cd74f26423f5f3fcf65856a53))
* **markdown:** replace goldmark with custom subset parser ([36be097](https://github.com/choiceoh/Deneb/commit/36be097b4c96872322ba47a141d0d8e3a0eec880))
* **markdown:** replace goldmark with custom subset parser ([ecb219f](https://github.com/choiceoh/Deneb/commit/ecb219f68eb07810e1883a7ab9bd897f9540c5db))
* **monitoring:** remove multi-channel abstraction ([53001f0](https://github.com/choiceoh/Deneb/commit/53001f02fb9756420a670c02cbbfab189f166bce))
* **monitoring:** remove multi-channel abstraction from watchdog and dispatch ([fed64a7](https://github.com/choiceoh/Deneb/commit/fed64a759990003e1344a614b6bfa1b733c232af))
* **provider:** fix Go acronym naming (ApiKey→APIKey, BaseUrl→BaseURL) ([6cd4878](https://github.com/choiceoh/Deneb/commit/6cd487831f734747644a0e7dbba0a63644e77966))
* **provider:** fix Go acronym naming conventions ([7dac49e](https://github.com/choiceoh/Deneb/commit/7dac49ed33c2f640afd14ba4c89c3325d074a5be))
* reorganize internal/ into logical groups ([cc493ac](https://github.com/choiceoh/Deneb/commit/cc493ac01ed36b8134787dea7d022b54baf08d62))
* reorganize internal/ packages into logical groups and restructure scripts/ ([53888f0](https://github.com/choiceoh/Deneb/commit/53888f028a43ea7d10473bbb2ddeb8ca39eb7ecc))
* replace mutex with channel patterns for signal, queue, and session manager ([4a0997c](https://github.com/choiceoh/Deneb/commit/4a0997c18c5d4b2e49c141d3cba442b68f682977))
* replace mutex with channel patterns for signals and serialization ([28dbfc5](https://github.com/choiceoh/Deneb/commit/28dbfc5118c5faae78a1118a2fff8496bc67c8a3))
* **scripts:** extract shared dev server library ([9a6335d](https://github.com/choiceoh/Deneb/commit/9a6335d84117415ab1eb6d1921baedb5f34d78a6))
* **scripts:** extract shared dev server library ([d27cf4e](https://github.com/choiceoh/Deneb/commit/d27cf4ed0996d4b10454140e3b734a549c66cd3f))
* **server:** remove RL/RLM system ([794687f](https://github.com/choiceoh/Deneb/commit/794687f533b2fc8560e1f19090deb60742bf6d01))
* **server:** remove RL/RLM system ([e5d0e4f](https://github.com/choiceoh/Deneb/commit/e5d0e4f4cf9416fbe3a1aa6bf58cbbdcdb170bdf))
* **session:** make Manager zero-value safe via sync.Once lazy init ([2442db3](https://github.com/choiceoh/Deneb/commit/2442db3b0a8e8f5c7361ed7bce2cee604a9660d6))
* **session:** make Manager zero-value safe via sync.Once lazy init ([29022e8](https://github.com/choiceoh/Deneb/commit/29022e83d2d174eb456ac34f522eba7d39964098))
* **test:** add testutil.Must generic helper for one-line error assertions ([af69751](https://github.com/choiceoh/Deneb/commit/af6975142e81eb4af2065c536f65b412620069ac))
* **test:** adopt cmp.Diff for full struct comparison ([788efa4](https://github.com/choiceoh/Deneb/commit/788efa4132397d862d7ea61aba3da6efff6f63c7))
* **test:** adopt cmp.Diff for full struct comparison in round-trip and deserialization tests ([ef922d1](https://github.com/choiceoh/Deneb/commit/ef922d1af894e06115edc24d08c9b4fe8c7f2294))
* **test:** extract t.Helper() test helpers ([9175b10](https://github.com/choiceoh/Deneb/commit/9175b108d43e80c12307fbbcb8905354fa0e5c94))
* **test:** extract t.Helper() test helpers to reduce setup duplication ([fe21d94](https://github.com/choiceoh/Deneb/commit/fe21d948337410749b5a0d360d199d5c88935864))
* **test:** introduce testutil.NoError helper to reduce err!=nil boilerplate ([cd10890](https://github.com/choiceoh/Deneb/commit/cd1089065715a32b60249e8748d133d8dff4a1af))
* **test:** introduce testutil.NoError to reduce boilerplate ([c00e9d8](https://github.com/choiceoh/Deneb/commit/c00e9d83295f30d7ef341a23342b638b4a54ee47))
* **toolreg:** switch tool schema source from YAML to JSON ([39ee793](https://github.com/choiceoh/Deneb/commit/39ee7939c51c2ea5e3dc9eb68267cf23c12df81e))
* **toolreg:** switch tool schema source from YAML to JSON ([e16a5e9](https://github.com/choiceoh/Deneb/commit/e16a5e9b8a374d84aea35d9605b54368d0b997e9))
* **tools:** decompose gitParams god struct into per-subcommand types ([856196d](https://github.com/choiceoh/Deneb/commit/856196d33564982ccc289dbd2e9d8c71216a6d87))
* **tools:** decompose gitParams god struct into per-subcommand types ([2a652f9](https://github.com/choiceoh/Deneb/commit/2a652f9c5be3303aab85c3da65ca216010b9e39f))
* **tools:** split stubs_exec.go into cron and session files ([1dd4b1f](https://github.com/choiceoh/Deneb/commit/1dd4b1f4bfffe3e622834672df6b4b4f7593a3e3))
* **tools:** split stubs_exec.go into cron and session files ([826e601](https://github.com/choiceoh/Deneb/commit/826e6014913fb97fcb28f7bd2be85c1e169238f8))
* **wiki:** remove RLM system, keep wiki only ([e050cd3](https://github.com/choiceoh/Deneb/commit/e050cd3e3b463ac096ee8363df64bd5f51864d28))
* **wiki:** remove RLM, align with Karpathy wiki concept ([09a36b7](https://github.com/choiceoh/Deneb/commit/09a36b7d4576d010ba55a29ac63d9571ecc6ad4a))
* **wiki:** simplify diary to raw append, remove curator RLM ([f28249b](https://github.com/choiceoh/Deneb/commit/f28249b8d05f9d882a3f6e5f896262e834ff6e17))
* **wiki:** simplify diary to raw append, remove curator RLM ([808600f](https://github.com/choiceoh/Deneb/commit/808600fdbb42af5cda60a5c7af9ad7536d13b977))

## [4.21.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.20.0...deneb-v4.21.0) (2026-04-07)


### ✨ Features

* **chat:** promote sessions_spawn to eager tool ([068638c](https://github.com/choiceoh/Deneb/commit/068638c485ce9c315fc9ec0848df6694c5e25bf8))
* **chat:** promote sessions_spawn to eager tool and strengthen sub-agent prompt ([d3671e8](https://github.com/choiceoh/Deneb/commit/d3671e875e43a9b8ac24e0dd520cb919487f1534))
* **mcp:** expose RLM observation tools for Claude Code ([2efbea3](https://github.com/choiceoh/Deneb/commit/2efbea398014b906e1db1b06996ce301f5caf7c9))
* **rlm:** add observation trace for loop introspection ([af6e051](https://github.com/choiceoh/Deneb/commit/af6e05143bea1ded89b1b70029e24a69a25b2cf4))
* **rlm:** add observation trace for RLM loop introspection ([244f027](https://github.com/choiceoh/Deneb/commit/244f027c6773ef0d9e9b48ffa620860318e19397))
* **rlm:** wire LLMBatchFn and RLMQueryFn into chat REPL ([5280550](https://github.com/choiceoh/Deneb/commit/528055018a91ad2cc12f15d6f131af83cbb6577b))
* **rlm:** wire LLMBatchFn and RLMQueryFn into chat REPL environment ([86fff12](https://github.com/choiceoh/Deneb/commit/86fff1299c591975848821841ffa261c8aaa619e))
* **test:** add benchmark suite — HLE, ARC-AGI-2, BrowseComp, LLM-as-Judge ([e901156](https://github.com/choiceoh/Deneb/commit/e9011567bf33a5c64256e88f44fa8672764eae6d))
* **test:** add benchmark suite — HLE, ARC-AGI-2, BrowseComp, LLM-as-Judge, Pairwise ([c4aa192](https://github.com/choiceoh/Deneb/commit/c4aa19278ccc166f5d3f9b1298d0bfa12581f4b8))
* **test:** expand bench-challenge to 14 tests — add 8 BrowseComp questions ([ea8c1f2](https://github.com/choiceoh/Deneb/commit/ea8c1f2a51084b5dfa996562773a124c2cb07324))
* **wiki:** add Karpathy Wiki + RLM scaffold navigation ([9a4bb6c](https://github.com/choiceoh/Deneb/commit/9a4bb6cfc29e8ca4f672fd3a962be18c703c5cd8))
* **wiki:** Karpathy Wiki + RLM scaffold navigation ([fce848a](https://github.com/choiceoh/Deneb/commit/fce848a296b164b7a343651f2f6644144f0ada72))


### 🔧 Internal

* **autoresearch:** extract shared helpers from iteration functions ([112ea63](https://github.com/choiceoh/Deneb/commit/112ea639e4e8c4532275abbd56991a426dd7cb9c))
* **autoresearch:** extract shared helpers from iteration functions ([01bd139](https://github.com/choiceoh/Deneb/commit/01bd139955cd78aac01761c22dde5058eef3b018))

## [4.20.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.19.1...deneb-v4.20.0) (2026-04-07)


### ✨ Features

* **chat:** allow read tool in conversation mode ([d14984d](https://github.com/choiceoh/Deneb/commit/d14984d589d6297101d1da7f7c5d3f56f7143a20))
* **chat:** allow read tool in conversation mode ([fda8fdf](https://github.com/choiceoh/Deneb/commit/fda8fdf802d9b4575a5679d73c3c98316d80f668))


### 🐛 Bug Fixes

* **chat:** deliver subagent results to parent notification ([06623f6](https://github.com/choiceoh/Deneb/commit/06623f616d7756c38298313622969a9abbcca491))
* **chat:** prevent NO_REPLY suppression on subagent notification runs ([51a490e](https://github.com/choiceoh/Deneb/commit/51a490e11bdaac0d86c9d077489fd4cd58c7d918))
* **chat:** remove subagent polling inducements ([e60773f](https://github.com/choiceoh/Deneb/commit/e60773fb651183239c4d9a381cb01aad0cd2ecaa))
* **chat:** remove subagent polling inducements, add SpawnFlag for yield behavior ([89b6bc7](https://github.com/choiceoh/Deneb/commit/89b6bc729bf3d252547fe67d600c6b888748d478))
* **chat:** use AllText for session LastOutput so subagent results are delivered ([9647acc](https://github.com/choiceoh/Deneb/commit/9647acc2e71e3a9cc58e575c0c5a106c810bfaab))
* **cron:** disable cron scheduler on dev instances ([515b9df](https://github.com/choiceoh/Deneb/commit/515b9df80fdfb1900dadecb96004491c1ff7c13b))
* **cron:** disable cron scheduler on dev instances to prevent duplicate email analysis ([e25f4a1](https://github.com/choiceoh/Deneb/commit/e25f4a1df2d6354886a470ec015162ff16327a81))
* **llm:** stop usage-only chunk from overwriting tool_use stop reason ([4d9edf7](https://github.com/choiceoh/Deneb/commit/4d9edf716fd6ec30e89efcf2ef017935004fc5b5))
* **llm:** stop usage-only chunk from overwriting tool_use stop reason ([82bec0c](https://github.com/choiceoh/Deneb/commit/82bec0c07958bbaec48e9a81b4e108a9c72e8381))
* **telegram:** make PrimaryChatID deterministic across restarts ([1b6fd95](https://github.com/choiceoh/Deneb/commit/1b6fd95bfbf3d624588ba0ae3766fb9424791f78))
* **telegram:** make PrimaryChatID deterministic across restarts ([26744d2](https://github.com/choiceoh/Deneb/commit/26744d2e58efd9e73191a568ff3116546d4a2866))


### ⚡ Performance

* **boot:** cap boot history to 30K tokens, remove MEMORY.md from context ([eb02cb8](https://github.com/choiceoh/Deneb/commit/eb02cb8567442e0a56000a1ef4834716ef3de745))
* **boot:** cap boot session history to 30K tokens and remove MEMORY.md from context files ([8e8aaba](https://github.com/choiceoh/Deneb/commit/8e8aaba75f21bad2d2ca520b0a2b665d043e4518))


### 🔧 Internal

* **chat:** remove knowledge prefetch, fix context file list ([4dedb3c](https://github.com/choiceoh/Deneb/commit/4dedb3c46a736e4919fe08e41faf55cab2fba4bf))
* **chat:** remove knowledge prefetch, fix context file list ([5cc18c1](https://github.com/choiceoh/Deneb/commit/5cc18c1a4876096161dca7870010aade4583f898))
* **telegram:** remove tool progress tracker ([5d69ef7](https://github.com/choiceoh/Deneb/commit/5d69ef75f87e94bc6aa0e8d130e73d9b50c57867))
* **telegram:** remove tool progress tracker ([9f8fe32](https://github.com/choiceoh/Deneb/commit/9f8fe32a832504a5df6945172a8328a9c287eefe))
* **test:** replace WebSocket tests with Telegram-based testing ([d6e1c6e](https://github.com/choiceoh/Deneb/commit/d6e1c6e85f22f745f54ae02783a544e85a21c5a4))
* **test:** replace WebSocket tests with Telegram-based testing ([221f722](https://github.com/choiceoh/Deneb/commit/221f7222979701454d6d034360b882a791d2055c))

## [4.19.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.19.0...deneb-v4.19.1) (2026-04-07)


### 🔧 Internal

* **chat:** remove SendLite ([b19bcbe](https://github.com/choiceoh/Deneb/commit/b19bcbe2614481050d3b6bbcee4c7e097be783ba))
* **chat:** remove SendLite, use full pipeline for boot task ([650997a](https://github.com/choiceoh/Deneb/commit/650997a5fb36b1253b3917365752645759f8ff28))
* **testing:** remove vchat mock Telegram test environment ([1f1f98e](https://github.com/choiceoh/Deneb/commit/1f1f98efd9480c10caa65479805b9d10bf545835))
* **testing:** remove vchat mock Telegram test environment ([e80d3ca](https://github.com/choiceoh/Deneb/commit/e80d3ca24386db1b30ae21ef7b0c1bd3124dd9bd))

## [4.19.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.18.1...deneb-v4.19.0) (2026-04-07)


### ✨ Features

* **chat:** add emergency compaction summarization, RLM sub-agent system prompt inheritance, and REPL environment initialization ([40d42a8](https://github.com/choiceoh/Deneb/commit/40d42a8ef7d13f6e19ee62b11e952730371275e6))
* **chat:** emergency compaction + RLM sub-agent improvements + REPL init ([bb5e020](https://github.com/choiceoh/Deneb/commit/bb5e02056e07527b882e82d38969d516ecb2e4e4))
* **localai:** apply Gemma 4 vendor-recommended sampling defaults ([b3e4434](https://github.com/choiceoh/Deneb/commit/b3e4434ce9e929e6f52534da4fa68249971c4b56))
* **localai:** apply Gemma 4 vendor-recommended sampling defaults ([51a5da1](https://github.com/choiceoh/Deneb/commit/51a5da1d382eb9e044909257de9a80485cf2382a))
* **rlm:** add memory_recall read tool and tier-1 auto-injection ([9a06d10](https://github.com/choiceoh/Deneb/commit/9a06d10e7e88a6266c20294095c94ee00514c44a))
* **rlm:** add memory_recall tool and tier-1 auto-injection ([357dc84](https://github.com/choiceoh/Deneb/commit/357dc84165b4815da49b3010c1d9df4283eba373))
* **rlm:** add wiki write-back path with proactive knowledge recording ([92079cc](https://github.com/choiceoh/Deneb/commit/92079cc946580216c5577a671538d60f1b26d844))
* **rlm:** add wiki write-back path with proactive knowledge recording ([8aa36cf](https://github.com/choiceoh/Deneb/commit/8aa36cf8ac86db5e7bed6820a3f85f1e6de4e2b8))
* **rlm:** connect mail analysis and morning letter to RLM diary ([bd0e496](https://github.com/choiceoh/Deneb/commit/bd0e4966a635f3ede4b8531d85964b21246cde31))
* **rlm:** connect mail analysis and morning letter to RLM diary ([e00a723](https://github.com/choiceoh/Deneb/commit/e00a723cc12d2685fd0a95d8ca8deb611ccfdcb8))


### 🐛 Bug Fixes

* **rlm:** move RLM handler registration from Early to Late phase ([2026b2f](https://github.com/choiceoh/Deneb/commit/2026b2f65ebd7507c9f51893650a2269d7cbdb09))
* **rlm:** move RLM handler registration from Early to Late phase ([5108d7c](https://github.com/choiceoh/Deneb/commit/5108d7cf35fab5188dda63c0560de07dcccda39a))
* **telegram:** use detached context for progress message edits ([26c1e35](https://github.com/choiceoh/Deneb/commit/26c1e35b424d1a8234830093c8e14da3c3498a07))
* **telegram:** use detached context for progress message edits ([02689a0](https://github.com/choiceoh/Deneb/commit/02689a0df920bfa8f87ec8a488b67e8209f45b96))


### 🔧 Internal

* **gateway:** remove Talk, Wizard, and Vega dead modules ([c7936e1](https://github.com/choiceoh/Deneb/commit/c7936e17cf41f65b42cba0c0a7da99be22991ae2))
* **gateway:** remove Talk, Wizard, and Vega dead modules ([b7f3ace](https://github.com/choiceoh/Deneb/commit/b7f3ace3365f5dca8f93493ef04e3ca7fa1c4213))
* **localai:** remove auto-memory, activity summary, and session memory hooks ([58fd825](https://github.com/choiceoh/Deneb/commit/58fd825a59ba74dcc4aef394b19d12a03b24e4d5))
* **localai:** remove auto-memory, activity summary, and session memory hooks ([784fb9d](https://github.com/choiceoh/Deneb/commit/784fb9d534298aa9e80223342daba6abdfa18986))
* **localai:** remove CJK token blocking ([f776580](https://github.com/choiceoh/Deneb/commit/f77658072b56ac8378edef7d54beffc4631d042e))
* **localai:** remove CJK token blocking feature ([351b2ca](https://github.com/choiceoh/Deneb/commit/351b2cad077a1e672ff5d09164e642080fed6d09))
* **localai:** remove proactive context ([baa0c38](https://github.com/choiceoh/Deneb/commit/baa0c38dbdeb3d5de568aa5f1d210414f79a8952))
* **localai:** remove proactive context feature ([208ac14](https://github.com/choiceoh/Deneb/commit/208ac1425ef062bac0ac081621b9acc9150ebb2d))
* **localai:** remove redundant memory/summary hooks ([64896e3](https://github.com/choiceoh/Deneb/commit/64896e3f58a4d878d3559977f2747720678e148a))

## [4.18.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.18.0...deneb-v4.18.1) (2026-04-06)


### 🐛 Bug Fixes

* **chat:** remove user-facing tools from channelSilentTools ([a9663d5](https://github.com/choiceoh/Deneb/commit/a9663d56efcffa2ad83edd3243d73c74ad6ff4bd))
* **chat:** remove user-facing tools from channelSilentTools ([e359750](https://github.com/choiceoh/Deneb/commit/e3597500c3625e872a4c1a3a965dabb00b70c06e))

## [4.18.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.17.0...deneb-v4.18.0) (2026-04-06)


### ✨ Features

* **heartbeat:** reduce heartbeat interval to 3 minutes ([3973f33](https://github.com/choiceoh/Deneb/commit/3973f3354662df6f219269cf0890fe1f38380d0e))
* **heartbeat:** reduce heartbeat interval to 3 minutes ([d0704ac](https://github.com/choiceoh/Deneb/commit/d0704ac30098f21718d327a27a52de679c0bdba7))
* **heartbeat:** tighten to 2min interval, 1min idle threshold ([d02e866](https://github.com/choiceoh/Deneb/commit/d02e866b3bb16c5352000c79b96364dc63491323))
* **modelrole:** default main model to local vLLM (unify main/lightweight) ([8c52fbf](https://github.com/choiceoh/Deneb/commit/8c52fbf80c10c8e26ae17aa1f5fc11683402de30))
* **modelrole:** unify main/lightweight to local vLLM ([f46ce6d](https://github.com/choiceoh/Deneb/commit/f46ce6d37d644a492fccf550bd565e5320604ae2))
* **rlm:** implement independent iteration loop ([dd66888](https://github.com/choiceoh/Deneb/commit/dd66888140162acea1adf21edd3a07451551bc1e))
* **rlm:** implement independent iteration loop (alexzhang13/rlm) ([8ccd038](https://github.com/choiceoh/Deneb/commit/8ccd038b0589190cb95878dba00380762b89a773))
* **rlm:** tune defaults and prompts to match original RLM ([4e7f44a](https://github.com/choiceoh/Deneb/commit/4e7f44ae7fd153e80acaf488fb1a8f0f17610d94))
* **rlm:** tune token limits, prompts, and defaults to match original RLM ([4f358e6](https://github.com/choiceoh/Deneb/commit/4f358e6478ddcd41826168597782c771e9599c72))
* **rlm:** wire Phase 1 tools to wiki backend and add RPC layer ([9236c31](https://github.com/choiceoh/Deneb/commit/9236c3174e6d6ecb3f6a82f16826fa9657c2dc2e))
* **rlm:** wire Phase 1 tools to wiki backend and add RPC layer ([876ed29](https://github.com/choiceoh/Deneb/commit/876ed293f6e511ef8acbce98604e590614cfc185))


### 🔧 Internal

* **rlm:** consolidate wiki access exclusively through RLM ([6edeefa](https://github.com/choiceoh/Deneb/commit/6edeefac001cbdbbd06c14f18177c262cbd8a5bc))
* **rlm:** consolidate wiki access exclusively through RLM ([472bc52](https://github.com/choiceoh/Deneb/commit/472bc52d6e2124c18494968c4e9329ab5da7fb85))

## [4.17.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.16.1...deneb-v4.17.0) (2026-04-06)


### ✨ Features

* **rlm:** add Starlark REPL for RLM context exploration ([0f68b9e](https://github.com/choiceoh/Deneb/commit/0f68b9eb821ad3742eaf2cfbd355ce9fb9887a39))
* **rlm:** Starlark REPL for RLM context exploration ([1845f56](https://github.com/choiceoh/Deneb/commit/1845f56c1033b9bb1ef9a90060df30d2bfd385de))


### 🔧 Internal

* **codegen:** port Python code generators to Go ([a2e0637](https://github.com/choiceoh/Deneb/commit/a2e0637f5cac77e4f54c64a6af17f98ba1c561fb))
* **codegen:** port Python code generators to Go ([1d06b51](https://github.com/choiceoh/Deneb/commit/1d06b513a737055439704e8825ddb21fad21c7e8))
* **vega:** remove memory_search + vega systems replaced by wiki ([c9ec0a0](https://github.com/choiceoh/Deneb/commit/c9ec0a0c7ab1946e313fb270817761031aa7b11b))

## [4.16.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.16.0...deneb-v4.16.1) (2026-04-06)


### 🔧 Internal

* **chat:** wire wiki as default knowledge base, disconnect memory+vega ([2fe5869](https://github.com/choiceoh/Deneb/commit/2fe5869e55520bb7ad0c88eaceada25d2c041230))
* **chat:** wire wiki as default knowledge base, disconnect memory+vega ([40848f8](https://github.com/choiceoh/Deneb/commit/40848f8fe0852638db9986a5c6457e33859d1999))
* **ffi:** replace Rust FFI with pure Go for 4 modules ([edfcb48](https://github.com/choiceoh/Deneb/commit/edfcb483b9032e1a3cce7bf14ca93af46253f7cc))
* **ffi:** replace Rust FFI with pure Go for markdown, parsing, media, security ([cec2132](https://github.com/choiceoh/Deneb/commit/cec2132bea0204845979f1015dcb6e82e84798bd))

## [4.16.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.15.1...deneb-v4.16.0) (2026-04-06)


### ✨ Features

* **chat:** add RLM context externalization and sub-LLM spawning ([ac459e1](https://github.com/choiceoh/Deneb/commit/ac459e116bf5c1163eeb07db140a9c68449f2738))
* **chat:** RLM context externalization + sub-LLM spawning ([a13ca55](https://github.com/choiceoh/Deneb/commit/a13ca5515b8440d9c2536c2c1f8cf5a9a864d038))
* **markdown:** add pure-Go markdown-to-IR parser ([26e4316](https://github.com/choiceoh/Deneb/commit/26e43161d2fe15497fb9b302d02a49254bbd4be1))
* **markdown:** add pure-Go markdown-to-IR parser (coremarkdown package) ([64cae94](https://github.com/choiceoh/Deneb/commit/64cae9433477e3346ea9d10345802c12d9bd4516))
* **media:** add pure-Go MIME detection (coremedia package) ([cd09029](https://github.com/choiceoh/Deneb/commit/cd09029b73e850cabddcc6747c91aaad29b9bc9b))
* **media:** add pure-Go MIME detection (coremedia) ([a554729](https://github.com/choiceoh/Deneb/commit/a554729304c31dd9a708672661fcbdacaf958ecf))
* **parsing:** port HTML-to-Markdown from Rust to pure Go ([22677e0](https://github.com/choiceoh/Deneb/commit/22677e0cbc739ddd394d092d34a694fcc08821a7))
* **parsing:** port HTML-to-Markdown from Rust to pure Go ([b53dd6e](https://github.com/choiceoh/Deneb/commit/b53dd6ef7e03affedf7f22bcd0e73f9708fdf8c3))
* **parsing:** port URL extract, media tokens, base64 from Rust to pure Go ([f1a1257](https://github.com/choiceoh/Deneb/commit/f1a1257b763641d2d46b469ab520718a83c4387e))
* **parsing:** port URL extract, media tokens, base64 from Rust to pure Go ([4a7b459](https://github.com/choiceoh/Deneb/commit/4a7b459518abf79bfb8dde057fe9af0b499e9420))
* **protocol:** port protocol validation from Rust to pure Go ([a885fdb](https://github.com/choiceoh/Deneb/commit/a885fdb991f663a761d6ef556afbb8b43de54725))
* **protocol:** port protocol validation from Rust to pure Go ([a46ae71](https://github.com/choiceoh/Deneb/commit/a46ae71174c96524d5cbea35e16b1e83653a3e35))
* **testing:** add real Telegram e2e testing via Telethon ([7d9e09d](https://github.com/choiceoh/Deneb/commit/7d9e09db74ee21fd90eec7e358ebdf4a553ff058))
* **testing:** real Telegram e2e testing + always prod config ([1902819](https://github.com/choiceoh/Deneb/commit/19028198debc913fbc4a07ea862b8b310089650a))
* **wiki:** add LLM wiki knowledge base (Karpathy pattern) ([315244d](https://github.com/choiceoh/Deneb/commit/315244df1ca688ac17726e6b5e767b814588a548))
* **wiki:** add LLM wiki knowledge base (Karpathy pattern) ([4867e07](https://github.com/choiceoh/Deneb/commit/4867e0711d90174a062a6e9ea990c749b78f6203))


### 🐛 Bug Fixes

* **context:** drain expand_stack fully when depth reaches zero ([b11c67c](https://github.com/choiceoh/Deneb/commit/b11c67cc2f2ea170ca4612aac8497e084418a94d))
* **context:** drain expand_stack fully when depth reaches zero ([4670a20](https://github.com/choiceoh/Deneb/commit/4670a208faa0a48661628494e98b85d96b83c793))
* **cron:** resolve delivery target for cron job output ([1d0ee9f](https://github.com/choiceoh/Deneb/commit/1d0ee9f76f68c9c670d6c9aed8f879912f629125))
* **cron:** resolve delivery target from Telegram config for cron job output ([1145ea7](https://github.com/choiceoh/Deneb/commit/1145ea73cc26108ec07e51cc2dea83ed6bfe45f5))
* **memory:** prevent aurora-dream panic from crashing gateway ([14225f2](https://github.com/choiceoh/Deneb/commit/14225f2db977599d0585cd9a0df85da5acff36ac))
* **memory:** prevent aurora-dream panic from crashing gateway ([5b11800](https://github.com/choiceoh/Deneb/commit/5b1180069ae1b058e849d0cffa699ea03cf9ac76))
* **rlm:** add RLM tool approval policies, remove spawn batch maxItems hardcode ([ff3c25b](https://github.com/choiceoh/Deneb/commit/ff3c25ba221dcef1a82d4024cddb06673783b274))
* **rlm:** add tool approval policies, remove maxItems hardcode ([add0df4](https://github.com/choiceoh/Deneb/commit/add0df451e861f2c1d69ea4faab8304fc89be58b))
* **rlm:** atomic budget reservation, deterministic TOC, batch cancellation ([bc991f4](https://github.com/choiceoh/Deneb/commit/bc991f4655c2301d9f681be77e4e376803d5d777))
* **rlm:** atomic budget reservation, deterministic TOC, batch cancellation ([256ddf1](https://github.com/choiceoh/Deneb/commit/256ddf104cebb4ba5af8eec7019cc02ae2ce7616))
* **rlm:** per-call token budget, batch concurrency limit, encoding fixes ([a3d89b8](https://github.com/choiceoh/Deneb/commit/a3d89b8e43c40420c0d4f5a223348ae68febc0cb))
* **rlm:** per-call token budget, batch concurrency, encoding fixes ([82a920d](https://github.com/choiceoh/Deneb/commit/82a920dae8c7834843f8fad02d267c7716f6e715))
* **rlm:** remove dead Consume method, harden token budget and spawn tools ([c9b4927](https://github.com/choiceoh/Deneb/commit/c9b4927a15ba2fcafb38f7dd5dfb6aead9b3527c))
* **rlm:** remove dead Consume method, harden token budget and spawn tools ([a2c85a5](https://github.com/choiceoh/Deneb/commit/a2c85a5354ee3cbf6bc977ecb5ac906803306a39))
* **rlm:** settle token budget double-counting, wire MaxSubSpawns config ([7bba0a1](https://github.com/choiceoh/Deneb/commit/7bba0a12cd8367d089e1d94bee9c4f2414a83a78))
* **rlm:** settle token budget double-counting, wire MaxSubSpawns config, handle marshal errors ([b686013](https://github.com/choiceoh/Deneb/commit/b6860138dfeabb13a3a6f3601a48f9099ad79138))
* **testing:** load .env in dev-live-test.sh for status messages ([b00d97d](https://github.com/choiceoh/Deneb/commit/b00d97d21f4eb61c9c061d3336d1cde943fcea62))


### 🔧 Internal

* **localai:** replace qwen3.5 with gemma4 ([878e780](https://github.com/choiceoh/Deneb/commit/878e78004bdb5ebd70aea64988fb41941caf2711))
* **localai:** replace qwen3.5 with gemma4 as default lightweight model ([71be220](https://github.com/choiceoh/Deneb/commit/71be2206f4a4edbc37baba6a4d50dc7e718f369d))
* **security:** extract coresecurity package from ffi noffi ([aebdf9d](https://github.com/choiceoh/Deneb/commit/aebdf9d04541c3b8c3aeb74f4e32fc671acd6abb))
* **security:** extract coresecurity package from ffi noffi fallbacks ([14de7f9](https://github.com/choiceoh/Deneb/commit/14de7f997a2ef26464e5cf6c3eb525706dfe2625))
* **testing:** remove --prod-parity mode, always use production config ([96fe1a4](https://github.com/choiceoh/Deneb/commit/96fe1a4a57fdcdf26d02d2b1b92a78e859578980))
* **wiki:** Go-native FTS5, remove auto-injection, wire RLM ([32a3812](https://github.com/choiceoh/Deneb/commit/32a3812ce20700a455e9972b694e5a35e28b1a83))
* **wiki:** replace ripgrep with Go-native FTS5, remove auto-injection, wire RLM ([8c58efe](https://github.com/choiceoh/Deneb/commit/8c58efeb28e3442e2bb8edc0bf9f85462362c57a))

## [4.15.1](https://github.com/choiceoh/Deneb/compare/deneb-v4.15.0...deneb-v4.15.1) (2026-04-06)


### 🐛 Bug Fixes

* **chat:** strip bare [toolname] text leaked by GLM models ([ba47bf7](https://github.com/choiceoh/Deneb/commit/ba47bf71209d48a9e51a4c45467d3c7e4a50ec83))
* **chat:** strip bare [toolname] text leaked by GLM models ([ee2d5ec](https://github.com/choiceoh/Deneb/commit/ee2d5ec05a64732a012a63ef0a9f3889d8c7c428))

## [4.15.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.14.0...deneb-v4.15.0) (2026-04-06)


### ✨ Features

* **rl:** task-specific RL training pipeline ([f6f2236](https://github.com/choiceoh/Deneb/commit/f6f2236d76e86088119e900f8810081cf345b885))


### 🐛 Bug Fixes

* **rl:** resolve merge conflicts with main (PR [#1274](https://github.com/choiceoh/Deneb/issues/1274)) ([1e8cb47](https://github.com/choiceoh/Deneb/commit/1e8cb47ac22883df5f3a9042e10852ff67266fc4))

## [4.14.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.13.0...deneb-v4.14.0) (2026-04-06)


### ✨ Features

* **chat:** auto-silence management tools on Telegram ([711dfc1](https://github.com/choiceoh/Deneb/commit/711dfc1b6fede0a35f8690ff16bf177731e3c978))
* **chat:** auto-silence management tools on Telegram channel ([3a85089](https://github.com/choiceoh/Deneb/commit/3a8508908cbba0d21268cba071dffb67c6722087))
* Hermes-inspired skill genesis + RL self-learning pipeline ([c50553f](https://github.com/choiceoh/Deneb/commit/c50553f174895956f18935c8a1c6f6f2e2703e94))
* **rl:** add RL self-learning pipeline with sglang+Tinker-Atropos ([a4ec11f](https://github.com/choiceoh/Deneb/commit/a4ec11fa16b39918750b390fa3c1928061835656))
* **skills:** add skill genesis — auto-create skills from experience ([24ea353](https://github.com/choiceoh/Deneb/commit/24ea353ad7b4382d409511b0e63df831eace836d))


### 🐛 Bug Fixes

* **chat:** drain pending queue on run error ([be378ba](https://github.com/choiceoh/Deneb/commit/be378ba4170daba36f92c9124792b82be7ec3f3a))
* **chat:** drain pending queue on run error to prevent lost messages ([c5989f6](https://github.com/choiceoh/Deneb/commit/c5989f624f1bbdd0d31be2205df6f3c2e751b889))
* **chat:** enable mid-loop compaction for all modes ([9880b91](https://github.com/choiceoh/Deneb/commit/9880b913102822655cac77eed213ca7d0203c122))
* **chat:** enable mid-loop compaction for all modes, not just work mode ([d7c5394](https://github.com/choiceoh/Deneb/commit/d7c53945278a296481a7cf91ba3c46a647f8f136))
* **cron:** route all cron RPC handlers through persistent Service ([0db5062](https://github.com/choiceoh/Deneb/commit/0db5062756bf7773ec53aedde37dd461b7455227))
* **cron:** route all cron RPC handlers through persistent Service ([6afb541](https://github.com/choiceoh/Deneb/commit/6afb541b12df149e7a3bb3deed8b43070d4b72df))
* **genesis:** use correct sqlite driver name and import ([7f631c9](https://github.com/choiceoh/Deneb/commit/7f631c9e760a5397b2324d91fb777303522d12be))
* **genesis:** use correct sqlite driver name and import ([569fc25](https://github.com/choiceoh/Deneb/commit/569fc25838c2846eebec374fb9caa911fbb150d7))
* **memory:** strip code blocks from live and session memory ([90697a8](https://github.com/choiceoh/Deneb/commit/90697a8471588eecd208e919488acb67af38f248))
* **memory:** strip code blocks from live and session memory to prevent bloat ([f18453e](https://github.com/choiceoh/Deneb/commit/f18453e4ef9ccadd5f8aaf63eb4e85cd0fef3b79))
* **rl:** adapter hot-swap, server context, health check fixes ([8e94e4f](https://github.com/choiceoh/Deneb/commit/8e94e4fbba423c3ba4644c7055673c5edf48b19d))
* **rl:** wire RLService into buildHub and server lifecycle ([efd2fc8](https://github.com/choiceoh/Deneb/commit/efd2fc8305075c08af5caf05d34f86de3dd6f01b))
* **rl:** wire RLService into buildHub and server lifecycle ([a6afc4d](https://github.com/choiceoh/Deneb/commit/a6afc4da8601b01e7b66c5f74bea35c1cee6f42a))
* **server:** suppress spurious handshake-failed warnings for EOF disconnects ([c4a2c80](https://github.com/choiceoh/Deneb/commit/c4a2c80498e49d938f2bc26781dd19877d9b0916))
* **server:** suppress spurious handshake-failed warnings for EOF disconnects ([521e141](https://github.com/choiceoh/Deneb/commit/521e14179d54520805f849431be70747c6509c45))


### 🔧 Internal

* remove ~6300 lines of unreferenced dead code ([779d5b0](https://github.com/choiceoh/Deneb/commit/779d5b0b2a9b2d02264b44555678540a0988c8e6))
* remove skeleton stub code ([ec3c3c3](https://github.com/choiceoh/Deneb/commit/ec3c3c3acbecf0f166a5cb93d2bbbc4650623edc))
* remove skeleton stub code (browser auth, Vega embed, transcript fallback) ([e9ecaf5](https://github.com/choiceoh/Deneb/commit/e9ecaf552dbfec3829c4e3dd9b7ff294da1c7f12))
* remove unreferenced dead code across Go, Rust, and infra ([b0d186f](https://github.com/choiceoh/Deneb/commit/b0d186f773feb6425ccf44c193979c362ec78459))
* **skills:** move genesis RPC registration to method_registry.go ([e2b0e09](https://github.com/choiceoh/Deneb/commit/e2b0e09e533eb795ea15323a395cfcab85ae82fd))

## [4.13.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.12.0...deneb-v4.13.0) (2026-04-06)


### ✨ Features

* **agent:** adaptive tool concurrency for exec commands ([8120ca3](https://github.com/choiceoh/Deneb/commit/8120ca362fdaa3a63219248b15b2423f3f548431))
* **agent:** add adaptive tool concurrency for exec commands ([edd6905](https://github.com/choiceoh/Deneb/commit/edd6905f2e34fff8710524e2351fbf6d77e9ba35))
* **agent:** add autonomous agent capabilities inspired by OpenClaw ([50bbba5](https://github.com/choiceoh/Deneb/commit/50bbba5761af262474234469bc04b12840190a0e))
* **agent:** add autonomous agent capabilities inspired by OpenClaw ([8c21208](https://github.com/choiceoh/Deneb/commit/8c21208cec5b7306f84e7429b6a80ee4e7e705a2))
* **agent:** add tool loop detection, memory safety, and hook activation ([297a900](https://github.com/choiceoh/Deneb/commit/297a9009daa3e43c020fd38b4f8ea0f5b8cbb7a2))
* **agent:** tool loop detection, memory safety, hook activation ([a408b1e](https://github.com/choiceoh/Deneb/commit/a408b1e47106f85c939991add8a8e1f4b785cfd4))
* **chat:** add OpenClaw-inspired prompt improvements — execution bias, safety, narration, cron, silent reply ([2bff59a](https://github.com/choiceoh/Deneb/commit/2bff59ac73cca7dea833fe487fb7b7a3355eb547))
* **chat:** add SendLite for lightweight system background tasks ([f7d32d2](https://github.com/choiceoh/Deneb/commit/f7d32d2f86ac184933f3b92765158278119e5e90))
* **chat:** add SendLite for lightweight system background tasks ([86dd9c4](https://github.com/choiceoh/Deneb/commit/86dd9c44d863b7528154e62e51e3b818e849c641))
* **chat:** add subagent completion notification with debounced batching ([8cc6912](https://github.com/choiceoh/Deneb/commit/8cc69121ca046d97e596e3eaf9f301c0aeff22fa))
* **chat:** subagent completion notification with debounced batching ([68a80b6](https://github.com/choiceoh/Deneb/commit/68a80b6d85301526322ece13ce3ee86552a11b9b))
* **memory:** add timestamp and length scaling to fact extraction ([1eb42dc](https://github.com/choiceoh/Deneb/commit/1eb42dc7aefa6de50549b8ba22b09afbb7afb421))
* **memory:** add timestamp requirement and length scaling to fact extraction prompt ([d6a6d6e](https://github.com/choiceoh/Deneb/commit/d6a6d6e967c5c8aade7e6acc880f1835ab0f0355))
* **skills:** restructure skills with hermes-agent patterns ([ebabae2](https://github.com/choiceoh/Deneb/commit/ebabae268f23236c7887f3f75998f879ab68a086))
* **skills:** restructure skills with hermes-agent patterns ([038bce1](https://github.com/choiceoh/Deneb/commit/038bce195d5545efa86ab2879f4925821d0242f1))


### 🐛 Bug Fixes

* **chat:** add defensive merge for consecutive same-role messages ([70dc22c](https://github.com/choiceoh/Deneb/commit/70dc22ce67145708145596dd1f7b5fbd4de6e759))
* **chat:** add defensive merge for consecutive same-role messages ([fdacaca](https://github.com/choiceoh/Deneb/commit/fdacacab688553f8009e6bd08ca70711bed76d82))
* **skills:** improve skill_manage patch with line-based fuzzy matching ([166f078](https://github.com/choiceoh/Deneb/commit/166f0783035b6682dfde9540e7d2c8feb980ee86))
* **skills:** improve skill_manage patch with line-based fuzzy matching ([8a61efb](https://github.com/choiceoh/Deneb/commit/8a61efb480630af9add257e3d825873270b0f6a0))
* **telegram:** add code fence safe chunking for HTML splits ([899e1a9](https://github.com/choiceoh/Deneb/commit/899e1a927b87cf7ee2a2615c6e95292eee188b70))
* **telegram:** add code fence safe chunking for HTML splits ([8859e7b](https://github.com/choiceoh/Deneb/commit/8859e7becc1d8f8ef4844eebfbabe3da0c9b524a))


### ⚡ Performance

* **chat:** activate 3-tier prompt cache boundaries ([314fbc6](https://github.com/choiceoh/Deneb/commit/314fbc6b68ed360d28cc2418befae802010c6eca))
* **chat:** activate 3-tier prompt cache boundaries ([fef847c](https://github.com/choiceoh/Deneb/commit/fef847c1b0817a1481669a14b29c1d210d8a894d))
* **chat:** adaptive mid-loop compaction threshold ([e86d38d](https://github.com/choiceoh/Deneb/commit/e86d38df7ab36d4aa69b6a67ab9660269d92702f))
* **chat:** adaptive mid-loop compaction threshold based on message size ([6939dec](https://github.com/choiceoh/Deneb/commit/6939dec856f83222ff6825d25a23854ca0634b56))
* **chat:** head/tail truncation for large tool results ([511c903](https://github.com/choiceoh/Deneb/commit/511c9039b8440046d4143c1ccc2b5047c72ce03b))
* **chat:** head/tail truncation for large tool results ([56c5d2b](https://github.com/choiceoh/Deneb/commit/56c5d2bb743234fc4d80b72fc40010cc98b0cfdc))
* **chat:** scope RunCache invalidation by path ([0759fb0](https://github.com/choiceoh/Deneb/commit/0759fb059149f8c79096cb96547e28067d7d521e))
* **chat:** scope RunCache invalidation to mutated file's directory ([58e3bec](https://github.com/choiceoh/Deneb/commit/58e3bec77bedb9379806d746f12720fa1de5d1b7))


### 🔧 Internal

* **agent:** extract HookCompositor for stream hook fan-out ([05536db](https://github.com/choiceoh/Deneb/commit/05536dbf28a2af5685b694ff9b271417d6641d48))
* **agent:** extract HookCompositor for stream hook fan-out ([23b9128](https://github.com/choiceoh/Deneb/commit/23b912899539d3f7ccde5c7127a0a717c558ba1d))
* **chat:** improve system prompt — Korean core instructions, cache fix, token savings ([f0f05b4](https://github.com/choiceoh/Deneb/commit/f0f05b4409ae3e57b4b259004c499dfd813c0f19))
* **chat:** improve system prompt — Korean core instructions, cache fix, token savings ([07026cb](https://github.com/choiceoh/Deneb/commit/07026cb5785813c15ddd7bebdf6df23039320bd9))
* remove over-engineered subsystems (-11.2K LOC) ([a69006a](https://github.com/choiceoh/Deneb/commit/a69006a2bfd0bb38ec9b911e78151fb284792fe0))
* remove over-engineered subsystems for single-user deployment ([dbb1af0](https://github.com/choiceoh/Deneb/commit/dbb1af0a971a1c423869ce1d82d23bdd9b0f07ad))
* **shadow:** remove shadow session monitoring system ([5ccbdd1](https://github.com/choiceoh/Deneb/commit/5ccbdd156ceb9345997d7420c93b50ddc04153d1))
* **shadow:** remove shadow session monitoring system ([798ad01](https://github.com/choiceoh/Deneb/commit/798ad01012e481bd031737d4a18d3456de1a4cfc))

## [4.12.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.11.0...deneb-v4.12.0) (2026-04-06)


### ✨ Features

* **chat:** add context usage and compaction status to /status ([5a0c5b1](https://github.com/choiceoh/Deneb/commit/5a0c5b19fe125121d407800e7c581595ae2d92ae))
* **chat:** add context usage and compaction status to /status slash command ([42e822d](https://github.com/choiceoh/Deneb/commit/42e822db127bc8768ac0f05c953a3c343b8bf11e))
* **chat:** show live token budget usage in /status alongside Aurora ([4f0cd8a](https://github.com/choiceoh/Deneb/commit/4f0cd8a034be249c67b30fdbc01684707f7436ce))
* **cron:** enhance cron tool with cron expression, persistent storage, and run history ([3af70ec](https://github.com/choiceoh/Deneb/commit/3af70ec33b7a0a3d9dc2a62ce686640654b3c03a))
* **cron:** 크론 도구 기능 강화 ([6219541](https://github.com/choiceoh/Deneb/commit/621954165319439f367c70c28c298a85bdd1c3fa))
* **gateway:** add observability metrics and WAL session persistence ([6ca1600](https://github.com/choiceoh/Deneb/commit/6ca16009eed77d67ea17246065d221fdfba82327))
* **gateway:** add observability metrics and WAL-based session persistence ([3cb6aab](https://github.com/choiceoh/Deneb/commit/3cb6aab8a594dbd053c7fc79b3c92fafec2ee602))
* **memory:** raise similarity threshold for session memory injection ([6e608be](https://github.com/choiceoh/Deneb/commit/6e608be307640745010e55dd7c262516ea448e01))
* **memory:** raise similarity threshold for session memory injection ([a2f07c6](https://github.com/choiceoh/Deneb/commit/a2f07c6c677d67930f274fe8789b5fa36c84e2f6))


### 🐛 Bug Fixes

* **chat:** stop emitting [tool_name] brackets in session memory transcripts ([eb615e7](https://github.com/choiceoh/Deneb/commit/eb615e7fe0075dd4d8a5b153e90e1ba7d80f596f))
* **chat:** stop emitting [tool_name] brackets in session memory transcripts ([7df7663](https://github.com/choiceoh/Deneb/commit/7df7663deb4ef68d2c2bfc4ca6f7cf6b8be4fee7))

## [4.11.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.10.0...deneb-v4.11.0) (2026-04-06)


### ✨ Features

* **chat:** enable continue_run in normal mode ([e1d4d03](https://github.com/choiceoh/Deneb/commit/e1d4d03d430063dcf5043b520a090f9c93de0b26))
* **chat:** enable continue_run in normal mode ([3082889](https://github.com/choiceoh/Deneb/commit/308288915a1592385f47838ef6eb086a52ba04c7))


### 🐛 Bug Fixes

* **build:** add cuda build tag for reliable CUDA linking ([fa460e7](https://github.com/choiceoh/Deneb/commit/fa460e76666ab5084247ed66e7c4ea95b7fbf228))
* **chat:** make cron tool eager instead of deferred ([f7a1946](https://github.com/choiceoh/Deneb/commit/f7a1946a5f6d3086b329e7cba63afbcf895286e3))
* **chat:** make cron tool eager instead of deferred ([c619f4d](https://github.com/choiceoh/Deneb/commit/c619f4d3340566336aecf3939fbc041049affd9a))
* **cron:** prevent duplicate execution and stale job data ([07208cc](https://github.com/choiceoh/Deneb/commit/07208cc2cc880b595dbf034e2cb4ad4393c0803a))
* **cron:** prevent duplicate execution and stale job data ([1b24764](https://github.com/choiceoh/Deneb/commit/1b247647d17927f3cd2e38c1a7940b4b61daf10d))

## [4.10.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.9.0...deneb-v4.10.0) (2026-04-06)


### ✨ Features

* **agent:** add streaming idle watchdog to detect LLM stream stalls ([16cf982](https://github.com/choiceoh/Deneb/commit/16cf9829bb126d80c50ae0ce6dcc18a0670fba96))
* **agent:** add streaming idle watchdog to detect LLM stream stalls ([6156ef6](https://github.com/choiceoh/Deneb/commit/6156ef687a5bcf82a004fa56783f985518f941a4))
* **aurora:** add compaction benchmark for autoresearch ([f549d10](https://github.com/choiceoh/Deneb/commit/f549d1087210bcc0545804b9a6eade53d2f43c20))
* **aurora:** add compaction benchmark for autoresearch optimization ([cf2c988](https://github.com/choiceoh/Deneb/commit/cf2c988844a328f660fc0ce58c89e113a07eb19b))
* **aurora:** sync rich content blocks to Aurora for compaction timeline ([6c61463](https://github.com/choiceoh/Deneb/commit/6c6146359e627f1d717107e1c6d648c2bacdfbf6))
* **autoresearch:** add cache_enabled option for expensive experiment operations ([f229dba](https://github.com/choiceoh/Deneb/commit/f229dbaa07c80b844739636c51e352e5f657ecca))
* **autoresearch:** add cache_enabled option for expensive experiment operations ([ce57671](https://github.com/choiceoh/Deneb/commit/ce57671bf9981bc534918af4233fb21f335a31ff))
* **autoresearch:** add dedup, windowing, resume, metric extraction, and archival ([8aad86b](https://github.com/choiceoh/Deneb/commit/8aad86b735c1883c1855d2ccac0785cd0d10af5a))
* **autoresearch:** add metric caching, persistent server, and parallel experiments ([a8b6f64](https://github.com/choiceoh/Deneb/commit/a8b6f64850956d45c0b493b9e17657e9007302b4))
* **autoresearch:** add RPC handler package for direct runner control ([aa6ae31](https://github.com/choiceoh/Deneb/commit/aa6ae3126adb4c0d8371ea55d3884bbf0d8fad79))
* **autoresearch:** add RPC handler package for direct runner control ([85cd1d4](https://github.com/choiceoh/Deneb/commit/85cd1d4a05e72bbb0ec3b911b13626c65fa19b07))
* **autoresearch:** add Status RPC + MCP tools + handler registration ([1072ff9](https://github.com/choiceoh/Deneb/commit/1072ff91257ffaf81de75da106fcab7d6149ca75))
* **autoresearch:** add Status RPC + MCP tools + handler registration ([01e6cb6](https://github.com/choiceoh/Deneb/commit/01e6cb6773a2199371ad134901592bc892a096df))
* **autoresearch:** auto-generate patterns and add fallback matching ([98bebf0](https://github.com/choiceoh/Deneb/commit/98bebf0f18addb3e367058842fc9ed7e914296a1))
* **autoresearch:** auto-generate patterns and add fallback matching for constants ([b7668be](https://github.com/choiceoh/Deneb/commit/b7668be84067c5fc43032747b8b343ecfe8f7ef5))
* **autoresearch:** auto-stop after max iterations and send completion report ([f67cc14](https://github.com/choiceoh/Deneb/commit/f67cc14d4ecee37c8f11d13769354f29f7cd8b10))
* **autoresearch:** auto-stop after max iterations and send completion report ([f2ed359](https://github.com/choiceoh/Deneb/commit/f2ed359ca01e060807037ea8cb6d66964e8e969f))
* **autoresearch:** expose runner as RPC methods + MCP tools ([0fc3e43](https://github.com/choiceoh/Deneb/commit/0fc3e432266992457bd0d3b355583fdbe560ba7c))
* **autoresearch:** increase default time budget to 60 minutes ([344abb3](https://github.com/choiceoh/Deneb/commit/344abb32669b8ccf4378457951062dda340adb71))
* **autoresearch:** metric caching, persistent server, parallel experiments ([7d4c033](https://github.com/choiceoh/Deneb/commit/7d4c033019882e4b48dfee7de1ac977476a51d7b))
* **autoresearch:** use lightweight local model instead of external API ([52d58f5](https://github.com/choiceoh/Deneb/commit/52d58f55f1cc80235f830529eb30ee0d467c9ae2))
* **autoresearch:** 오토리서치 인프라 강화 ([b6c75d3](https://github.com/choiceoh/Deneb/commit/b6c75d34c277b0c5289369f3b172ca1c94272834))
* **banner:** show embedder and reranker status in startup banner ([418ac3a](https://github.com/choiceoh/Deneb/commit/418ac3a3dc9aabb728cc61cedcf9fa4eac3c521e))
* **banner:** show embedder and reranker status in startup banner ([0f7c96f](https://github.com/choiceoh/Deneb/commit/0f7c96f4b15395428d686f064f21ccd2ecd32b9b))
* **bridge:** add bridge agent tool for Deneb → Claude Code ([485a550](https://github.com/choiceoh/Deneb/commit/485a55035e80147374a7dbb53f5b81ea96eb5b99))
* **bridge:** add bridge agent tool for Deneb → Claude Code ([a09fca4](https://github.com/choiceoh/Deneb/commit/a09fca4c172d22d9b755ce77714b59a7f6b4fa39))
* **bridge:** add scripts/bridge for quick inter-agent messaging ([196a454](https://github.com/choiceoh/Deneb/commit/196a454f991dba5be65d287c56e04c979e6e2abe))
* **bridge:** add scripts/bridge for quick messaging ([94b66ca](https://github.com/choiceoh/Deneb/commit/94b66cafd1c77cfba5b82dc05018bbc84eefac9e))
* **bridge:** auto-notify Deneb on commit and PR via hook ([b30c8d8](https://github.com/choiceoh/Deneb/commit/b30c8d87d8006f5359c4798f12d000b97cee4bf2))
* **bridge:** auto-notify Deneb on commit/PR via hook ([1820058](https://github.com/choiceoh/Deneb/commit/1820058535a98a077e0d30cc5eb43ac313d628b7))
* **bridge:** auto-trigger LLM run on bridge message ([93266f8](https://github.com/choiceoh/Deneb/commit/93266f81cb0a4a460219927351ca7b20e300fc6b))
* **bridge:** auto-trigger LLM run on bridge message ([1f2cb37](https://github.com/choiceoh/Deneb/commit/1f2cb37d41e4e397be823b9eca343cf15dd27629))
* **bridge:** inject bridge messages into active session context ([45de9fb](https://github.com/choiceoh/Deneb/commit/45de9fbfa1ba88428a15ea31d38f82e7c9945753))
* **build:** parallelize make check, add check/fast ([15ef111](https://github.com/choiceoh/Deneb/commit/15ef1117fb7487f79a02f3378a37e850b65a20c2))
* **build:** parallelize make check, add make check/fast for pre-commit ([e363add](https://github.com/choiceoh/Deneb/commit/e363add3aa8a20ce721cd6b5a22bbc7c1468ca2b))
* **chat:** add /mode slash command for conversation mode ([571e63c](https://github.com/choiceoh/Deneb/commit/571e63c0c0538e07fac5cd6202755ef97ea21d6e))
* **chat:** add /mode slash command to toggle conversation mode ([fa0f5d9](https://github.com/choiceoh/Deneb/commit/fa0f5d9a771e49a6fba613edd722282f32fd4886))
* **chat:** add ConcurrencySafe field to ToolDef for declarative parallel tool execution ([d3f617b](https://github.com/choiceoh/Deneb/commit/d3f617b11768d801eaf553bd93a7aeab6773a51e))
* **chat:** add deep work mode for 2-3 hour autonomous agent sessions ([26c1018](https://github.com/choiceoh/Deneb/commit/26c10188aaa4059c3a3b7a92f3df784936884bef))
* **chat:** add deep work mode for 2-3 hour autonomous sessions ([75f9841](https://github.com/choiceoh/Deneb/commit/75f9841e4f95dae09e441f0f748dffe4fd27e0bc))
* **chat:** add file staleness detection to prevent stale overwrites ([0a67361](https://github.com/choiceoh/Deneb/commit/0a6736143fea021e95b834cebf40a5862dadcbb2))
* **chat:** add file staleness detection to prevent stale overwrites ([fec68ec](https://github.com/choiceoh/Deneb/commit/fec68ec4368570b348583dcdb22bdfa5111c0359))
* **chat:** add mid-loop compaction to prevent context exhaustion ([a3b6519](https://github.com/choiceoh/Deneb/commit/a3b65196b7b72b0c755670fb4c5e51cde730dc19))
* **chat:** add mid-loop compaction to prevent context exhaustion during agent runs ([dc2107c](https://github.com/choiceoh/Deneb/commit/dc2107c3689919e78a891834c08ec62a4dbe87c6))
* **chat:** add progress narration instructions to system prompt ([697807e](https://github.com/choiceoh/Deneb/commit/697807ecb49f2cb969e6e3950975bed4a74d7a62))
* **chat:** auto-scale max output tokens on truncation recovery ([a9d862d](https://github.com/choiceoh/Deneb/commit/a9d862d9fc2b7046927a1bbff23d00b029e17bf7))
* **chat:** auto-scale max output tokens on truncation recovery ([e1d201e](https://github.com/choiceoh/Deneb/commit/e1d201e55d4a620b2f5df8ef88c746f6fe00b4ed))
* **chat:** declarative ConcurrencySafe field for parallel tool execution ([ed443b1](https://github.com/choiceoh/Deneb/commit/ed443b1dbdfdcd93cd031c4d2b9521c934afc3af))
* **chat:** enrich /status command with full gateway and session details ([16f72bc](https://github.com/choiceoh/Deneb/commit/16f72bc158d1d18308a68ba9f4f2f78902c92a46))
* **chat:** enrich /status with full gateway/session dashboard ([f7513cd](https://github.com/choiceoh/Deneb/commit/f7513cd5fd49e6bb53f2c4e9a01983b63caebbb4))
* **chat:** fix autonomous continuation — timeout recovery, auto-verify, turn awareness ([abdf767](https://github.com/choiceoh/Deneb/commit/abdf7679cd9eca7efe1e6ba426a906a99f325e2f))
* **chat:** fix autonomous continuation — timeout recovery, auto-verify, turn awareness ([88f7d7e](https://github.com/choiceoh/Deneb/commit/88f7d7e6880e38dd4a2f26e8a59ef9cd2da052e2))
* **chat:** implement deferred tools for token-efficient tool loading ([9685889](https://github.com/choiceoh/Deneb/commit/9685889244151afd0f8dd31150628452622566dd))
* **chat:** implement deferred tools for token-efficient tool loading ([d559e23](https://github.com/choiceoh/Deneb/commit/d559e238f1acc2e175a7e855b61c58b578e20879))
* **chat:** improve episodic memory and session memory redesign ([6c20e65](https://github.com/choiceoh/Deneb/commit/6c20e655bde7ca1e5a6603ac154a0b6dd057f088))
* **chat:** improve episodic memory via compaction timeline and session memory redesign ([1708f14](https://github.com/choiceoh/Deneb/commit/1708f14dc8cae857f8a2a9bac29517e64b09cbb6))
* **chat:** increase nudge continuation limits ([e7cddcb](https://github.com/choiceoh/Deneb/commit/e7cddcb18ab7b6044f3bef6bda9973b0bbb1e730))
* **chat:** increase nudge continuation limits for longer autonomous runs ([e311fb6](https://github.com/choiceoh/Deneb/commit/e311fb699f68919c5be716c3f118d3789e4f208d))
* **chat:** inject conversation mode awareness into system prompt ([c657783](https://github.com/choiceoh/Deneb/commit/c657783c6b4ba3e7dd6d124077aa52bf44353a4f))
* **chat:** inject conversation mode awareness into system prompt ([779adcd](https://github.com/choiceoh/Deneb/commit/779adcdcb64055d678de85c165db62f027510798))
* **chat:** inject subagent and autoresearch results into parent session context ([c9612eb](https://github.com/choiceoh/Deneb/commit/c9612eb08b9f50e2411418548366c4bab537cdc9))
* **chat:** inject subagent/autoresearch results into parent context ([134fe1c](https://github.com/choiceoh/Deneb/commit/134fe1c10ad19214976ac346f1cde1b9b91835e4))
* **chat:** persist rich messages per-turn for agent short-term memory ([458d9a0](https://github.com/choiceoh/Deneb/commit/458d9a0f85a1ead9ffbab98b183dbd534fa7b5aa))
* **chat:** persist rich messages per-turn for short-term memory ([5f30d86](https://github.com/choiceoh/Deneb/commit/5f30d863b5a00e535c0798a9e8dae1dd26f248e1))
* **chat:** relax tool limits for single-user DGX environment ([6247dbc](https://github.com/choiceoh/Deneb/commit/6247dbc576f42f85ecca90fcdc9054681599be49))
* **chat:** relax tool limits for single-user DGX environment ([e932879](https://github.com/choiceoh/Deneb/commit/e93287944dccf077f6f3221742a29bf9b9baaa89))
* **devenv:** expand permissions, add pre-commit gate hook and GitHub MCP ([510ec09](https://github.com/choiceoh/Deneb/commit/510ec09304217ee8f792b3ce44755400b00c87f2))
* **devenv:** expand permissions, add pre-commit gate hook and GitHub MCP ([2c85493](https://github.com/choiceoh/Deneb/commit/2c85493523b56c886b49a13c4a6ec2aa33f96a4c))
* **devenv:** expand permissions, add pre-commit gate hook and GitHub MCP ([b715edd](https://github.com/choiceoh/Deneb/commit/b715edde534db8c332eba57bd6e5c185b63e1884))
* **devtools:** add live testing, quality metrics, and optimization loop ([2dad1bd](https://github.com/choiceoh/Deneb/commit/2dad1bd7a8bf89930e770fded6d7d4b880d73169))
* **devtools:** add live testing, quality metrics, and optimization loop ([1d60f07](https://github.com/choiceoh/Deneb/commit/1d60f076e91fb574fd9132826acfaaebd3d2ef11))
* **gateway:** switch lightweight LLM from sglang to vllm and add /mail command ([13d52ac](https://github.com/choiceoh/Deneb/commit/13d52acf3ab4e2cd186c55e5140b0d3c7cbc563f))
* **gateway:** switch lightweight LLM to vllm + add /mail command ([7e483e8](https://github.com/choiceoh/Deneb/commit/7e483e8c1d803aa1ee6ec28d1d80c545dd315377))
* **live-test:** add reproduction commands for user symptom verification ([3339825](https://github.com/choiceoh/Deneb/commit/3339825521e44e4091bf43ea7ef4e0d5d8465ead))
* **live-test:** add reproduction commands for user symptom verification ([109ceb8](https://github.com/choiceoh/Deneb/commit/109ceb84e2e9a737b0d7dc2ec922870f6df61b62))
* **mcp:** add inter-agent bridge for Claude Code ↔ Deneb communication ([25016b2](https://github.com/choiceoh/Deneb/commit/25016b2fdc51c88018ac21c44b8870c258e4653f))
* **mcp:** inter-agent bridge for Claude Code ↔ Deneb ([0589435](https://github.com/choiceoh/Deneb/commit/0589435cfb08723ec091e74e984c6c05218e9f48))
* **memory:** add cluster-based semantic consolidation to dreaming ([bca4b37](https://github.com/choiceoh/Deneb/commit/bca4b379b4a696cb0000928aa931e8a3ddaf4371))
* **memory:** add consolidate phase + increase dreaming timeout ([146f553](https://github.com/choiceoh/Deneb/commit/146f553d8d01235786bc50cb577ae901d982e940))
* **memory:** add DecayPower param for autoresearch ([2e1d002](https://github.com/choiceoh/Deneb/commit/2e1d0022144a1d4021b6b2b8d41b618a4fff0ded))
* **memory:** add DecayPower param for autoresearch ([787ce76](https://github.com/choiceoh/Deneb/commit/787ce7605c3e421e519a294254c091273f43a42d))
* **memory:** add dream-once CLI for one-shot dreaming cycles ([3632339](https://github.com/choiceoh/Deneb/commit/36323397d25de08a9eb336818352da540eec5b8d))
* **memory:** add dream-once CLI for one-shot dreaming cycles ([f9bf134](https://github.com/choiceoh/Deneb/commit/f9bf134d0cda33acaba2f67345f8dd8d1770d630))
* **memory:** expand BGE-M3 embedding to dedup, dreaming, and context assembly ([1a6c1dc](https://github.com/choiceoh/Deneb/commit/1a6c1dcdfd41c8ae4fd05785d3aebbb791b6fd64))
* **memory:** expand BGE-M3 embedding to dedup, dreaming, context ([ade39be](https://github.com/choiceoh/Deneb/commit/ade39be9f6b575c8277fc5f1cea87af51c635e2c))
* **memory:** implement consolidate phase for dreaming ([1d2739a](https://github.com/choiceoh/Deneb/commit/1d2739a19edff101b4bab5e8c7f03c42a1abc0bb))
* **memory:** implement consolidate phase for dreaming ([0f7825a](https://github.com/choiceoh/Deneb/commit/0f7825adb6a59251959ad15ef686e4411bf3909c))
* **memory:** integrate query expansion into memory FTS search ([740b16c](https://github.com/choiceoh/Deneb/commit/740b16c8391476de88c34717aece035ac9460eff))
* **memory:** integrate Rust keyword extraction and Vega LLM expansion into memory FTS search ([ea44c03](https://github.com/choiceoh/Deneb/commit/ea44c0367cd3084c92a4b1773ea96538e090ee0d))
* **ml:** add local GGUF embedding via llama.cpp ([bc90ea9](https://github.com/choiceoh/Deneb/commit/bc90ea987fb85d2da0e0afd0f0dcd75b06aeab5d))
* **ml:** add local GGUF embedding via llama.cpp ([57e37b1](https://github.com/choiceoh/Deneb/commit/57e37b102ff680683b706d016c3b162d2ae79b8b))
* **parsing:** improve parsing module across 7 areas ([1cd4f3e](https://github.com/choiceoh/Deneb/commit/1cd4f3ecf3427b9f8d8d390d125f261159a31348))
* **parsing:** improve parsing module across 7 areas ([65d31bf](https://github.com/choiceoh/Deneb/commit/65d31bf27c9722e670f3a1bf4e9a50f3b71de3a7))
* **server:** add Claudeneb — Anthropic Messages API proxy for Claude Desktop ([7ccced9](https://github.com/choiceoh/Deneb/commit/7ccced983ddaf31df128313ce8994caa85413610))
* **server:** add Claudeneb — Claude Desktop + Deneb integration ([bc8c699](https://github.com/choiceoh/Deneb/commit/bc8c6994fde1081fd7d2cecb6ea8185730eb0e8b))
* **session:** add 3-mode system (normal/chat/work) ([1e601d0](https://github.com/choiceoh/Deneb/commit/1e601d07a5808ee1cf0bad998e9bb9f864479223))
* **session:** add 3-mode system (normal/chat/work) ([2321041](https://github.com/choiceoh/Deneb/commit/2321041653ab0b72a46042a82ae2d389a6356acd))
* **sglang:** add centralized hub for token budget, priority queue, and cache ([2190611](https://github.com/choiceoh/Deneb/commit/2190611276e0a39711b218873211fa848804e39a))
* **sglang:** block CJK tokens to prevent Chinese output ([c6a3baa](https://github.com/choiceoh/Deneb/commit/c6a3baa21648360224cbc3b785ad8ee4dfad004f))
* **sglang:** block CJK tokens to prevent Chinese output from local model ([8024788](https://github.com/choiceoh/Deneb/commit/8024788ac571ddde89035a8505e99353c34569aa))
* **sglang:** centralized hub for token budget, priority queue, and cache ([42d3193](https://github.com/choiceoh/Deneb/commit/42d31930ad12241ebe5efc4532cf3a2bbeb4c648))
* **skills:** port Claude Code command patterns to skills system ([db83d91](https://github.com/choiceoh/Deneb/commit/db83d91df4c7baa0a5e3696c376b23db7fa413fd))
* **skills:** port Claude Code command patterns to skills system ([e3f93d6](https://github.com/choiceoh/Deneb/commit/e3f93d6b59fbaf959b01b941a67c82725f22fa5a))
* **telegram:** add /morning slash command for direct morning letter dispatch ([3c4f303](https://github.com/choiceoh/Deneb/commit/3c4f303ec4666a75733b7486fdab5e0f27fe3789))
* **telegram:** add /morning slash command for morning letter ([fe0ac6e](https://github.com/choiceoh/Deneb/commit/fe0ac6e87fd70fa44f5a5acc5c72f82e32353dbb))
* **telegram:** add virtual Telegram testing (vchat) ([8c63553](https://github.com/choiceoh/Deneb/commit/8c6355373e79673abcb2a640b515a8ee031f9e44))
* **telegram:** add virtual Telegram testing (vchat) ([5b707a6](https://github.com/choiceoh/Deneb/commit/5b707a6014fcf6d74bf5b33219c554f8fc6be7d2))
* **telegram:** show tool arguments and elapsed time in progress tracker ([146ecaa](https://github.com/choiceoh/Deneb/commit/146ecaa614697ffefb3bd998420ce086ff362488))
* **telegram:** show tool arguments and elapsed time in progress tracker ([e4c1fce](https://github.com/choiceoh/Deneb/commit/e4c1fceb63735d93cf7aa8344d14c71d70e58a11))
* **test:** add 300 data-driven quality test cases ([b4ced50](https://github.com/choiceoh/Deneb/commit/b4ced50b7f990689800eb3e5021429c25f18d2f1))
* **test:** add 300 data-driven quality test cases with YAML-driven runner ([bf9df15](https://github.com/choiceoh/Deneb/commit/bf9df15e2c244117a5d34c588e801a0497bc14bd))
* **test:** add 31 compaction tests and ko+en language check ([75d0602](https://github.com/choiceoh/Deneb/commit/75d06029bd83e577411e24a9bc588e7728d418e5))
* **test:** add 6 user-specific quality tests for gmail, calendar, morning letter ([e529066](https://github.com/choiceoh/Deneb/commit/e5290662addb9a126acbc1b47147fc9548af167c))
* **test:** add deep tool and edge case quality test scenarios ([5648cab](https://github.com/choiceoh/Deneb/commit/5648cab2c08deb3a8935af40102e889204204b9d))
* **test:** add deep tool and edge case quality test scenarios ([92ec0b9](https://github.com/choiceoh/Deneb/commit/92ec0b92ed4023440c539ce78fcfb110fc74d550))
* **test:** add persistent quality test result recording ([2e15cf9](https://github.com/choiceoh/Deneb/commit/2e15cf9db9d556f03850ba93105f352b0f74cc80))
* **test:** add persistent quality test result recording with SQLite ([4e4b1d0](https://github.com/choiceoh/Deneb/commit/4e4b1d0eb0f9b06ec3a2f564c1e0f092c67ae36d))
* **testing:** add compaction live test with chat.inject ([297fd06](https://github.com/choiceoh/Deneb/commit/297fd062d07d58b93ea57a18270e4a467949ad20))
* **testing:** add compaction live test with chat.inject ([4666925](https://github.com/choiceoh/Deneb/commit/466692579880bacf770a57b85514fb2c087da1ef))
* **testing:** add prod-parity mode for dev/test environment fidelity ([0e1ff0c](https://github.com/choiceoh/Deneb/commit/0e1ff0c62543390b5b13489e49e9b529afefdc02))
* **testing:** add prod-parity mode for dev/test environment fidelity ([73fdc1c](https://github.com/choiceoh/Deneb/commit/73fdc1c267ca964f00dbb1c9bed38b1df8bccf12))
* **testing:** enhance live testing for AI agents ([669044c](https://github.com/choiceoh/Deneb/commit/669044cef2de6d160423d018009162e428fe2ad9))
* **testing:** enhance live testing for AI agents ([d96ff1a](https://github.com/choiceoh/Deneb/commit/d96ff1a497941a208d41863b3a0d4f56763a3805))
* **vega:** migrate reranker from Jina API to local jina-reranker-v3 ([7128437](https://github.com/choiceoh/Deneb/commit/712843732ea4f259f0608eebaf23b23240b07efa))
* **vega:** migrate reranker to local jina-reranker-v3 ([344791a](https://github.com/choiceoh/Deneb/commit/344791a8af6d6ed4b2dcba78f73cdcce1aced211))
* **web:** improve web tool usability for AI agent ([5408d3b](https://github.com/choiceoh/Deneb/commit/5408d3b9d89510b5146b433cf54845daac85ff60))
* **web:** improve web tool usability for AI agent ([b5139cd](https://github.com/choiceoh/Deneb/commit/b5139cdd26be150e6bb279cf10928c32d6fcdcd7))


### 🐛 Bug Fixes

* **agent:** add panic recovery to tool executor and improve quality tests ([0bf5894](https://github.com/choiceoh/Deneb/commit/0bf589411fbf1e93a8d367a36c3e7683fe732038))
* **agent:** add panic recovery to tool executor and update concurrency list ([90beab7](https://github.com/choiceoh/Deneb/commit/90beab7c3f594fe86103f618193a416342d1e0f0))
* **autoresearch:** double chart resolution for Telegram legibility ([a7a969a](https://github.com/choiceoh/Deneb/commit/a7a969a63780f55a1078e1ad42eb8743550ce299))
* **autoresearch:** double chart resolution for Telegram legibility ([c49c2ac](https://github.com/choiceoh/Deneb/commit/c49c2ac895477f6d25949849274c0ba217a21e3c))
* **autoresearch:** fd leak, delta reset, multi-match warning ([482348a](https://github.com/choiceoh/Deneb/commit/482348a0d521a03b4895cf68c216794313156612))
* **autoresearch:** fix state corruption and metric extraction bugs ([01bdce8](https://github.com/choiceoh/Deneb/commit/01bdce8a6698c38604a5e30759d70ed322497739))
* **autoresearch:** increase chart resolution for Telegram legibility ([7c84186](https://github.com/choiceoh/Deneb/commit/7c84186c2180f54cce85c24b57ef0878c2583aa0))
* **autoresearch:** run experiments in isolated git worktree ([3302aa8](https://github.com/choiceoh/Deneb/commit/3302aa88dca6ccb425383a1827cc2a9ec5777e46))
* **autoresearch:** run experiments in isolated git worktree to prevent pull conflicts ([a75017c](https://github.com/choiceoh/Deneb/commit/a75017cb5369d9b295141c3a88ee4beb171e347d))
* **bridge:** deduplicate by targeting single Telegram session ([1edf058](https://github.com/choiceoh/Deneb/commit/1edf05849843d43a6ce8bbdb5d570be1821f2e8a))
* **bridge:** derive delivery context for Telegram response ([292dd04](https://github.com/choiceoh/Deneb/commit/292dd049140d472351f9e415b047da5aa5c9ccb0))
* **bridge:** derive delivery context from session key ([b3b0fed](https://github.com/choiceoh/Deneb/commit/b3b0fed46146d0f33d4d2e1f9060e795b50f1ed2))
* **bridge:** fix source default and WS reconnection ([6cf6639](https://github.com/choiceoh/Deneb/commit/6cf6639e7c35e8f24ef52b7cccfdc19bfcd5b905))
* **bridge:** fix source default and WS reconnection ([58eb774](https://github.com/choiceoh/Deneb/commit/58eb774bff986fe3bbb63d37eab3154b1c0a5de7))
* **bridge:** inject into telegram direct session, not shadow ([044eba0](https://github.com/choiceoh/Deneb/commit/044eba0cc05c2121eae717d5e5cdb4afbc137432))
* **bridge:** inject into telegram direct session, not shadow ([cc36e70](https://github.com/choiceoh/Deneb/commit/cc36e70e37befecf782463101addbac7b08a2af1))
* **bridge:** remove background curl in hook (exit kills it) ([e3d0a26](https://github.com/choiceoh/Deneb/commit/e3d0a260e3f1a5fb05e6b43bf07ad14ffe760076))
* **bridge:** remove background curl in notify hook ([8a4dcf8](https://github.com/choiceoh/Deneb/commit/8a4dcf81c8221da4887ea200841e5daeb2a6f442))
* **bridge:** rename prompt section + add tool usage instructions ([475a387](https://github.com/choiceoh/Deneb/commit/475a387599bd3a951a4e1498b6485a49a9f0fdac))
* **bridge:** send to single most-recent Telegram session ([cafd3b6](https://github.com/choiceoh/Deneb/commit/cafd3b67e827ed37aad2a87df11159c5c552ea43))
* **bridge:** strengthen bridge agent prompt for reliable recognition ([eb138ec](https://github.com/choiceoh/Deneb/commit/eb138ec68c95d6ac2ab97361930a32f644ce6e58))
* **bridge:** sync injected messages to Aurora store ([33d68cf](https://github.com/choiceoh/Deneb/commit/33d68cf9924a3d39db387f8390e6771e7741283b))
* **bridge:** sync to Aurora store + strengthen prompt ([0e9587c](https://github.com/choiceoh/Deneb/commit/0e9587c47d8064fb8ceedd18927147be33452819))
* **bridge:** use user role for injected messages ([df67c5d](https://github.com/choiceoh/Deneb/commit/df67c5dbff7324546741c56925187ed28ea665ef))
* **bridge:** use user role for injected messages ([eb7e3ae](https://github.com/choiceoh/Deneb/commit/eb7e3ae9ae9bfbd37ce0dc917376b2a883fc0bae))
* **chat:** add warn logs for silent no-response delivery paths ([0869888](https://github.com/choiceoh/Deneb/commit/0869888c4342e17a2c5db978623b453e5cffe54a))
* **chat:** apply TruncateForLLM to unbounded tools, log silent errors, expand cache, parallelize search_and_read ([7ab7981](https://github.com/choiceoh/Deneb/commit/7ab7981a9df8d0a4801130ca32ea33b9edae2be4))
* **chat:** disable reasoning mode for all internal sglang calls ([dc16792](https://github.com/choiceoh/Deneb/commit/dc16792e673f1a75dcc76240f818f2cc3dbee932))
* **chat:** disable reasoning mode for all internal sglang calls ([4a6ee7d](https://github.com/choiceoh/Deneb/commit/4a6ee7d8fcf45cca196ec2fc81081583d00fa2e6))
* **chat:** improve LLM stream parsing quality ([4eed706](https://github.com/choiceoh/Deneb/commit/4eed706e11bbc654cf34bb5a980048be793f1d7f))
* **chat:** improve LLM stream parsing quality and error visibility ([a687fe6](https://github.com/choiceoh/Deneb/commit/a687fe612a4f2ce4a5d4b3f949d7193bf5dffbfa))
* **chat:** model switch + silent delivery diagnostics ([c50e98d](https://github.com/choiceoh/Deneb/commit/c50e98decb7f901725128aed04900ff9d3c1d7d9))
* **chat:** persist tool activity summary to prevent agent amnesia ([ce58a29](https://github.com/choiceoh/Deneb/commit/ce58a2955bfdbda65cc1227dc18a7f71ddb9da5b))
* **chat:** persist tool activity summary to prevent agent amnesia ([c853ef2](https://github.com/choiceoh/Deneb/commit/c853ef2602ca5be4886b1835d8cb6bb5c92db10b))
* **chat:** prevent LLM from mimicking tool call syntax ([59177b1](https://github.com/choiceoh/Deneb/commit/59177b12fae81bef2e9070c93a40f524a5948750))
* **chat:** prevent LLM from mimicking tool call syntax in responses ([8105a60](https://github.com/choiceoh/Deneb/commit/8105a609aaf870964e8651719ec902c5acf7bb3b))
* **chat:** prevent parallel runs and use local model for autoresearch ([088b356](https://github.com/choiceoh/Deneb/commit/088b3561011c7c423020e1b1e55b4781d619ec8a))
* **chat:** prevent parallel runs by adding clientRunId to Telegram dispatch ([c7e6677](https://github.com/choiceoh/Deneb/commit/c7e6677b0a616cf2345e2aa0e2499ec1e0eb2bbd))
* **chat:** prevent sglang zombie requests via server-side timeout and unified semaphore ([591f4bd](https://github.com/choiceoh/Deneb/commit/591f4bdd328d3582e088e7129d3aced37682e79b))
* **chat:** prevent sglang zombie requests via server-side timeout and unified semaphore ([96e897b](https://github.com/choiceoh/Deneb/commit/96e897b5dc76d4536ab5fed42cb95c2a8dfeaa93))
* **chat:** remove coding-only system prompt branch for Telegram ([d485bfd](https://github.com/choiceoh/Deneb/commit/d485bfda875fb8b3ceda2c4f42c25407a337e8de))
* **chat:** remove coding-only system prompt, unify for Telegram ([0ec70e5](https://github.com/choiceoh/Deneb/commit/0ec70e5ad3bd633c3bd484d42cc0d46df304e385))
* **chat:** resolve /chart using autoresearch runner workdir ([034fbdb](https://github.com/choiceoh/Deneb/commit/034fbdb9fbe81a43e7bb324585ed89cfd706b068))
* **chat:** resolve /chart using autoresearch runner workdir ([3d33104](https://github.com/choiceoh/Deneb/commit/3d331041350943454ebf77c0264c02bfdb22ffdd))
* **chat:** resolve data races and remove dead fields in chat handler ([36ab8e4](https://github.com/choiceoh/Deneb/commit/36ab8e4c1ef5d9250c3cc41822c02364230a13bb))
* **chat:** resolve LLM client from model registry on provider switch ([de8b618](https://github.com/choiceoh/Deneb/commit/de8b618f3d140c1d21e39d841ec75187d4ae06d7))
* **chat:** restrict Aurora store access to main sessions only ([405a482](https://github.com/choiceoh/Deneb/commit/405a482ffbfdc707fa98db8cc96247de48686fc4))
* **chat:** restrict Aurora store access to main sessions only ([5253952](https://github.com/choiceoh/Deneb/commit/5253952f3f371725660cca5efebe531563dc306d))
* **chat:** strengthen anti-hallucination prompt + clean up consolidate stub ([aeec10f](https://github.com/choiceoh/Deneb/commit/aeec10f8d5f69312940a69acf47c2a819ed651f6))
* **chat:** strip [tool:NAME(ARGS)] leaked markup from replies ([3f1e5f0](https://github.com/choiceoh/Deneb/commit/3f1e5f0cf669d87fe996755f38d7e6150bb624a5))
* **chat:** strip [tool:NAME(ARGS)] leaked markup from replies ([75eb904](https://github.com/choiceoh/Deneb/commit/75eb90422fc150ac50981beae8da30c86647736d))
* **chat:** suppress NO_REPLY leak + quality test improvements ([fb0c82e](https://github.com/choiceoh/Deneb/commit/fb0c82eebe124d125ea4fbeffbf268fbad063eb8))
* **chat:** suppress NO_REPLY token leaking to RPC/WebSocket clients ([f59a4ed](https://github.com/choiceoh/Deneb/commit/f59a4ed9488e4e837559661f7f4d48a16c0a8f3b))
* **chat:** update midLoop compaction comment to match 0.80 threshold ([8c4eec2](https://github.com/choiceoh/Deneb/commit/8c4eec22062190cf3f53c74ce39d95be187c5fe0))
* **cli:** resolve all 59 clippy warnings in deneb-cli ([e7e1626](https://github.com/choiceoh/Deneb/commit/e7e1626343e7ac34bcec3065c3cd690ecb65db15))
* **cli:** resolve all 59 clippy warnings in deneb-cli ([a5bc35d](https://github.com/choiceoh/Deneb/commit/a5bc35dca84182dcea2016672b40ce23ad93328d))
* **cli:** resolve cargo fmt drift and 40 clippy warnings in test code ([735ad11](https://github.com/choiceoh/Deneb/commit/735ad1105d8d6edd0221d28a76dde8b7e2ef5614))
* **cli:** resolve cargo fmt drift and clippy warnings ([3e11b5e](https://github.com/choiceoh/Deneb/commit/3e11b5e92f28827239f2ad317a89588138406502))
* **compact:** add emergency drop, summarizer fallback, and consolidate to 1 test ([a0ad25b](https://github.com/choiceoh/Deneb/commit/a0ad25b80ebe4c842568e11523565cb4064592c5))
* **compact:** emergency drop, summarizer fallback, consolidate tests ([bcc5270](https://github.com/choiceoh/Deneb/commit/bcc5270107b126d21a970fba3e4721cbd8584aff))
* **compact:** improve compaction fact preservation and test filler strategy ([fceec2a](https://github.com/choiceoh/Deneb/commit/fceec2a783ed16e02707bce7a76233cbe82b5ad1))
* **compact:** improve compaction fact preservation and test filler strategy ([50eccb9](https://github.com/choiceoh/Deneb/commit/50eccb9db8ab41778099010251082c032aa64a1e))
* **compact:** increase filler to 15K×7 for reliable compaction trigger ([c811f80](https://github.com/choiceoh/Deneb/commit/c811f809862577d0284b01d643c1ba01afce1d0a))
* **devenv:** remove hardcoded /home/user/Deneb paths ([5a16cf5](https://github.com/choiceoh/Deneb/commit/5a16cf5981bcaa56ea2a17fad5fa55c4936486c2))
* **devenv:** remove hardcoded /home/user/Deneb paths for portability ([625b8a4](https://github.com/choiceoh/Deneb/commit/625b8a407c477a721c402aba5f1533498ff49d7f))
* **dream:** add missing metrics to aurora-dream report and skip no-op cycles ([b3b9371](https://github.com/choiceoh/Deneb/commit/b3b9371979eec1b2fda18ff9e893b65a367ccf54))
* **dream:** add missing metrics to aurora-dream report and skip no-op cycles ([5ad468e](https://github.com/choiceoh/Deneb/commit/5ad468e0a6e169caa1a0546e59b848dc8a092ee8))
* **embedding:** skip LocalEmbedder when ML feature not compiled ([299b82c](https://github.com/choiceoh/Deneb/commit/299b82c9088feef3b9739de9d43699406e72f8aa))
* **embedding:** skip LocalEmbedder when ML feature not compiled ([897a98f](https://github.com/choiceoh/Deneb/commit/897a98ffa47b2840a2c6d1933bf8fa4c9d9e277d))
* **gateway:** check rand.Read errors and strengthen linting config ([3278261](https://github.com/choiceoh/Deneb/commit/32782619d62242932c9fc4fbd2a18818c83cd207))
* **gateway:** check rand.Read errors and strengthen linting config ([db97a4e](https://github.com/choiceoh/Deneb/commit/db97a4e624b161564a08bf6e0a37ba407bc41802))
* **llm:** resolve ZAI_API_KEY for autoresearch, validate relation types ([c6af125](https://github.com/choiceoh/Deneb/commit/c6af125b72e955f023b6cbc1f105f4c63c40bd4f))
* **llm:** resolve ZAI_API_KEY for autoresearch, validate relation types ([1868dd6](https://github.com/choiceoh/Deneb/commit/1868dd6619cb58239120a479c0794b5cd7e23355))
* **localai:** eliminate goroutine leak in requestQueue.PopWait ([7af1c18](https://github.com/choiceoh/Deneb/commit/7af1c18b3d828b31c111f173170fa562a706be45))
* **localai:** eliminate goroutine leak in requestQueue.PopWait ([36f387b](https://github.com/choiceoh/Deneb/commit/36f387b480655b56a801fccfc339a5c176605fa1))
* **mcp:** align MCP tool schemas with actual RPC handler parameters ([217a7c8](https://github.com/choiceoh/Deneb/commit/217a7c80e625ab5c7b41b748f775265cbf4fa252))
* **mcp:** align MCP tool schemas with RPC handlers ([be6fde0](https://github.com/choiceoh/Deneb/commit/be6fde03ab7e3b2880addd7eab2a9dfaad530133))
* **memory:** add missing decay_power to search benchmark params ([b8799f4](https://github.com/choiceoh/Deneb/commit/b8799f41ee90644e3c903e8e9fe8c513dc1f9944))
* **memory:** add missing decay_power to search benchmark params ([0dcb452](https://github.com/choiceoh/Deneb/commit/0dcb452752c4d5382635aac7cd2aa971794a59f0))
* **memory:** allow 2-char English abbreviations as keywords ([87e246c](https://github.com/choiceoh/Deneb/commit/87e246ca1e4b79585a134d94ff8a9a5a5c1087dc))
* **memory:** allow 2-char English abbreviations as keywords ([dce8486](https://github.com/choiceoh/Deneb/commit/dce8486e03dd2707a8d13ca6c7f172fc1c6ef870))
* **memory:** fix SQLite deadlock in benchmark embedder and tune vector search params ([750448d](https://github.com/choiceoh/Deneb/commit/750448dcac0c7e03e6831d117862ae715673f583))
* **memory:** handle double-encoded JSON from guided_json models ([1082500](https://github.com/choiceoh/Deneb/commit/108250022f755b0305c5f8427176b2ffa02270a8))
* **memory:** handle double-encoded JSON from guided_json models ([af42bef](https://github.com/choiceoh/Deneb/commit/af42beff90468035e21822dc71af024d34177539))
* **memory:** increase dreaming maxTokens to prevent JSON parse failures ([5e5ec3e](https://github.com/choiceoh/Deneb/commit/5e5ec3ea1aa2146d56c5a86eb8e506b9b0a1cccc))
* **memory:** increase dreaming maxTokens to prevent JSON parse failures ([a41af0d](https://github.com/choiceoh/Deneb/commit/a41af0d539fc27a63cad1bda1b2a5bb593e65719))
* **memory:** Korean prefix matching + trigram OR search ([15ba15a](https://github.com/choiceoh/Deneb/commit/15ba15ab9f6893a15d9a3b5592c25c969a68b012))
* **memory:** Korean prefix matching + trigram OR search ([6be8c2d](https://github.com/choiceoh/Deneb/commit/6be8c2d585c2872703580510a3763f36890d50ac))
* **memory:** make RecoverTruncated JSON-string-aware ([20ee790](https://github.com/choiceoh/Deneb/commit/20ee79043f7b924891472450b21e51793e73e293))
* **memory:** make RecoverTruncated JSON-string-aware for dreaming batch recovery ([e2b8103](https://github.com/choiceoh/Deneb/commit/e2b810332b38ddf0b6113b436302f5a8f5da1156))
* **memory:** use guided_json for SGLang constrained decoding ([4287f1f](https://github.com/choiceoh/Deneb/commit/4287f1fa74f71d11ea62a1b45535efc936b2d598))
* **memory:** use guided_json for SGLang constrained decoding ([a811830](https://github.com/choiceoh/Deneb/commit/a81183029ce625b0b4aa6bdb6cf8ee971f005458))
* **memory:** use json_schema constrained decoding for fact extraction ([630e208](https://github.com/choiceoh/Deneb/commit/630e208f43ba3ac88f5ceffe545352cf4792104c))
* **memory:** use json_schema constrained decoding for fact extraction ([7af9c66](https://github.com/choiceoh/Deneb/commit/7af9c66bdd9ed4d779249cbfb5557ef800858ae7))
* **ml:** suppress llama.cpp verbose internal logs ([6aee7b4](https://github.com/choiceoh/Deneb/commit/6aee7b496104c110169806f8849ede44fb200b1b))
* **ml:** suppress llama.cpp verbose internal logs ([c810ee1](https://github.com/choiceoh/Deneb/commit/c810ee10bf246a292711b2d382a57db51015aa1d))
* **ml:** suppress llama.cpp verbose stderr logs ([321c390](https://github.com/choiceoh/Deneb/commit/321c3908c4e3e36a9c06f7c707a46f5c8c30833f))
* **ml:** suppress llama.cpp verbose stderr logs during inference ([865427b](https://github.com/choiceoh/Deneb/commit/865427bd2734e510877adc605a1d710e488ea443))
* **modelrole:** correct Z.AI default model name ([8e6b6b7](https://github.com/choiceoh/Deneb/commit/8e6b6b7446f53ea753b875bde96b55dd3b218a93))
* **modelrole:** correct Z.AI default model name from glm-5turbo to glm-5-turbo ([b5f2884](https://github.com/choiceoh/Deneb/commit/b5f28840c8c7034cfcfbfb70ceb645704a0bccd4))
* **modelrole:** restore fallback role to Google Gemini for external API resilience ([519c013](https://github.com/choiceoh/Deneb/commit/519c0138ef2dfc97d5a7262a76af5ff6a50b6e86))
* **process:** swap drain-before-wait order to prevent agent hang ([b7bef81](https://github.com/choiceoh/Deneb/commit/b7bef81207e442a56b118d26a84df15201cc61c6))
* **process:** swap drain-before-wait order to prevent agent hang ([07c8c04](https://github.com/choiceoh/Deneb/commit/07c8c04e1a9824bf5ba9b9a8d92d5c37b73b4bfc))
* **proto:** reserve removed PLUGIN_KIND_CHANNEL enum value, regenerate pb.go ([a7d1fc8](https://github.com/choiceoh/Deneb/commit/a7d1fc8429207b1de0d2595dd5e31b4ea2ff6999))
* **provider:** expand env vars in prewarm API key and base URL ([e1d5986](https://github.com/choiceoh/Deneb/commit/e1d5986074eff98a934712de0c38d0fb656f4caf))
* **provider:** skip prewarm retries on permanent HTTP errors ([fbc9ab5](https://github.com/choiceoh/Deneb/commit/fbc9ab5daa18ccde648d862567cdb052025773bb))
* **provider:** skip prewarm retries on permanent HTTP errors (401, 403) ([79e93d2](https://github.com/choiceoh/Deneb/commit/79e93d2253bb2e101a8477594027629178d01b34))
* **rpc:** propagate context cancellation and improve test coverage ([fb01bdc](https://github.com/choiceoh/Deneb/commit/fb01bdc882230a5a1dd7ae2766a4cef04b3dc51e))
* **rpc:** propagate context cancellation in safeCall and fix test bounds ([1f48c47](https://github.com/choiceoh/Deneb/commit/1f48c47bc7aede12e2141fdc36e1fc94d778484d))
* **scripts:** add missing sessionKey to dev-live-test chat command ([a26d92a](https://github.com/choiceoh/Deneb/commit/a26d92aeec1b364d7ffe6b4bb15ff42bed398561))
* **scripts:** add missing sessionKey to dev-live-test chat command ([85176ae](https://github.com/choiceoh/Deneb/commit/85176ae5868d4b5a26f29ec9012b1d9e6bc95cfa))
* **scripts:** update /home/admin paths to /home/choiceoh ([892dbe7](https://github.com/choiceoh/Deneb/commit/892dbe72d38895e0315de04907a9d1b1e19d2d93))
* **scripts:** update hardcoded /home/admin/deneb paths to /home/choiceoh/deneb ([c903c28](https://github.com/choiceoh/Deneb/commit/c903c285df6434f25f1350075e06a1de9d872d53))
* **server,ffi,session:** harden shutdown, FFI panics, logging, timeouts, and session races ([9808fa2](https://github.com/choiceoh/Deneb/commit/9808fa23b4fc8ef142ee23036396c6c132b0cae6))
* **server,ffi,session:** harden shutdown, FFI panics, logging, timeouts, and session races ([0f84858](https://github.com/choiceoh/Deneb/commit/0f848583f13f6a399d9737c0d6ba8211663d5cd1))
* **server:** improve WebSocket connection stability under GPU load ([269b19c](https://github.com/choiceoh/Deneb/commit/269b19cb647068c7523da17b422f74bb6a396738))
* **server:** improve WebSocket stability under GPU load ([7442eb7](https://github.com/choiceoh/Deneb/commit/7442eb796f74b234ad9cf41f610a4746cf7810b1))
* **server:** resolve sglang.New type mismatch and apply gofmt after rebase ([fc09d53](https://github.com/choiceoh/Deneb/commit/fc09d53a715c8ce668d3c10fdad10926098a400d))
* **server:** suppress websocket log noise from health check probes ([908d00b](https://github.com/choiceoh/Deneb/commit/908d00b9c11c7ca6a74bee1dd1edfa2c81f78064))
* **server:** suppress websocket log noise from health check probes ([24ca5d9](https://github.com/choiceoh/Deneb/commit/24ca5d958426df9375556db2bd809aeef8f2890f))
* **sglang:** increase health check timeout and intervals for thinking models ([26969c0](https://github.com/choiceoh/Deneb/commit/26969c0cfe5740477f1ea730407556ed36b237bf))
* **sglang:** increase health check timeout for thinking models ([bff7959](https://github.com/choiceoh/Deneb/commit/bff79597e31462f87d4f1be727eaeeeecc885933))
* **sglang:** make CJK block opt-in to fix broken importance/session memory ([f064562](https://github.com/choiceoh/Deneb/commit/f064562a92e14f5efb35a7ee51b37a2d13119016))
* **sglang:** make CJK block opt-in to fix broken importance/session memory ([7842508](https://github.com/choiceoh/Deneb/commit/78425080c83150ee7c3fe96f07c1f89c66890d8a))
* **sglang:** remove NoThinking from health check inference probe ([ccc5566](https://github.com/choiceoh/Deneb/commit/ccc55662afbe1f9eaf6de3290dbb3f1c7eae785f))
* **sglang:** remove NoThinking from health check inference probe ([100e8ea](https://github.com/choiceoh/Deneb/commit/100e8ea23036c164bfc2d73ee509b79122faad06))
* **sglang:** replace inference health check with /models ping ([63f5335](https://github.com/choiceoh/Deneb/commit/63f5335385904a17910c28c5434e48e567a616c5))
* **sglang:** replace inference health check with lightweight /models ping ([763496c](https://github.com/choiceoh/Deneb/commit/763496cfb1831c619bd2b5f1153f2e128fc1da07))
* **sglang:** retry on Atlas tokenizer UTF-8 panic ([95feea1](https://github.com/choiceoh/Deneb/commit/95feea1433b208bdea07fd96b01ae22b9a48a7aa))
* **sglang:** retry with user-message padding on Atlas tokenizer panic ([6fe51c3](https://github.com/choiceoh/Deneb/commit/6fe51c33dd330bf5961211e1d1a4ab4b5eac3bd6))
* **sglang:** skip proactive context immediately when sglang is down ([9dd7d37](https://github.com/choiceoh/Deneb/commit/9dd7d377714891f963739869223d34992b8f0665))
* **sglang:** switch health probe to non-streaming Complete ([70333aa](https://github.com/choiceoh/Deneb/commit/70333aafa04114ca8ac134610e6f815e15b3b38d))
* **sglang:** switch health probe to non-streaming for thinking models ([5061a02](https://github.com/choiceoh/Deneb/commit/5061a02028344e1534667fffe553b845d1094aa4))
* **telegram:** improve progress status summary for purpose-level context ([7101713](https://github.com/choiceoh/Deneb/commit/7101713b89dde3645d52e8e37dd0ab7ea8266f8e))
* **telegram:** improve progress status summary prompt for purpose-level context ([1b47d54](https://github.com/choiceoh/Deneb/commit/1b47d54fa2b76f805c8c33a8e6f6190bae18f1fc))
* **telegram:** stop deleting draft message on tool start ([d97c92c](https://github.com/choiceoh/Deneb/commit/d97c92c13688846c1d5bee8a87fd7b97661a2159))
* **telegram:** stop deleting draft message on tool start ([cc132e1](https://github.com/choiceoh/Deneb/commit/cc132e1d9c37fbfc55a1ab5107490e2a71080eaa))
* **test:** clear DENEB_GATEWAY_TOKEN in bootstrap config tests ([5a21877](https://github.com/choiceoh/Deneb/commit/5a218771cedd750b1b29d300128d4eb83158b6f1))
* **test:** clear DENEB_GATEWAY_TOKEN in bootstrap config tests ([6d145d8](https://github.com/choiceoh/Deneb/commit/6d145d83392c013e19a0ea707ed753c11e72066c))
* **test:** clear SGLANG_API_KEY in resolveLocalAIAPIKey test ([e88313f](https://github.com/choiceoh/Deneb/commit/e88313f1104bc16f610bd620310aac9f81b932e4))
* **test:** fix 9 broken tests across 8 packages ([d85550d](https://github.com/choiceoh/Deneb/commit/d85550d4e37fd5c3288f4e6a05bf6f49fd9d2065))
* **test:** fix 9 broken tests across plugin, skills, telegram, autoresearch, chat, gmail, and media ([3d6031d](https://github.com/choiceoh/Deneb/commit/3d6031d929dbdf060fa746218e8b68ed0b4fe25d))
* **test:** fix flaky knowledge prefetch and worker pool dispatch tests ([b272f40](https://github.com/choiceoh/Deneb/commit/b272f40a9f3ac31ced18df092d83b5b13d37fe31))
* **test:** fix flaky knowledge prefetch and worker pool dispatch tests ([44ca736](https://github.com/choiceoh/Deneb/commit/44ca736257353924b333736e66f540ecf8d46638))
* **test:** resolve flaky tests in knowledge, hooks, and rpc ([a5cf972](https://github.com/choiceoh/Deneb/commit/a5cf972f61c9e5262c48269284b74fcb6c9a4f34))
* **test:** resolve flaky tests in knowledge, hooks, and rpc packages ([329c75b](https://github.com/choiceoh/Deneb/commit/329c75b9d478bf7ebc7fda530cf7eb57c4469dc9))
* **unified:** retry schema init on SQLITE_BUSY ([a63a532](https://github.com/choiceoh/Deneb/commit/a63a532a29de8d5b24f641c657bf9e6c0f9f9635))
* **unified:** retry schema init on SQLITE_BUSY with exponential backoff ([7fd3d20](https://github.com/choiceoh/Deneb/commit/7fd3d202a0c2ad63e770bdf090119e54e17be250))


### ⚡ Performance

* **chat:** raise context budget to 204K and compaction threshold to 80% ([bb44f55](https://github.com/choiceoh/Deneb/commit/bb44f5597a9fe6b6207ead1d0fdcc39710150175))
* **chat:** raise context budget to 204K and compaction threshold to 80% ([7575887](https://github.com/choiceoh/Deneb/commit/757588749382128f404236774905ede1290d0a5b))
* **chat:** reduce sglang session memory load via delta, debounce, and trivial skip ([c5263cc](https://github.com/choiceoh/Deneb/commit/c5263cc6e33340a11d14ccdc89835d321dccf00e))
* **chat:** reduce sglang session memory load via delta, debounce, and trivial skip ([f3e9313](https://github.com/choiceoh/Deneb/commit/f3e9313426d9d592b9790d68ad10a2530bc7d390))
* **chat:** reduce token budgets and split memory vs live budgets ([a739818](https://github.com/choiceoh/Deneb/commit/a739818df2b57e2b05c4e9249c00d1791c31be0b))
* **chat:** reduce token budgets and split memory vs live budgets ([c2fb12d](https://github.com/choiceoh/Deneb/commit/c2fb12d430fa101d8fe4f8876abde04cabbd3b49))
* **chat:** tune proactive timeout from 35s to 15s ([1115c06](https://github.com/choiceoh/Deneb/commit/1115c0625b005ca788b2455b61a57ee696a0687c))
* **memory:** batch embedding API calls in dreaming cycle ([6466153](https://github.com/choiceoh/Deneb/commit/6466153746748226cebbbc53d96e9506dcf317e9))
* **memory:** batch embedding API calls in dreaming cycle ([2c484fb](https://github.com/choiceoh/Deneb/commit/2c484fb8f1153b50a3fdcb112d8c95704c1a3dcc))
* **memory:** improve search MRR +22% with Korean prefix matching and vector tuning ([5802c1a](https://github.com/choiceoh/Deneb/commit/5802c1aebc79630f448669c5a7068743f284cca4))
* **memory:** tune vector search params for BGE-M3 ([31ed6da](https://github.com/choiceoh/Deneb/commit/31ed6da391971dce0f4396319a28d245045e671b))
* **memory:** tune vector search params for BGE-M3 local embedding ([14714ba](https://github.com/choiceoh/Deneb/commit/14714ba6ea4e46ef129d8cb7df8e36677ef5bb31))
* **memory:** tune vector search params via autoresearch ([784b686](https://github.com/choiceoh/Deneb/commit/784b6869b5c4a79a8a49edd739ba339197be6b1b))
* **memory:** tune vector search params via autoresearch ([0d4f746](https://github.com/choiceoh/Deneb/commit/0d4f7466e4492630e42d3616f4db59477be85bc6))
* phase 1 architecture improvements ([8ece0b7](https://github.com/choiceoh/Deneb/commit/8ece0b7ed5a5b0f4c87f8eec15c03e3392ac08a2))
* phase 1 architecture improvements — token unification, search type dedup, prompt caching ([aea5f9a](https://github.com/choiceoh/Deneb/commit/aea5f9a0e89c1f3cf88f12b9a05c755418ba7597))
* quick wins — avoid clone in match, lazy regex, log silent errors, cache tool schema ([860d0f6](https://github.com/choiceoh/Deneb/commit/860d0f66b8c913c2b7fd0b0eb248008e3283f42b))
* quick wins — avoid clone, lazy regex, log errors, cache schema ([3cdc353](https://github.com/choiceoh/Deneb/commit/3cdc3536f67c83e80af222edf465e68b1dcafb27))
* **test:** parallelize live tests and reduce polling overhead ([6fa6d69](https://github.com/choiceoh/Deneb/commit/6fa6d69d71793e9d1fe5e5e797bc78078283a0fa))
* **test:** parallelize live tests and reduce polling overhead ([12e9c72](https://github.com/choiceoh/Deneb/commit/12e9c72d5da6fb0cc11dac83792a9501f3521167))
* **web:** add connection pooling, singleflight dedup, and parallel link enrichment ([44f3a5a](https://github.com/choiceoh/Deneb/commit/44f3a5a6f9e685b97f5b3185a1f7a67a0fb0acb8))
* **web:** add connection pooling, singleflight dedup, and parallel link enrichment ([159c16f](https://github.com/choiceoh/Deneb/commit/159c16f354343618e8b67ec0c29160e05046748f))


### 🔧 Internal

* **aurora,memory:** remove standalone store constructors and legacy schemas ([8f05fb8](https://github.com/choiceoh/Deneb/commit/8f05fb82f50514c02103102f90704f7d7c30caa2))
* **aurora,memory:** remove standalone store constructors and legacy schemas ([93bda2a](https://github.com/choiceoh/Deneb/commit/93bda2a9677140dcca81e28ca06790fa8858f01c))
* **autoreply:** consolidate thinking config, fix ignored errors, improve error logging ([48ea21e](https://github.com/choiceoh/Deneb/commit/48ea21e435dfd799768f317c35ce4202d7e7c878))
* **autoreply:** consolidate thinking config, fix ignored errors, improve error logging ([ae0c955](https://github.com/choiceoh/Deneb/commit/ae0c9551189763060cf0cbaf2e90bd700da0e821))
* **autoreply:** decompose MsgContext god object into embedded sub-structs ([d462cfd](https://github.com/choiceoh/Deneb/commit/d462cfd22dc774f30015c12948a77f9001e736a1))
* **autoreply:** decompose MsgContext god object into embedded sub-structs ([6d74dcc](https://github.com/choiceoh/Deneb/commit/6d74dcc551d5d5b1414473e41e630fb0d81b5b06))
* **autoreply:** remove facade indirection, fix swallowed errors, add structured error types ([5dadb30](https://github.com/choiceoh/Deneb/commit/5dadb302ee1a314edfbd3199c88dd23307462287))
* **autoreply:** remove facade, fix swallowed errors, add AgentErrorKind ([bbd4255](https://github.com/choiceoh/Deneb/commit/bbd4255d26419bdcd928a8b98cdb57484a40f1c0))
* **autoresearch:** split runner.go into executor, prompt_builder, reporter ([f342a84](https://github.com/choiceoh/Deneb/commit/f342a84d789b4cc994fa0623f8a6558214313f6e))
* **autoresearch:** split runner.go into executor, prompt_builder, reporter ([157f619](https://github.com/choiceoh/Deneb/commit/157f619e606cfefeb499a3db9b516b51dbc3a8ac))
* **chat:** code quality improvements across chat module ([2b5ea97](https://github.com/choiceoh/Deneb/commit/2b5ea97e1184e76418cd73d7f70f179c35633b17))
* **chat:** code quality improvements across chat module ([ec9e2b0](https://github.com/choiceoh/Deneb/commit/ec9e2b00f159f5a73c8679fc3bcf0b38b0314a27))
* **chat:** consolidate dual compaction into single Rust source of truth ([39e34f5](https://github.com/choiceoh/Deneb/commit/39e34f507fd69e0075bfdb0b89473d1fb008a5c9))
* **chat:** consolidate dual compaction into single Rust source of truth ([93a5102](https://github.com/choiceoh/Deneb/commit/93a5102951e650f01cafc1e1485261554070390f))
* **chat:** decouple chat from autoreply via chatport interface package ([a4f464a](https://github.com/choiceoh/Deneb/commit/a4f464abba1fa82ded9dad53ffefe79c58322388))
* **chat:** decouple chat from autoreply via chatport leaf package ([2f46f07](https://github.com/choiceoh/Deneb/commit/2f46f071ac7960e4f75f8b791306c1b8ed9dd4d2))
* **chat:** extract pilot, knowledge, compaction subpackages ([002dc10](https://github.com/choiceoh/Deneb/commit/002dc10c05a11153e1fe514e1dbceb74902e2a20))
* **chat:** extract pilot, knowledge, compaction subpackages ([1422f3b](https://github.com/choiceoh/Deneb/commit/1422f3b0eb102ea08803893a13b878051b76a48b))
* **chat:** reduce coding-agent bias in system prompt ([3b9aadb](https://github.com/choiceoh/Deneb/commit/3b9aadbebee54478847610507d9a6a31319de2f5))
* **chat:** reduce coding-agent bias in system prompt ([e11170a](https://github.com/choiceoh/Deneb/commit/e11170ae633473a70db2701d14d475124b3e09eb))
* **chat:** switch image/tool LLM client from fallback to lightweight sglang ([2a1fb56](https://github.com/choiceoh/Deneb/commit/2a1fb56344bebda9193e02f85a46d2a805ac05af))
* **compact:** 30K×7 file-based filler, 5 core compaction tests ([764501a](https://github.com/choiceoh/Deneb/commit/764501a70a9d43c31d7f9a4dbd66cb760c3f7adb))
* **compaction:** unify token estimation, fix persist errors, remove legacy path ([444c333](https://github.com/choiceoh/Deneb/commit/444c3330ee379fdcddb0df93832f92d52ba88074))
* **compaction:** unify token estimation, fix persist errors, remove legacy path ([991a9c8](https://github.com/choiceoh/Deneb/commit/991a9c85f54dcbf8b43455968e71df795701de86))
* **compact:** reduce to 5 core tests with 30K×7 filler files ([ea5f43a](https://github.com/choiceoh/Deneb/commit/ea5f43a408d454aa72cb2cf12dfbc2d4d9e09bd2))
* **compact:** use pre-built 15K filler files instead of runtime generation ([3f066e1](https://github.com/choiceoh/Deneb/commit/3f066e1fc1a4ebde58ead98cf858fc3760956933))
* **config:** add typed constants, hook validation, named defaults, and RPC pre-write validation ([8da4c9a](https://github.com/choiceoh/Deneb/commit/8da4c9ac629b6fce74f56d1726d224cd9c373e6f))
* **config:** typed constants, hook validation, and RPC pre-write validation ([ccbc45f](https://github.com/choiceoh/Deneb/commit/ccbc45fe5b5a84f06c3449f1cafce26e29539ff1))
* **core:** fix remaining clippy warnings in ancillary files ([38af828](https://github.com/choiceoh/Deneb/commit/38af828141b6b3ad6d6da21016ca91ef6201c73b))
* **core:** resolve all clippy warnings across core-rs ([910d85a](https://github.com/choiceoh/Deneb/commit/910d85aa70e8c2fae06c5dedbc1fccd33684762b))
* **core:** resolve all clippy warnings across core-rs workspace ([21d7ab5](https://github.com/choiceoh/Deneb/commit/21d7ab5d89fbbf39c797e8ccc6d48ad84a28e276))
* **ffi:** harden FFI module with safety fixes and cleanup ([dc8b35d](https://github.com/choiceoh/Deneb/commit/dc8b35dcf4e6e11085ae89c5f04079b1b48a85d6))
* **ffi:** harden FFI module with safety fixes and cleanup ([93afcc2](https://github.com/choiceoh/Deneb/commit/93afcc2c694cad00d496060046d72631203584ff))
* fix code smells across gateway and core ([02c35c8](https://github.com/choiceoh/Deneb/commit/02c35c87be5193809e6959d3b26aafac7e8f4d0d))
* fix code smells across gateway and core ([5f27160](https://github.com/choiceoh/Deneb/commit/5f27160dec64cf01ff3145bbb5e70da2b22f196b))
* **gateway:** deduplicate transcript adapter and HTTP backoff ([296c122](https://github.com/choiceoh/Deneb/commit/296c122662721442c92e9c04170a52bcc1217ca6))
* **gateway:** deduplicate transcript adapter and HTTP backoff logic ([7c736fc](https://github.com/choiceoh/Deneb/commit/7c736fc6066ba1912e5660a5fe0273facd2576bd))
* **gateway:** rename sglang hub to local AI hub ([48bde3a](https://github.com/choiceoh/Deneb/commit/48bde3a42c8df3c3962b447b758641dc814d02ac))
* **gateway:** rename sglang hub to local AI hub ([6bbcddb](https://github.com/choiceoh/Deneb/commit/6bbcddbd74bd127dd88097ee28c490b3873acffb))
* **logging:** unify console format and trim verbose startup logs ([b30f766](https://github.com/choiceoh/Deneb/commit/b30f76684194ea031a057a14bf0ecada850f640c))
* **logging:** unify console format and trim verbose startup logs ([9772f1d](https://github.com/choiceoh/Deneb/commit/9772f1d7c4667bbc3af676ed4b9ba69b545adfa1))
* **memory:** consolidate tunable constants into search_params.go ([89f0f4e](https://github.com/choiceoh/Deneb/commit/89f0f4e1e2c0fd9e71907af012a7905e17cf7736))
* **memory:** consolidate tunable constants into search_params.go ([bdf2ac1](https://github.com/choiceoh/Deneb/commit/bdf2ac15082ad5931a98ea4ec5b381d85cbca26b))
* **memory:** replace recall LLM with cross-encoder reranker ([3d73008](https://github.com/choiceoh/Deneb/commit/3d73008ac94d20d5244c8462a90f1747270d0cca))
* **memory:** replace recall LLM with cross-encoder reranker ([f725252](https://github.com/choiceoh/Deneb/commit/f72525221ba43a1c5924f53d253ec0faa8037882))
* minimize dependencies across Go and Rust ([b103b95](https://github.com/choiceoh/Deneb/commit/b103b95e165b4b15e5faa84352aacc5ccbb6c5fe))
* minimize dependencies across Go and Rust ([8921916](https://github.com/choiceoh/Deneb/commit/892191619d8f7851ef2f993bc555a131ba566570))
* **modelrole:** remove pilot role, unify all non-main usage under lightweight sglang ([d83a5dc](https://github.com/choiceoh/Deneb/commit/d83a5dc1f96b5a9820e0d4125c4c77d0e7cf4359))
* **modelrole:** switch pilot and fallback roles from Google to local sglang ([685fc4e](https://github.com/choiceoh/Deneb/commit/685fc4e0e6c0db82db57365d10520fd4142e41ea))
* **parsing:** improve parsing module robustness and maintainability ([d43bb18](https://github.com/choiceoh/Deneb/commit/d43bb18e42d0e0552d9ccbf2777bb4484a8fe8f1))
* **parsing:** improve quality and consolidate Go/Rust parsing ([73c66f7](https://github.com/choiceoh/Deneb/commit/73c66f7fc1a67adcdd20f7b6d793a6e18c86ed36))
* **parsing:** improve robustness and maintainability ([c8a7131](https://github.com/choiceoh/Deneb/commit/c8a7131839e9353320b8426f422a8c24bb17d6f7))
* **parsing:** improve Rust parsing module quality and consolidation ([0cbd9aa](https://github.com/choiceoh/Deneb/commit/0cbd9aa2e47f5263d72ac03d940107e56b740b82))
* **pilot:** remove pilot tool ([54f7c93](https://github.com/choiceoh/Deneb/commit/54f7c93920bea66fd3797b97fdde38aab83e496a))
* **pilot:** remove pilot tool, keep sglang LLM infrastructure ([2ae4d57](https://github.com/choiceoh/Deneb/commit/2ae4d57a91825363143935300ad96d071580c84f))
* **plugin:** remove channel abstraction, rename channels.* RPC to telegram.* ([4be3d48](https://github.com/choiceoh/Deneb/commit/4be3d4819e60c0bd764626c17d393b70841c7c02))
* **plugin:** remove channel abstraction, rename channels.* to telegram.* ([ef9cf6e](https://github.com/choiceoh/Deneb/commit/ef9cf6ea30d51a9b58d907c67a39891817300119))
* **protocol:** replace validation boilerplate with define_schema! macro ([78c1283](https://github.com/choiceoh/Deneb/commit/78c1283316954346940377ed77e735ff55f7413e))
* **protocol:** replace validation boilerplate with define_schema! macro ([b2c9fc4](https://github.com/choiceoh/Deneb/commit/b2c9fc4f944eb9182fcbc6cc552ed846ca7d9f3b))
* **proto:** unify FFI error codes into proto/gateway.proto as single source ([2a981ca](https://github.com/choiceoh/Deneb/commit/2a981cad031c5f85804001607cb1d63cef57b9bf))
* **proto:** unify FFI error codes into single source ([649459d](https://github.com/choiceoh/Deneb/commit/649459d0abb5696282a8da26c5b3216e1564d3a6))
* **provider:** remove startup model prewarm ([56a2a99](https://github.com/choiceoh/Deneb/commit/56a2a99221ae1985ae66c87edc43a07d7b520fdd))
* **provider:** remove startup model prewarm ([72ab68b](https://github.com/choiceoh/Deneb/commit/72ab68b793fb5cd0d4ce942d602d3c33deb836df))
* **rpc:** migrate verbose error/response patterns to rpcerr and rpcutil helpers ([6c2bfd8](https://github.com/choiceoh/Deneb/commit/6c2bfd841690b62694bbd3ab4b19345f480da36c))
* **rpc:** migrate verbose patterns to rpcerr/rpcutil helpers ([f7313af](https://github.com/choiceoh/Deneb/commit/f7313af8e14cc75a0d24b92dfd160f6422815860))
* **server:** decompose inbound.go into focused files ([13d16c3](https://github.com/choiceoh/Deneb/commit/13d16c37ab68f0024b0f5bc00e4a454e12966b64))
* **server:** decompose inbound.go into models, dashboard, and deps files ([b3d2abb](https://github.com/choiceoh/Deneb/commit/b3d2abb721afeb9a2fa829f9e7925f87086e357a))
* **server:** decompose server.go god object into focused files ([0d9729a](https://github.com/choiceoh/Deneb/commit/0d9729aa3ab638ca75344474b05386e07f7976c0))
* **server:** decompose ServerIntegrations into 5 cohesive subsystems ([2c46340](https://github.com/choiceoh/Deneb/commit/2c46340d0d715ec2690f8770a0687c7e89840acb))
* **server:** decompose ServerIntegrations into 5 cohesive subsystems ([ce85485](https://github.com/choiceoh/Deneb/commit/ce8548535b879a1cf215b9bc258ec4726a534807))
* **server:** extract options and init methods from server.go ([e2dd60e](https://github.com/choiceoh/Deneb/commit/e2dd60e6a5ac6767d3adb50f5d3a1bc5e3581a42))
* **server:** remove dead ConversationBindingStore ([0674dda](https://github.com/choiceoh/Deneb/commit/0674ddad94c118337fb498109314ff2aadcf63fb))
* **server:** remove dead ConversationBindingStore and channel selection branching ([4cc9797](https://github.com/choiceoh/Deneb/commit/4cc9797fc0254a579ef3e380279f4d2f8daa7069))
* **server:** replace init panics with error returns ([a4cfbd9](https://github.com/choiceoh/Deneb/commit/a4cfbd9b1371be1ca79b2e373420fec92b416708))
* **server:** switch gmail poll and vega recall from fallback to lightweight sglang ([722cffb](https://github.com/choiceoh/Deneb/commit/722cffb7f0da90562ca1671d2d1d7b610f3f3536))
* **shadow:** remove unused GitHub tracker and webhook tool ([0a3483a](https://github.com/choiceoh/Deneb/commit/0a3483aa4bda61f0e14cde23d06feea440c80f64))
* **shadow:** remove unused GitHub tracker, webhook tool, and shadow RPC handlers ([c3b69aa](https://github.com/choiceoh/Deneb/commit/c3b69aac0cf9e693108759fa6f059b28116eac36))
* **shadow:** slim down to 2 modules, inject into system prompt ([81aa1a0](https://github.com/choiceoh/Deneb/commit/81aa1a0c0d997c8179a331a1bf901b1dbfb1fd4b))
* **shadow:** slim down to 2 modules, inject into system prompt ([84af7f5](https://github.com/choiceoh/Deneb/commit/84af7f50321dd04afc1c4d12f0ab5daf8d306016))
* **test:** consolidate quality tests 300→159 and expand edge cases ([ba5efc2](https://github.com/choiceoh/Deneb/commit/ba5efc23467a34bb0b5fcdbc61c4e666317c4925))
* **test:** consolidate quality tests 300→159 and expand edge cases 25→50 ([4b20122](https://github.com/choiceoh/Deneb/commit/4b20122d6b47467a505ba9d0d071e09e28ec8b6f))
* **test:** merge safety tests into edge category with rewritten messages ([75dcdd4](https://github.com/choiceoh/Deneb/commit/75dcdd40c043b6beda925bca4d825e4097ee6a0d))
* **test:** replace safety-derived edge tests with genuine edge cases ([7cb0d64](https://github.com/choiceoh/Deneb/commit/7cb0d6415c9bcbaf4345a088f75ef227504eb5db))

## [4.9.0](https://github.com/choiceoh/Deneb/compare/deneb-v4.8.0...deneb-v4.9.0) (2026-04-02)


### ✨ Features

* **agent:** add streaming tool execution for reduced latency ([681db07](https://github.com/choiceoh/Deneb/commit/681db077d347f707a615a7c84e7c7d6e804fb52e))
* **agent:** extend default agent timeout from 10m to 30m ([fcf9db6](https://github.com/choiceoh/Deneb/commit/fcf9db6fd3b28aacec58bcc320b867ec7d0be108))
* **autoreply:** wire thinking package into reply pipeline ([72be5b3](https://github.com/choiceoh/Deneb/commit/72be5b31d7978fbb2a377fc5de832c1d8d4fc926))
* **autoresearch:** add constants override mode ([3356d7d](https://github.com/choiceoh/Deneb/commit/3356d7df0fa28f6dddfd3fa8e6c61426e351d2aa))
* **autoresearch:** add PNG chart visualization for experiment results ([3c0d42d](https://github.com/choiceoh/Deneb/commit/3c0d42d4264efd17fdd5ba1d05981814c250c3d5))
* **autoresearch:** extract hard-coded constants into configurable Params struct ([bff3bcc](https://github.com/choiceoh/Deneb/commit/bff3bcc87684ca7c2d057487c8fb4dc163e8aa95))
* **chat,agent:** wire all 15 techniques into actual execution paths ([64a8b84](https://github.com/choiceoh/Deneb/commit/64a8b84a1ad6aaa406d2b5b87ec776964e94b7ae))
* **chat,hooks:** add Phase 4 hook and DI enhancements ([d18d969](https://github.com/choiceoh/Deneb/commit/d18d969b93aa2271505b2245799de5708f36ef75))
* **chat:** add /chart slash command for autoresearch visualization ([8b88352](https://github.com/choiceoh/Deneb/commit/8b883527e709c38c0c3b1774a3884f3416d9a52e))
* **chat:** add autonomous agent turn continuation ([0468a91](https://github.com/choiceoh/Deneb/commit/0468a9106c5764a39ef322e96eac57e3e9874e16))
* **chat:** add LLM-powered recall follow-up instead of raw data delivery ([5cf5edd](https://github.com/choiceoh/Deneb/commit/5cf5eddc9d3d73bdc6f07cbda5908178e733468f))
* **chat:** add Phase 1 techniques from Claude Code analysis ([150e939](https://github.com/choiceoh/Deneb/commit/150e939b689da6951ea5f0df06d147013fcd5653))
* **chat:** add Phase 1 techniques from Claude Code analysis ([5b338a9](https://github.com/choiceoh/Deneb/commit/5b338a905d3b46e74f83950bc89c7c796e20318c))
* **chat:** add Phase 2 context & compaction techniques ([e28a4b5](https://github.com/choiceoh/Deneb/commit/e28a4b559cb67497d878aa2da6075f26eab6d779))
* **chat:** add Phase 3 tool system enhancements ([4d92b3d](https://github.com/choiceoh/Deneb/commit/4d92b3d048da34f110810704784ecd621bbae502))
* **chat:** add repo URL to Nev identity in system prompt ([204a55b](https://github.com/choiceoh/Deneb/commit/204a55ba6e206fac838ac3d01e5dd41e2f02c6dd))
* **chat:** add separate default model for sub-agents ([2312f39](https://github.com/choiceoh/Deneb/commit/2312f3904d03717647f96b9e517a37e673fcf665))
* **chat:** convert recall from parallel goroutine to agent-driven tool ([daf1ca0](https://github.com/choiceoh/Deneb/commit/daf1ca0e3a3753dd67054a900ef0ac936b1ce906))
* **chat:** enhance sub-agent usage and separate model/key config ([eed2a58](https://github.com/choiceoh/Deneb/commit/eed2a587b77cecf03d4ce9f8d844a6a54b5239c2))
* **chat:** enhance sub-agent usage with prompt guidance and broader coordinator detection ([c4005bb](https://github.com/choiceoh/Deneb/commit/c4005bb2a1b33f77f7bf75af1cd48bb646a27320))
* **chat:** guard mediaSendFn with mutex, register /chart in Telegram menu ([e062ed3](https://github.com/choiceoh/Deneb/commit/e062ed35e004c4ba0d1042be4ab20af00ed1c422))
* **chat:** support separate API key for sub-agents via provider config ([285fdeb](https://github.com/choiceoh/Deneb/commit/285fdebe91aa09dc10e9ce41aa294484b07a932d))
* **cron,session:** complete cron-session integration and subagent hardening ([b57971a](https://github.com/choiceoh/Deneb/commit/b57971a8a13dc376e6a4325caf0ed1ca515ef1a8))
* **diary:** skip heartbeat when user is active (idle &lt; 5 min) ([65d97d0](https://github.com/choiceoh/Deneb/commit/65d97d08abb54bd6061b01f1f9175d6806dd4348))
* **hooks:** wire internalHooks system to all hook fire points ([318e950](https://github.com/choiceoh/Deneb/commit/318e95065f0b3bf7e4cf194da0d9e05d34ee6db6))
* **mcp:** add MCP server for Claude Desktop integration ([664b6cb](https://github.com/choiceoh/Deneb/commit/664b6cbf70351ba501cfffc1eee35506d28983b8))
* **mcp:** auto-build deneb-mcp and generate .mcp.json in session start hook ([530280e](https://github.com/choiceoh/Deneb/commit/530280ee059b86c903f268824d62f62cf737f6ef))
* **memory:** add daily activity log and recall actions ([afa6809](https://github.com/choiceoh/Deneb/commit/afa68097343d5c2bd165516fd1ed73a878f56772))
* **memory:** add diary-to-SQL migration periodic task ([677ffc8](https://github.com/choiceoh/Deneb/commit/677ffc8ed6741fb62f22279630090d0a40af9946))
* **memory:** add search quality benchmark for autoresearch ([6ba9fef](https://github.com/choiceoh/Deneb/commit/6ba9fef4c6c31cfa6d28ae9ff5fec5c4094b193d))
* **memory:** rebalance extraction toward factual events and steep recency decay ([a86e83d](https://github.com/choiceoh/Deneb/commit/a86e83d58420ef96d7d99763760a82b6d59459b4))
* **memory:** rename daily logs to diary, add 2-hour heartbeat ([77b1e76](https://github.com/choiceoh/Deneb/commit/77b1e76100f55cac7646e8c297d7a09e9e516b81))
* **monitoring:** add /zerocalls Telegram command for dead RPC detection ([0840045](https://github.com/choiceoh/Deneb/commit/0840045ba863f105318b59b559c88203537a522e))
* **monitoring:** add wire I/O counters for all inter-subsystem wires ([85cbc2b](https://github.com/choiceoh/Deneb/commit/85cbc2b6c12ce265cfc24db5503fb631b3e62cec))
* **session:** unify cron/subagent session management with new Kind types ([12d70a0](https://github.com/choiceoh/Deneb/commit/12d70a0ac20f8c95766917ae5e672fd3a04c0e01))
* **shadow:** add 8 extended monitoring modules ([4ccf836](https://github.com/choiceoh/Deneb/commit/4ccf83626999d1f83519e66787c1843b83ee68f2))
* **shadow:** add shadow session monitoring service ([ff74096](https://github.com/choiceoh/Deneb/commit/ff74096434f55d482796d37b5212987f1854b6c3))
* **shadow:** integrate Kairos GitHub webhook events into shadow session ([701e301](https://github.com/choiceoh/Deneb/commit/701e30185b769cb746f67dd9c60e4a743fcf4cac))
* **tasks:** add unified background task control plane ([22f3665](https://github.com/choiceoh/Deneb/commit/22f366560d5486a143fd6c2bb14141eebfe047c7))
* **telegram:** add /models quick-change command with inline keyboard ([001d040](https://github.com/choiceoh/Deneb/commit/001d040a7270248f0a5a36da880b2964ae6eed2e))
* **telegram:** add extra models (glm-5v-turbo, glm-5.1) to /models quick-change ([b7c6483](https://github.com/choiceoh/Deneb/commit/b7c6483af09f24eceb948c7633b42bf3f5cbd2c1))
* **telegram:** add sglang-powered status summaries to progress tracker ([e578474](https://github.com/choiceoh/Deneb/commit/e5784744f5013d7017996210fad657482b097e88))
* **telegram:** persist /models quick-change selection to deneb.json ([3df70ae](https://github.com/choiceoh/Deneb/commit/3df70aed9e6714d855daa9d95fa293e1c1cf000b))
* **telegram:** persist /models quick-change selection to deneb.json ([74a75b9](https://github.com/choiceoh/Deneb/commit/74a75b91f2004e9d454b0af7313c7ef0769605a2))


### 🐛 Bug Fixes

* add /exec node parsing and support ExecHost 'node' ([fadb4a0](https://github.com/choiceoh/Deneb/commit/fadb4a0ed0584a43cbcf01b1455a0e76e6de69b5))
* **aurora:** add diagnostic logging to dreaming cycle for zero-result debugging ([a0640b0](https://github.com/choiceoh/Deneb/commit/a0640b057f3882a679b76eb372496f49ced6203e))
* **build:** apply NO_PROXY fix to Makefile Go targets for Claude Code containers ([8bcf206](https://github.com/choiceoh/Deneb/commit/8bcf206e8a97ddf469b78ff4242f8ef75a94392c))
* **build:** pre-cache Go modules in session start hook for offline builds ([bad48f9](https://github.com/choiceoh/Deneb/commit/bad48f9939f4dd597463939e9e185b11eb5494b2))
* **chat:** adapt callLocalLLM + test/chore cleanup ([12e0eea](https://github.com/choiceoh/Deneb/commit/12e0eea20dac91b0c1a2ccbf2dfcce941427120b))
* **chat:** adapt callLocalLLM to LLMSynthesizer type via closure wrapper ([32e5ea8](https://github.com/choiceoh/Deneb/commit/32e5ea80bf8aba7b2a3c3441b94935bf56af765a))
* **chat:** add additionalProperties to bare object schemas for strict LLM API validation ([c2e5a61](https://github.com/choiceoh/Deneb/commit/c2e5a61f5cf8b445c3eb025d808c830a8639e623))
* **chat:** add ClientRunID to continuation runs and restrict continue_run to async paths ([ba26586](https://github.com/choiceoh/Deneb/commit/ba26586fb1a1a10ee356d5d4ba9c575ccbf696c1))
* **chat:** clear pending queue on SessionsSend/SessionsSteer interrupt ([0ad9d14](https://github.com/choiceoh/Deneb/commit/0ad9d140c6504492235ba39ab21766630c9583e4))
* **chat:** decouple session memory context from shared memory goroutine ([56c2b1a](https://github.com/choiceoh/Deneb/commit/56c2b1a86b1433bb45711f0a1ff420254d551df2))
* **chat:** emit additionalProperties as schema object in tool-schema-gen ([0fe6cd9](https://github.com/choiceoh/Deneb/commit/0fe6cd9f68789a7834ed6572d06eddb2bf932193))
* **chat:** fix 13 bugs from PR [#1005](https://github.com/choiceoh/Deneb/issues/1005) code review ([d0384f6](https://github.com/choiceoh/Deneb/commit/d0384f6f7e9a0dbbaaf20657b9f2ff963ff10217))
* **chat:** fix 13 bugs from PR [#1005](https://github.com/choiceoh/Deneb/issues/1005) code review ([3f19919](https://github.com/choiceoh/Deneb/commit/3f199198e5a759d012a9a7e745cffb7dce3fb59e))
* **chat:** fix LLM timeout errors for local SGLang ([2d88c96](https://github.com/choiceoh/Deneb/commit/2d88c9694c174ecbf953fc324ca21cfaa74db4b8))
* **chat:** fix normalizeFileType mapping — proto→protobuf, not protobuf→proto ([3caaa32](https://github.com/choiceoh/Deneb/commit/3caaa3283492d301dc75e2f21a91ec9591250c54))
* **chat:** fix sub-agent model resolution ignoring subagentDefaultModel ([c9ce2cf](https://github.com/choiceoh/Deneb/commit/c9ce2cf9975a9cadc6f650493b80ab98815f9d54))
* **chat:** handle comma-separated glob patterns in grep/search tools ([50465ff](https://github.com/choiceoh/Deneb/commit/50465fffb07c747db258e24e3ef23303181b6075))
* **chat:** handle comma-separated glob patterns in grep/search tools ([c8ab5e2](https://github.com/choiceoh/Deneb/commit/c8ab5e2cf34d69e2742adebad2256c7affab6168))
* **chat:** harden grep tool with fileType normalization, multi-stage fallback, and diagnostic errors ([d2e721f](https://github.com/choiceoh/Deneb/commit/d2e721ff8e1a03a7460c02508a24a2c5fd2c41f3))
* **chat:** increase SGLang timeout constants to prevent context deadline exceeded ([57b5009](https://github.com/choiceoh/Deneb/commit/57b50096632ca78d03d5fcf417b52aade45ed93a))
* **chat:** inherit session model in sub-agent runs ([ce34598](https://github.com/choiceoh/Deneb/commit/ce34598301f8fca0455875d3ef1b1bc1def9cc62))
* **chat:** prevent diary heartbeat from contaminating user Aurora context ([c8c4db6](https://github.com/choiceoh/Deneb/commit/c8c4db6b2220b451a1e1142692ec26d854f41127))
* **chat:** push remaining fixed files (restore, orchestration) ([52b9877](https://github.com/choiceoh/Deneb/commit/52b98775912bfc6bb48220cf781e2a40a38b5611))
* **chat:** remaining files for PR [#1005](https://github.com/choiceoh/Deneb/issues/1005) bug fixes ([1a9332f](https://github.com/choiceoh/Deneb/commit/1a9332f0963fdb74e2d557d895010df2cb96d22a))
* **chat:** resolve role names in model fallback chain for sub-agents ([ed9998f](https://github.com/choiceoh/Deneb/commit/ed9998f8ec85d84432c578b801df092229eca2ba))
* **chat:** return cached file content instead of misleading 'already in context' message ([72abc7a](https://github.com/choiceoh/Deneb/commit/72abc7ab378e4ac460fb5c2c66e80e4e4ab41bdf))
* **chat:** revert default model to glm-5-turbo ([3f2218d](https://github.com/choiceoh/Deneb/commit/3f2218de9dbbb6b2a5eed9ac56132f7fb41611cd))
* **chat:** sanitize draft streaming text to prevent command exposure ([32ac2dd](https://github.com/choiceoh/Deneb/commit/32ac2ddab756bf01a70b7ba78ecf98a5ce785648))
* **chat:** separate rg stdout/stderr and salvage partial grep results ([8443eaa](https://github.com/choiceoh/Deneb/commit/8443eaa7fa403ce835079caccfdf6c7aff1ed3ca))
* **chat:** stop delivering raw recall data to user ([edf90aa](https://github.com/choiceoh/Deneb/commit/edf90aaa5c2929debb74be946f58653ab05ff87a))
* **chat:** use consistent runesPerToken divisor for aurora token estimation ([59d662f](https://github.com/choiceoh/Deneb/commit/59d662f553fa3fda613a75821472ec3e42ae692f))
* **chat:** UTF-8 safe rune-based truncation in compaction.go ([128bbff](https://github.com/choiceoh/Deneb/commit/128bbffbb94f0c4c2a6d46363302fd4759b67470))
* **chore:** add GOPATH/bin to PATH in setup-dev-env for go install discovery ([537bca4](https://github.com/choiceoh/Deneb/commit/537bca4abe352549ccb14b4bf601aac94841060d))
* **cron:** align cronTranscriptCloner.CloneRecent return type with interface ([e27c3d8](https://github.com/choiceoh/Deneb/commit/e27c3d8b0f93813eadb1914486aa92bb45840ce2))
* **cron:** prevent stale scheduler pointer after config reload ([005a8c6](https://github.com/choiceoh/Deneb/commit/005a8c6cb68dd16c507d9a459b9ccb93b561c158))
* **docs:** remove bloated install-script.svg ([d878445](https://github.com/choiceoh/Deneb/commit/d87844522aee7e25566a299cad528cb17e41f76a))
* **docs:** remove bloated install-script.svg terminal recording ([cb29b4b](https://github.com/choiceoh/Deneb/commit/cb29b4bfef66dd33202fcdf1e92a206b8f8e0ba0))
* **llm:** add per-request minimum timeout to prevent late-run context deadline exceeded ([8d0ceb0](https://github.com/choiceoh/Deneb/commit/8d0ceb0dd309551bfd1d76f8dbe42f07cee6aef6))
* **llm:** capture input tokens from finish_reason chunk in OpenAI streaming ([0f72582](https://github.com/choiceoh/Deneb/commit/0f72582d99ddf607f00531867aa03e3c9f482204))
* **llm:** send Authorization header for sglang provider ([7a62390](https://github.com/choiceoh/Deneb/commit/7a62390167dbee395e3e0ee9629ac7b045de9489))
* **mcp:** enrich deneb_system_check prompt with providers and runtime status ([ea99e6c](https://github.com/choiceoh/Deneb/commit/ea99e6c2e91ed6a055f7930eecc593ad296ddb39))
* **memory:** fix entity type CHECK constraint migration and validation ([0c5aaf2](https://github.com/choiceoh/Deneb/commit/0c5aaf2100e80c16e3a8e3ff3f0ff9601f2563e1))
* **memory:** migrate entity CHECK constraint in unified store ([e83a73d](https://github.com/choiceoh/Deneb/commit/e83a73d4b8a541c553ed2727eec5a4664ff6b509))
* **memory:** revert to modernc/sqlite to restore FTS5 support ([a4db38c](https://github.com/choiceoh/Deneb/commit/a4db38ccbdd43c77a3e6b25b09c1a679108398fb))
* **modelrole:** use correct gemini-3.1-pro-preview model ID for fallback ([1a25f10](https://github.com/choiceoh/Deneb/commit/1a25f10a909af269a053da120cba02c39e8c1505))
* **multi:** resolve 11 bugs from PR [#992](https://github.com/choiceoh/Deneb/issues/992)-[#1001](https://github.com/choiceoh/Deneb/issues/1001) code review ([b34fa4f](https://github.com/choiceoh/Deneb/commit/b34fa4f99eb672988ac8ba81efe95a6b7e9a71ee))
* **rpc:** remove duplicate node.event registration and dead hub.Config ([387cfea](https://github.com/choiceoh/Deneb/commit/387cfeab954a41940d952bd10117bd35b1b6b28f))
* **rpc:** remove duplicate node.event registration and dead hub.Config ([0f4e481](https://github.com/choiceoh/Deneb/commit/0f4e4815b674ae2a85fa887a09e5c08c278dfd44))
* **server:** downgrade websocket handshake failure log from Warn to Debug ([6b1ab46](https://github.com/choiceoh/Deneb/commit/6b1ab46603b654696c6c474f0ac2e30be8583c4a))
* **server:** remove duplicate LifecycleMethods registration and delete obsolete plan doc ([5270f37](https://github.com/choiceoh/Deneb/commit/5270f37e4d31c5c0c8ea3743a022ae686d9f5f48))
* **server:** resolve init order deps and stale pointer from PR953 ([2d74786](https://github.com/choiceoh/Deneb/commit/2d74786faacb8c0c25893f75d1a0da29d13c4631))
* **server:** resolve init order deps from PR953 hub wiring refactor ([c8526cb](https://github.com/choiceoh/Deneb/commit/c8526cb1d69f4635480477c4e262b34261e53a51))
* **server:** silence routine websocket handshake failures (timeout, closed conn) ([0909e19](https://github.com/choiceoh/Deneb/commit/0909e19f345c5363e4913141c5704f745ac30072))
* **shadow,acp:** fix race conditions and logic errors from deep review ([b4a1cbb](https://github.com/choiceoh/Deneb/commit/b4a1cbbd0ad9472c3c7904a4d88d39b95820fb20))
* **shadow,autoresearch:** resolve deadlocks and float precision bug ([7520288](https://github.com/choiceoh/Deneb/commit/7520288dc2c2c031f90a7c6f723462c0585d3168))
* **shadow,cron,autoresearch:** fix deadlock, data race, leak, and validation bugs ([e59a86d](https://github.com/choiceoh/Deneb/commit/e59a86dfdc7bd71e94d0fd1e3be95dd974a9ecd1))
* **task:** replace undefined NewResponseResult with MustResponseOK ([4af0592](https://github.com/choiceoh/Deneb/commit/4af05924c600c082c82e343bc18128f685495c79))
* **tasks:** address PR [#973](https://github.com/choiceoh/Deneb/issues/973)/[#975](https://github.com/choiceoh/Deneb/issues/975) review — resolve CloneRecent signature mismatch, RWMutex for task store reads, CancelFlow audit, dead code removal, Kind.IsInternal(), IsCronRunSessionKey precision ([042ab60](https://github.com/choiceoh/Deneb/commit/042ab60e2fe50547cc0e49f0f7b3f3f72d9ebf31))
* **telegram:** disable reasoning mode for activity summary LLM calls ([40eaff4](https://github.com/choiceoh/Deneb/commit/40eaff4baa18dda6be443df59cc4993183a4c30b))
* **telegram:** eliminate agent streaming flicker on completion ([db1b01e](https://github.com/choiceoh/Deneb/commit/db1b01ebe87d24ab02988216d98e72a44c2a2ac0))
* **telegram:** strip thinking preambles from progress tracker summaries ([329402c](https://github.com/choiceoh/Deneb/commit/329402c36ef6e581c5dc05e539a824566d946b3c))
* **telegram:** strip thinking preambles from progress tracker summaries ([c470ae9](https://github.com/choiceoh/Deneb/commit/c470ae96786c5cc9685bdbb5248d15c47db3f8c7))
* **telegram:** suppress message-not-modified error in draft stream edits ([7803fb8](https://github.com/choiceoh/Deneb/commit/7803fb8843643c32d756c35af834450c317994e1))
* **telegram:** suppress message-not-modified error in draft stream edits ([02d2342](https://github.com/choiceoh/Deneb/commit/02d2342c4f384de33c9b3e5191f64031d4b190ba))
* **telegram:** use chat_template_kwargs for sglang thinking disable ([31c9bc2](https://github.com/choiceoh/Deneb/commit/31c9bc2b27141f69efd2cd2885207fb71415e5a7))
* **test:** resolve 10 failing tests across Go gateway ([aed0d74](https://github.com/choiceoh/Deneb/commit/aed0d74026cc4cf969e81291b4498d86dfdf17f4))


### ⚡ Performance

* **build:** reduce binary sizes — add Rust release profile, Go strip flags, remove chrono-tz ([68daf14](https://github.com/choiceoh/Deneb/commit/68daf14783d70ff0a610683eb22e716d63857aab))
* **build:** reduce binary sizes — add Rust release profile, Go strip flags, remove chrono-tz ([9479223](https://github.com/choiceoh/Deneb/commit/94792231661a308c8b660d56fe7a71409aa405ce))
* **gateway:** replace modernc/sqlite with mattn/go-sqlite3 for smaller binary ([30d6902](https://github.com/choiceoh/Deneb/commit/30d69026003753b1f0b836da65ea5e785d2639a1))
* **hook:** improve session start hook with parallel installs and compact output ([24e6411](https://github.com/choiceoh/Deneb/commit/24e6411935c3e5e04786624023b14464a32fb58a))


### 🔧 Internal

* **chat,rpc:** add callback mutex and atomic RegisterDomain ([b6371a7](https://github.com/choiceoh/Deneb/commit/b6371a72f38c711f6a44dffcdd86e43c05b60b12))
* **chat,rpc:** add callback mutex and atomic RegisterDomain ([8811d89](https://github.com/choiceoh/Deneb/commit/8811d894c9f26db75f9a842e5d7195e7b878632d))
* **chat:** make recall follow-up serial instead of async ([afece40](https://github.com/choiceoh/Deneb/commit/afece4002c4d06d7e428bf011fe88d87d2b39c25))
* **chat:** remove polaris system knowledge agent ([56f93fb](https://github.com/choiceoh/Deneb/commit/56f93fb5abe3463f29d84df6c9a6063c361b2f9c))
* **chat:** remove polaris system knowledge agent ([1478ef8](https://github.com/choiceoh/Deneb/commit/1478ef8a7916d51f9e666386450c9f82aaf8f8f4))
* **chat:** remove toolreg_bridge.go backward-compat shim ([8869bb9](https://github.com/choiceoh/Deneb/commit/8869bb9e8b8b7efe1b016eede76ac7d283681c56))
* **chat:** unify main and image models to glm-5v-turbo ([62bb169](https://github.com/choiceoh/Deneb/commit/62bb169478138e4454fa0a656ad01e15807cf2b2))
* **core:** remove NAPI/Node.js binding infrastructure ([42a5a71](https://github.com/choiceoh/Deneb/commit/42a5a71dafa39a0a36447dcc08b82199239124dd))
* **gateway,core,cli:** remove unused node/device subsystem ([ebfb577](https://github.com/choiceoh/Deneb/commit/ebfb577dd5890c7f184c4d3fdca685f287283ba6))
* **gateway:** extract ChatPipeline and expand HandlerConfig ([519a950](https://github.com/choiceoh/Deneb/commit/519a950f272a9bbe5c86dcc5f1662186074b965c))
* **gateway:** improve GatewayHub wiring safety and consistency ([92055f3](https://github.com/choiceoh/Deneb/commit/92055f37241bab3373c0fae661f2a27d3444a2af))
* **gateway:** unify BroadcastFunc and create GatewayHub ([e6b3159](https://github.com/choiceoh/Deneb/commit/e6b3159a9bc0d126648e7c894e9f9271317be025))
* **llm:** remove Anthropic API support, unify on OpenAI-compatible API only ([e49d788](https://github.com/choiceoh/Deneb/commit/e49d788b8d771e3902a25d93b190eb90e27cf3b0))
* **memory:** remove non-Korean/English stop words and abort keywords ([d6e04bf](https://github.com/choiceoh/Deneb/commit/d6e04bf78a74c53621bb6ef18befe7bdf4e21e08))
* **memory:** replace halfLife with steepness, relax dreaming prune threshold ([657c1dd](https://github.com/choiceoh/Deneb/commit/657c1dda4899a0675d5a62d2c874df9e754c6d9c))
* **monitoring:** replace wire I/O counters with rpc_zero_calls report ([468042c](https://github.com/choiceoh/Deneb/commit/468042c9c0e20c3ee8920665297f84be1976a24a))
* **server:** add hub guardrails — validation, snapshot test, wiring rules ([343442c](https://github.com/choiceoh/Deneb/commit/343442cc68e5396294cf688f1e0bdee820e979bb))
* **server:** move GatewayHub to rpcutil and inline Deps assembly ([f533871](https://github.com/choiceoh/Deneb/commit/f533871d40555939e7b2c252f20ac1b46c9edebf))
* **server:** wire GatewayHub and consolidate RPC registration (steps 2+4) ([e7cbdfb](https://github.com/choiceoh/Deneb/commit/e7cbdfbba05288620a4307d9808cae6350c46c2c))
* **session:** consolidate session management across cron, subagent, and shadow sessions ([78d5c68](https://github.com/choiceoh/Deneb/commit/78d5c684159b3b84bf128c0af22677ea68228e15))
* **unified:** remove legacy store migration code ([1efc467](https://github.com/choiceoh/Deneb/commit/1efc467a1f52545fc7db1cd12fa297a08b2c2879))
