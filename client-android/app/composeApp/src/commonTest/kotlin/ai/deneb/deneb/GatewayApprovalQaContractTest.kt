package ai.deneb.deneb

import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull

class GatewayApprovalQaContractTest {
    @Test
    fun approvalQaGroundingLabelDoesNotPromiseOptionalAnalysisOrAttachments() {
        assertEquals("본문 및 가용한 첨부 근거", APPROVAL_QA_GROUNDING_LABEL)
        assertFalse(APPROVAL_QA_GROUNDING_LABEL.contains("분석"))
    }

    @Test
    fun askApprovalSendsGroundingIdentityAndBoundedHistoryWithoutAnAction() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"answer":"[본문] 공급가액은 3억원입니다."}""")

        val answer = f.client.askApproval(
            docId = " 42 ",
            question = " 금액이 같아? ",
            title = "견적 결재",
            folder = "pending",
            history = (1..10).map { "질문 $it" to "답변 $it" },
        )

        assertEquals("[본문] 공급가액은 3억원입니다.", answer)
        val params = f.transport.singleRequest().requireRpc("miniapp.groupware.approvals.ask")
        assertEquals("42", params["docId"]?.jsonPrimitive?.content)
        assertEquals("금액이 같아?", params["question"]?.jsonPrimitive?.content)
        assertEquals("견적 결재", params["title"]?.jsonPrimitive?.content)
        assertEquals("pending", params["folder"]?.jsonPrimitive?.content)
        val history = params["history"]!!.jsonArray
        assertEquals(8, history.size)
        assertEquals("질문 3", history.first().jsonObject["q"]?.jsonPrimitive?.content)
        assertEquals("답변 10", history.last().jsonObject["a"]?.jsonPrimitive?.content)
        assertFalse(params.containsKey("decision"))
        assertFalse(params.containsKey("comment"))
    }

    @Test
    fun askApprovalRejectsBlankIdentityOrQuestionBeforeTransport() = runTest {
        val f = gatewayClientFixture()

        assertNull(f.client.askApproval("", "질문"))
        assertNull(f.client.askApproval("42", "  "))
        assertEquals(0, f.transport.requests.size)
    }

    @Test
    fun askApprovalBoundsQuestionAndEachHistoryFieldWithoutSplittingEmoji() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"answer":"확인했습니다."}""")
        val oversizedEmoji = "😀".repeat(2_001)
        val oversizedAnswer = "답".repeat(2_001)

        f.client.askApproval(
            docId = "42",
            question = oversizedEmoji,
            history = listOf(oversizedEmoji to oversizedAnswer),
        )

        val params = f.transport.singleRequest().requireRpc("miniapp.groupware.approvals.ask")
        assertEquals("😀".repeat(2_000), params["question"]?.jsonPrimitive?.content)
        val turn = params["history"]!!.jsonArray.single().jsonObject
        assertEquals("😀".repeat(2_000), turn["q"]?.jsonPrimitive?.content)
        assertEquals("답".repeat(2_000), turn["a"]?.jsonPrimitive?.content)
    }
}
