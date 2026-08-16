package ai.deneb

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith

/**
 * Navigation persistence contracts for every typed native route.
 *
 * Route arguments contain opaque mail IDs, paths, browser URLs, cron IDs, and
 * Korean labels. Exact serial names and lossless JSON values are required for
 * process-death restoration and deep-link back-stack reconstruction.
 */
class AppRouteSerializationContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        encodeDefaults = true
    }

    private fun SerialDescriptor.fieldNames(): List<String> = (0 until elementsCount).map(::getElementName)

    private fun futureRouteMetadataEnvelope(baseline: JsonObject): JsonObject = JsonObject(
        baseline + (
            "futureRouteMetadata" to buildJsonObject {
                put("source", "notification")
                put("version", 99)
            }
            ),
    )

    private fun <T> assertObjectRouteIdentity(
        serialName: String,
        route: T,
        serializer: KSerializer<T>,
    ) {
        val encoded = json.encodeToJsonElement(serializer, route)

        assertEquals(serialName, serializer.descriptor.serialName, serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames(), serialName)
        assertEquals(JsonObject(emptyMap()), encoded, serialName)
        assertEquals(route, json.decodeFromJsonElement(serializer, encoded), serialName)
    }

    private fun <T> assertOpaqueRoutePreservesArguments(
        serialName: String,
        fieldNames: List<String>,
        inputJson: String,
        serializer: KSerializer<T>,
    ) {
        val input = json.parseToJsonElement(inputJson).jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals(serialName, serializer.descriptor.serialName, serialName)
        assertEquals(fieldNames, serializer.descriptor.fieldNames(), serialName)
        assertEquals(input, encoded, serialName)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded), serialName)
    }

    private fun <T> assertOpaqueRouteIgnoresFutureMetadata(
        inputJson: String,
        serializer: KSerializer<T>,
    ) {
        val baseline = json.parseToJsonElement(inputJson).jsonObject
        val future = futureRouteMetadataEnvelope(baseline)

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    private fun opaqueRouteCases(): List<() -> Unit> = listOf(
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_feed",
                fieldNames = listOf("openItemId", "openItemCreatedAtMs"),
                inputJson = """{"openItemId":"feed-한글 /?#","openItemCreatedAtMs":123456789}""",
                serializer = DenebFeed.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_mail_detail",
                fieldNames = listOf("id"),
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebMailDetail.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_approval_detail",
                fieldNames = listOf("docId", "title", "drafter", "date", "canAct", "folder"),
                inputJson = """{"docId":"doc-한글 /?#","title":"품의","drafter":"홍길동","date":"2026-07-16","canAct":true,"folder":"pending"}""",
                serializer = DenebApprovalDetail.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_calendar_event",
                fieldNames = listOf("id"),
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebCalendarEvent.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_calendar_add",
                fieldNames = listOf("dateIso"),
                inputJson = """{"dateIso":"dateIso-한글 /?#"}""",
                serializer = DenebCalendarAdd.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_calendar_edit",
                fieldNames = listOf("id"),
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebCalendarEdit.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_todo_add",
                fieldNames = listOf("dueIso"),
                inputJson = """{"dueIso":"dueIso-한글 /?#"}""",
                serializer = DenebTodoAdd.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_todo_edit",
                fieldNames = listOf("id"),
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebTodoEdit.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_wiki",
                fieldNames = listOf("path"),
                inputJson = """{"path":"path-한글 /?#"}""",
                serializer = DenebWiki.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_person",
                fieldNames = listOf("sender"),
                inputJson = """{"sender":"sender-한글 /?#"}""",
                serializer = DenebPerson.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_notebooks",
                fieldNames = listOf("openId"),
                inputJson = """{"openId":"openId-한글 /?#"}""",
                serializer = DenebNotebooks.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_category_pages",
                fieldNames = listOf("category"),
                inputJson = """{"category":"category-한글 /?#"}""",
                serializer = DenebCategoryPages.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_skill",
                fieldNames = listOf("name"),
                inputJson = """{"name":"name-한글 /?#"}""",
                serializer = DenebSkill.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_browser",
                fieldNames = listOf("url"),
                inputJson = """{"url":"url-한글 /?#"}""",
                serializer = DenebBrowser.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_cron",
                fieldNames = listOf("cronId"),
                inputJson = """{"cronId":"cronId-한글 /?#"}""",
                serializer = DenebCron.serializer(),
            )
        },
        {
            assertOpaqueRoutePreservesArguments(
                serialName = "deneb_cron_edit",
                fieldNames = listOf("cronId"),
                inputJson = """{"cronId":"cronId-한글 /?#"}""",
                serializer = DenebCronEdit.serializer(),
            )
        },
    )

    private fun opaqueRouteFutureMetadataCases(): List<() -> Unit> = listOf(
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"openItemId":"feed-한글 /?#","openItemCreatedAtMs":123456789}""",
                serializer = DenebFeed.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebMailDetail.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"docId":"doc-한글 /?#","title":"품의","drafter":"홍길동","date":"2026-07-16","canAct":true,"folder":"pending"}""",
                serializer = DenebApprovalDetail.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebCalendarEvent.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"dateIso":"dateIso-한글 /?#"}""",
                serializer = DenebCalendarAdd.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebCalendarEdit.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"dueIso":"dueIso-한글 /?#"}""",
                serializer = DenebTodoAdd.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"id":"id-한글 /?#"}""",
                serializer = DenebTodoEdit.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"path":"path-한글 /?#"}""",
                serializer = DenebWiki.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"sender":"sender-한글 /?#"}""",
                serializer = DenebPerson.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"openId":"openId-한글 /?#"}""",
                serializer = DenebNotebooks.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"category":"category-한글 /?#"}""",
                serializer = DenebCategoryPages.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"name":"name-한글 /?#"}""",
                serializer = DenebSkill.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"url":"url-한글 /?#"}""",
                serializer = DenebBrowser.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"cronId":"cronId-한글 /?#"}""",
                serializer = DenebCron.serializer(),
            )
        },
        {
            assertOpaqueRouteIgnoresFutureMetadata(
                inputJson = """{"cronId":"cronId-한글 /?#"}""",
                serializer = DenebCronEdit.serializer(),
            )
        },
    )

    @Test
    fun objectRoutesKeepStableRouteIdentity() {
        listOf(
            { assertObjectRouteIdentity("home", Home, Home.serializer()) },
            { assertObjectRouteIdentity("deneb_config", DenebConfig, DenebConfig.serializer()) },
            { assertObjectRouteIdentity("deneb_fleet", DenebFleet, DenebFleet.serializer()) },
            { assertObjectRouteIdentity("deneb_mail", DenebMail, DenebMail.serializer()) },
            { assertObjectRouteIdentity("deneb_calendar", DenebCalendar, DenebCalendar.serializer()) },
            { assertObjectRouteIdentity("deneb_search", DenebSearch, DenebSearch.serializer()) },
            { assertObjectRouteIdentity("deneb_more", DenebMore, DenebMore.serializer()) },
            { assertObjectRouteIdentity("deneb_people", DenebPeople, DenebPeople.serializer()) },
            { assertObjectRouteIdentity("deneb_approvals", DenebApprovals, DenebApprovals.serializer()) },
            { assertObjectRouteIdentity("deneb_feed_log", DenebFeedLog, DenebFeedLog.serializer()) },
            { assertObjectRouteIdentity("deneb_groupware", DenebGroupware, DenebGroupware.serializer()) },
            { assertObjectRouteIdentity("deneb_contacts", DenebContacts, DenebContacts.serializer()) },
            { assertObjectRouteIdentity("deneb_categories", DenebCategories, DenebCategories.serializer()) },
            { assertObjectRouteIdentity("deneb_diary", DenebDiary, DenebDiary.serializer()) },
            { assertObjectRouteIdentity("deneb_dashboard", DenebDashboard, DenebDashboard.serializer()) },
            { assertObjectRouteIdentity("deneb_project_digests", DenebProjectDigests, DenebProjectDigests.serializer()) },
            { assertObjectRouteIdentity("deneb_org", DenebOrgChart, DenebOrgChart.serializer()) },
            { assertObjectRouteIdentity("deneb_files", DenebFiles, DenebFiles.serializer()) },
        ).forEach { it() }
    }

    @Test
    fun opaqueRoutesPreserveRouteArguments() {
        opaqueRouteCases().forEach { it() }
    }

    @Test
    fun opaqueRoutesIgnoreUnknownFutureRouteMetadata() {
        opaqueRouteFutureMetadataCases().forEach { it() }
    }

    @Test
    fun opaqueRoutesRejectMissingRequiredArguments() {
        listOf(
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebMailDetail.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebApprovalDetail.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebCalendarEvent.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebCalendarAdd.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebCalendarEdit.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebTodoEdit.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebWiki.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebPerson.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebCategoryPages.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebSkill.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebBrowser.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebCron.serializer(), buildJsonObject {}) } },
            { assertFailsWith<SerializationException> { json.decodeFromJsonElement(DenebCronEdit.serializer(), buildJsonObject {}) } },
        ).forEach { it() }
    }

    @Test
    fun backwardCompatibleDefaultArgumentsRestoreFromEmptyPayload() {
        listOf(
            {
                val restored = json.decodeFromJsonElement(DenebFeed.serializer(), buildJsonObject {})
                assertEquals(DenebFeed(), restored)
            },
            {
                val restored = json.decodeFromJsonElement(DenebTodoAdd.serializer(), buildJsonObject {})
                assertEquals(DenebTodoAdd(), restored)
            },
            {
                val restored = json.decodeFromJsonElement(DenebNotebooks.serializer(), buildJsonObject {})
                assertEquals(DenebNotebooks(), restored)
            },
        ).forEach { it() }
    }
}
