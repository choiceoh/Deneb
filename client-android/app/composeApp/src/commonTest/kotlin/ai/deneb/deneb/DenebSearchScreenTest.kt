package ai.deneb.deneb

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class DenebSearchScreenTest {
    @Test
    fun startingNewQueryClearsPreviousResultsBeforeNetworkCompletion() {
        val previous = SearchResults(
            wiki = listOf(SearchHit(path = "old", title = "이전 결과", snippet = "old", category = "wiki")),
            diary = emptyList(),
            people = emptyList(),
        )

        val started = SearchRunState(failed = true, results = previous).starting("새 질의")

        assertEquals(true, started.searching)
        assertEquals(false, started.failed)
        assertEquals(null, started.results)
        assertEquals("새 질의", started.submittedQuery)
        val completed = started.completed(started.generation, "새 질의", previous)
        assertEquals(false, completed.searching)
        assertEquals(previous, completed.results)
    }

    @Test
    fun editingQueryDuringRequestInvalidatesItsLateResponseAndAllowsANewSubmission() {
        val oldResults = SearchResults(
            wiki = listOf(SearchHit(path = "old", title = "이전 결과", snippet = "old", category = "wiki")),
            diary = emptyList(),
            people = emptyList(),
        )
        val first = SearchRunState().starting("이전 질의")

        val edited = first.queryEdited("새 질의")

        assertEquals(false, edited.searching)
        assertEquals(null, edited.results)
        val afterLateResponse = edited.completed(first.generation, "이전 질의", oldResults)
        assertEquals(edited, afterLateResponse)
        val second = edited.starting("새 질의")
        assertEquals(true, second.searching)
        assertEquals("새 질의", second.submittedQuery)
    }

    @Test
    fun factMarkersAlwaysChooseInlineReadOnlyHandling() {
        fun hit(
            path: String = "wiki/page.md",
            resultKind: String = "",
            readOnly: Boolean = false,
            factId: String = "",
        ) = SearchHit(path, "title", "candidate", "wiki", resultKind, readOnly, factId)

        assertFalse(hit().isCurrentFactHit())
        assertTrue(hit(resultKind = "fact").isCurrentFactHit())
        assertTrue(hit(readOnly = true).isCurrentFactHit())
        assertTrue(hit(factId = "fact-123").isCurrentFactHit())
        assertTrue(hit(path = "@facts/fact-123.md").isCurrentFactHit())
        assertTrue(hit(path = "@facts\\fact-123.md").isCurrentFactHit())
    }

    @Test
    fun factClickNeverInvokesEditableWikiNavigation() {
        val fact = SearchHit(
            path = "@facts/fact-123.md",
            title = "Fact",
            snippet = "",
            category = "fact-plane",
            resultKind = "fact",
            readOnly = true,
            factId = "fact-123",
        )
        var wikiPath: String? = null
        var inlineFact: SearchHit? = null

        openSearchWikiHit(fact, onOpenWiki = { wikiPath = it }, onOpenFact = { inlineFact = it })

        assertNull(wikiPath)
        assertEquals(fact, inlineFact)

        val page = fact.copy(path = "wiki/page.md", resultKind = "page", readOnly = false, factId = "")
        inlineFact = null
        openSearchWikiHit(page, onOpenWiki = { wikiPath = it }, onOpenFact = { inlineFact = it })
        assertEquals(page.path, wikiPath)
        assertNull(inlineFact)
    }

    @Test
    fun eachFactTapClearsPreviousPayloadAndFailedOldRefRequiresNewSearch() {
        val old = SearchHit(
            path = "@facts/fact-old.md",
            title = "Old",
            snippet = "",
            category = "fact-plane",
            resultKind = "fact",
            readOnly = true,
            factId = "fact-old",
            subjectId = "project:alpha",
        )
        val previous = CurrentFactDetail(
            path = "@facts/fact-previous.md",
            title = "Previous",
            summary = "",
            body = "previous value",
        )
        val state = CurrentFactDetailState(
            path = previous.path,
            detail = previous,
        )

        val loading = state.starting(old)

        assertTrue(loading.loading)
        assertEquals(old.path, loading.path)
        assertEquals(old.factId, loading.factId)
        assertEquals(old.subjectId, loading.subjectId)
        assertNull(loading.detail)

        val rejected = loading.completed(loading.generation, old.path, null)
        assertFalse(rejected.loading)
        assertTrue(rejected.newSearchRequired)
        assertNull(rejected.detail)
    }

    @Test
    fun queryEditOrNewerFactTapDiscardsLateFactPayload() {
        val firstHit = SearchHit(
            "@facts/first.md",
            "First",
            "",
            "fact-plane",
            resultKind = "fact",
            factId = "first",
        )
        val secondHit = firstHit.copy(path = "@facts/second.md", factId = "second")
        val first = CurrentFactDetailState().starting(firstHit)
        val second = first.starting(secondHit)
        val late = CurrentFactDetail(firstHit.path, "Late", "", "stale payload")

        assertEquals(second, second.completed(first.generation, firstHit.path, late))

        val cleared = second.cleared()
        val current = CurrentFactDetail(secondHit.path, "Current", "", "current payload")
        assertEquals(cleared, cleared.completed(second.generation, secondHit.path, current))
        assertNull(cleared.detail)
    }

    @Test
    fun searchQueryBoundCountsEmojiAsOneUnicodeCodePoint() {
        assertEquals("😀".repeat(500), boundSearchQuery("😀".repeat(501)))
    }

    @Test
    fun oversizedSearchFileRequiresConfirmationBeforeDownload() {
        fun hit(size: Long) = SearchFileResult(
            path = "docs/report.pdf",
            name = "report.pdf",
            size = size,
            snippet = "",
            score = 1.0,
        )

        assertFalse(searchFileNeedsDownloadConfirmation(hit(0L)))
        assertFalse(searchFileNeedsDownloadConfirmation(hit(SEARCH_FILE_DIRECT_OPEN_MAX_BYTES)))
        assertTrue(searchFileNeedsDownloadConfirmation(hit(SEARCH_FILE_DIRECT_OPEN_MAX_BYTES + 1)))
    }

    @Test
    fun sourceNoticesDistinguishFailureFromAValidEmptySource() {
        val notices = searchSourceNotices(
            SearchSourceAvailability(
                wiki = "partial",
                diary = "offline",
                people = "timeout",
                files = "unavailable",
                mail = "error",
            ),
        )

        assertEquals(
            listOf(
                "위키 · 일부 결과만 표시",
                "일기 · 기기 저장본",
                "사람 · 시간 초과",
                "파일 · 현재 사용할 수 없음",
                "메일 · 검색 실패",
            ),
            notices,
        )
    }

    @Test
    fun fileLocatorPreservesHeadingAndMatchedLineRange() {
        val locator = searchFileLocator(
            SearchFileResult(
                path = "docs/quote.pdf",
                name = "quote.pdf",
                snippet = "공급가액 3억원",
                score = 0.93,
                startLine = 31,
                endLine = 35,
                kind = "pdf",
                heading = "견적 합계",
            ),
        )

        assertEquals("견적 합계 · 31–35행", locator)
    }
}
