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
        assertTrue(s.meta.contains("제목: 다과비 품의"))
        assertTrue(!s.meta.contains("그룹웨어"))
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
}
