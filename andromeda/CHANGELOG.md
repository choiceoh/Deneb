# Changelog

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
