package ai.deneb.data

import com.russhwolf.settings.MapSettings
import kotlinx.coroutines.test.runTest
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlin.time.Clock

class HeartbeatManagerTest {

    private data class Fixture(
        val settings: AppSettings,
        val memories: MemoryStore,
        val tasks: TaskStore,
        val emails: EmailStore,
        val manager: HeartbeatManager,
    )

    private fun fixture(): Fixture {
        val settings = AppSettings(MapSettings())
        val memories = MemoryStore(settings)
        val tasks = TaskStore(settings)
        val emails = EmailStore(settings)
        return Fixture(
            settings = settings,
            memories = memories,
            tasks = tasks,
            emails = emails,
            manager = HeartbeatManager(settings, memories, tasks, emails),
        )
    }

    private fun emailAccount(id: String = "personal") = EmailAccount(
        id = id,
        email = "$id@example.com",
        imapHost = "imap.example.com",
        smtpHost = "smtp.example.com",
        username = id,
    )

    private fun email(accountId: String = "personal") = EmailMessage(
        uid = 42,
        accountId = accountId,
        from = "sender@example.com",
        subject = "Review",
        preview = "Please review the document",
    )

    private fun sms(id: Long = 7) = SmsMessage(
        id = id,
        address = "+821012345678",
        date = 1,
        preview = "Verification code",
    )

    @Test
    fun missingAndMalformedConfigFallBackToDefaults() {
        val f = fixture()

        assertEquals(HeartbeatConfig(), f.manager.getConfig())

        f.settings.setHeartbeatConfigJson("not-json")
        assertEquals(HeartbeatConfig(), f.manager.getConfig())

        f.settings.setHeartbeatConfigJson("{}")
        assertEquals(HeartbeatConfig(), f.manager.getConfig())
    }

    @Test
    fun configRoundTripsEveryField() {
        val f = fixture()
        val expected = HeartbeatConfig(
            enabled = false,
            intervalMinutes = 45,
            activeHoursStart = 6,
            activeHoursEnd = 23,
            lastHeartbeatEpochMs = 123_456,
            heartbeatInstanceId = "instance-a",
        )

        f.manager.saveConfig(expected)

        assertEquals(expected, f.manager.getConfig())
        assertTrue(f.settings.getHeartbeatConfigJson().contains("instance-a"))
    }

    @Test
    fun disabledHeartbeatIsNeverDue() {
        val f = fixture()
        f.manager.saveConfig(
            HeartbeatConfig(
                enabled = false,
                intervalMinutes = 0,
                activeHoursStart = 0,
                activeHoursEnd = 24,
            ),
        )

        assertFalse(f.manager.isHeartbeatDue())
    }

    @Test
    fun heartbeatOutsideActiveHoursIsNotDue() {
        val f = fixture()
        f.manager.saveConfig(
            HeartbeatConfig(
                enabled = true,
                intervalMinutes = 1,
                activeHoursStart = 0,
                activeHoursEnd = 0,
                lastHeartbeatEpochMs = 0,
            ),
        )

        assertFalse(f.manager.isHeartbeatDue())
    }

    @Test
    fun elapsedHeartbeatInsideAllDayWindowIsDue() {
        val f = fixture()
        f.manager.saveConfig(
            HeartbeatConfig(
                enabled = true,
                intervalMinutes = 1,
                activeHoursStart = 0,
                activeHoursEnd = 24,
                lastHeartbeatEpochMs = 0,
            ),
        )

        assertTrue(f.manager.isHeartbeatDue())
    }

    @Test
    fun recentHeartbeatInsideActiveHoursWaitsForInterval() {
        val f = fixture()
        f.manager.saveConfig(
            HeartbeatConfig(
                enabled = true,
                intervalMinutes = 30,
                activeHoursStart = 0,
                activeHoursEnd = 24,
                lastHeartbeatEpochMs = Clock.System.now().toEpochMilliseconds(),
            ),
        )

        assertFalse(f.manager.isHeartbeatDue())
    }

    @Test
    fun missingAndMalformedLogReadAsEmpty() {
        val f = fixture()

        assertEquals(emptyList(), f.manager.getHeartbeatLog())

        f.settings.setHeartbeatLogJson("broken")
        assertEquals(emptyList(), f.manager.getHeartbeatLog())
    }

    @Test
    fun heartbeatLogIsNewestFirstAndCappedAtFive() {
        val f = fixture()

        repeat(7) { index ->
            f.manager.recordHeartbeat(success = index % 2 == 0, error = "error-$index")
        }

        val log = f.manager.getHeartbeatLog()
        assertEquals(5, log.size)
        assertEquals(listOf("error-6", "error-5", "error-4", "error-3", "error-2"), log.map { it.error })
        assertEquals(listOf(true, false, true, false, true), log.map { it.success })
        assertTrue(log.all { it.timestampEpochMs > 0 })
    }

    @Test
    fun markExecutedUpdatesOnlyLastHeartbeatFields() {
        val f = fixture()
        val original = HeartbeatConfig(
            enabled = false,
            intervalMinutes = 17,
            activeHoursStart = 4,
            activeHoursEnd = 19,
            lastHeartbeatEpochMs = 1,
            heartbeatInstanceId = "worker",
        )
        val before = Clock.System.now().toEpochMilliseconds()

        f.manager.markHeartbeatExecuted(original)

        val saved = f.manager.getConfig()
        assertEquals(original.copy(lastHeartbeatEpochMs = saved.lastHeartbeatEpochMs), saved)
        assertTrue(saved.lastHeartbeatEpochMs >= before)
        assertTrue(saved.lastHeartbeatEpochMs <= Clock.System.now().toEpochMilliseconds())
    }

    @Test
    fun emptyPromptUsesDefaultAndCustomPromptWinsVerbatim() {
        val f = fixture()

        val defaultPrompt = f.manager.buildHeartbeatPrompt()
        assertTrue(defaultPrompt.startsWith(HeartbeatManager.DEFAULT_HEARTBEAT_PROMPT))

        f.settings.setHeartbeatPrompt("CUSTOM CHECK")
        val custom = f.manager.buildHeartbeatPrompt()
        assertTrue(custom.startsWith("CUSTOM CHECK\n"))
        assertFalse(custom.contains(HeartbeatManager.DEFAULT_HEARTBEAT_PROMPT))
    }

    @Test
    fun managerWiresTasksMemoriesMailAndSmsIntoPrompt() = runTest {
        val f = fixture()
        f.settings.setSmsEnabled(true)
        f.emails.addAccount(emailAccount())
        f.emails.updateSyncState(
            EmailSyncState(
                accountId = "personal",
                unreadCount = 3,
                lastSyncEpochMs = 123,
            ),
        )
        f.tasks.addTask(
            description = "Standing check",
            prompt = "Inspect the queue",
            scheduledAtEpochMs = 0,
            trigger = TaskTrigger.HEARTBEAT,
        )
        f.tasks.addTask(
            description = "One shot",
            prompt = "Follow up",
            scheduledAtEpochMs = Long.MAX_VALUE,
            trigger = TaskTrigger.TIME,
        )
        f.memories.store("preference", "Use concise Korean", MemoryCategory.PREFERENCE)
        repeat(4) { f.memories.reinforceMemory("preference") }

        val prompt = f.manager.buildHeartbeatPrompt(
            recentResponses = listOf("HEARTBEAT_OK"),
            pendingEmails = listOf(email()),
            pendingSms = listOf(sms()),
        )

        assertTrue(prompt.contains("## Heartbeat Additions"))
        assertTrue(prompt.contains("Standing check"))
        assertTrue(prompt.contains("## Pending Tasks"))
        assertTrue(prompt.contains("One shot"))
        assertTrue(prompt.contains("## Previous Heartbeat Results"))
        assertTrue(prompt.contains("personal@example.com"))
        assertTrue(prompt.contains("## New Emails"))
        assertTrue(prompt.contains("## New SMS"))
        assertTrue(prompt.contains("## Promotion Candidates"))
        assertTrue(prompt.contains("Use concise Korean"))
    }

    @Test
    fun featureGatesSuppressPendingMailAndSmsSections() = runTest {
        val f = fixture()
        f.emails.addAccount(emailAccount())
        f.settings.setEmailEnabled(false)
        f.settings.setSmsEnabled(false)

        val prompt = f.manager.buildHeartbeatPrompt(
            pendingEmails = listOf(email()),
            pendingSms = listOf(sms()),
        )

        assertFalse(prompt.contains("## Email Status"))
        assertFalse(prompt.contains("## New Emails"))
        assertFalse(prompt.contains("## New SMS"))
    }

    @Test
    fun unknownPendingEmailAccountFallsBackToAccountId() = runTest {
        val f = fixture()

        val prompt = f.manager.buildHeartbeatPrompt(
            pendingEmails = listOf(email(accountId = "orphan-account")),
        )

        assertTrue(prompt.contains("[orphan-account]"))
    }
}
