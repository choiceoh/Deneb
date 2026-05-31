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
