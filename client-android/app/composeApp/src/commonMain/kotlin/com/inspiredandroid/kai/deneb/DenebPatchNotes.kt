package com.inspiredandroid.kai.deneb

// Compiled-in patch notes — the record of what each native-client build shipped.
//
// This is deliberately separate from [UpdateManifest.notes]: that field describes a
// *newer* build fetched from the gateway's version.json and only appears when an
// update is available. These notes are the changelog for builds up to and including
// the one the user is running, so they work offline and survive the gateway being
// down. The settings "버전" card surfaces them on demand (no auto-popup).
//
// Newest first. The head entry must match [DENEB_VERSION_NAME] / [DENEB_VERSION_CODE].
// When you bump the version in DenebUpdate.kt + libs.versions.toml for a new APK,
// prepend a matching entry here so the running build can describe itself.

/** One released build and the user-facing highlights it introduced. */
data class DenebPatchNote(
    val version: String,
    val code: Int,
    val highlights: List<String>,
)

val DENEB_PATCH_NOTES: List<DenebPatchNote> = listOf(
    DenebPatchNote(
        version = "2.9.17",
        code = 140,
        highlights = listOf(
            "설정 모델 탭에 응답 상태 색상 점 — 초록=응답 가능, 빨강=응답 없음, 노랑=미확인 (채움=현재 선택)",
        ),
    ),
    DenebPatchNote(
        version = "2.9.16",
        code = 139,
        highlights = listOf(
            "모델 전환기에 모델별 실제 브랜드 아이콘(흑백) — Claude·GPT·Gemini·Kimi·DeepSeek 등을 한눈에 구분",
        ),
    ),
    DenebPatchNote(
        version = "2.9.15",
        code = 138,
        highlights = listOf(
            "토픽 전환을 오른쪽 드로어로 — 상단바 해시태그(#)를 누르면 업무·잡담·코딩을 한눈에 보고 고를 수 있어요",
        ),
    ),
    DenebPatchNote(
        version = "2.9.14",
        code = 137,
        highlights = listOf(
            "알림 주입 방식 선택 — 도착 즉시 자동 주입(기본)과 탭해서 보내는 수동 주입을 설정에서 전환",
        ),
    ),
    DenebPatchNote(
        version = "2.9.13",
        code = 136,
        highlights = listOf(
            "캡처한 알림(카톡·메일 등)을 즉시 처리 — 60초 폴링 대기 없이 바로 트리아지",
        ),
    ),
    DenebPatchNote(
        version = "2.9.12",
        code = 135,
        highlights = listOf(
            "능동 알림을 탭하면 보고가 있는 업무 토픽으로 바로 이동",
        ),
    ),
    DenebPatchNote(
        version = "2.9.11",
        code = 134,
        highlights = listOf(
            "알림 캡처 — 설정에서 받을 앱을 직접 고를 수 있어요(비우면 전체)",
        ),
    ),
    DenebPatchNote(
        version = "2.9.10",
        code = 133,
        highlights = listOf(
            "능동 알림 — 모닝레터·메일분석을 게이트웨이가 만든 즉시 푸시(주기 대기 없이)",
        ),
    ),
    DenebPatchNote(
        version = "2.9.9",
        code = 132,
        highlights = listOf(
            "버그 수정 — 답변을 위키에 기록할 때 스트리밍된 본문이 사라지던 문제",
        ),
    ),
    DenebPatchNote(
        version = "2.9.8",
        code = 131,
        highlights = listOf(
            "재생성(regen) 버튼 수정 — 마지막 답변을 다시 생성하도록 동작",
        ),
    ),
    DenebPatchNote(
        version = "2.9.7",
        code = 130,
        highlights = listOf(
            "모닝레터·메일분석이 업무 토픽에도 표시 (텔레그램과 함께)",
            "좌측 드로어를 미니앱식 타이포 메뉴로 정리",
        ),
    ),
    DenebPatchNote(
        version = "2.9.6",
        code = 129,
        highlights = listOf(
            "접근성 — 입력바 아이콘(보내기·중지·첨부)에 TalkBack 라벨",
            "설정 탭 목록(사람·크론·토픽문서)도 부드럽게 등장하는 모션",
        ),
    ),
    DenebPatchNote(
        version = "2.9.5",
        code = 128,
        highlights = listOf(
            "유지보수 빌드 — 최신 변경 반영 및 안정화",
        ),
    ),
    DenebPatchNote(
        version = "2.9.4",
        code = 127,
        highlights = listOf(
            "답변이 생성될 때 깜빡이는 타이핑 커서 — 스트리밍이 한눈에",
            "드로어·목록·일정·검색 탭에 햅틱 — 손끝 피드백 통일",
            "사람 목록 항목이 부드럽게 나타나는 모션",
        ),
    ),
    DenebPatchNote(
        version = "2.9.3",
        code = 126,
        highlights = listOf(
            "UI 폴리싱 — 브랜드 블루 컬러 일관화, Pretendard 한글 자간 정리",
            "불러올 때 스켈레톤(시머) 표시 — 빈 화면 대신 부드럽게 채워짐",
            "전송·탭에 햅틱, 따뜻해진 오류 카드, 시간대 인사",
            "브랜드 테두리를 잔잔한 오로라 스윕으로 통일",
        ),
    ),
    DenebPatchNote(
        version = "2.7.7",
        code = 120,
        highlights = listOf(
            "음성 캡처 앱 단축키 — 홈 화면 단축키로 바로 말해서 Deneb에 받아쓰기",
            "토픽 전환 버튼(업무·잡담·코딩)과 좌측 내비게이션 드로어",
        ),
    ),
    DenebPatchNote(
        version = "2.7.6",
        code = 119,
        highlights = listOf(
            "이미지 캡처 — 사진·스크린샷을 Deneb에 공유하면 게이트웨이가 OCR로 텍스트를 읽어 처리",
        ),
    ),
    DenebPatchNote(
        version = "2.7.5",
        code = 118,
        highlights = listOf(
            "채팅 응답 토큰 단위 스트리밍 — 답변이 실시간으로 흘러나옴",
            "알림 캡처 탭 — 다른 앱 알림을 읽어와 탭으로 분류·처리",
        ),
    ),
    DenebPatchNote(
        version = "2.7.4",
        code = 117,
        highlights = listOf(
            "공유 시트 캡처 — 다른 앱에서 텍스트를 공유하면 바로 Deneb 채팅으로",
        ),
    ),
    DenebPatchNote(
        version = "2.7.3",
        code = 116,
        highlights = listOf(
            "역할별 모델 선택 — 메인·경량·폴백 모델을 각각 지정",
        ),
    ),
    DenebPatchNote(
        version = "2.7.1",
        code = 114,
        highlights = listOf(
            "크론 상세 화면 — 일정·지시·배달·상태 확인, 활성화·실행·삭제",
        ),
    ),
    DenebPatchNote(
        version = "2.7.0",
        code = 113,
        highlights = listOf(
            "캘린더 심화 + 위키 페이지 메타데이터 편집",
        ),
    ),
)
