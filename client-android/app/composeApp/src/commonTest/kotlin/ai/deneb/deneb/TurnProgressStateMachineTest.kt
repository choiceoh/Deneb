package ai.deneb.deneb

import ai.deneb.ui.chat.History
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNotEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

@OptIn(ExperimentalCoroutinesApi::class)
class TurnProgressStateMachineTest {

    private fun history(vararg rows: History) = MutableStateFlow(rows.toList())

    private fun user(id: String = "user", text: String = "question") = History(
        id = id,
        role = History.Role.USER,
        content = text,
    )

    private fun assistant(id: String = "assistant", text: String = "answer") = History(
        id = id,
        role = History.Role.ASSISTANT,
        content = text,
    )

    private fun started(
        tool: String = "mail_search",
        id: String = "tool-1",
        detail: String = "",
    ) = ToolEvent(
        state = "started",
        tool = tool,
        toolUseId = id,
        detail = detail,
    )

    private fun completed(
        tool: String = "mail_search",
        id: String = "tool-1",
        isError: Boolean = false,
        resultSummary: String = "",
    ) = ToolEvent(
        state = "completed",
        tool = tool,
        toolUseId = id,
        isError = isError,
        resultSummary = resultSummary,
    )

    private fun List<History>.progressRows() = filter { it.role == History.Role.TOOL_EXECUTING && it.isStatusMessage }

    @Test
    fun gatewayProgressAddsAndUpdatesOneServerOwnedPhaseRow() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)

        progress.onProgress(ProgressEvent(phase = "preparing", label = "대화 맥락을 준비하고 있습니다"))
        val id = rows.value.last().id
        progress.onProgress(ProgressEvent(phase = "recalling", label = "관련 기억을 확인하고 있습니다"))
        progress.onThinking("provider reasoning preview")

        assertEquals(1, rows.value.progressRows().size)
        assertEquals(id, rows.value.last().id)
        assertEquals("recalling", rows.value.last().content)
        assertEquals("관련 기억을 확인하고 있습니다", rows.value.last().toolName)
    }

    @Test
    fun toolTemporarilyReplacesPhaseAndNextProgressRestoresIt() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onProgress(ProgressEvent(phase = "preparing", label = "준비 중"))

        progress.onTool(started())
        assertEquals(ToolStatusLabels.label("mail_search"), rows.value.single().toolName)

        progress.onTool(completed())
        progress.onProgress(ProgressEvent(phase = "reviewing", label = "확인한 결과를 검토하고 있습니다"))
        assertEquals("확인한 결과를 검토하고 있습니다", rows.value.single().toolName)
    }

    @Test
    fun answerDeltaKeepsTheWritingPhaseVisibleUntilTurnClear() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onProgress(ProgressEvent(phase = "writing", label = "답변을 작성하고 있습니다"))

        progress.onDelta()
        assertEquals("답변을 작성하고 있습니다", rows.value.single().toolName)

        progress.clear()
        assertTrue(rows.value.isEmpty())
    }

    @Test
    fun thinkingPulseAddsOneStatusRowAfterExistingHistory() = runTest {
        val rows = history(user(), assistant())
        val progress = TurnProgress(rows, this)

        progress.onThinking("발신인 이력 확인")

        assertEquals(3, rows.value.size)
        val status = rows.value.last()
        assertEquals(History.Role.TOOL_EXECUTING, status.role)
        assertEquals("thinking", status.content)
        assertTrue(status.isStatusMessage)
        assertTrue(status.toolName.orEmpty().contains("발신인 이력 확인"))
    }

    @Test
    fun blankThinkingPulseUsesBaseThinkingLabelWithoutColon() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onThinking("")

        val label = rows.value.single().toolName.orEmpty()
        assertEquals(ToolStatusLabels.THINKING, label)
        assertFalse(label.endsWith(':'))
    }

    @Test
    fun repeatedThinkingPulseUpdatesExistingRowInsteadOfAppending() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)
        progress.onThinking("first")
        val firstId = rows.value.last().id

        progress.onThinking("second")

        assertEquals(1, rows.value.progressRows().size)
        assertEquals(firstId, rows.value.last().id)
        assertTrue(rows.value.last().toolName.orEmpty().endsWith("second"))
    }

    @Test
    fun blankRepeatedThinkingPulseDoesNotEraseVisiblePreview() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onThinking("keep me")
        val label = rows.value.single().toolName

        progress.onThinking("")

        assertEquals(label, rows.value.single().toolName)
        assertEquals(1, rows.value.size)
    }

    @Test
    fun thinkingPulseNeverMutatesUnrelatedRows() = runTest {
        val originalUser = user(text = "immutable question")
        val originalAssistant = assistant(text = "immutable answer")
        val rows = history(originalUser, originalAssistant)
        val progress = TurnProgress(rows, this)

        progress.onThinking("preview")

        assertEquals(originalUser, rows.value[0])
        assertEquals(originalAssistant, rows.value[1])
    }

    @Test
    fun deltaRemovesThinkingRowImmediately() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)
        progress.onThinking("preview")

        progress.onDelta()

        assertEquals(listOf(user()), rows.value)
    }

    @Test
    fun repeatedDeltaWhileNothingVisibleIsNoOp() = runTest {
        val original = listOf(user(), assistant())
        val rows = MutableStateFlow(original)
        val progress = TurnProgress(rows, this)

        progress.onDelta()
        progress.onDelta()

        assertEquals(original, rows.value)
    }

    @Test
    fun toolStartReplacesThinkingWithToolRow() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)
        progress.onThinking("planning")

        progress.onTool(started(tool = "web_search", detail = "Deneb"))

        val status = rows.value.progressRows().single()
        assertEquals("web_search", status.content)
        assertFalse(status.toolName.orEmpty().contains(ToolStatusLabels.THINKING))
        assertTrue(status.toolName.orEmpty().contains("Deneb"))
    }

    @Test
    fun clearRemovesVisibleThinkingAndPreservesConversation() = runTest {
        val original = user()
        val rows = history(original)
        val progress = TurnProgress(rows, this)
        progress.onThinking("temporary")

        progress.clear()

        assertEquals(listOf(original), rows.value)
    }

    @Test
    fun clearIsIdempotentWithThinkingRow() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onThinking("temporary")

        progress.clear()
        progress.clear()

        assertTrue(rows.value.isEmpty())
    }

    @Test
    fun thinkingCanBeShownAgainAfterClear() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onThinking("first")
        val firstId = rows.value.single().id
        progress.clear()

        progress.onThinking("second")

        assertEquals(1, rows.value.size)
        assertEquals(firstId, rows.value.single().id)
        assertTrue(rows.value.single().toolName.orEmpty().endsWith("second"))
    }

    @Test
    fun startedToolCarriesContentRoleAndReadableLabel() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onTool(started(tool = "mail_search"))

        val row = rows.value.single()
        assertEquals("mail_search", row.content)
        assertEquals(History.Role.TOOL_EXECUTING, row.role)
        assertTrue(row.isStatusMessage)
        assertEquals(ToolStatusLabels.label("mail_search"), row.toolName)
    }

    @Test
    fun startedToolAppendsNonBlankDetailToLabel() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onTool(started(tool = "mail_search", detail = "계약서"))

        assertEquals("${ToolStatusLabels.label("mail_search")}: 계약서", rows.value.single().toolName)
    }

    @Test
    fun startedToolWithBlankDetailDoesNotLeaveDanglingSeparator() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onTool(started(detail = ""))

        assertFalse(rows.value.single().toolName.orEmpty().endsWith(":"))
    }

    @Test
    fun blankToolUseIdFallsBackToToolNameForCompletionPairing() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "web_search", id = ""))

        progress.onTool(completed(tool = "web_search", id = ""))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        assertEquals(ToolStatusLabels.REVIEWING, rows.value.single().toolName)
        assertEquals(ToolStatusLabels.trailLabel("web_search"), progress.footprint())
    }

    @Test
    fun twoDistinctToolIdsCanRunConcurrently() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)

        progress.onTool(started(id = "one", detail = "A"))
        progress.onTool(started(id = "two", detail = "B"))

        assertEquals(2, rows.value.progressRows().size)
        assertEquals(setOf("A", "B"), rows.value.progressRows().map { it.toolName.orEmpty().substringAfterLast(' ') }.toSet())
    }

    @Test
    fun sameToolNameWithDifferentIdsGetsIndependentRows() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onTool(started(id = "one"))
        progress.onTool(started(id = "two"))

        assertEquals(2, rows.value.size)
        assertNotEquals(rows.value[0].id, rows.value[1].id)
    }

    @Test
    fun duplicateStartedFrameForSameIdDoesNotCreateZombieRow() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(id = "same", detail = "first"))

        progress.onTool(started(id = "same", detail = "retry"))

        assertEquals(1, rows.value.progressRows().size)
        assertTrue(rows.value.single().toolName.orEmpty().endsWith("retry"))
    }

    @Test
    fun duplicateStartedFramePreservesOriginalRowIdentity() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(id = "same"))
        val id = rows.value.single().id

        progress.onTool(started(id = "same", detail = "updated"))

        assertEquals(id, rows.value.single().id)
    }

    @Test
    fun unknownToolStateDoesNotMutateHistoryOrFootprint() = runTest {
        val original = user()
        val rows = history(original)
        val progress = TurnProgress(rows, this)

        progress.onTool(ToolEvent(state = "queued", tool = "mail_search", toolUseId = "one"))

        assertEquals(listOf(original), rows.value)
        assertNull(progress.footprint())
    }

    @Test
    fun blankToolAndIdAreIgnored() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onTool(ToolEvent(state = "started"))
        progress.onTool(ToolEvent(state = "completed"))

        assertTrue(rows.value.isEmpty())
        assertNull(progress.footprint())
    }

    @Test
    fun completionWithoutMatchingStartDoesNotPolluteFootprint() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onTool(completed(id = "missing"))

        assertTrue(rows.value.isEmpty())
        assertNull(progress.footprint())
    }

    @Test
    fun duplicateCompletionIsCountedOnlyOnce() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())

        progress.onTool(completed())
        progress.onTool(completed())

        assertEquals(ToolStatusLabels.trailLabel("mail_search"), progress.footprint())
    }

    @Test
    fun successfulLastToolBecomesReviewingContinuityAfterMinimumDisplay() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())

        progress.onTool(completed())
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        val row = rows.value.single()
        assertEquals("continuity", row.content)
        assertEquals(ToolStatusLabels.REVIEWING, row.toolName)
    }

    @Test
    fun successfulToolShowsFinishedLabelWhileTheRowLingers() = runTest {
        // The row stays on screen briefly after the call returns; holding the
        // in-progress label there read as if the tool were still running (the
        // failure path already relabelled — this is its missing sibling).
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(detail = "target"))

        progress.onTool(completed())
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS - 1)
        runCurrent()

        assertEquals(ToolStatusLabels.doneLabel("mail_search"), rows.value.single().toolName)
        assertEquals("mail_search", rows.value.single().content)
    }

    @Test
    fun completedToolRendersTheGatewaysResultSummary() = runTest {
        // The gateway owns the wording; the client appends it verbatim.
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(detail = "target"))

        progress.onTool(completed(resultSummary = "3건 · 5줄"))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS - 1)
        runCurrent()

        assertEquals(
            ToolStatusLabels.doneLabel("mail_search") + ": 3건 · 5줄",
            rows.value.single().toolName,
        )
    }

    @Test
    fun summaryCarryingRowLingersToTheRaisedFloor() = runTest {
        // A summary is real information — the row holds past the plain-label
        // floor (1.5s was measured too short to reliably read it) and becomes
        // the reviewing continuity only after the raised floor.
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())

        progress.onTool(completed(resultSummary = "3건 · 5줄"))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()
        assertEquals(
            ToolStatusLabels.doneLabel("mail_search") + ": 3건 · 5줄",
            rows.value.single().toolName,
        )

        advanceTimeBy(DenebGatewayClient.MIN_SUMMARY_DISPLAY_MS - DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS)
        runCurrent()
        assertEquals(ToolStatusLabels.REVIEWING, rows.value.single().toolName)
    }

    @Test
    fun failedToolRendersTheGatewaysResultSummary() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())

        progress.onTool(completed(isError = true, resultSummary = "exit code 2 · 5줄"))
        runCurrent()

        assertEquals(
            ToolStatusLabels.failureLabel("mail_search") + ": exit code 2 · 5줄",
            rows.value.single().toolName,
        )
    }

    @Test
    fun answerDeltaRemovesReviewingContinuity() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)
        progress.onTool(started())
        progress.onTool(completed())
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        progress.onDelta()

        assertEquals(listOf(user()), rows.value)
    }

    @Test
    fun deltaBeforeDelayedReviewSwapPreventsRowResurrection() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())
        progress.onTool(completed())

        progress.onDelta()
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 10)
        runCurrent()

        assertTrue(rows.value.isEmpty())
    }

    @Test
    fun thinkingPulseReplacesReviewingContinuity() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())
        progress.onTool(completed())
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        progress.onThinking("next step")

        val row = rows.value.single()
        assertEquals("thinking", row.content)
        assertTrue(row.toolName.orEmpty().endsWith("next step"))
    }

    @Test
    fun nextToolStartRemovesPriorReviewingContinuity() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(id = "one"))
        progress.onTool(completed(id = "one"))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        progress.onTool(started(tool = "web_search", id = "two"))

        assertEquals(1, rows.value.size)
        assertEquals("web_search", rows.value.single().content)
    }

    @Test
    fun completingOneOfTwoToolsRemovesOnlyThatRowAfterDelay() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "mail_search", id = "one"))
        progress.onTool(started(tool = "web_search", id = "two"))

        progress.onTool(completed(tool = "mail_search", id = "one"))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        assertEquals(1, rows.value.size)
        assertEquals("web_search", rows.value.single().content)
    }

    @Test
    fun finalParallelToolTransitionsItsOwnRowToReviewing() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "mail_search", id = "one"))
        progress.onTool(started(tool = "web_search", id = "two"))
        progress.onTool(completed(tool = "mail_search", id = "one"))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        progress.onTool(completed(tool = "web_search", id = "two"))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        assertEquals(1, rows.value.size)
        assertEquals(ToolStatusLabels.REVIEWING, rows.value.single().toolName)
    }

    @Test
    fun failedToolSwitchesToReadableFailureLabel() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "mail_search"))

        progress.onTool(completed(tool = "mail_search", isError = true))

        assertEquals(ToolStatusLabels.failureLabel("mail_search"), rows.value.single().toolName)
    }

    @Test
    fun failedToolRowIsHeldThenRemoved() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())
        progress.onTool(completed(isError = true))

        advanceTimeBy(DenebGatewayClient.FAILURE_DISPLAY_MS - 1)
        runCurrent()
        assertEquals(1, rows.value.size)

        advanceTimeBy(2)
        runCurrent()
        assertTrue(rows.value.isEmpty())
    }

    @Test
    fun clearBeforeFailureTimerExpiresPreventsZombieRow() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)
        progress.onTool(started())
        progress.onTool(completed(isError = true))

        progress.clear()
        advanceTimeBy(DenebGatewayClient.FAILURE_DISPLAY_MS + 1)
        runCurrent()

        assertEquals(listOf(user()), rows.value)
    }

    @Test
    fun failedLastToolDoesNotPretendResultsAreBeingReviewed() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())

        progress.onTool(completed(isError = true))
        advanceTimeBy(DenebGatewayClient.FAILURE_DISPLAY_MS + 1)
        runCurrent()

        assertTrue(rows.value.isEmpty())
    }

    @Test
    fun failedToolContributesWarningToFootprint() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "web_search"))

        progress.onTool(completed(tool = "web_search", isError = true))

        assertEquals("${ToolStatusLabels.trailLabel("web_search")} ⚠", progress.footprint())
    }

    @Test
    fun footprintIsNullBeforeAnyMatchedCompletion() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())

        assertNull(progress.footprint())
    }

    @Test
    fun footprintCountsRepeatedCompletedTool() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        repeat(3) { index ->
            progress.onTool(started(tool = "mail_search", id = "id-$index"))
            progress.onTool(completed(tool = "mail_search", id = "id-$index"))
        }

        assertEquals("${ToolStatusLabels.trailLabel("mail_search")} ×3", progress.footprint())
    }

    @Test
    fun anyFailureMarksAggregatedToolEvenWhenLaterAttemptSucceeds() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "mail_search", id = "one"))
        progress.onTool(completed(tool = "mail_search", id = "one", isError = true))
        progress.onTool(started(tool = "mail_search", id = "two"))
        progress.onTool(completed(tool = "mail_search", id = "two"))

        val footprint = progress.footprint().orEmpty()
        assertTrue(footprint.contains("×2"))
        assertTrue(footprint.endsWith("⚠"))
    }

    @Test
    fun footprintKeepsFirstCompletionOrderAcrossDistinctTools() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "web_search", id = "web"))
        progress.onTool(started(tool = "mail_search", id = "mail"))

        progress.onTool(completed(tool = "mail_search", id = "mail"))
        progress.onTool(completed(tool = "web_search", id = "web"))

        assertEquals(
            "${ToolStatusLabels.trailLabel("mail_search")} · ${ToolStatusLabels.trailLabel("web_search")}",
            progress.footprint(),
        )
    }

    @Test
    fun footprintCapsNamedToolsAndReportsRemainder() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        val tools = listOf("mail_search", "web_search", "calendar", "wiki", "files", "exec", "memory")
        tools.forEachIndexed { index, tool ->
            progress.onTool(started(tool = tool, id = "id-$index"))
            progress.onTool(completed(tool = tool, id = "id-$index"))
        }

        val footprint = progress.footprint().orEmpty()
        assertTrue(footprint.endsWith("외 2"))
        assertFalse(footprint.contains(ToolStatusLabels.trailLabel("memory")))
    }

    @Test
    fun exactlyMaximumDistinctToolsHasNoRemainderSuffix() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        val tools = listOf("one", "two", "three", "four", "five")
        tools.forEach { tool ->
            progress.onTool(started(tool = tool, id = tool))
            progress.onTool(completed(tool = tool, id = tool))
        }

        val footprint = progress.footprint().orEmpty()
        assertEquals(DenebGatewayClient.FOOTPRINT_MAX_TOOLS - 1, footprint.count { it == '·' })
        assertFalse(footprint.contains(" 외 "))
    }

    @Test
    fun clearRemovesAllOwnedRowsButNotForeignStatusRows() = runTest {
        val foreign = History(
            id = "foreign",
            role = History.Role.TOOL_EXECUTING,
            content = "foreign",
            toolName = "foreign",
            isStatusMessage = true,
        )
        val rows = history(user(), foreign)
        val progress = TurnProgress(rows, this)
        progress.onThinking("plan")
        progress.onTool(started(id = "one"))
        progress.onTool(started(id = "two"))

        progress.clear()

        assertEquals(listOf(user(), foreign), rows.value)
    }

    @Test
    fun clearRemainsSafeAfterDelayedSuccessRemovalRuns() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "mail_search", id = "one"))
        progress.onTool(started(tool = "web_search", id = "two"))
        progress.onTool(completed(tool = "mail_search", id = "one"))
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        progress.clear()
        runCurrent()

        assertTrue(rows.value.isEmpty())
    }

    @Test
    fun clearBeforeDelayedContinuitySwapPreventsReappearance() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started())
        progress.onTool(completed())

        progress.clear()
        advanceTimeBy(DenebGatewayClient.MIN_PROGRESS_DISPLAY_MS + 1)
        runCurrent()

        assertTrue(rows.value.isEmpty())
    }

    @Test
    fun errorFlagOnStartedFrameDoesNotPrematurelyMarkFailure() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)

        progress.onTool(started().copy(isError = true))

        assertEquals(ToolStatusLabels.label("mail_search"), rows.value.single().toolName)
        assertNull(progress.footprint())
    }

    @Test
    fun completionDetailDoesNotOverwriteStableFailureLabel() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "web_search", detail = "query"))

        progress.onTool(completed(tool = "web_search", isError = true).copy(detail = "raw server detail"))

        assertEquals(ToolStatusLabels.failureLabel("web_search"), rows.value.single().toolName)
    }

    @Test
    fun parallelCompletionFootprintReflectsCompletionNotStartOrder() = runTest {
        val rows = history()
        val progress = TurnProgress(rows, this)
        progress.onTool(started(tool = "first", id = "one"))
        progress.onTool(started(tool = "second", id = "two"))
        progress.onTool(started(tool = "third", id = "three"))

        progress.onTool(completed(tool = "third", id = "three"))
        progress.onTool(completed(tool = "first", id = "one"))
        progress.onTool(completed(tool = "second", id = "two"))

        assertEquals(
            listOf("third", "first", "second"),
            progress.footprint().orEmpty().split(" · "),
        )
    }

    /**
     * A slow tool used to freeze its row: the gateway sends started → completed
     * with nothing in between, so a 49-second call (skill_lifecycle p95,
     * measured 2026-08-30) left a motionless "…중" and no way to tell work from
     * a hang. The row now counts, but only once it has run long enough to look
     * stuck — a number on the median millisecond-long call would be noise.
     */
    @Test
    fun aSlowToolRowStartsCountingSecondsAndAFastOneNeverDoes() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)

        progress.onTool(started())
        val label = rows.value.last().toolName
        assertFalse(label.orEmpty().contains("초"), "a just-started row must not show a count: $label")

        advanceTimeBy(3_000)
        runCurrent()
        assertFalse(
            rows.value.last().toolName.orEmpty().contains("초"),
            "a tool that finishes quickly must never show a count",
        )

        advanceTimeBy(6_000)
        runCurrent()
        val counting = rows.value.last().toolName.orEmpty()
        assertTrue(counting.contains("초"), "a long-running row should count: $counting")

        advanceTimeBy(2_000)
        runCurrent()
        assertNotEquals(counting, rows.value.last().toolName, "the count should keep moving")

        // Completion hands the row to the continuity status ("결과 검토 중…"), so
        // a row still exists — what must stop is the counting. A ticker that
        // survived would keep stamping seconds over that status.
        progress.onTool(completed())
        advanceTimeBy(10_000)
        runCurrent()
        val labels = rows.value.progressRows().mapNotNull { it.toolName }
        assertTrue(
            labels.none { it.contains("초") },
            "no row may keep counting after the tool completed: $labels",
        )
    }

    /** clear() runs in ask()'s finally — a cancelled stream must not leave a ticker running. */
    @Test
    fun clearStopsAnyRunningElapsedTicker() = runTest {
        val rows = history(user())
        val progress = TurnProgress(rows, this)

        progress.onTool(started())
        advanceTimeBy(9_000)
        runCurrent()
        progress.clear()
        advanceTimeBy(5_000)
        runCurrent()
        assertTrue(rows.value.progressRows().isEmpty(), "clear() must leave no progress rows behind")
    }
}
