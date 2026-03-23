# Deneb Changelog

## v3.5.7 — 2026-03-23

프로젝트 문서 전면 최신화.

### Changes

- **프로젝트 문서 최신화** — README, VISION, CONTRIBUTING, SECURITY, CHANGELOG를 현재 v3.5.7 기준으로 전면 갱신
- **Node.js 요구사항 통일** — 최소 22.16.0, 권장 Node 24로 전 문서 일치화
- **코드베이스 규모 갱신** — 실측 기준 ~440K LOC 반영
- **개발 커맨드 갱신** — `pnpm check` (oxlint + oxfmt) 기준으로 통일
- **아키텍처 다이어그램 갱신** — 현재 `src/` 디렉토리 구조 반영 (plugin-sdk, routing, tts, web-search, vega)

## v3.2 — 2026-03-21

ACP (Claude Code) 연동 활성화, 코드 구조 리팩토링.

### Changes

- **ACP/Claude Code 연동** — acpx 플러그인 활성화, `acp.allowedAgents` 설정
- **코드 리팩토링** — 대형 파일에서 9개 전용 모듈 추출 (PR #22)

## v3.0 — 2026-03-21

Deneb 최초 릴리스. 독립 프로젝트로 시작.

### Core Features

- **Aurora Memory Module** — AI-agent-first 메모리 파일 관리 (memory-md-manager)
- **Vega QMD 백엔드 통합** — VegaMemoryManager, LCM 네이티브화
- **Vega CLI 래퍼** — bin/vega wrapper + install.sh
- **Lossless Context Management (LCM)** — DAG-based compaction, background observer, multi-layer recall
- **컨텍스트 엔진** — transcript maintenance 기능
- **Telegram custom apiRoot** 지원

### Improvements

- Telegram 전용 빌드 (미사용 채널 어댑터 제거)
- 에러 리질리언스 계층 추가
- Rolldown 빌드 안정화 (stale .js 정리, clean:true)
- Subagent 타임아웃 시 부분 진행 결과 포함
- JSONL 세션 로그 트렁케이션 (디스크 과다 사용 방지)
- Compaction 후 세션 JSONL 자동 트렁케이션

### Fixes

- Telegram 스트리밍 미리보기 종료 시 message:sent hook 발행
- Telegram dmPolicy pairing 경고
- 시퀀스 갭 브로드캐스트 스킵
- 잘못된 형식의 replay tool call sanitize
- Thread binding 직렬화
- Telegram accountId 누락 시 잘못된 봇 라우팅 방지
