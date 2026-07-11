package ai.deneb

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
import kotlin.test.assertTrue

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

    @Test
    fun homeKeepsStableObjectRouteIdentity() {
        val serializer = Home.serializer()
        val encoded = json.encodeToJsonElement(serializer, Home)

        assertEquals("home", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(Home, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebFeedKeepsStableObjectRouteIdentity() {
        val serializer = DenebFeed.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebFeed)

        assertEquals("deneb_feed", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebFeed, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebConfigKeepsStableObjectRouteIdentity() {
        val serializer = DenebConfig.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebConfig)

        assertEquals("deneb_config", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebConfig, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebFleetKeepsStableObjectRouteIdentity() {
        val serializer = DenebFleet.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebFleet)

        assertEquals("deneb_fleet", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebFleet, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebMailKeepsStableObjectRouteIdentity() {
        val serializer = DenebMail.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebMail)

        assertEquals("deneb_mail", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebMail, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCalendarKeepsStableObjectRouteIdentity() {
        val serializer = DenebCalendar.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebCalendar)

        assertEquals("deneb_calendar", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebCalendar, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebSearchKeepsStableObjectRouteIdentity() {
        val serializer = DenebSearch.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebSearch)

        assertEquals("deneb_search", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebSearch, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebMoreKeepsStableObjectRouteIdentity() {
        val serializer = DenebMore.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebMore)

        assertEquals("deneb_more", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebMore, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebPeopleKeepsStableObjectRouteIdentity() {
        val serializer = DenebPeople.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebPeople)

        assertEquals("deneb_people", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebPeople, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebContactsKeepsStableObjectRouteIdentity() {
        val serializer = DenebContacts.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebContacts)

        assertEquals("deneb_contacts", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebContacts, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCategoriesKeepsStableObjectRouteIdentity() {
        val serializer = DenebCategories.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebCategories)

        assertEquals("deneb_categories", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebCategories, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebDiaryKeepsStableObjectRouteIdentity() {
        val serializer = DenebDiary.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebDiary)

        assertEquals("deneb_diary", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebDiary, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebDashboardKeepsStableObjectRouteIdentity() {
        val serializer = DenebDashboard.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebDashboard)

        assertEquals("deneb_dashboard", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebDashboard, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebProjectDigestsKeepsStableObjectRouteIdentity() {
        val serializer = DenebProjectDigests.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebProjectDigests)

        assertEquals("deneb_project_digests", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebProjectDigests, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebOrgChartKeepsStableObjectRouteIdentity() {
        val serializer = DenebOrgChart.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebOrgChart)

        assertEquals("deneb_org", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebOrgChart, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebFilesKeepsStableObjectRouteIdentity() {
        val serializer = DenebFiles.serializer()
        val encoded = json.encodeToJsonElement(serializer, DenebFiles)

        assertEquals("deneb_files", serializer.descriptor.serialName)
        assertEquals(emptyList(), serializer.descriptor.fieldNames())
        assertEquals(JsonObject(emptyMap()), encoded)
        assertEquals(DenebFiles, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebMailDetailPreservesOpaqueRouteArguments() {
        val serializer = DenebMailDetail.serializer()
        val input = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_mail_detail", serializer.descriptor.serialName)
        assertEquals(listOf("id"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebMailDetailIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebMailDetail.serializer()
        val baseline = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebMailDetailRejectsMissingRequiredId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebMailDetail.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebCalendarEventPreservesOpaqueRouteArguments() {
        val serializer = DenebCalendarEvent.serializer()
        val input = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_calendar_event", serializer.descriptor.serialName)
        assertEquals(listOf("id"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCalendarEventIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebCalendarEvent.serializer()
        val baseline = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebCalendarEventRejectsMissingRequiredId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebCalendarEvent.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebCalendarAddPreservesOpaqueRouteArguments() {
        val serializer = DenebCalendarAdd.serializer()
        val input = json.parseToJsonElement("""{"dateIso":"dateIso-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_calendar_add", serializer.descriptor.serialName)
        assertEquals(listOf("dateIso"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCalendarAddIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebCalendarAdd.serializer()
        val baseline = json.parseToJsonElement("""{"dateIso":"dateIso-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebCalendarAddRejectsMissingRequiredDateIso() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebCalendarAdd.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebCalendarEditPreservesOpaqueRouteArguments() {
        val serializer = DenebCalendarEdit.serializer()
        val input = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_calendar_edit", serializer.descriptor.serialName)
        assertEquals(listOf("id"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCalendarEditIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebCalendarEdit.serializer()
        val baseline = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebCalendarEditRejectsMissingRequiredId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebCalendarEdit.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebTodoAddPreservesOpaqueRouteArguments() {
        val serializer = DenebTodoAdd.serializer()
        val input = json.parseToJsonElement("""{"dueIso":"dueIso-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_todo_add", serializer.descriptor.serialName)
        assertEquals(listOf("dueIso"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebTodoAddIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebTodoAdd.serializer()
        val baseline = json.parseToJsonElement("""{"dueIso":"dueIso-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebTodoAddRestoresItsBackwardCompatibleDefaultArgument() {
        val restored = json.decodeFromJsonElement(
            DenebTodoAdd.serializer(),
            buildJsonObject {},
        )

        assertEquals(DenebTodoAdd(), restored)
    }

    @Test
    fun denebTodoEditPreservesOpaqueRouteArguments() {
        val serializer = DenebTodoEdit.serializer()
        val input = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_todo_edit", serializer.descriptor.serialName)
        assertEquals(listOf("id"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebTodoEditIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebTodoEdit.serializer()
        val baseline = json.parseToJsonElement("""{"id":"id-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebTodoEditRejectsMissingRequiredId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebTodoEdit.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebWikiPreservesOpaqueRouteArguments() {
        val serializer = DenebWiki.serializer()
        val input = json.parseToJsonElement("""{"path":"path-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_wiki", serializer.descriptor.serialName)
        assertEquals(listOf("path"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebWikiIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebWiki.serializer()
        val baseline = json.parseToJsonElement("""{"path":"path-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebWikiRejectsMissingRequiredPath() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebWiki.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebPersonPreservesOpaqueRouteArguments() {
        val serializer = DenebPerson.serializer()
        val input = json.parseToJsonElement("""{"sender":"sender-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_person", serializer.descriptor.serialName)
        assertEquals(listOf("sender"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebPersonIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebPerson.serializer()
        val baseline = json.parseToJsonElement("""{"sender":"sender-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebPersonRejectsMissingRequiredSender() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebPerson.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebNotebooksPreservesOpaqueRouteArguments() {
        val serializer = DenebNotebooks.serializer()
        val input = json.parseToJsonElement("""{"openId":"openId-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_notebooks", serializer.descriptor.serialName)
        assertEquals(listOf("openId"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebNotebooksIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebNotebooks.serializer()
        val baseline = json.parseToJsonElement("""{"openId":"openId-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebNotebooksRestoresItsBackwardCompatibleDefaultArgument() {
        val restored = json.decodeFromJsonElement(
            DenebNotebooks.serializer(),
            buildJsonObject {},
        )

        assertEquals(DenebNotebooks(), restored)
    }

    @Test
    fun denebCategoryPagesPreservesOpaqueRouteArguments() {
        val serializer = DenebCategoryPages.serializer()
        val input = json.parseToJsonElement("""{"category":"category-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_category_pages", serializer.descriptor.serialName)
        assertEquals(listOf("category"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCategoryPagesIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebCategoryPages.serializer()
        val baseline = json.parseToJsonElement("""{"category":"category-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebCategoryPagesRejectsMissingRequiredCategory() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebCategoryPages.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebSkillPreservesOpaqueRouteArguments() {
        val serializer = DenebSkill.serializer()
        val input = json.parseToJsonElement("""{"name":"name-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_skill", serializer.descriptor.serialName)
        assertEquals(listOf("name"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebSkillIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebSkill.serializer()
        val baseline = json.parseToJsonElement("""{"name":"name-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebSkillRejectsMissingRequiredName() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebSkill.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebBrowserPreservesOpaqueRouteArguments() {
        val serializer = DenebBrowser.serializer()
        val input = json.parseToJsonElement("""{"url":"url-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_browser", serializer.descriptor.serialName)
        assertEquals(listOf("url"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebBrowserIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebBrowser.serializer()
        val baseline = json.parseToJsonElement("""{"url":"url-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebBrowserRejectsMissingRequiredUrl() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebBrowser.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebCronPreservesOpaqueRouteArguments() {
        val serializer = DenebCron.serializer()
        val input = json.parseToJsonElement("""{"cronId":"cronId-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_cron", serializer.descriptor.serialName)
        assertEquals(listOf("cronId"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCronIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebCron.serializer()
        val baseline = json.parseToJsonElement("""{"cronId":"cronId-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebCronRejectsMissingRequiredCronId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebCron.serializer(),
                buildJsonObject {},
            )
        }
    }

    @Test
    fun denebCronEditPreservesOpaqueRouteArguments() {
        val serializer = DenebCronEdit.serializer()
        val input = json.parseToJsonElement("""{"cronId":"cronId-한글 /?#"}""").jsonObject

        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals("deneb_cron_edit", serializer.descriptor.serialName)
        assertEquals(listOf("cronId"), serializer.descriptor.fieldNames())
        assertEquals(input, encoded)
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded))
    }

    @Test
    fun denebCronEditIgnoresUnknownFutureRouteMetadata() {
        val serializer = DenebCronEdit.serializer()
        val baseline = json.parseToJsonElement("""{"cronId":"cronId-한글 /?#"}""").jsonObject
        val future = JsonObject(
            baseline + (
                "futureRouteMetadata" to buildJsonObject {
                    put("source", "notification")
                    put("version", 99)
                }
                ),
        )

        assertEquals(
            json.decodeFromJsonElement(serializer, baseline),
            json.decodeFromJsonElement(serializer, future),
        )
    }

    @Test
    fun denebCronEditRejectsMissingRequiredCronId() {
        assertFailsWith<SerializationException> {
            json.decodeFromJsonElement(
                DenebCronEdit.serializer(),
                buildJsonObject {},
            )
        }
    }
}
