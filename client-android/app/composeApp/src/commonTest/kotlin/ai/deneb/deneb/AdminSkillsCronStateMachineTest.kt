package ai.deneb.deneb

import ai.deneb.data.TaskStatus
import ai.deneb.data.TaskTrigger
import ai.deneb.deneb.generated.MiniappCronDetail
import ai.deneb.deneb.generated.MiniappCronRow
import ai.deneb.deneb.generated.SelfCorrectionCandidate
import ai.deneb.deneb.generated.SelfImprovementCodingListResponse
import ai.deneb.deneb.generated.SkillDetailResponse
import ai.deneb.deneb.generated.SkillLifecycleEvent
import ai.deneb.deneb.generated.SkillRow
import ai.deneb.deneb.generated.SkillsLifecycleResponse
import ai.deneb.deneb.generated.SkillsListResponse
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNotNull
import kotlin.test.assertNull
import kotlin.test.assertTrue

class AdminSkillsCronStateMachineTest {

    private val json = Json { encodeDefaults = true }

    private fun skill(name: String, editable: Boolean = false) = SkillRow(
        name = name,
        description = "description-$name",
        category = "coding",
        editable = editable,
        deletable = editable,
    )

    private fun cron(
        id: String,
        name: String = "",
        schedule: String = "",
        prompt: String = "",
        nextRunAtMs: Long = 0,
        lastError: String = "",
        consecutiveErrors: Int = 0,
    ) = MiniappCronRow(
        id = id,
        name = name,
        schedule = schedule,
        payloadPreview = prompt,
        nextRunAtMs = nextRunAtMs,
        lastError = lastError,
        consecutiveErrors = consecutiveErrors,
    )

    private fun skillsPayload(vararg skills: SkillRow): String = json.encodeToString(
        SkillsListResponse(skills = skills.toList(), count = skills.size),
    )

    private fun cronPayload(vararg jobs: MiniappCronRow): String = json.encodeToString(
        CronListPayload(jobs.toList()),
    )

    @Test
    fun refreshSkillsReplacesSnapshotWithServerRows() = runTest {
        val f = gatewayClientFixture()
        f.client._denebSkills.value = listOf(skill("old"))
        f.transport.enqueueRpc(skillsPayload(skill("one"), skill("two", editable = true)))

        assertTrue(f.client.refreshSkills())

        assertEquals(listOf("one", "two"), f.client.denebSkills.value.map { it.name })
        assertTrue(f.client.denebSkills.value.last().editable)
    }

    @Test
    fun refreshSkillsPreservesLastSnapshotOnTransportFailure() = runTest {
        val f = gatewayClientFixture()
        f.client._denebSkills.value = listOf(skill("cached"))
        f.transport.enqueueJson("bad")

        assertFalse(f.client.refreshSkills())

        assertEquals(listOf("cached"), f.client.denebSkills.value.map { it.name })
    }

    @Test
    fun refreshSkillsAcceptsAuthoritativeEmptyList() = runTest {
        val f = gatewayClientFixture()
        f.client._denebSkills.value = listOf(skill("cached"))
        f.transport.enqueueRpc(skillsPayload())

        assertTrue(f.client.refreshSkills())

        assertTrue(f.client.denebSkills.value.isEmpty())
    }

    @Test
    fun skillLifecycleSendsLimitAndOptionalSkillFilter() = runTest {
        val f = gatewayClientFixture()
        val response = SkillsLifecycleResponse(
            events = listOf(SkillLifecycleEvent(type = "evolved", skillName = "github", at = 10)),
            count = 1,
        )
        f.transport.enqueueRpc(json.encodeToString(response))

        val events = f.client.fetchSkillLifecycle(limit = 17, skillName = "github")

        assertEquals(response.events, events)
        val params = f.transport.singleRequest().requireRpc("miniapp.skills.lifecycle")
        assertEquals(17, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals("github", params["skillName"]?.jsonPrimitive?.content)
    }

    @Test
    fun blankSkillLifecycleFilterIsOmitted() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(SkillsLifecycleResponse()))

        f.client.fetchSkillLifecycleResponse(skillName = "  ")

        val params = f.transport.singleRequest().rpcParams.orEmpty()
        assertFalse(params.containsKey("skillName"))
        assertEquals(60, params["limit"]?.jsonPrimitive?.content?.toInt())
    }

    @Test
    fun lifecycleFailureReturnsNullNotEmptyActivity() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("bad")

        assertNull(f.client.fetchSkillLifecycle())
    }

    @Test
    fun selfImprovementQueueSendsStatusWhenNonBlank() = runTest {
        val f = gatewayClientFixture()
        val candidate = SelfCorrectionCandidate(id = "c1", status = "proposed", title = "Fix retry")
        val response = SelfImprovementCodingListResponse(candidates = listOf(candidate), count = 1)
        f.transport.enqueueRpc(json.encodeToString(response))

        val result = f.client.fetchSelfImprovementCodingQueue(limit = 12, status = "proposed")

        assertEquals(listOf(candidate), result?.candidates)
        val params = f.transport.singleRequest().requireRpc("miniapp.self_improvement_coding.list")
        assertEquals(12, params["limit"]?.jsonPrimitive?.content?.toInt())
        assertEquals("proposed", params["status"]?.jsonPrimitive?.content)
    }

    @Test
    fun blankSelfImprovementStatusIsOmittedForAllStatusesView() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(json.encodeToString(SelfImprovementCodingListResponse()))

        f.client.fetchSelfImprovementCodingQueue(status = "")

        assertFalse(f.transport.singleRequest().rpcParams.orEmpty().containsKey("status"))
    }

    @Test
    fun candidateConvenienceReturnsRowsAndPreservesNullFailure() = runTest {
        val success = gatewayClientFixture()
        val rows = listOf(SelfCorrectionCandidate(id = "one"), SelfCorrectionCandidate(id = "two"))
        success.transport.enqueueRpc(json.encodeToString(SelfImprovementCodingListResponse(candidates = rows)))
        assertEquals(rows, success.client.fetchSelfImprovementCodingCandidates())

        val failure = gatewayClientFixture()
        failure.transport.enqueueJson("bad")
        assertNull(failure.client.fetchSelfImprovementCodingCandidates())
    }

    @Test
    fun skillDetailSendsExactNameAndReturnsBody() = runTest {
        val f = gatewayClientFixture()
        val response = SkillDetailResponse(
            skill = skill("github", editable = true),
            body = "# GitHub\nbody",
            path = "/skills/github/SKILL.md",
        )
        f.transport.enqueueRpc(json.encodeToString(response))

        val result = f.client.fetchSkillDetail("github")

        assertEquals(response, result)
        val params = f.transport.singleRequest().requireRpc("miniapp.skills.detail")
        assertEquals("github", params["name"]?.jsonPrimitive?.content)
    }

    @Test
    fun updateSkillRefreshesListOnlyAfterSuccessfulWrite() = runTest {
        val success = gatewayClientFixture()
        success.transport.enqueueWrite(ok = true)
        success.transport.enqueueRpc(skillsPayload(skill("updated")))

        assertNull(success.client.updateSkill("github", "# Updated"))
        assertEquals(listOf("miniapp.skills.update", "miniapp.skills.list"), success.transport.requestMethods())
        val params = success.transport.requests.first().rpcParams.orEmpty()
        assertEquals("github", params["name"]?.jsonPrimitive?.content)
        assertEquals("# Updated", params["body"]?.jsonPrimitive?.content)

        val failure = gatewayClientFixture()
        failure.transport.enqueueWrite(ok = false, message = "protected")
        assertEquals("protected", failure.client.updateSkill("system", "body"))
        assertEquals(1, failure.transport.requests.size)
    }

    @Test
    fun deleteSkillRefreshesListOnlyAfterSuccessfulWrite() = runTest {
        val success = gatewayClientFixture()
        success.transport.enqueueWrite(ok = true)
        success.transport.enqueueRpc(skillsPayload())

        assertNull(success.client.deleteSkill("local"))
        assertEquals(listOf("miniapp.skills.delete", "miniapp.skills.list"), success.transport.requestMethods())
        assertEquals("local", success.transport.requests.first().rpcParams?.get("name")?.jsonPrimitive?.content)

        val failure = gatewayClientFixture()
        failure.transport.enqueueWrite(ok = false, message = "cannot delete")
        assertEquals("cannot delete", failure.client.deleteSkill("bundled"))
        assertEquals(1, failure.transport.requests.size)
    }

    @Test
    fun skillMutationCancellationPropagatesWithoutRefresh() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel skill"))

        val failure = assertFailsWith<CancellationException> {
            f.client.updateSkill("local", "body")
        }

        assertEquals("cancel skill", failure.message)
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun scheduledTaskRefreshRequestsDisabledJobsToo() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(cronPayload())

        assertTrue(f.client.loadScheduledTasks())

        val params = f.transport.singleRequest().requireRpc("miniapp.crons.list")
        assertEquals(true, params["includeDisabled"]?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun scheduledTaskRefreshFiltersBlankAndDeduplicatesIds() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            cronPayload(
                cron(""),
                cron("one", name = "first"),
                cron("one", name = "latest"),
                cron("two"),
            ),
        )

        f.client.loadScheduledTasks()

        assertEquals(listOf("one", "two"), f.client.denebScheduledTasks.value.map { it.id })
        assertEquals("latest", f.client.denebScheduledTasks.value.first().description)
    }

    @Test
    fun cronRowMapsRuntimeTaskFieldsAndFallbacks() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            cronPayload(
                cron(
                    id = "job-1",
                    name = "",
                    schedule = "0 9 * * *",
                    prompt = "Prepare report",
                    nextRunAtMs = 123,
                    lastError = "last failure",
                    consecutiveErrors = 3,
                ),
            ),
        )

        f.client.loadScheduledTasks()
        val task = f.client.denebScheduledTasks.value.single()

        assertEquals("job-1", task.description)
        assertEquals("Prepare report", task.prompt)
        assertEquals(123, task.scheduledAtEpochMs)
        assertEquals("0 9 * * *", task.cron)
        assertEquals(TaskTrigger.CRON, task.trigger)
        assertEquals(TaskStatus.PENDING, task.status)
        assertEquals("last failure", task.lastResult)
        assertEquals(3, task.consecutiveFailures)
    }

    @Test
    fun blankCronAndErrorMapToNullOptionalFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(cronPayload(cron("job")))

        f.client.loadScheduledTasks()
        val task = f.client.denebScheduledTasks.value.single()

        assertNull(task.cron)
        assertNull(task.lastResult)
    }

    @Test
    fun failedScheduledTaskRefreshPreservesSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(cronPayload(cron("cached")))
        f.client.loadScheduledTasks()
        f.transport.enqueueJson("bad")

        assertFalse(f.client.loadScheduledTasks())

        assertEquals(listOf("cached"), f.client.denebScheduledTasks.value.map { it.id })
    }

    @Test
    fun removeCronRefreshesEvenWhenRemoveFails() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("bad")
        f.transport.enqueueRpc(cronPayload(cron("still-there")))

        assertFalse(f.client.removeCron("job"))

        assertEquals(listOf("miniapp.crons.remove", "miniapp.crons.list"), f.transport.requestMethods())
        assertEquals("job", f.transport.requests.first().rpcParams?.get("id")?.jsonPrimitive?.content)
        assertEquals("still-there", f.client.denebScheduledTasks.value.single().id)
    }

    @Test
    fun runCronAndEnabledPatchUseExactMethods() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")
        f.transport.enqueueRpc("{}")

        assertTrue(f.client.runCron("job"))
        assertTrue(f.client.setCronEnabled("job", false))

        val run = f.transport.requests[0].requireRpc("miniapp.crons.run")
        assertEquals("job", run["id"]?.jsonPrimitive?.content)
        val update = f.transport.requests[1].requireRpc("miniapp.crons.update")
        assertEquals("job", update["id"]?.jsonPrimitive?.content)
        assertEquals(false, update["enabled"]?.jsonPrimitive?.content?.toBoolean())
    }

    @Test
    fun fetchCronMapsFullDetailContract() = runTest {
        val f = gatewayClientFixture()
        val detail = MiniappCronDetail(
            id = "job",
            name = "Morning",
            enabled = true,
            schedule = "daily",
            scheduleSpec = "09:00",
            scheduleKind = "every",
            timezone = "Asia/Seoul",
            payloadKind = "agentTurn",
            prompt = "Brief me",
            model = "main/model",
            deliveryChannel = "telegram",
            deliveryTo = "room",
            nextRunAtMs = 100,
            lastDeliveryStatus = "sent",
            lastError = "",
            consecutiveErrors = 2,
            autoDisabledAtMs = 0,
        )
        f.transport.enqueueRpc(json.encodeToString(detail))

        val result = f.client.fetchCron("job")

        assertNotNull(result)
        assertEquals("job", result.id)
        assertEquals("Morning", result.name)
        assertTrue(result.enabled)
        assertEquals("09:00", result.scheduleSpec)
        assertEquals("Asia/Seoul", result.timezone)
        assertEquals("Brief me", result.prompt)
        assertEquals("telegram", result.deliveryChannel)
        assertEquals(2, result.consecutiveErrors)
    }

    @Test
    fun updateCronOmitsEveryNullPatchField() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "reject")

        val error = f.client.updateCron(id = "job")

        assertEquals("reject", error)
        val params = f.transport.singleRequest().requireRpc("miniapp.crons.update")
        assertEquals(setOf("id"), params.keys)
    }

    @Test
    fun updateCronIncludesExplicitBlankValuesForClearingFields() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "reject")

        f.client.updateCron(
            id = "job",
            name = "",
            schedule = "",
            tz = "",
            prompt = "",
            model = "",
        )

        val params = f.transport.singleRequest().rpcParams.orEmpty()
        assertEquals(setOf("id", "name", "schedule", "tz", "prompt", "model"), params.keys)
        assertTrue(params.filterKeys { it != "id" }.values.all { it.jsonPrimitive.content.isEmpty() })
    }

    @Test
    fun successfulCronUpdateRefreshesCachedRows() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = true)
        f.transport.enqueueRpc(cronPayload(cron("job", name = "Updated")))

        assertNull(f.client.updateCron("job", name = "Updated"))

        assertEquals("Updated", f.client.denebScheduledTasks.value.single().description)
        assertEquals(listOf("miniapp.crons.update", "miniapp.crons.list"), f.transport.requestMethods())
    }

    @Test
    fun rejectedCronUpdateDoesNotRefresh() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueWrite(ok = false, message = "invalid schedule")

        assertEquals("invalid schedule", f.client.updateCron("job", schedule = "bad"))

        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun cronReadCancellationPropagatesWithoutClearingSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(cronPayload(cron("cached")))
        f.client.loadScheduledTasks()
        f.transport.enqueueFailure(CancellationException("cancel cron"))

        val failure = assertFailsWith<CancellationException> { f.client.loadScheduledTasks() }

        assertEquals("cancel cron", failure.message)
        assertEquals("cached", f.client.denebScheduledTasks.value.single().id)
    }
}
