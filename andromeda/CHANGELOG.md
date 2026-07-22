# Changelog

## [0.0.101](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.100...andromeda-v0.0.101) (2026-07-22)


### ✨ Features

* **capture:** convert HWP / legacy Office / ODF / video attachments to readable form ([#4104](https://github.com/choiceoh/Deneb/issues/4104)) ([4e50a60](https://github.com/choiceoh/Deneb/commit/4e50a60aa0c6ac5a37b6cd39e623c7c4d28f0e63))

## [0.0.100](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.99...andromeda-v0.0.100) (2026-07-22)


### ✨ Features

* **andromeda:** move mail detail actions onto the tab row (right-aligned) ([#4099](https://github.com/choiceoh/Deneb/issues/4099)) ([999ba0e](https://github.com/choiceoh/Deneb/commit/999ba0edba094668a343ef1f8fac54e6d532517f))
* **capture:** attach N files in one agent turn via miniapp.capture.batch ([#4100](https://github.com/choiceoh/Deneb/issues/4100)) ([6f1ca0e](https://github.com/choiceoh/Deneb/commit/6f1ca0eff7faf115f34c3f21f45dfdf8e0733f48))


### 🐛 Bug Fixes

* **andromeda:** stop multi-file attach batch from dropping every file after the first ([#4094](https://github.com/choiceoh/Deneb/issues/4094)) ([46df416](https://github.com/choiceoh/Deneb/commit/46df4168e3e9a7e030d904b3de9166a7baa4aa7b))

## [0.0.99](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.98...andromeda-v0.0.99) (2026-07-21)


### 🐛 Bug Fixes

* **andromeda:** render mail analysis through AssistantText so the lead deneb-ui card renders ([#4088](https://github.com/choiceoh/Deneb/issues/4088)) ([56bf8b9](https://github.com/choiceoh/Deneb/commit/56bf8b931572479cfd45c5f255ffcbfa9df51cbf))

## [0.0.98](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.97...andromeda-v0.0.98) (2026-07-21)


### 🐛 Bug Fixes

* **andromeda:** 결재 검토 모드가 기안자 인물 위키를 여는 소음 제거 — 제목만 매칭 ([#4026](https://github.com/choiceoh/Deneb/issues/4026)) ([768ca06](https://github.com/choiceoh/Deneb/commit/768ca0600ab681a8c6e07f07b5ca2145ac57c7eb))
* **andromeda:** 모닝 브리핑 투어 즉시 반응 — today 로컬 오픈 + 데네브 패널 펼침 ([#4033](https://github.com/choiceoh/Deneb/issues/4033)) ([42a73be](https://github.com/choiceoh/Deneb/commit/42a73bea3a5ff5ca5b5b445f4e6e1d2030034300))
* **andromeda:** 오늘 질문 대기 KPI가 미응답 피드 인박스로 열리게 ([#4038](https://github.com/choiceoh/Deneb/issues/4038)) ([eedc7af](https://github.com/choiceoh/Deneb/commit/eedc7af4bd10e3172c45b268e2fef0f3eaee15a2))

## [0.0.97](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.96...andromeda-v0.0.97) (2026-07-20)


### 🐛 Bug Fixes

* **andromeda:** route session-less workfeed action prompts into the AI panel ([#4019](https://github.com/choiceoh/Deneb/issues/4019)) ([f4bcc74](https://github.com/choiceoh/Deneb/commit/f4bcc746c3daa424ffc008f0a42f206fb11ee800))

## [0.0.96](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.95...andromeda-v0.0.96) (2026-07-19)


### ✨ Features

* **andromeda:** TS state-register — tsc 타입체커 기반 워크스테이션 상태 w/r 맵 ([#4003](https://github.com/choiceoh/Deneb/issues/4003)) ([f5674b1](https://github.com/choiceoh/Deneb/commit/f5674b159b694e37f5f8a8271431e171e6b4df5c))
* **andromeda:** 그리드 행 우클릭 컨텍스트 메뉴 (메일·할일) — 감사 W7 잔여분 ([#3959](https://github.com/choiceoh/Deneb/issues/3959)) ([31a2fe5](https://github.com/choiceoh/Deneb/commit/31a2fe53d8a1928fe7c4a5597859c80331ee104a))
* **audit:** doc-ref-lint — 문서 코드참조 validate-or-freeze 게이트 (arXiv:2607.13285) + 실측 rot 수정 ([#3966](https://github.com/choiceoh/Deneb/issues/3966)) ([835e7bc](https://github.com/choiceoh/Deneb/commit/835e7bcf09504be7acbdaf7c929413dfee2ff8e2))
* **audit:** doc-ref-lint 경고 전수 클린 — 휴리스틱 개선 + 문서 rot 수리 ([#3991](https://github.com/choiceoh/Deneb/issues/3991)) ([4453301](https://github.com/choiceoh/Deneb/commit/445330129a0b51cc173ad6575b88ef162185df09))
* **workstation:** 활용 2탄 — 알림 복귀 내비·모닝 브리핑 투어·효용 원장 관찰 카드 ([#3951](https://github.com/choiceoh/Deneb/issues/3951)) ([97f97df](https://github.com/choiceoh/Deneb/commit/97f97dffdd9e3e28d0eae45f41a86f25d4712ca7))
* **workstation:** 활용 3탄 — 결재 검토 모드·컨텍스트 팔로우·효용 원장 자기조정 ([#3954](https://github.com/choiceoh/Deneb/issues/3954)) ([dbea2df](https://github.com/choiceoh/Deneb/commit/dbea2df5fff773c98517b7ec6c2ff09cb355b00f))
* 모닝레터 마감줄 롱프레스 완료 처리 (deneb-ui longpress + 위키 due_done) ([#3979](https://github.com/choiceoh/Deneb/issues/3979)) ([6200730](https://github.com/choiceoh/Deneb/commit/620073020c3e24ce6542f9b3a01a412a14f1fe83))

## [0.0.95](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.94...andromeda-v0.0.95) (2026-07-18)


### ✨ Features

* **andromeda:** 인터랙티브 카드 응답 표시·회신 개선 — 카드 응답 말풍선 + 최신 답변만 콜백 허용 ([#3933](https://github.com/choiceoh/Deneb/issues/3933)) ([56b42be](https://github.com/choiceoh/Deneb/commit/56b42be58b8684eb7a8f11727304d18e065becd6))
* **chat:** deneb-html 스타일 다양화 — 테마 3종 + 유틸리티 마이크로 디자인 시스템 ([#3939](https://github.com/choiceoh/Deneb/issues/3939)) ([b88fc17](https://github.com/choiceoh/Deneb/commit/b88fc17f73f00c17bfdd4e3ff9063ec371c12e56))
* **chat:** deneb-html 클라이언트 베이스 스타일시트 — 일관 타이포 + 생성 3배 단축 ([#3938](https://github.com/choiceoh/Deneb/issues/3938)) ([bca0693](https://github.com/choiceoh/Deneb/commit/bca0693fb6830b503668c1a1681721ed96ce9d18))
* **chat:** 웹페이지형 인터랙티브 응답(deneb-html) + 카드 저작 인라인 계약·열화 구제 ([#3936](https://github.com/choiceoh/Deneb/issues/3936)) ([7f6b5b6](https://github.com/choiceoh/Deneb/commit/7f6b5b63054e65ad35a646030d2589b3c5a854c2))
* **chat:** 카드 발명태그 별칭화(3구현)·deneb-html 프리뷰 누출 방지·생성중 스켈레톤 ([#3937](https://github.com/choiceoh/Deneb/issues/3937)) ([8c0f5f4](https://github.com/choiceoh/Deneb/commit/8c0f5f479143243621cb12d44d2dd33163fb180c))
* **workstation:** 도구 활용 확장 — 능동 지침·spotlight·date 점프·todo prefill·계측 ([#3927](https://github.com/choiceoh/Deneb/issues/3927)) ([bee77c5](https://github.com/choiceoh/Deneb/commit/bee77c564001d082cace6ca0ef7ccc7a55f889fd))
* **workstation:** 활용 2탄 — 알림 복귀 내비·모닝 브리핑 투어·효용 원장 관찰 카드 ([#3931](https://github.com/choiceoh/Deneb/issues/3931)) ([fa69236](https://github.com/choiceoh/Deneb/commit/fa69236699b6998b10caa6f9d6175408a8e2d67e))


### 🐛 Bug Fixes

* **andromeda:** finish window.confirm sweep — 현장지도 날짜질문·업데이터 재시작을 앱 다이얼로그로 ([#3924](https://github.com/choiceoh/Deneb/issues/3924)) ([d7cea05](https://github.com/choiceoh/Deneb/commit/d7cea058ba2dcee5fff5101049b5a55ea5e19f42))
* **andromeda:** 분할 스트립·데네브 패널 리오픈 탭 겹침 해소 ([#3923](https://github.com/choiceoh/Deneb/issues/3923)) ([0ec0d5a](https://github.com/choiceoh/Deneb/commit/0ec0d5a05ca8b91cde61863d54eef00da1eccf4e))

## [0.0.94](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.93...andromeda-v0.0.94) (2026-07-18)


### ✨ Features

* **andromeda:** undo 토스트·앱 컨펌 통일 + 폴리시 묶음 + 코드 신택스 하이라이트 ([#3913](https://github.com/choiceoh/Deneb/issues/3913)) ([1a9294f](https://github.com/choiceoh/Deneb/commit/1a9294fac2a45ec00ca755218941eb90ed71cfed))
* **andromeda:** 줌(Ctrl+휠 영속) + 전역 오프라인 배너 ([#3910](https://github.com/choiceoh/Deneb/issues/3910)) ([5609e03](https://github.com/choiceoh/Deneb/commit/5609e03ead4572bb343088d0b70f5852309bd34e))
* **andromeda:** 챗 파리티 — 응답 복사·빈 상태 + 편집-재전송·응답 변형 ‹n/N› ([#3906](https://github.com/choiceoh/Deneb/issues/3906)) ([368e607](https://github.com/choiceoh/Deneb/commit/368e60779a7861bdc5173409d4bd0ca841b92adc))
* **andromeda:** 첨부 스테이징·썸네일 + 데스크톱 상주성 (트레이·OS알림·배지·창영속·단일인스턴스) ([#3908](https://github.com/choiceoh/Deneb/issues/3908)) ([b4decee](https://github.com/choiceoh/Deneb/commit/b4decee39f919f829bd78a46af164407cec99f4e))
* **modelpicker:** per-model rolling 24h usage in the picker (runs, tokens, cache reads) ([#3894](https://github.com/choiceoh/Deneb/issues/3894)) ([fc8c9ad](https://github.com/choiceoh/Deneb/commit/fc8c9adf7a0ca7ad9a8b27bf6c5b718cedf8fa17))
* **sessions:** 대화 이름변경 RPC + 드로어 검색·더 보기 (miniapp.sessions.rename) ([#3907](https://github.com/choiceoh/Deneb/issues/3907)) ([38762f9](https://github.com/choiceoh/Deneb/commit/38762f9dd66edad643cc06170d33a18a24b5f9de))


### 🐛 Bug Fixes

* **andromeda:** Cargo.lock에 W6 플러그인 크레이트 반영 (rust --locked 레인 복구) ([#3912](https://github.com/choiceoh/Deneb/issues/3912)) ([ecb7899](https://github.com/choiceoh/Deneb/commit/ecb7899f1d955f257692795b9af51aa19139e4b3))
* **andromeda:** 감사 결함 5종 — 죽은 토큰·이중 제출·빈 오늘 랜딩·대화 60개 잘림·업데이터 동의 UX ([#3904](https://github.com/choiceoh/Deneb/issues/3904)) ([f9a8f65](https://github.com/choiceoh/Deneb/commit/f9a8f652592d7e548fbeadf0d0a512f0e55878e7))

## [0.0.93](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.92...andromeda-v0.0.93) (2026-07-17)


### ✨ Features

* **andromeda:** 오늘 콕핏 — KPI 스트립·일정 타임라인·마감 레이더·결재 섹션 ([#3863](https://github.com/choiceoh/Deneb/issues/3863)) ([527d5a5](https://github.com/choiceoh/Deneb/commit/527d5a5450584024453313185385c4bafbda10f4))
* **andromeda:** 타일 분할 워크스페이스·커맨드 팔레트·데네브 화면 조종 버스 ([#3850](https://github.com/choiceoh/Deneb/issues/3850)) ([58c6d1b](https://github.com/choiceoh/Deneb/commit/58c6d1b6a243a1a31c524a01ea07a92d48289d73))
* **gateway:** workstation 챗 도구 — 데네브가 안드로메다 화면을 직접 조종 ([#3861](https://github.com/choiceoh/Deneb/issues/3861)) ([fb2c5df](https://github.com/choiceoh/Deneb/commit/fb2c5dfc406c95148ced8f10dc735d4ff5fb7441))


### 🔧 Internal

* **deps:** Refine·extended-icons·play-review 제거 및 DIY 대체 ([#3879](https://github.com/choiceoh/Deneb/issues/3879)) ([b33e6c6](https://github.com/choiceoh/Deneb/commit/b33e6c66cd00f659c371567694f5333a19e716b7))

## [0.0.92](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.91...andromeda-v0.0.92) (2026-07-16)


### ✨ Features

* **andromeda:** consume gateway change-feed (mail/approvals/wiki) in native-sync catch-up ([#3851](https://github.com/choiceoh/Deneb/issues/3851)) ([17696ff](https://github.com/choiceoh/Deneb/commit/17696ff101ee2286737bd48d0379af9b5df2fb88))

## [0.0.91](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.90...andromeda-v0.0.91) (2026-07-16)


### ✨ Features

* **andromeda:** 결재 반려 사유·첨부 바이너리·choices 배선 ([#3823](https://github.com/choiceoh/Deneb/issues/3823)) ([0149415](https://github.com/choiceoh/Deneb/commit/0149415f387efc1d8a00ce78df6cdeb986e4be55))


### 🐛 Bug Fixes

* **andromeda:** 결재 분석 로딩 문구 — 캐시조회 vs LLM 구분 ([#3828](https://github.com/choiceoh/Deneb/issues/3828)) ([94c0fd0](https://github.com/choiceoh/Deneb/commit/94c0fd0013e7a326a21281c973591909e75b3fb0))

## [0.0.90](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.89...andromeda-v0.0.90) (2026-07-16)


### ✨ Features

* **andromeda:** approval attachments + markdown parity (sticky/합계/footnotes) ([#3819](https://github.com/choiceoh/Deneb/issues/3819)) ([6c54c7a](https://github.com/choiceoh/Deneb/commit/6c54c7a4679187e6ca1759c691d9b318370a0d1e))
* **sitemap:** 상세에서 현장 상태 칩으로 변경 ([#3814](https://github.com/choiceoh/Deneb/issues/3814)) ([1f04a07](https://github.com/choiceoh/Deneb/commit/1f04a075a69b2cd6230c50d8b62b7523f8d47b96))
* **sitemap:** 현장 페이지 생성·일정 편집·미배치 상세 ([#3820](https://github.com/choiceoh/Deneb/issues/3820)) ([4f84126](https://github.com/choiceoh/Deneb/commit/4f8412678328fb623816f842b67de7a4d63d3d38))

## [0.0.89](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.88...andromeda-v0.0.89) (2026-07-16)


### 🐛 Bug Fixes

* **andromeda:** interrupt paragraphs for GFM tables in approval bodies ([#3800](https://github.com/choiceoh/Deneb/issues/3800)) ([931c649](https://github.com/choiceoh/Deneb/commit/931c6490c3e64f2b878fef6126d1b04585cec96d))
* **andromeda:** markdown normalizers + approval body polish ([#3806](https://github.com/choiceoh/Deneb/issues/3806)) ([fb42ff6](https://github.com/choiceoh/Deneb/commit/fb42ff6488cae02ecab611e9b73c8b8e1e182d06))
* **sitemap:** 기본 필터를 공사중(개설)만 표시 ([#3802](https://github.com/choiceoh/Deneb/issues/3802)) ([323742c](https://github.com/choiceoh/Deneb/commit/323742cb35fa1b3ca52f6c9b43e20b6fa3ba3e62))

## [0.0.88](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.87...andromeda-v0.0.88) (2026-07-16)


### ✨ Features

* **groupware:** approval body cache + folder hint + wiki log of analyses ([#3796](https://github.com/choiceoh/Deneb/issues/3796)) ([d8eda32](https://github.com/choiceoh/Deneb/commit/d8eda32ac0425e1d42f07b27e836761b4d84eab8))

## [0.0.87](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.86...andromeda-v0.0.87) (2026-07-16)


### ✨ Features

* **groupware:** default 모듈·인버터 scope + sales period tabs rework ([#3794](https://github.com/choiceoh/Deneb/issues/3794)) ([e6a3cb8](https://github.com/choiceoh/Deneb/commit/e6a3cb8645af677d7afc0485f554473ad66c0bcd))

## [0.0.86](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.85...andromeda-v0.0.86) (2026-07-16)


### ✨ Features

* **client:** 그룹웨어 플랫 탭·게시판 본문 시트·새로고침 ([#3790](https://github.com/choiceoh/Deneb/issues/3790)) ([24629d4](https://github.com/choiceoh/Deneb/commit/24629d495e188e50fbc4c87dc640d52992030ae7))

## [0.0.85](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.84...andromeda-v0.0.85) (2026-07-15)


### ✨ Features

* **client:** native rows for groupware ERP snapshot ([#3786](https://github.com/choiceoh/Deneb/issues/3786)) ([c418f3b](https://github.com/choiceoh/Deneb/commit/c418f3b24845d621bfbb148756648c4aa7a6281e))
* **client:** pivot to mail, quiet pending filter, approval detail reorder ([#3788](https://github.com/choiceoh/Deneb/issues/3788)) ([e6af3c3](https://github.com/choiceoh/Deneb/commit/e6af3c336106c5772aea8ff5676e1829d5e40395))
* **client:** zune-style pivot header + groupware surface upgrades ([#3785](https://github.com/choiceoh/Deneb/issues/3785)) ([40db5ae](https://github.com/choiceoh/Deneb/commit/40db5ae49e7a7522cf56a9ca3d306ca79c5d1595))

## [0.0.84](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.83...andromeda-v0.0.84) (2026-07-15)


### ✨ Features

* **wiki:** 현장 공정 일정 지도 타임라인 + 임박 검사일 서피싱 ([#3772](https://github.com/choiceoh/Deneb/issues/3772)) ([9a44aab](https://github.com/choiceoh/Deneb/commit/9a44aabaa906e6c08f038d257c4facf4f707780f))

## [0.0.83](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.82...andromeda-v0.0.83) (2026-07-15)


### ✨ Features

* **client:** 전자결재 피드 카드 딥링크·소스 표현 ([#3770](https://github.com/choiceoh/Deneb/issues/3770)) ([15a3836](https://github.com/choiceoh/Deneb/commit/15a38365f779652e555025eb572eb025a8aac381))
* **groupware:** 결재 AI 분석·날짜별 표면 + ERP 허브 ([#3768](https://github.com/choiceoh/Deneb/issues/3768)) ([c6bf0a4](https://github.com/choiceoh/Deneb/commit/c6bf0a4802c2b87fa553b5a76ac3a8c9833fbbf5))
* **groupware:** 반려 사유 E2E 입력·전달 ([#3771](https://github.com/choiceoh/Deneb/issues/3771)) ([924893f](https://github.com/choiceoh/Deneb/commit/924893f328a8e5dd2b9991949fb3fe0be8c12d19))


### 🐛 Bug Fixes

* **client:** separate approval sections and restore act buttons ([#3778](https://github.com/choiceoh/Deneb/issues/3778)) ([72d2d41](https://github.com/choiceoh/Deneb/commit/72d2d414f709fd6202d4edf8c0bfba745d511320))
* **client:** 결재 본문 마크다운 표 렌더 ([#3774](https://github.com/choiceoh/Deneb/issues/3774)) ([dae33e5](https://github.com/choiceoh/Deneb/commit/dae33e5b7a69c4c4ab7728e1f3a28f80b28f7315))
* **client:** 그룹웨어·결재 화면 퀄리티 정리 ([#3779](https://github.com/choiceoh/Deneb/issues/3779)) ([e882133](https://github.com/choiceoh/Deneb/commit/e88213300fb7c5b6c36e27174ae17416632d695d))

## [0.0.82](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.81...andromeda-v0.0.82) (2026-07-15)


### ✨ Features

* **sitemap:** client-android 현장 지도 포팅 + andromeda 휠 줌·팬 ([#3737](https://github.com/choiceoh/Deneb/issues/3737)) ([d82ecba](https://github.com/choiceoh/Deneb/commit/d82ecbac006a6bb637c4b4ab26f820afc6842510))
* **wiki:** 현장 서브페이지 데이터 모델 + 지도 상태 필터 ([#3744](https://github.com/choiceoh/Deneb/issues/3744)) ([b68ae20](https://github.com/choiceoh/Deneb/commit/b68ae203b5bf96e34d9e95c9b3390e536c6ea7bd))

## [0.0.81](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.80...andromeda-v0.0.81) (2026-07-15)


### ✨ Features

* **groupware:** Amaranth HMAC 리더·툴·피드 승인칩 ([#3733](https://github.com/choiceoh/Deneb/issues/3733)) ([f480684](https://github.com/choiceoh/Deneb/commit/f48068476ca0ef31a67b61a1866936e7ebb0c81f))

## [0.0.80](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.79...andromeda-v0.0.80) (2026-07-15)


### ✨ Features

* **andromeda:** 관찰 팬 — miniapp.observe 동작·로그 대시보드 ([#3684](https://github.com/choiceoh/Deneb/issues/3684)) ([cf10bf5](https://github.com/choiceoh/Deneb/commit/cf10bf58940a895a7cbccd87a91b14cba8f924b0))
* **rsi:** measure post-deploy impact ([#3704](https://github.com/choiceoh/Deneb/issues/3704)) ([68583c5](https://github.com/choiceoh/Deneb/commit/68583c5781755f636b12851a7f38753238a503a5))
* **sitemap:** 현장 지도 — 한국 시군구·읍면 지도에 에너지원·특성·용량 인코딩 ([#3703](https://github.com/choiceoh/Deneb/issues/3703)) ([df0c299](https://github.com/choiceoh/Deneb/commit/df0c29984e7f36f87bb7ca4851770449382b3c5c))

## [0.0.79](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.78...andromeda-v0.0.79) (2026-07-15)


### 🔧 Internal

* **health:** Health Bench 2.0 84.8→88.2 — contracts·fanout·tests·guides + baseline ([#3679](https://github.com/choiceoh/Deneb/issues/3679)) ([0d2d48b](https://github.com/choiceoh/Deneb/commit/0d2d48bd4127361e03fbbbb711bffa5e04c614f4))

## [0.0.78](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.77...andromeda-v0.0.78) (2026-07-15)


### 🐛 Bug Fixes

* **health:** andromeda·kotlin 테스트 intent-naming (Wave1 PR-B) ([#3654](https://github.com/choiceoh/Deneb/issues/3654)) ([7b0af73](https://github.com/choiceoh/Deneb/commit/7b0af73db6b6a3a17f55ed3dde10e67e2b6415ec))

## [0.0.77](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.76...andromeda-v0.0.77) (2026-07-13)


### 🐛 Bug Fixes

* **rsi:** harden closed-loop delivery and dispatch ([#3628](https://github.com/choiceoh/Deneb/issues/3628)) ([cd74b65](https://github.com/choiceoh/Deneb/commit/cd74b65dba71259be0f538d814972095aa1e28a6))

## [0.0.76](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.75...andromeda-v0.0.76) (2026-07-13)


### ✨ Features

* **andromeda:** RSI Pane 강화 — 진화 건강 카드·L2/L3 드릴·L4 후보 소스·자동배차 칩·상세 모달 ([#3595](https://github.com/choiceoh/Deneb/issues/3595)) ([e98e07a](https://github.com/choiceoh/Deneb/commit/e98e07ae9b2abf1d7bf7a7a44fda6fd8f445d109))
* close recursive self-improvement feedback loops ([#3622](https://github.com/choiceoh/Deneb/issues/3622)) ([2142504](https://github.com/choiceoh/Deneb/commit/2142504306cb785d6e8a8996f86aec7a70c38dad))
* **rsi:** 자가교정 루프 관측성·안정성 업그레이드 + 워크트리 가드 파일 경로 인식 ([#3605](https://github.com/choiceoh/Deneb/issues/3605)) ([188c400](https://github.com/choiceoh/Deneb/commit/188c4007c207fda19d731f77b52690044cb9bb3f))


### 🐛 Bug Fixes

* **genesis:** RSI 상태·표면 티어·재오픈 캡 Go/Python 패리티 복구 ([#3610](https://github.com/choiceoh/Deneb/issues/3610)) ([0e8950e](https://github.com/choiceoh/Deneb/commit/0e8950ebe80fe338faa99e60026c13cdcc85f19e))
* **rsi:** L4 배차 교착·accepted 백로그 가시성·스윕 억제 ([#3612](https://github.com/choiceoh/Deneb/issues/3612)) ([680ec36](https://github.com/choiceoh/Deneb/commit/680ec36742de561eab32f0bb282ff9c4bfb82112))
* **rsi:** 미흡수 봇 리뷰 지적 흡수 — L4 배차·스윕·Cursor 가드·RSI UI ([#3618](https://github.com/choiceoh/Deneb/issues/3618)) ([7fc2580](https://github.com/choiceoh/Deneb/commit/7fc2580657e7bfd0f8e92f6848e0727b915958d9))

## [0.0.75](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.74...andromeda-v0.0.75) (2026-07-13)


### ✨ Features

* **rsi:** RSI 화면용 데이터 노출 — 구조적 건강 블록·후보 자동배차 플래그·배차 허용목록 tool-quality ([#3588](https://github.com/choiceoh/Deneb/issues/3588)) ([03072bd](https://github.com/choiceoh/Deneb/commit/03072bd89a6041396440998dde7deb949181f03f))


### 🐛 Bug Fixes

* **andromeda:** 카드 stat 박스 제거 — 네이티브 파리티(카드 내 박스겹침 해소) ([#3590](https://github.com/choiceoh/Deneb/issues/3590)) ([0f9bd6f](https://github.com/choiceoh/Deneb/commit/0f9bd6fd79507b5a6a09e9401e2ad6706d8cb3b1))

## [0.0.74](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.73...andromeda-v0.0.74) (2026-07-13)


### ✨ Features

* **andromeda:** 우측 데네브 채팅 패널 기본 접힘 — 작업 영역 넓게, 우측 탭으로 열기 ([#3585](https://github.com/choiceoh/Deneb/issues/3585)) ([0358ad1](https://github.com/choiceoh/Deneb/commit/0358ad1db7089f4cee0b33ff88eba7b513894e1a))
* **andromeda:** 카드 비주얼 업그레이드 v1 — stat 수치 타이포(단위 서픽스·델타 칩)·막대차트 그라디언트/그리드 ([#3584](https://github.com/choiceoh/Deneb/issues/3584)) ([a9b4d4c](https://github.com/choiceoh/Deneb/commit/a9b4d4cd8e40dea9c9a666a3e885de59f147e379))
* **andromeda:** 카드 테이블 dense 모드 — 3열+ CJK 테이블 타입 한 단 낮춤(어절 fit·네이티브 파리티) ([#3581](https://github.com/choiceoh/Deneb/issues/3581)) ([839c42d](https://github.com/choiceoh/Deneb/commit/839c42de2967f1595d7bf5a56f4559a48cafeaad))

## [0.0.73](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.72...andromeda-v0.0.73) (2026-07-13)


### ✨ Features

* **andromeda:** 능동형 질문 카드 데스크톱 답변 어포던스 — 피드 상세 답변 칩(action.run)·question 플래그 존중 ([#3564](https://github.com/choiceoh/Deneb/issues/3564)) ([43c5524](https://github.com/choiceoh/Deneb/commit/43c5524696b2cfc7de80f9524c53197ff91c0df7))
* **andromeda:** 데스크톱 카드 노드 렌더 파리티 — 카운트다운 라이브 틱·미결정 진행바·2글자 아바타/아이콘 폴백·secondary 뱃지 분리 ([#3571](https://github.com/choiceoh/Deneb/issues/3571)) ([409bf12](https://github.com/choiceoh/Deneb/commit/409bf12dfb27146699f0423b8737cf00e0defa9f))
* **andromeda:** 카드 노드 렌더 파리티 2탄 — box 정렬·이미지 대체박스·슬라이더 범위정규화·아이콘 매핑 확장 ([#3577](https://github.com/choiceoh/Deneb/issues/3577)) ([7fc26bb](https://github.com/choiceoh/Deneb/commit/7fc26bbc25b54a9093d402338c543ff1db90b4d8))
* **andromeda:** 카드 막대차트 세로 SVG 파리티 — 가로 CSS 막대→네이티브 캔버스형 세로 컬럼(값 라벨 상단·0=막대없음) ([#3573](https://github.com/choiceoh/Deneb/issues/3573)) ([8130a06](https://github.com/choiceoh/Deneb/commit/8130a0630175e84158de62a2beed6d477f750865))

## [0.0.72](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.71...andromeda-v0.0.72) (2026-07-13)


### ✨ Features

* **rsi:** 2차 패스 후보 3건 구현 — 커리큘럼 출처 접지 게이트 · BINEVAL 자문 방향 · SOP 마이너 ([#3557](https://github.com/choiceoh/Deneb/issues/3557)) ([715602b](https://github.com/choiceoh/Deneb/commit/715602b8942439a5ff99f0c83e1042b37b0ec93e))

## [0.0.71](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.70...andromeda-v0.0.71) (2026-07-13)


### 🐛 Bug Fixes

* **andromeda:** Cargo.lock 앱 버전 0.0.70 동기화 — release 커밋 이후 깨진 cargo check --locked 복구 ([#3552](https://github.com/choiceoh/Deneb/issues/3552)) ([465bfe9](https://github.com/choiceoh/Deneb/commit/465bfe99aab223c190521e9a67d4a5000e9a256e))

## [0.0.70](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.69...andromeda-v0.0.70) (2026-07-12)


### ✨ Features

* **andromeda:** RSI 허브 드릴다운 — L1/L4 카드 인라인 상세 ([#3509](https://github.com/choiceoh/Deneb/issues/3509)) ([45157f4](https://github.com/choiceoh/Deneb/commit/45157f4d133d7e4c8c720e72722e6d4ca2a9a124))
* **andromeda:** 데스크톱 표 정합 — fit-first 랩·어절 줄바꿈·숫자 열 자동 정렬·tabular figures ([#3517](https://github.com/choiceoh/Deneb/issues/3517)) ([e0ba611](https://github.com/choiceoh/Deneb/commit/e0ba611fbf14aab87bd2a44c31b18e046aa47bca))
* **rsi:** P5 선제 L4 공급 — health-finding 마이너 + record RPC + staged 가시성 ([#3528](https://github.com/choiceoh/Deneb/issues/3528)) ([f17d98f](https://github.com/choiceoh/Deneb/commit/f17d98ff119b0baf71025ab765c1a5db5d22d2a3))

## [0.0.69](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.68...andromeda-v0.0.69) (2026-07-12)


### ✨ Features

* **andromeda:** 미학적 폴리싱 — GridNotice 스피너/재시도·접근성·디자인 토큰 정합 ([265792f](https://github.com/choiceoh/Deneb/commit/265792f8dde4a043806a7479a0d87a7c204f4aff))
* **andromeda:** 재귀적 자가개선 루프 상태 페인 — L1-L4 상태 카드 ([#3497](https://github.com/choiceoh/Deneb/issues/3497)) ([b2d31ac](https://github.com/choiceoh/Deneb/commit/b2d31accf480395a2d5ec8c71648e553af267b12))
* **denebui:** 카드 렌더 폴리싱 — 상태 톤·코드 크롬·아이콘/차트 패리티·조형 계약 ([#3498](https://github.com/choiceoh/Deneb/issues/3498)) ([31ccf3c](https://github.com/choiceoh/Deneb/commit/31ccf3c4db783c76c720ac917097fe454f5cec3c))
* **native:** 재귀적 자가개선 루프 상태 화면 (2/3) ([#3496](https://github.com/choiceoh/Deneb/issues/3496)) ([42033e0](https://github.com/choiceoh/Deneb/commit/42033e00c15d73660b3e49f4a20302524731c47a))


### 🐛 Bug Fixes

* **ci:** close Health Bench follow-up checks ([#3504](https://github.com/choiceoh/Deneb/issues/3504)) ([e165101](https://github.com/choiceoh/Deneb/commit/e16510192dbc723b9dd7901fc3e36d5bd9e67334))
* **denebui:** 관용 렌더러 — 프로즈에 붙은 펜스 오프너 인식, 인라인 공백 보존, ul 순서·표 섹션 보정 ([#3499](https://github.com/choiceoh/Deneb/issues/3499)) ([4ed1b71](https://github.com/choiceoh/Deneb/commit/4ed1b71b0dafc24be023458020e83c45e227c755))
* **denebui:** 관용 렌더러 2차 — 글루된 클로저·원라이너 펜스·아이콘 카탈로그 확충 ([#3503](https://github.com/choiceoh/Deneb/issues/3503)) ([dcb9816](https://github.com/choiceoh/Deneb/commit/dcb98162249fac5e9c13f9c5ecaba7179513936b))
* **denebui:** 렌더 정렬 3차 — 피드/푸시 펜스 누출 근본수정·채팅 이중 카드 제거·required 피드백·조형 관측 ([#3506](https://github.com/choiceoh/Deneb/issues/3506)) ([f227075](https://github.com/choiceoh/Deneb/commit/f227075e341c3d93ab05c6a8dab05fbb663fde2c))
* **rsi:** RSI 뷰어 한글화 + 카드 탭 상세(층 역할 설명) ([#3502](https://github.com/choiceoh/Deneb/issues/3502)) ([46772c2](https://github.com/choiceoh/Deneb/commit/46772c2bfd136487d1b0d41f06647fa8ced7ffdf))


### 🔧 Internal

* improve runtime health and Health Bench 2.2 ([#3501](https://github.com/choiceoh/Deneb/issues/3501)) ([4b3882f](https://github.com/choiceoh/Deneb/commit/4b3882f2652434fe5a2b3ec69e2d8eef4db9971f))

## [0.0.68](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.67...andromeda-v0.0.68) (2026-07-12)


### ✨ Features

* **rsi:** 재귀적 자가개선 루프 상태 RPC — miniapp.rsi.status + //deneb:wire(양 클라 생성) ([#3492](https://github.com/choiceoh/Deneb/issues/3492)) ([2f485dc](https://github.com/choiceoh/Deneb/commit/2f485dc9217e487724482d2d7a4ab8c488027326))

## [0.0.67](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.66...andromeda-v0.0.67) (2026-07-12)


### 🔧 Internal

* raise code health with typed boundaries and maintainable tests ([#3481](https://github.com/choiceoh/Deneb/issues/3481)) ([2af4ef2](https://github.com/choiceoh/Deneb/commit/2af4ef29991779f94f0b999c2bd41994c8700e90))

## [0.0.66](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.65...andromeda-v0.0.66) (2026-07-12)


### ✨ Features

* **genesis:** RSI P2 — 피드 카드 원탭 채택/기각 ([#3456](https://github.com/choiceoh/Deneb/issues/3456)) ([8bc8252](https://github.com/choiceoh/Deneb/commit/8bc825266075a5112a7b04e5d011f3159a067151))


### 🐛 Bug Fixes

* **andromeda:** [#3438](https://github.com/choiceoh/Deneb/issues/3438) 플레이크 2건 탈레이스 — attach 재진입·ProjectHome AI 투영 비동기 단언을 waitFor로 ([#3442](https://github.com/choiceoh/Deneb/issues/3442)) ([5f1e807](https://github.com/choiceoh/Deneb/commit/5f1e807a53b04445bc2d5c4ad0ce9038616cbbb0))

## [0.0.65](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.64...andromeda-v0.0.65) (2026-07-11)


### 🐛 Bug Fixes

* [#3438](https://github.com/choiceoh/Deneb/issues/3438) 후속 main 수리 — gofmt/gofumpt 16파일 정리·bootstrap 경계 테스트 계약 수정·andromeda 테스트 TZ 고정 ([#3441](https://github.com/choiceoh/Deneb/issues/3441)) ([12ecbb2](https://github.com/choiceoh/Deneb/commit/12ecbb2e56475c7dab0576a109e1c0d31e67f597))

## [0.0.64](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.63...andromeda-v0.0.64) (2026-07-11)


### ✨ Features

* **andromeda:** 메일·위키·브리핑 답변 인쇄 — window.print + [@media](https://github.com/media) print 서브트리 격리 ([#3400](https://github.com/choiceoh/Deneb/issues/3400)) ([723cc60](https://github.com/choiceoh/Deneb/commit/723cc6099d7bd7b31063daf0e94a994353f18f92))


### 🐛 Bug Fixes

* **runtime:** harden recovery and health reporting ([#3397](https://github.com/choiceoh/Deneb/issues/3397)) ([9d74081](https://github.com/choiceoh/Deneb/commit/9d74081fa924701d746e260f4c1f62ac4231cb46))


### 🔧 Internal

* **andromeda:** FleetPane 1178 LOC 분할 — 탭 뷰·카드는 FleetViews, 타입·헬퍼는 fleetHelpers (순수 이동) ([#3405](https://github.com/choiceoh/Deneb/issues/3405)) ([a033fe1](https://github.com/choiceoh/Deneb/commit/a033fe1d2d10b4a014880916b546ed301fb4929a))
* **andromeda:** WikiPane 모달 분리 — 이동·새 페이지·미저장 모달을 WikiModals로 (순수 이동) ([#3407](https://github.com/choiceoh/Deneb/issues/3407)) ([4bc37f2](https://github.com/choiceoh/Deneb/commit/4bc37f2cc957a56fcb1e7685ca848576036ae063))
* **andromeda:** 챗 surface 중복 제거 — ChatView·AIPanel 공유 로직 추출 (컴포저·첨부 파이프라인·모델 로딩·답변 액션) ([#3403](https://github.com/choiceoh/Deneb/issues/3403)) ([c4497c5](https://github.com/choiceoh/Deneb/commit/c4497c5b6a589f1d7c1f22a77d75135775f01888))
* **andromeda:** 컴포넌트 파일의 비컴포넌트 export 분리 — react-refresh 경고 7개 제거 ([#3393](https://github.com/choiceoh/Deneb/issues/3393)) ([025c6b3](https://github.com/choiceoh/Deneb/commit/025c6b30daf48d1240d860a5f0a5f877f1cf285c))
* raise code health to 90.1 ([#3438](https://github.com/choiceoh/Deneb/issues/3438)) ([0dbc45d](https://github.com/choiceoh/Deneb/commit/0dbc45d681a062ae09907f574f93ba8c132375cd))

## [0.0.63](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.62...andromeda-v0.0.63) (2026-07-09)


### 🐛 Bug Fixes

* **skills:** 프로푸스 라이프사이클 로그 판정 라벨 정밀화 — create↔genesis 자동/수동 구분 ([#3360](https://github.com/choiceoh/Deneb/issues/3360)) ([e08b5ec](https://github.com/choiceoh/Deneb/commit/e08b5ec942d65e4624f567783db87eb74d67ee0d))

## [0.0.62](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.61...andromeda-v0.0.62) (2026-07-09)


### 🔧 Internal

* 코드모드(Code Mode) 제거 — git-worktree 코딩 세션·miniapp.code.* RPC·네이티브/andromeda UI·구현자 프롬프트 프로파일 ([#3354](https://github.com/choiceoh/Deneb/issues/3354)) ([ca47311](https://github.com/choiceoh/Deneb/commit/ca47311ed8cb51c38f30bd016f0236f136cf5342))

## [0.0.61](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.60...andromeda-v0.0.61) (2026-07-09)


### 🐛 Bug Fixes

* **andromeda:** PDF 미리보기가 원본 소스로 뜨는 문제 — blob MIME 재스탬프 ([#3346](https://github.com/choiceoh/Deneb/issues/3346)) ([5910f1e](https://github.com/choiceoh/Deneb/commit/5910f1e2912d06bcab972d5d383b120c2d8e1fa0))

## [0.0.60](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.59...andromeda-v0.0.60) (2026-07-09)


### 🐛 Bug Fixes

* **andromeda:** 메일 열람 시 날짜 페이저 튕김·미열람 수정 — Date 헤더와 수신일 불일치 가드 + 고정 행 ([#3308](https://github.com/choiceoh/Deneb/issues/3308)) ([3a9a34d](https://github.com/choiceoh/Deneb/commit/3a9a34d81b5b80c23222fc6236bb6172db5e1c4d))

## [0.0.59](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.58...andromeda-v0.0.59) (2026-07-08)


### ✨ Features

* **skills:** 자가개선 코딩 퍼널 가시성 — 큐 침묵을 소진/미발생으로 화면에서 구분 ([#3301](https://github.com/choiceoh/Deneb/issues/3301)) ([30b5a5c](https://github.com/choiceoh/Deneb/commit/30b5a5c0406a6f73380f302d63cfb1b0b67c397a))

## [0.0.58](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.57...andromeda-v0.0.58) (2026-07-08)


### ✨ Features

* **org:** 조직도-위키 통합 — 통합검색 회상소스·인물 해소·네이티브 위키 링크 ([#3287](https://github.com/choiceoh/Deneb/issues/3287)) ([f6d0db5](https://github.com/choiceoh/Deneb/commit/f6d0db5b44aa4449536ac166e2a2cac5be914886))

## [0.0.57](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.56...andromeda-v0.0.57) (2026-07-07)


### ✨ Features

* **andromeda:** 노트북 자료 영역 3단계 높이 토글 — 접힘·기본·확대 ([#3272](https://github.com/choiceoh/Deneb/issues/3272)) ([33da011](https://github.com/choiceoh/Deneb/commit/33da0115384e782807c05178e1da7ba8ff3796be))


### ⚡ Performance

* **mail:** 메일 아카이브 로컬 저장소 도입 — mail_archive 12.9s→47ms ([#3270](https://github.com/choiceoh/Deneb/issues/3270)) ([c4f9ec3](https://github.com/choiceoh/Deneb/commit/c4f9ec367e7053c9be3582c2787b0f92fb76f45a))

## [0.0.56](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.55...andromeda-v0.0.56) (2026-07-07)


### 🐛 Bug Fixes

* **andromeda:** deneb-ui 인라인 렌더러 링크 지원 — 네이티브 InlineTokenizer 패리티 ([#3266](https://github.com/choiceoh/Deneb/issues/3266)) ([12f8cdf](https://github.com/choiceoh/Deneb/commit/12f8cdf4f2c44508ea4aa03768d73d75d8c9cb74))
* **native:** 렌더러 인라인 마크다운 전수 감사 — 표 셀·경보·인용 별표 수리 (3구현) ([#3264](https://github.com/choiceoh/Deneb/issues/3264)) ([229915b](https://github.com/choiceoh/Deneb/commit/229915bed9e2de30ab300504dfc8a240e4686489))
* **native:** 리스트 키·타임라인 제목 인라인 마크다운 — **키** 리터럴 별표 수리 ([#3260](https://github.com/choiceoh/Deneb/issues/3260)) ([497b361](https://github.com/choiceoh/Deneb/commit/497b361fc140675bdf4c7e5c43d366097f50026e))
* **native:** 슬라이더 역범위 크래시 가드 + 꺾은선 음수값 in-plot (3구현 감사 2라운드) ([#3265](https://github.com/choiceoh/Deneb/issues/3265)) ([9b5e8ba](https://github.com/choiceoh/Deneb/commit/9b5e8badd951f9e7fbbe40842268da16642cda00))

## [0.0.55](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.54...andromeda-v0.0.55) (2026-07-07)


### ✨ Features

* **andromeda:** 노트북 상단 자료 영역 접기/펼치기 토글 ([#3254](https://github.com/choiceoh/Deneb/issues/3254)) ([5283de0](https://github.com/choiceoh/Deneb/commit/5283de06635bba78911f2b8064909a42e9df6260))
* **wiki:** 거래처를 프로젝트 위계 최상단으로 — client 필드·모아보기 그룹핑·회상 앵커·백필 도구 ([#3257](https://github.com/choiceoh/Deneb/issues/3257)) ([4878a53](https://github.com/choiceoh/Deneb/commit/4878a53e6180592595161bda7e9513eabae6e351))

## [0.0.54](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.53...andromeda-v0.0.54) (2026-07-07)


### ✨ Features

* **andromeda:** deneb-ui 데스크톱 패리티 라운드 + [#3233](https://github.com/choiceoh/Deneb/issues/3233) 리뷰 6건 반영 ([#3235](https://github.com/choiceoh/Deneb/issues/3235)) ([9da12f2](https://github.com/choiceoh/Deneb/commit/9da12f275e90346f6d1314a72321601f8c0d0d15))
* **andromeda:** 파일 미리보기를 인라인 탭에서 팝업 모달로 전환 ([#3245](https://github.com/choiceoh/Deneb/issues/3245)) ([31aebd6](https://github.com/choiceoh/Deneb/commit/31aebd6926339c3ebfed2bb17fd06615a5ce175e))
* **chat:** deneb-ui 파서 관용화+자동 보정 라운드 — 3구현 동기 (v2.1) ([#3247](https://github.com/choiceoh/Deneb/issues/3247)) ([237fb6c](https://github.com/choiceoh/Deneb/commit/237fb6c26eb5c2f0e47d3285af64ba4ce0ec3fe2))
* **native:** 아침레터 에디토리얼 리디자인 + 렌더러 3라운드 + 리뷰 반영 ([#3233](https://github.com/choiceoh/Deneb/issues/3233)) ([bf5c206](https://github.com/choiceoh/Deneb/commit/bf5c2064c82065699f6e0784a16ae8afcc257b54))


### 🐛 Bug Fixes

* **andromeda:** HWP 미리보기 글자 깨짐 수리 — 실 문서 섹션의 deflate 트레일링 패딩 허용 ([#3237](https://github.com/choiceoh/Deneb/issues/3237)) ([d43cac5](https://github.com/choiceoh/Deneb/commit/d43cac57935c3d4952266a265272f79f50d0c999))
* **native:** [#3234](https://github.com/choiceoh/Deneb/issues/3234)·[#3235](https://github.com/choiceoh/Deneb/issues/3235) 리뷰 반영 — stagger 노드키·모션 flip·카운트업 정밀도·badge 화이트리스트·음수 열 ([#3236](https://github.com/choiceoh/Deneb/issues/3236)) ([4ddfdcb](https://github.com/choiceoh/Deneb/commit/4ddfdcbe6638b7d90d00baa6c67ab31ac8f7f483))

## [0.0.53](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.52...andromeda-v0.0.53) (2026-07-05)


### ✨ Features

* **denebui:** 카드 상시관찰 + 인터랙티브 개방 + 우선 고려 + 정본 프리뷰 + 규칙 각인 ([#3206](https://github.com/choiceoh/Deneb/issues/3206)) ([a051d9b](https://github.com/choiceoh/Deneb/commit/a051d9b08daca4594b24efb8c9f996c8a75001c4))

## [0.0.52](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.51...andromeda-v0.0.52) (2026-07-05)


### ✨ Features

* **denebui:** deneb-ui 카드를 라벨 HTML 포맷으로 전면 전환 — JSON 수리계층 은퇴 ([#3202](https://github.com/choiceoh/Deneb/issues/3202)) ([a01b3bb](https://github.com/choiceoh/Deneb/commit/a01b3bb2d876a85ef006168bd9e8308f28579189))

## [0.0.51](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.50...andromeda-v0.0.51) (2026-07-05)


### 🔧 Internal

* **native:** 클라 RPC를 miniapp.mail.* 정식 네임스페이스로 전환 (Andromeda 포함) ([#3176](https://github.com/choiceoh/Deneb/issues/3176)) ([6ac4d2e](https://github.com/choiceoh/Deneb/commit/6ac4d2eb262a37959b8d995dad14929a67b98f87))

## [0.0.50](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.49...andromeda-v0.0.50) (2026-07-03)


### 🐛 Bug Fixes

* **andromeda:** 리뷰 잔여분 — 노트 저장 위장·첨부 중 세션 가드·HWP 상한/DIFAT·탭 동기화 외 ([#3066](https://github.com/choiceoh/Deneb/issues/3066)) ([335d5e4](https://github.com/choiceoh/Deneb/commit/335d5e4cbc72bc84cdc72764fa95fe25ef0ac2f3))

## [0.0.49](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.48...andromeda-v0.0.49) (2026-07-03)


### 🐛 Bug Fixes

* **andromeda:** 파일 pane 미저장 편집 유실·빈 파일 저장·HWP 방어 수정 ([#3058](https://github.com/choiceoh/Deneb/issues/3058)) ([82f3d7a](https://github.com/choiceoh/Deneb/commit/82f3d7aa16987ce9ace920999341f6ab85f460de))

## [0.0.48](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.47...andromeda-v0.0.48) (2026-07-03)


### ✨ Features

* **andromeda:** HWP 인앱 미리보기 — 텍스트·표·이미지 (순수 TS 파서 직접 구현) ([#3050](https://github.com/choiceoh/Deneb/issues/3050)) ([1e31736](https://github.com/choiceoh/Deneb/commit/1e317361540fa7cbba20f26013e6481d38c9e10b))
* **andromeda:** 파일 미리보기 + 라이브 편집 + 탭 — AionUi식 인앱 뷰어 ([#3048](https://github.com/choiceoh/Deneb/issues/3048)) ([91201fe](https://github.com/choiceoh/Deneb/commit/91201fed91b6ccc3004df67347ed4afa395ab00c))

## [0.0.47](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.46...andromeda-v0.0.47) (2026-07-02)


### ✨ Features

* **andromeda:** 위키 트리 탐색 — 폴더 계층 그대로 접고 펼치는 레일 ([#3031](https://github.com/choiceoh/Deneb/issues/3031)) ([df4dc97](https://github.com/choiceoh/Deneb/commit/df4dc979cd5c5bdd4a53e5639271d8e79df7f5a7))

## [0.0.46](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.45...andromeda-v0.0.46) (2026-07-02)


### 🐛 Bug Fixes

* **andromeda:** Ctrl+C 복사가 코드 화면 전환으로 둔갑하던 단축키 충돌 수정 ([#3024](https://github.com/choiceoh/Deneb/issues/3024)) ([ce26e7f](https://github.com/choiceoh/Deneb/commit/ce26e7ff90c2256632167a9050461003bba529fc))

## [0.0.45](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.44...andromeda-v0.0.45) (2026-07-02)


### ✨ Features

* **andromeda:** 노트북을 AI 작업대로 재구성 — 칩 자료 + 답변을 노트로 저장 ([#3016](https://github.com/choiceoh/Deneb/issues/3016)) ([937d15f](https://github.com/choiceoh/Deneb/commit/937d15f5b2775a234db331d421d9a7594234eae2))
* **andromeda:** 우측 Deneb 패널에 파일 첨부 (이미지 OCR·문서·녹음) ([#3018](https://github.com/choiceoh/Deneb/issues/3018)) ([27e4167](https://github.com/choiceoh/Deneb/commit/27e4167fdbfe2a4cf8e13b1791094e20ea472d4c))
* **andromeda:** 채팅 영역 전체를 무표시 드롭존으로 — 드래그 중일 때만 살짝 표시 ([#3019](https://github.com/choiceoh/Deneb/issues/3019)) ([fb150d8](https://github.com/choiceoh/Deneb/commit/fb150d8ba71600537370e93d14cd6d60239bb324))

## [0.0.44](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.43...andromeda-v0.0.44) (2026-07-01)


### ✨ Features

* **andromeda:** 메일 상세에 분석/본문 토글 (분석 기본) ([#3004](https://github.com/choiceoh/Deneb/issues/3004)) ([1d6dded](https://github.com/choiceoh/Deneb/commit/1d6dded1e92686d76cc0bfcacfbccd001ae02d4e))

## [0.0.43](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.42...andromeda-v0.0.43) (2026-06-30)


### ✨ Features

* **market:** copper in USD/tonne + big tiled 시장 card on the 오늘 dashboard ([#3003](https://github.com/choiceoh/Deneb/issues/3003)) ([1d35097](https://github.com/choiceoh/Deneb/commit/1d350973197be4ca188c3ea802d79c950426c3f0))

## [0.0.42](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.41...andromeda-v0.0.42) (2026-06-30)


### ✨ Features

* **andromeda:** wiki opens pages in preview by default (edit on toggle) ([#2999](https://github.com/choiceoh/Deneb/issues/2999)) ([0d5c6c3](https://github.com/choiceoh/Deneb/commit/0d5c6c304f47a52bfb47cb16455eda331c0dcbb5))


### 🐛 Bug Fixes

* **andromeda:** repair auto-updater endpoint via rolling andromeda-latest manifest ([#2997](https://github.com/choiceoh/Deneb/issues/2997)) ([57d99f9](https://github.com/choiceoh/Deneb/commit/57d99f9fd2fae643261b0fd9af1c6c00dee3f45f))
* **andromeda:** 메일 AI 분석 카드를 흰색으로 — 회색 배경에 표·내용이 묻히던 문제 ([#3000](https://github.com/choiceoh/Deneb/issues/3000)) ([51cda8a](https://github.com/choiceoh/Deneb/commit/51cda8a1b8647b4eea82ff7f3dcea8d2a58b8c81))

## [0.0.41](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.40...andromeda-v0.0.41) (2026-06-30)


### ✨ Features

* **andromeda:** wiki — compact rail, no stacking, and a collapsible Deneb panel ([#2992](https://github.com/choiceoh/Deneb/issues/2992)) ([0dcf730](https://github.com/choiceoh/Deneb/commit/0dcf7305706aba3525722b3bf142090c6aef5291))

## [0.0.40](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.39...andromeda-v0.0.40) (2026-06-30)


### ✨ Features

* **andromeda:** split the notebook top into a source list + detail pane ([#2989](https://github.com/choiceoh/Deneb/issues/2989)) ([e66e395](https://github.com/choiceoh/Deneb/commit/e66e39554f71cf4bcd9fb60c2989c11a3b1be633))

## [0.0.39](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.38...andromeda-v0.0.39) (2026-06-30)


### ✨ Features

* **andromeda:** dock the Deneb chat at the bottom on the notebook view ([#2986](https://github.com/choiceoh/Deneb/issues/2986)) ([7b1c707](https://github.com/choiceoh/Deneb/commit/7b1c707ba5a41abc9d24a9de7278649db0ecd705))

## [0.0.38](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.37...andromeda-v0.0.38) (2026-06-30)


### ✨ Features

* **andromeda:** rework the notebook pane around its actual workflow ([#2979](https://github.com/choiceoh/Deneb/issues/2979)) ([db21253](https://github.com/choiceoh/Deneb/commit/db212534528c13ade24e3783b43a0535475c6184))
* **andromeda:** 작업피드 행을 제목만 표기 (미리보기 줄 제거) ([#2981](https://github.com/choiceoh/Deneb/issues/2981)) ([14f2f56](https://github.com/choiceoh/Deneb/commit/14f2f567dad0c77cabba69ffc0e3885469eedd11))
* **calendar:** 일정 날짜·시간 입력 개선 (기본값·종료자동·칸분리·길이버튼) ([#2980](https://github.com/choiceoh/Deneb/issues/2980)) ([ca7db1d](https://github.com/choiceoh/Deneb/commit/ca7db1d099d412614cef2d595f8c82be8c9aad4f))
* **mail:** 메일 AI 분석 카드 접기/펼치기 토글 ([#2983](https://github.com/choiceoh/Deneb/issues/2983)) ([dbff334](https://github.com/choiceoh/Deneb/commit/dbff3344e6bc38e17705664fc3027fabc12a06aa))


### 🐛 Bug Fixes

* **andromeda:** 위키 내용 폭 — 접힘 기준을 뷰포트→워크영역(컨테이너)으로 ([#2977](https://github.com/choiceoh/Deneb/issues/2977)) ([2995c57](https://github.com/choiceoh/Deneb/commit/2995c579ed67f40c652e7c5c08b111042571cddd))

## [0.0.37](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.36...andromeda-v0.0.37) (2026-06-30)


### ✨ Features

* **andromeda:** order the project list by most-recently-updated ([#2976](https://github.com/choiceoh/Deneb/issues/2976)) ([5e258df](https://github.com/choiceoh/Deneb/commit/5e258df7d28c466de9496d1cd0ed4a5fc053c0a9))
* **andromeda:** 메일 받은편지함 날짜 페이저 (+ 공유 DayPager, 스킬 패널 리뷰 수정) ([#2975](https://github.com/choiceoh/Deneb/issues/2975)) ([dc42427](https://github.com/choiceoh/Deneb/commit/dc42427a6444d471149330ead7f53821e5f5b2bb))
* **market:** add 시장 card (FX/index/commodities) to the 오늘 dashboard ([#2971](https://github.com/choiceoh/Deneb/issues/2971)) ([0895f29](https://github.com/choiceoh/Deneb/commit/0895f29edb3dc3fb94ff8c09c8e0d5c77b868dd2))


### 🔧 Internal

* **workfeed:** 데스크톱 "작업피드" 표시 라벨을 "피드"로 변경 ([#2974](https://github.com/choiceoh/Deneb/issues/2974)) ([017b9d6](https://github.com/choiceoh/Deneb/commit/017b9d629a64c60daba52fcd28128166a845c5c8))

## [0.0.36](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.35...andromeda-v0.0.36) (2026-06-30)


### ✨ Features

* **andromeda:** 데스크탑에 스킬 패널 추가 (목록·상세·Propus 로그) ([#2966](https://github.com/choiceoh/Deneb/issues/2966)) ([cf52556](https://github.com/choiceoh/Deneb/commit/cf52556eaf52b7c529ca8ebb3f3ab2f0da6af069))
* **workfeed:** 작업피드 AI 분석 본문 기본 전체 펼침 + 접기 토글 ([#2970](https://github.com/choiceoh/Deneb/issues/2970)) ([7c25f50](https://github.com/choiceoh/Deneb/commit/7c25f50682ef4d2c4cd120953aee6d8a9597273b))

## [0.0.35](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.34...andromeda-v0.0.35) (2026-06-30)


### 🐛 Bug Fixes

* **andromeda:** deneb-ui 카드가 text 필드·문자열 list 항목도 렌더 — 모닝레터 깨짐 수정 ([#2961](https://github.com/choiceoh/Deneb/issues/2961)) ([64cbdb1](https://github.com/choiceoh/Deneb/commit/64cbdb180f2e5d621d66c709fb5db2b0f7d2db6d))
* **mail:** 상세 응답에 isUnread 추가 — 리스트 밖에서 연 메일도 자동 읽음 처리 ([#2963](https://github.com/choiceoh/Deneb/issues/2963)) ([abb4611](https://github.com/choiceoh/Deneb/commit/abb461123babe1d62ec013d77dd73b78299901de))
* **project:** 프로젝트 화면 단일 열 레이아웃 + 가독성 개선 ([#2965](https://github.com/choiceoh/Deneb/issues/2965)) ([344d4a4](https://github.com/choiceoh/Deneb/commit/344d4a47d93e25a31558b6f952c210b8018bb89e))

## [0.0.34](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.33...andromeda-v0.0.34) (2026-06-30)


### 🐛 Bug Fixes

* **calendar:** 인접월 일정이 월 목록에 새는 문제 (7월 일정이 6월 목록에 표시) ([#2958](https://github.com/choiceoh/Deneb/issues/2958)) ([b3dd4eb](https://github.com/choiceoh/Deneb/commit/b3dd4eb17486b36b8362f18734f1d0ab80e2d0ec))

## [0.0.33](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.32...andromeda-v0.0.33) (2026-06-30)


### ✨ Features

* **code:** 코딩 세션 닫기(보관) — 워크트리 보존하며 목록에서 치움 ([#2955](https://github.com/choiceoh/Deneb/issues/2955)) ([670213f](https://github.com/choiceoh/Deneb/commit/670213f24145028959308d1da78a9a06c48b6338))


### 🔧 Internal

* **code:** 새 작업을 우측 폼에서 왼쪽 버튼 모달로 이동 ([#2953](https://github.com/choiceoh/Deneb/issues/2953)) ([25dacec](https://github.com/choiceoh/Deneb/commit/25dacece01fee8610d2cb0a96a1cacbd9feaf31e))

## [0.0.32](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.31...andromeda-v0.0.32) (2026-06-29)


### ✨ Features

* **code:** 작업 상세에 PR 결과 링크 (miniapp.code.pr) ([#2947](https://github.com/choiceoh/Deneb/issues/2947)) ([f11db78](https://github.com/choiceoh/Deneb/commit/f11db78cdce62002aa8d9ea2c89102da804e320f))
* **code:** 코드 모드 우측에 작업 상세 패널 (진행 기록·검증) ([#2945](https://github.com/choiceoh/Deneb/issues/2945)) ([cc37428](https://github.com/choiceoh/Deneb/commit/cc3742818b0e6e28d9e65db3d1d69b9ef1374671))

## [0.0.31](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.30...andromeda-v0.0.31) (2026-06-29)


### ✨ Features

* **code:** 코드 모드 세션 상태 점 (진행중 초록/멈춤 검정/문제 빨강) ([#2942](https://github.com/choiceoh/Deneb/issues/2942)) ([bd79441](https://github.com/choiceoh/Deneb/commit/bd794412ea106e61dcdba9f86a2b660de152745e))

## [0.0.30](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.29...andromeda-v0.0.30) (2026-06-29)


### ✨ Features

* **code:** 코딩 모드 새 작업에서 작업 ID·제목 자동 생성 (입력 칸 제거) ([#2937](https://github.com/choiceoh/Deneb/issues/2937)) ([cc486e1](https://github.com/choiceoh/Deneb/commit/cc486e18ddab6c37f4b501ff61680d308c11113f))


### 🐛 Bug Fixes

* **andromeda:** sync work feed on proactive nudges + durable catch-up (작업 피드 동기화) ([#2940](https://github.com/choiceoh/Deneb/issues/2940)) ([4150e3c](https://github.com/choiceoh/Deneb/commit/4150e3c81fa44871a6384de075c9ba6ba947be8d))


### 🔧 Internal

* **code:** 코드 모드 우측 패널에서 중복 세션 목록 제거 ([#2939](https://github.com/choiceoh/Deneb/issues/2939)) ([a4fd8ba](https://github.com/choiceoh/Deneb/commit/a4fd8ba3c5dee6ce5d1dbb84659be3253c71cfe1))

## [0.0.29](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.28...andromeda-v0.0.29) (2026-06-28)


### ✨ Features

* **andromeda:** coding mode center-chat layout (중앙 코딩 채팅) ([#2935](https://github.com/choiceoh/Deneb/issues/2935)) ([0df3259](https://github.com/choiceoh/Deneb/commit/0df325993785e978234873d67ad69a3f55cd5ad7))
* **andromeda:** wire the chat to coding sessions (코딩 모드 연결) ([#2934](https://github.com/choiceoh/Deneb/issues/2934)) ([5a14437](https://github.com/choiceoh/Deneb/commit/5a14437afcccf02cca7cb539c0fd367586478c41))
* **code:** coding mode autonomous lifecycle — 완전 자동 (no manual buttons) ([#2936](https://github.com/choiceoh/Deneb/issues/2936)) ([df2fe5c](https://github.com/choiceoh/Deneb/commit/df2fe5cf790ef33442e5b7969663d984d7aed6dc))

## [0.0.28](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.27...andromeda-v0.0.28) (2026-06-28)


### ✨ Features

* **code:** git-worktree 바이브코딩 모드 — 게이트웨이 엔진 + Andromeda UI ([#2930](https://github.com/choiceoh/Deneb/issues/2930)) ([e0d25d0](https://github.com/choiceoh/Deneb/commit/e0d25d074882315f7aa73d8d4ed4a6b5b55021f6))

## [0.0.27](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.26...andromeda-v0.0.27) (2026-06-27)


### ✨ Features

* **project:** server-side project↔item matching via miniapp.project.linked ([#2905](https://github.com/choiceoh/Deneb/issues/2905)) ([278c768](https://github.com/choiceoh/Deneb/commit/278c768d55e60672597148b8a5ec3703b96f016e))

## [0.0.26](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.25...andromeda-v0.0.26) (2026-06-26)


### ✨ Features

* **notebook:** stamp resolved project refs at mail ingestion (각인) ([#2895](https://github.com/choiceoh/Deneb/issues/2895)) ([e4f36a2](https://github.com/choiceoh/Deneb/commit/e4f36a2022b945c8bea9917a32339c49ef2f2801))
* **project:** resolve owned pages server-side via the wiki graph (③ 서버측 매칭) ([#2899](https://github.com/choiceoh/Deneb/issues/2899)) ([b626c08](https://github.com/choiceoh/Deneb/commit/b626c0864b832bd2256471787dfaebf9a3a2e2ea))

## [0.0.25](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.24...andromeda-v0.0.25) (2026-06-26)


### ✨ Features

* **project:** ship frozen code in digest so 프로젝트 코너 matches items by code ([#2894](https://github.com/choiceoh/Deneb/issues/2894)) ([8e147e4](https://github.com/choiceoh/Deneb/commit/8e147e400037f34240cd837d3554a324a38862d1))

## [0.0.24](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.23...andromeda-v0.0.24) (2026-06-26)


### ✨ Features

* **andromeda:** 작업피드 액션 제거·정정 하단 이동으로 본문 와이드화 ([#2875](https://github.com/choiceoh/Deneb/issues/2875)) ([5b8aba4](https://github.com/choiceoh/Deneb/commit/5b8aba411315b872fe9293b624ad53951782ac8c))
* **exec:** 파괴적 명령 차단 (rm -rf /·디스크 포맷·fork bomb) + 노트북 set_mode RPC ([#2876](https://github.com/choiceoh/Deneb/issues/2876)) ([8d38778](https://github.com/choiceoh/Deneb/commit/8d38778e198032d37e526b066e20b8f56428f6e8))

## [0.0.23](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.22...andromeda-v0.0.23) (2026-06-24)


### ✨ Features

* **push:** proactive 알림 딥링크 타깃(kind+ref) + 데스크탑 클릭스루 ([#2869](https://github.com/choiceoh/Deneb/issues/2869)) ([4406432](https://github.com/choiceoh/Deneb/commit/44064326ca3f578503d743da475daf698613dec3))

## [0.0.22](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.21...andromeda-v0.0.22) (2026-06-24)


### ✨ Features

* **andromeda:** 능동 알림 패널 개선 + 로드 실패 가시화 ([#2867](https://github.com/choiceoh/Deneb/issues/2867)) ([eb945da](https://github.com/choiceoh/Deneb/commit/eb945dac33fbeec2367c882c595afb4b5fc5a6f3))
* **workfeed:** 작업피드 읽음 상태 — 게이트웨이 read RPC + andromeda 표시 ([#2865](https://github.com/choiceoh/Deneb/issues/2865)) ([4ac67cd](https://github.com/choiceoh/Deneb/commit/4ac67cd0634edef341d26f3efe0fe0835663dbb1))

## [0.0.21](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.20...andromeda-v0.0.21) (2026-06-24)


### ✨ Features

* **andromeda:** 작업피드를 날짜별 페이저로 (전날/다음날 이동) ([#2861](https://github.com/choiceoh/Deneb/issues/2861)) ([71f3d27](https://github.com/choiceoh/Deneb/commit/71f3d272f8710988c214735dbe8c2c4beaf3afa2))


### 🐛 Bug Fixes

* **andromeda:** clear mail unread state on open ([#2864](https://github.com/choiceoh/Deneb/issues/2864)) ([006e45c](https://github.com/choiceoh/Deneb/commit/006e45cafe3ec712a6b80c91fd953d31674f78e7))
* **andromeda:** require explicit project links on project home ([c2b3eee](https://github.com/choiceoh/Deneb/commit/c2b3eee0a6425b2b377bae09a89190f6dccfee71))

## [0.0.20](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.19...andromeda-v0.0.20) (2026-06-24)


### ✨ Features

* **andromeda:** add project home pane ([#2858](https://github.com/choiceoh/Deneb/issues/2858)) ([5f8e64e](https://github.com/choiceoh/Deneb/commit/5f8e64ee69cefe8f0ab84f71753f1f0305b07f05))
* **andromeda:** complete notebook source management ([c0f58b3](https://github.com/choiceoh/Deneb/commit/c0f58b31debb11ef2a5523655e79a28909843e39))
* **andromeda:** improve fleet usability ([#2859](https://github.com/choiceoh/Deneb/issues/2859)) ([387991e](https://github.com/choiceoh/Deneb/commit/387991e18458d23729b6681b6f0c0b43e25aaad0))
* **andromeda:** 메일 상세 — 발신자 카드 기본 접힘 + AI 분석 본문 위로 ([#2857](https://github.com/choiceoh/Deneb/issues/2857)) ([f045681](https://github.com/choiceoh/Deneb/commit/f04568132552cdd224f6441db9429edb65098d9c))
* **andromeda:** 작업피드를 날짜별 그룹으로 표시 ([#2856](https://github.com/choiceoh/Deneb/issues/2856)) ([b6453a9](https://github.com/choiceoh/Deneb/commit/b6453a9a6578143088f54663157ace7ed4397b48))

## [0.0.19](https://github.com/choiceoh/Deneb/compare/andromeda-v0.0.18...andromeda-v0.0.19) (2026-06-24)


### ✨ Features

* **andromeda:** improve chat attachment rendering ([#2851](https://github.com/choiceoh/Deneb/issues/2851)) ([210c98c](https://github.com/choiceoh/Deneb/commit/210c98c940e20e2e65d7576b281d8a721105febb))
* cache direct rpc panes ([fc91966](https://github.com/choiceoh/Deneb/commit/fc9196688c78858cd5fbd6ee30d44084168cbed7))

## [0.0.18](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.17...andromeda-v0.0.18) (2026-06-24)


### Features

* improve chat attachment rendering ([#135](https://github.com/choiceoh/andromeda/issues/135)) ([7858ddd](https://github.com/choiceoh/andromeda/commit/7858ddd9e7c7e0825a52f6ed940e2a4fefd88fae))


### Bug Fixes

* Deneb 응답중 별을 데네브 청백색 반짝임으로 다듬기 ([#133](https://github.com/choiceoh/andromeda/issues/133)) ([14159b1](https://github.com/choiceoh/andromeda/commit/14159b1b398ffe503e6b6de030cd49b0423724e8))
* 메일 AI분석 에러가 재연결/설정변경 시 안 지워지던 회귀 수정 ([#130](https://github.com/choiceoh/andromeda/issues/130)) ([05b5e4a](https://github.com/choiceoh/andromeda/commit/05b5e4a3359fb80c403348c712ab56b521eead94))

## [0.0.17](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.16...andromeda-v0.0.17) (2026-06-24)


### Features

* 위키 페이지 이동을 폴더 클릭 선택으로 변경 ([#123](https://github.com/choiceoh/andromeda/issues/123)) ([33a7198](https://github.com/choiceoh/andromeda/commit/33a7198376f3d08554924d22e41477db4f429cd3))

## [0.0.16](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.15...andromeda-v0.0.16) (2026-06-24)


### Features

* improve capture and workfeed bridge actions ([#115](https://github.com/choiceoh/andromeda/issues/115)) ([d0c2a8a](https://github.com/choiceoh/andromeda/commit/d0c2a8af0474f4e7ec2edf661cc98d5cb4c82453))
* 게이트웨이 계약 codegen — //deneb:wire → src/gen/miniappWire.ts ([#116](https://github.com/choiceoh/andromeda/issues/116)) ([434761f](https://github.com/choiceoh/andromeda/commit/434761fdcade4b1c1bb232da4e148ab895c1cf13))
* 노트북 인용자료에 위키 페이지 소스 추가 (note + wiki) ([#110](https://github.com/choiceoh/andromeda/issues/110)) ([846bc4c](https://github.com/choiceoh/andromeda/commit/846bc4c6b3a4b0452e5499c2749e8f36a85400bc))
* 채팅 탭 개선 — 컴포저 높이 버그·여러 대화·목록 갱신·자동 포커스 ([#109](https://github.com/choiceoh/andromeda/issues/109)) ([3854d23](https://github.com/choiceoh/andromeda/commit/3854d23b2c828cfc8d97bc1bfabd7162e3023c9e))
* 채팅 탭 파일 첨부 — 이미지 OCR·음성 전사·문서 추출 (miniapp.capture.*) ([#111](https://github.com/choiceoh/andromeda/issues/111)) ([bc38531](https://github.com/choiceoh/andromeda/commit/bc38531b5a19c3f63ff0abf63ecebcdde04a7238))
* 채팅 탭 폴리싱 — 스크롤-투-바텀·방향성 등장·인사말·포커스 글로우 ([#113](https://github.com/choiceoh/andromeda/issues/113)) ([10aab1d](https://github.com/choiceoh/andromeda/commit/10aab1d951ed96d315c6f0b512e3639fa4efd55f))
* 채팅 탭 항상 마운트 — 탭 전환에도 대화 유지 ([#106](https://github.com/choiceoh/andromeda/issues/106)) ([7506025](https://github.com/choiceoh/andromeda/commit/7506025ae3db96a695ae49fb082d9619fc799534))


### Bug Fixes

* align implemented panes with gateway contracts ([#112](https://github.com/choiceoh/andromeda/issues/112)) ([24c0114](https://github.com/choiceoh/andromeda/commit/24c01146a702cf3a61f03849682baccb154ab06e))

## [0.0.15](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.14...andromeda-v0.0.15) (2026-06-23)


### Features

* 비업무 채팅 탭 — 중앙 채팅 컬럼 + 우측 세션 목록 (네이티브 챗봇 대응) ([#102](https://github.com/choiceoh/andromeda/issues/102)) ([052bc8e](https://github.com/choiceoh/andromeda/commit/052bc8e1e475afb64d5b4f1e02fa1bb0027ff781))

## [0.0.14](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.13...andromeda-v0.0.14) (2026-06-23)


### Features

* 데네브 별 "응답 중" 인디케이터 — 네이티브 StarIndicator 포팅 ([#94](https://github.com/choiceoh/andromeda/issues/94)) ([37ec100](https://github.com/choiceoh/andromeda/commit/37ec100beb713704e8db333620109807e6bac934))


### Bug Fixes

* 업데이터 결과를 구분해 설정 UI에 정확한 메시지 표시 ([#96](https://github.com/choiceoh/andromeda/issues/96)) ([5a9e303](https://github.com/choiceoh/andromeda/commit/5a9e303f119da18749e84a0fefee69565cbc5440))
* 프레임리스 창 모서리 라운딩 (투명창 + macOSPrivateApi) ([#97](https://github.com/choiceoh/andromeda/issues/97)) ([4f87929](https://github.com/choiceoh/andromeda/commit/4f87929dcf27fc8f148dd1db285c09d81b09844b))

## [0.0.13](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.12...andromeda-v0.0.13) (2026-06-23)


### Features

* 메일 본문에서 서명·인용·광고 chrome 제거 (Deneb 게이트웨이 포팅) ([#91](https://github.com/choiceoh/andromeda/issues/91)) ([57621bc](https://github.com/choiceoh/andromeda/commit/57621bce886034a31f57987fd5a1ba2932e8e3e8))
* 애니메이션 폴리싱 2차 — 패널 전환 rise·thinking breathing·알림/드로어 등장 ([#92](https://github.com/choiceoh/andromeda/issues/92)) ([8efd748](https://github.com/choiceoh/andromeda/commit/8efd748b9d9c62d41ff6ebdd13a3e678769654b1))
* 오늘 대시보드 섹션 카탈로그 확장 (진행·연락처·크론) ([#90](https://github.com/choiceoh/andromeda/issues/90)) ([63a7da5](https://github.com/choiceoh/andromeda/commit/63a7da574ad9ca44385d4a2983c84957446cdde7))


### Bug Fixes

* Windows 빈 아이콘 — icon.ico를 BMP 항목 형식으로 재생성 ([#86](https://github.com/choiceoh/andromeda/issues/86)) ([64471ba](https://github.com/choiceoh/andromeda/commit/64471bac83b7ee0cd971d5b2f2609dcc1f360fcb))
* 일정 하단 패널 좌우폭을 상단 컬럼에 정렬 ([#93](https://github.com/choiceoh/andromeda/issues/93)) ([c6fac3e](https://github.com/choiceoh/andromeda/commit/c6fac3ed6f7ce0e1de255ad009d72ee8b79fe89b))

## [0.0.12](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.11...andromeda-v0.0.12) (2026-06-23)


### Features

* cache direct rpc panes ([e74139d](https://github.com/choiceoh/andromeda/commit/e74139dfa0eba71607dedb4606fb7c0aa9479a6b))
* cache direct rpc panes ([#78](https://github.com/choiceoh/andromeda/issues/78)) ([e74139d](https://github.com/choiceoh/andromeda/commit/e74139dfa0eba71607dedb4606fb7c0aa9479a6b))
* 설정 폴리싱 — 탭 키보드 내비·전환감·hover 피드백 보강 ([#84](https://github.com/choiceoh/andromeda/issues/84)) ([bda0cfa](https://github.com/choiceoh/andromeda/commit/bda0cfab682508ce1b767cb96cd285a403056e54))
* 설정에서 좌측 탭 순서 변경(▲▼) ([#75](https://github.com/choiceoh/andromeda/issues/75)) ([b73a24d](https://github.com/choiceoh/andromeda/commit/b73a24d0a1ad608eba612c50e88493ecec201e51))
* 설정을 탭으로 분할하고 작업 영역 폭을 채움 ([#80](https://github.com/choiceoh/andromeda/issues/80)) ([ecd31c4](https://github.com/choiceoh/andromeda/commit/ecd31c4f7083e8e43a1e4bf48c0257eddd850338))
* 애니메이션 폴리싱 강화 — 패널 전환·AI 턴/툴칩 등장·이징 통일 ([#82](https://github.com/choiceoh/andromeda/issues/82)) ([59d8093](https://github.com/choiceoh/andromeda/commit/59d80938f08dfcdbb41cba1c859cac3b48333392))
* 오늘 대시보드 사용자 커스텀 — 섹션 표시/숨김 + 순서 (인라인 편집) ([#85](https://github.com/choiceoh/andromeda/issues/85)) ([3f86e57](https://github.com/choiceoh/andromeda/commit/3f86e5781a3b4a196de562578f14f0e6521d3fff))
* 일정 달력 재설계 — 좌우폭 축소 + 우측 아젠다, borderless 그리드 ([#79](https://github.com/choiceoh/andromeda/issues/79)) ([4c73f7e](https://github.com/choiceoh/andromeda/commit/4c73f7e8f66bffccc7261ce9244ada3e826a41ae))
* 좌하단을 설정 아이콘으로 — 사이드바 게이트웨이 IP/연결 버튼 제거 ([#77](https://github.com/choiceoh/andromeda/issues/77)) ([4f74f7c](https://github.com/choiceoh/andromeda/commit/4f74f7c1a03f527222808abb5c24b77e24de3971))
* 채팅 입력창을 통합 컴포저로 — 자동 높이·버튼 내장 ([#76](https://github.com/choiceoh/andromeda/issues/76)) ([b9c531b](https://github.com/choiceoh/andromeda/commit/b9c531b5a9092e7c9c6c18a68ddd03d7baeec387))
* 채팅에서 AI가 능동적으로 UI를 그려 답변 (deneb-ui) ([#81](https://github.com/choiceoh/andromeda/issues/81)) ([32325a4](https://github.com/choiceoh/andromeda/commit/32325a438db67be588368e0368890f4edd79e991))
* 통합 검색을 구글식 중앙 정렬 → 검색 시 상단으로 ([#73](https://github.com/choiceoh/andromeda/issues/73)) ([609e249](https://github.com/choiceoh/andromeda/commit/609e249ec9aea4a05b6e01415cb3c61a416c5198))

## [0.0.11](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.10...andromeda-v0.0.11) (2026-06-23)


### Features

* add mail attachments wiki browse and files ([#68](https://github.com/choiceoh/andromeda/issues/68)) ([48be8c6](https://github.com/choiceoh/andromeda/commit/48be8c6ca397a45e5885c90bfff28d514892f6f5))
* 메일 보낸이 이름만 표시, 목록 인라인 삭제 제거 ([#69](https://github.com/choiceoh/andromeda/issues/69)) ([ee9e8d3](https://github.com/choiceoh/andromeda/commit/ee9e8d3f61ad7dca3971b62bc21d08d0e86a696d))
* 메일·시간 표시 개선 — 24시간제, 목록 제목만, 최근 메일 상대 시간 ([#60](https://github.com/choiceoh/andromeda/issues/60)) ([76e4624](https://github.com/choiceoh/andromeda/commit/76e46240494dbeb75a6b9519b87cea7b9ef24880))
* 문서 → 노트북(LM) — Deneb 거래 노트북 열람 + 근거 기반 AI Q&A ([#70](https://github.com/choiceoh/andromeda/issues/70)) ([b492a43](https://github.com/choiceoh/andromeda/commit/b492a430013855b34e2d845dc1c3788e0b77a9df))
* 설정에서 좌측 탭 표시 항목 켜고 끄기 ([#64](https://github.com/choiceoh/andromeda/issues/64)) ([a689493](https://github.com/choiceoh/andromeda/commit/a689493cc9dc7102ddd4cef23931be52690ad3a7))
* 할일 추가 모달(+버튼) · 메인 패널 폭 적응형 ([#71](https://github.com/choiceoh/andromeda/issues/71)) ([b8ce0ba](https://github.com/choiceoh/andromeda/commit/b8ce0baa582a2994c4239f36014ae7c2e99aa928))


### Bug Fixes

* 워크피드 답변을 실제 RPC로 교체 + 재생성/정정 추가 ([#66](https://github.com/choiceoh/andromeda/issues/66)) ([032fe5d](https://github.com/choiceoh/andromeda/commit/032fe5dd15552cf0c7dd8e96fe7de3a0d1cfcd8d))
* 위키를 열면 바로 카테고리/페이지 목록 표시 (검색 강요 제거) ([#63](https://github.com/choiceoh/andromeda/issues/63)) ([e28bd13](https://github.com/choiceoh/andromeda/commit/e28bd13daa22b71813db14e7907509ffd67cf742))

## [0.0.10](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.9...andromeda-v0.0.10) (2026-06-23)


### Features

* AI 패널 퀵액션 칩 제거 (우선순위/요약/후속조치) ([#59](https://github.com/choiceoh/andromeda/issues/59)) ([4fc24bd](https://github.com/choiceoh/andromeda/commit/4fc24bd8f2ad769723cd6e3be9ea3c9cb3992402))
* compact calendar markers and inline analysis ([#57](https://github.com/choiceoh/andromeda/issues/57)) ([ffbdaab](https://github.com/choiceoh/andromeda/commit/ffbdaabb052ee28de30ad442f73c618ffa678734))
* 오늘 대시보드 가독성·배치 개선 ([#54](https://github.com/choiceoh/andromeda/issues/54)) ([c625778](https://github.com/choiceoh/andromeda/commit/c625778310b1199e12db928ea47be228bd955ed8))

## [0.0.9](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.8...andromeda-v0.0.9) (2026-06-23)


### Features

* add dashboard deeplinks and cache refresh ([fe8c27a](https://github.com/choiceoh/andromeda/commit/fe8c27aed9d3dd26704e4cd69b35e82cd253e9fd))
* 마크다운 렌더러 네이티브 수준으로 개선 (GFM) ([#44](https://github.com/choiceoh/andromeda/issues/44)) ([9a66291](https://github.com/choiceoh/andromeda/commit/9a66291b98190fe37fa29c03d65c272e1a39c5b3))
* 마크다운 수식 렌더링 — KaTeX 도입 ([#48](https://github.com/choiceoh/andromeda/issues/48)) ([1119ddd](https://github.com/choiceoh/andromeda/commit/1119ddd787e16dcc74199f2ce357d8f325bbd8f7))
* 메일 상세 심화 — AI 분석·발신자 컨텍스트·질문·액션 (Phase B) ([#52](https://github.com/choiceoh/andromeda/issues/52)) ([c4853cd](https://github.com/choiceoh/andromeda/commit/c4853cd650fc8993a3091ee0bd140c77e690a0f7))
* 캘린더 현재 달 리스트에서 이미 지난 일정 숨김 ([#49](https://github.com/choiceoh/andromeda/issues/49)) ([2170a88](https://github.com/choiceoh/andromeda/commit/2170a885fd324e13ca6397ae11602a5dcb42f138))

## [0.0.8](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.7...andromeda-v0.0.8) (2026-06-23)


### Features

* AI 챗 패널 네이티브화 + 마크다운 렌더러 공용화 ([#37](https://github.com/choiceoh/andromeda/issues/37)) ([71cd687](https://github.com/choiceoh/andromeda/commit/71cd68703ca41a5f11254ac4e660e92f7cd51391))
* load calendar ranges and persist docs ([#40](https://github.com/choiceoh/andromeda/issues/40)) ([5a7e699](https://github.com/choiceoh/andromeda/commit/5a7e699cffc39be3ecc1c60477931897bc9ce7de))
* 달력 높이 축소 + 날짜 클릭으로 해당 날 일정 필터 ([#38](https://github.com/choiceoh/andromeda/issues/38)) ([bea8ed5](https://github.com/choiceoh/andromeda/commit/bea8ed588d71bd127ef23f993f2108b9d1373c45))

## [0.0.7](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.6...andromeda-v0.0.7) (2026-06-23)


### Features

* expand mail detail inline ([#33](https://github.com/choiceoh/andromeda/issues/33)) ([6faaefa](https://github.com/choiceoh/andromeda/commit/6faaefa6ab9aa180881b87f4d135163b81b2933e))
* 설정 화면 추가 (게이트웨이·로그 레벨·버전) ([#31](https://github.com/choiceoh/andromeda/issues/31)) ([6ccbd0c](https://github.com/choiceoh/andromeda/commit/6ccbd0cb089c7bb97f46ea2d400ea1b969d670a7))


### Bug Fixes

* 스크롤바를 warm Zen에 맞게 — hover 시 등장 + 창 스크롤바 제거 ([#34](https://github.com/choiceoh/andromeda/issues/34)) ([2e803f4](https://github.com/choiceoh/andromeda/commit/2e803f463dd32bff2a488c00f10464b49645c38e))

## [0.0.6](https://github.com/choiceoh/andromeda/compare/andromeda-v0.0.5...andromeda-v0.0.6) (2026-06-23)


### Features

* add mail reading detail view ([#19](https://github.com/choiceoh/andromeda/issues/19)) ([67cbf18](https://github.com/choiceoh/andromeda/commit/67cbf18d15f44c7a67c249e72a87c2326e53c6f7))
* cache mail and calendar lists ([#22](https://github.com/choiceoh/andromeda/issues/22)) ([5b33fc2](https://github.com/choiceoh/andromeda/commit/5b33fc210304df934d942aedb8b22db460966b3b))
* cache mail detail reads ([#26](https://github.com/choiceoh/andromeda/issues/26)) ([50ad297](https://github.com/choiceoh/andromeda/commit/50ad29791f196b831968aec4edac358368ccd725))
* improve Deneb AI collaboration panel ([#18](https://github.com/choiceoh/andromeda/issues/18)) ([9081adb](https://github.com/choiceoh/andromeda/commit/9081adb451cea6054373a3ed8a0029cc9e1b80e4))
* UI·UX 디자인 문서 + 패널 부유감 강화 (warm Zen) ([#21](https://github.com/choiceoh/andromeda/issues/21)) ([8767b72](https://github.com/choiceoh/andromeda/commit/8767b72d6d348c9798e45824f894ec3533f45da2))
* 기능 구현 깊이 개선 — 상세·편집 모달, 쓰기 UI, 액션 보강 ([#24](https://github.com/choiceoh/andromeda/issues/24)) ([7da51dd](https://github.com/choiceoh/andromeda/commit/7da51dd6c3993809f0bd48c5c8aac4f8d85d82ab))
* 일정 패널에 월간 달력 뷰 추가 ([#20](https://github.com/choiceoh/andromeda/issues/20)) ([2d9d14c](https://github.com/choiceoh/andromeda/commit/2d9d14c681472281b17a44a80ac3917f62f4c277))
* 프레임리스 창 + 좌상단 창 컨트롤 (타이틀바 제거) ([#29](https://github.com/choiceoh/andromeda/issues/29)) ([bcb8942](https://github.com/choiceoh/andromeda/commit/bcb8942996cb4c7b47221acc4579a89fd32e550e))
* 프로젝트 진행상황 패널 (Deneb project.digests 연동) ([#23](https://github.com/choiceoh/andromeda/issues/23)) ([1f991c1](https://github.com/choiceoh/andromeda/commit/1f991c19859d903431c1962f7d20b03cbfbbe328))
