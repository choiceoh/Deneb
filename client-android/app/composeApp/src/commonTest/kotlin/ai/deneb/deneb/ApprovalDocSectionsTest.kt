package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

class ApprovalDocSectionsTest {
    @Test
    fun splitsMetaLineBodyAttachments() {
        val sample = """
            [그룹웨어 전자결재 · 전체결재문서]
            조회: 99178

            제목: 다과비 품의
            문서번호: DOC-1
            기안: 김승리

            결재선
              1. 김승리 · 차장 · 승인
              2. 차남두 · 부장 · 대기

            본문
            | 항목 | 금액 |
            | --- | --- |
            | 다과 | 10,000 |

            첨부 (2건 · 내용 미열람)
            필요한 파일만 …
            1. 영수증.pdf · 12KB
            2. 견적.xlsx
        """.trimIndent()

        val s = parseApprovalDocBody(sample)
        assertEquals("다과비 품의", s.title)
        assertEquals("김승리", s.drafter)
        assertEquals(2, s.lineCount)
        assertTrue(s.line.contains("김승리"))
        assertTrue(s.body.contains("| 항목 | 금액 |"))
        assertEquals(2, s.attachmentCount)
        assertTrue(s.attachmentHeader.startsWith("첨부"))
    }

    @Test
    fun fallsBackToFullBodyWithoutMarkers() {
        val s = parseApprovalDocBody("그냥 본문만")
        assertEquals("그냥 본문만", s.body)
        assertEquals("", s.line)
        assertEquals("", s.attachments)
    }

    @Test
    fun prefersTrailingReaderAttachBlockOverBodyAttachList() {
        // Real-world shape (docId 99391): the drafter writes a bare "첨부" list
        // inside the body; the reader appends the structured "첨부 (N건 …)"
        // block at the end. The reader block must win.
        val sample = """
            제목: 공고료 지급 품의

            본문
            공고료를 지급하고자 합니다.

            첨부
            1. 신문공고증빙
            2. 거래명세서 끝.

            첨부 (2건 · 내용 미열람)
            필요한 파일만 …
            1. 공고증빙.pdf · 1.9MB
            2. 거래명세서.pdf · 55KB
        """.trimIndent()

        val s = parseApprovalDocBody(sample)
        assertTrue(s.body.contains("신문공고증빙"), "drafter's own list stays in the body")
        assertTrue(s.attachmentHeader.startsWith("첨부 (2건"))
        assertEquals(2, s.attachmentCount)
        val rows = parseAttachmentRows(s.attachments)
        assertEquals(listOf("공고증빙.pdf", "거래명세서.pdf"), rows.map { it.name })
    }

    @Test
    fun parsesAttachmentRowsWithAndWithoutMeta() {
        val rows = parseAttachmentRows(
            """
                필요한 파일만 …
                1. 영수증.pdf · 12KB
                2. 견적.xlsx
                3. 사진 자료.png · 1.2MB · 스캔
            """.trimIndent(),
        )
        assertEquals(3, rows.size)
        assertEquals(ApprovalAttachmentRow(1, "영수증.pdf", "12KB"), rows[0])
        assertEquals(ApprovalAttachmentRow(2, "견적.xlsx", ""), rows[1])
        assertEquals(ApprovalAttachmentRow(3, "사진 자료.png", "1.2MB · 스캔"), rows[2])
    }

    @Test
    fun attachmentRowsIgnoreNonNumberedLines() {
        assertEquals(emptyList<ApprovalAttachmentRow>(), parseAttachmentRows("내용 미열람 안내문"))
        assertEquals(emptyList<ApprovalAttachmentRow>(), parseAttachmentRows(""))
    }
}
