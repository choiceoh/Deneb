# Changelog

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
