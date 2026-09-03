package ai.deneb.deneb

import io.ktor.http.ContentType
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertTrue

/**
 * Endpoint-level safety matrix for native administrative and mutation facades.
 *
 * Each wrapper is checked independently for its exact method/key contract,
 * credential short-circuit, and structured-concurrency cancellation behavior.
 */
class GatewayEndpointSafetyMatrixTest {

    private fun gatewayEndpointSafetyCases(): List<suspend () -> Unit> = listOf(
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.refreshModels()

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.models.list")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(emptySet(), params.keys)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.refreshModels()

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel refreshModels"))

            val failure = assertFailsWith<CancellationException> {
                f.client.refreshModels()
            }

            assertEquals("cancel refreshModels", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.models.list", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.setRoleModel("provider/model", "fallback")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.models.set")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id", "role"), params.keys)
            assertEquals("provider/model", params["id"]?.jsonPrimitive?.content)
            assertEquals("fallback", params["role"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.setRoleModel("provider/model", "fallback")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel setRoleModel"))

            val failure = assertFailsWith<CancellationException> {
                f.client.setRoleModel("provider/model", "fallback")
            }

            assertEquals("cancel setRoleModel", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.models.set", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.addCustomModel("https://model.example/v1", "org/model")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.models.add_custom")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("endpoint", "model"), params.keys)
            assertEquals("https://model.example/v1", params["endpoint"]?.jsonPrimitive?.content)
            assertEquals("org/model", params["model"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.addCustomModel("https://model.example/v1", "org/model")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel addCustomModel"))

            val failure = assertFailsWith<CancellationException> {
                f.client.addCustomModel("https://model.example/v1", "org/model")
            }

            assertEquals("cancel addCustomModel", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.models.add_custom", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.deleteCustomModel("custom:model")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.models.delete_custom")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id"), params.keys)
            assertEquals("custom:model", params["id"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.deleteCustomModel("custom:model")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel deleteCustomModel"))

            val failure = assertFailsWith<CancellationException> {
                f.client.deleteCustomModel("custom:model")
            }

            assertEquals("cancel deleteCustomModel", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.models.delete_custom", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.refreshSkills()

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.skills.list")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(emptySet(), params.keys)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.refreshSkills()

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel refreshSkills"))

            val failure = assertFailsWith<CancellationException> {
                f.client.refreshSkills()
            }

            assertEquals("cancel refreshSkills", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.skills.list", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.fetchSkillLifecycle(limit = 17, skillName = "coding/github")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.skills.lifecycle")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("limit", "skillName", "translate"), params.keys)
            assertEquals(17, params["limit"]?.jsonPrimitive?.content?.toInt())
            assertEquals("coding/github", params["skillName"]?.jsonPrimitive?.content)
            // The Propus timeline is read by a person and the review models
            // answer in English, so the client asks for Korean. Tooling that
            // reads the same log omits the flag and keeps the original words.
            assertEquals("true", params["translate"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.fetchSkillLifecycle(limit = 17, skillName = "coding/github")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel fetchSkillLifecycle"))

            val failure = assertFailsWith<CancellationException> {
                f.client.fetchSkillLifecycle(limit = 17, skillName = "coding/github")
            }

            assertEquals("cancel fetchSkillLifecycle", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.skills.lifecycle", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.fetchSelfImprovementCodingQueue(limit = 23, status = "review")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.self_improvement_coding.list")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("limit", "status", "translate"), params.keys)
            assertEquals(23, params["limit"]?.jsonPrimitive?.content?.toInt())
            assertEquals("review", params["status"]?.jsonPrimitive?.content)
            // This screen is read by a person and the review models answer in
            // English, so the client always asks for Korean. The flag is opt-in
            // on the gateway so the dispatch selector and the L4 miners — which
            // feed a coding agent its instructions — keep the original text.
            assertEquals("true", params["translate"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.fetchSelfImprovementCodingQueue(limit = 23, status = "review")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel fetchSelfImprovementCodingQueue"))

            val failure = assertFailsWith<CancellationException> {
                f.client.fetchSelfImprovementCodingQueue(limit = 23, status = "review")
            }

            assertEquals("cancel fetchSelfImprovementCodingQueue", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.self_improvement_coding.list", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.fetchSkillDetail("coding/github")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.skills.detail")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("name"), params.keys)
            assertEquals("coding/github", params["name"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.fetchSkillDetail("coding/github")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel fetchSkillDetail"))

            val failure = assertFailsWith<CancellationException> {
                f.client.fetchSkillDetail("coding/github")
            }

            assertEquals("cancel fetchSkillDetail", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.skills.detail", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.updateSkill("local/skill", "# Skill\nBody")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.skills.update")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("name", "body"), params.keys)
            assertEquals("local/skill", params["name"]?.jsonPrimitive?.content)
            assertEquals("# Skill\nBody", params["body"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.updateSkill("local/skill", "# Skill\nBody")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel updateSkill"))

            val failure = assertFailsWith<CancellationException> {
                f.client.updateSkill("local/skill", "# Skill\nBody")
            }

            assertEquals("cancel updateSkill", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.skills.update", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.deleteSkill("local/skill")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.skills.delete")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("name"), params.keys)
            assertEquals("local/skill", params["name"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.deleteSkill("local/skill")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel deleteSkill"))

            val failure = assertFailsWith<CancellationException> {
                f.client.deleteSkill("local/skill")
            }

            assertEquals("cancel deleteSkill", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.skills.delete", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.removeCron("cron-7")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.crons.remove")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id"), params.keys)
            assertEquals("cron-7", params["id"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.removeCron("cron-7")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel removeCron"))

            val failure = assertFailsWith<CancellationException> {
                f.client.removeCron("cron-7")
            }

            assertEquals("cancel removeCron", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.crons.remove", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.refreshScheduledTasks()

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.crons.list")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("includeDisabled"), params.keys)
            assertEquals(true, params["includeDisabled"]?.jsonPrimitive?.content?.toBoolean())
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.refreshScheduledTasks()

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel refreshScheduledTasks"))

            val failure = assertFailsWith<CancellationException> {
                f.client.refreshScheduledTasks()
            }

            assertEquals("cancel refreshScheduledTasks", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.crons.list", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.runCron("cron-run")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.crons.run")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id"), params.keys)
            assertEquals("cron-run", params["id"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.runCron("cron-run")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel runCron"))

            val failure = assertFailsWith<CancellationException> {
                f.client.runCron("cron-run")
            }

            assertEquals("cancel runCron", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.crons.run", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.fetchCron("cron-detail")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.crons.get")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id"), params.keys)
            assertEquals("cron-detail", params["id"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.fetchCron("cron-detail")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel fetchCron"))

            val failure = assertFailsWith<CancellationException> {
                f.client.fetchCron("cron-detail")
            }

            assertEquals("cancel fetchCron", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.crons.get", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.setCronEnabled("cron-toggle", false)

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.crons.update")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id", "enabled"), params.keys)
            assertEquals("cron-toggle", params["id"]?.jsonPrimitive?.content)
            assertEquals(false, params["enabled"]?.jsonPrimitive?.content?.toBoolean())
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.setCronEnabled("cron-toggle", false)

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel setCronEnabled"))

            val failure = assertFailsWith<CancellationException> {
                f.client.setCronEnabled("cron-toggle", false)
            }

            assertEquals("cancel setCronEnabled", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.crons.update", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.updateCron("cron-edit", name = "Daily", schedule = "0 9 * * *", tz = "Asia/Seoul", prompt = "Brief", model = "provider/model")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.crons.update")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id", "name", "schedule", "tz", "prompt", "model"), params.keys)
            assertEquals("cron-edit", params["id"]?.jsonPrimitive?.content)
            assertEquals("Daily", params["name"]?.jsonPrimitive?.content)
            assertEquals("0 9 * * *", params["schedule"]?.jsonPrimitive?.content)
            assertEquals("Asia/Seoul", params["tz"]?.jsonPrimitive?.content)
            assertEquals("Brief", params["prompt"]?.jsonPrimitive?.content)
            assertEquals("provider/model", params["model"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.updateCron("cron-edit", name = "Daily", schedule = "0 9 * * *", tz = "Asia/Seoul", prompt = "Brief", model = "provider/model")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel updateCron"))

            val failure = assertFailsWith<CancellationException> {
                f.client.updateCron("cron-edit", name = "Daily", schedule = "0 9 * * *", tz = "Asia/Seoul", prompt = "Brief", model = "provider/model")
            }

            assertEquals("cancel updateCron", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.crons.update", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueRpc(payload = "{}", ok = false)

            f.client.refreshClientStatus()

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.client.hello")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(emptySet(), params.keys)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.refreshClientStatus()

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel refreshClientStatus"))

            val failure = assertFailsWith<CancellationException> {
                f.client.refreshClientStatus()
            }

            assertEquals("cancel refreshClientStatus", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.client.hello", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.registerPushToken("fcm-token", "android")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.push.register")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("token", "platform"), params.keys)
            assertEquals("fcm-token", params["token"]?.jsonPrimitive?.content)
            assertEquals("android", params["platform"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.registerPushToken("fcm-token", "android")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel registerPushToken"))

            val failure = assertFailsWith<CancellationException> {
                f.client.registerPushToken("fcm-token", "android")
            }

            assertEquals("cancel registerPushToken", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.push.register", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.unregisterPushToken("fcm-token")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.push.unregister")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("token"), params.keys)
            assertEquals("fcm-token", params["token"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.unregisterPushToken("fcm-token")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel unregisterPushToken"))

            val failure = assertFailsWith<CancellationException> {
                f.client.unregisterPushToken("fcm-token")
            }

            assertEquals("cancel unregisterPushToken", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.push.unregister", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.createCalendarEvent("Summary", "Description", "Room", false, "start", "end", "Asia/Seoul")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.calendar.create")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("summary", "description", "location", "allDay", "start", "end", "timeZone"), params.keys)
            assertEquals("Summary", params["summary"]?.jsonPrimitive?.content)
            assertEquals("Description", params["description"]?.jsonPrimitive?.content)
            assertEquals("Room", params["location"]?.jsonPrimitive?.content)
            assertEquals(false, params["allDay"]?.jsonPrimitive?.content?.toBoolean())
            assertEquals("start", params["start"]?.jsonPrimitive?.content)
            assertEquals("end", params["end"]?.jsonPrimitive?.content)
            assertEquals("Asia/Seoul", params["timeZone"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.createCalendarEvent("Summary", "Description", "Room", false, "start", "end", "Asia/Seoul")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel createCalendarEvent"))

            val failure = assertFailsWith<CancellationException> {
                f.client.createCalendarEvent("Summary", "Description", "Room", false, "start", "end", "Asia/Seoul")
            }

            assertEquals("cancel createCalendarEvent", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.calendar.create", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.deleteCalendarEvent("event-7")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.calendar.delete")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id"), params.keys)
            assertEquals("event-7", params["id"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.deleteCalendarEvent("event-7")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel deleteCalendarEvent"))

            val failure = assertFailsWith<CancellationException> {
                f.client.deleteCalendarEvent("event-7")
            }

            assertEquals("cancel deleteCalendarEvent", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.calendar.delete", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.createTodo("Title", "Note", "2026-07-31", true)

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.todo.create")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("title", "note", "due", "dueAllDay"), params.keys)
            assertEquals("Title", params["title"]?.jsonPrimitive?.content)
            assertEquals("Note", params["note"]?.jsonPrimitive?.content)
            assertEquals("2026-07-31", params["due"]?.jsonPrimitive?.content)
            assertEquals(true, params["dueAllDay"]?.jsonPrimitive?.content?.toBoolean())
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.createTodo("Title", "Note", "2026-07-31", true)

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel createTodo"))

            val failure = assertFailsWith<CancellationException> {
                f.client.createTodo("Title", "Note", "2026-07-31", true)
            }

            assertEquals("cancel createTodo", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.todo.create", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.setTodoDone("todo-7", true)

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.todo.set_done")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id", "done"), params.keys)
            assertEquals("todo-7", params["id"]?.jsonPrimitive?.content)
            assertEquals(true, params["done"]?.jsonPrimitive?.content?.toBoolean())
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.setTodoDone("todo-7", true)

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel setTodoDone"))

            val failure = assertFailsWith<CancellationException> {
                f.client.setTodoDone("todo-7", true)
            }

            assertEquals("cancel setTodoDone", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.todo.set_done", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.deleteTodo("todo-7")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.todo.delete")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("id"), params.keys)
            assertEquals("todo-7", params["id"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.deleteTodo("todo-7")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel deleteTodo"))

            val failure = assertFailsWith<CancellationException> {
                f.client.deleteTodo("todo-7")
            }

            assertEquals("cancel deleteTodo", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.todo.delete", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.filesDelete("Folder/file.txt")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.files.delete")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("path"), params.keys)
            assertEquals("Folder/file.txt", params["path"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.filesDelete("Folder/file.txt")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel filesDelete"))

            val failure = assertFailsWith<CancellationException> {
                f.client.filesDelete("Folder/file.txt")
            }

            assertEquals("cancel filesDelete", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.files.delete", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.filesMkdir("Folder/New")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.files.mkdir")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("path"), params.keys)
            assertEquals("Folder/New", params["path"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.filesMkdir("Folder/New")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel filesMkdir"))

            val failure = assertFailsWith<CancellationException> {
                f.client.filesMkdir("Folder/New")
            }

            assertEquals("cancel filesMkdir", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.files.mkdir", f.transport.singleRequest().rpcMethod)
        },
        {
            val f = gatewayClientFixture(token = "endpoint-token")
            f.transport.enqueueWrite(ok = false, message = "rejected")

            f.client.filesMove("Folder/old", "Folder/new")

            val request = f.transport.requests.first()
            val params = request.requireRpc("miniapp.files.move")
            assertEquals("POST", request.method.value)
            assertEquals(ContentType.Application.Json, request.bodyContentType?.withoutParameters())
            assertEquals("endpoint-token", request.header(DenebGatewayClient.CLIENT_TOKEN_HEADER))
            assertEquals(setOf("src", "dst"), params.keys)
            assertEquals("Folder/old", params["src"]?.jsonPrimitive?.content)
            assertEquals("Folder/new", params["dst"]?.jsonPrimitive?.content)
        },
        {
            val f = gatewayClientFixture(token = "")

            f.client.filesMove("Folder/old", "Folder/new")

            assertTrue(f.transport.requests.isEmpty())
        },
        {
            val f = gatewayClientFixture()
            f.transport.enqueueFailure(CancellationException("cancel filesMove"))

            val failure = assertFailsWith<CancellationException> {
                f.client.filesMove("Folder/old", "Folder/new")
            }

            assertEquals("cancel filesMove", failure.message)
            assertEquals(1, f.transport.requests.size)
            assertEquals("miniapp.files.move", f.transport.singleRequest().rpcMethod)
        },
    )

    @Test
    fun gatewayEndpointsSkipNetworkWhenCredentialMissingAndPropagateCancel() = runTest {
        for (case in gatewayEndpointSafetyCases()) {
            case()
        }
    }
}
