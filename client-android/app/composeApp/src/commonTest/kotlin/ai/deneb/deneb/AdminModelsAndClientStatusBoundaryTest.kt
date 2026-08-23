package ai.deneb.deneb

import ai.deneb.deneb.generated.ModelSection
import ai.deneb.deneb.generated.RoleModel
import io.ktor.http.HttpStatusCode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.test.runTest
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertFalse
import kotlin.test.assertNull
import kotlin.test.assertTrue

class AdminModelsAndClientStatusBoundaryTest {

    private val json = Json { encodeDefaults = true }

    private fun wireModel(
        id: String,
        display: String = "",
        label: String = "",
        health: String = "",
        custom: Boolean = false,
        deletable: Boolean = false,
        unhealthy: Boolean = false,
        note: String = "",
    ) = ai.deneb.deneb.generated.ModelOption(
        id = id,
        display = display,
        label = label,
        health = health,
        custom = custom,
        deletable = deletable,
        unhealthy = unhealthy,
        note = note,
    )

    private fun modelsPayload(
        current: String = "",
        roles: List<RoleModel> = emptyList(),
        sections: List<ModelSection> = emptyList(),
        advisories: List<String> = emptyList(),
        mainHasVision: Boolean = false,
    ): String = json.encodeToString(
        ModelsPayload(
            current = current,
            roles = roles,
            sections = sections,
            advisories = advisories,
            mainHasVision = mainHasVision,
        ),
    )

    @Test
    fun refreshModelsFlattensSectionsInServerOrder() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            modelsPayload(
                sections = listOf(
                    ModelSection(title = "Cloud", models = listOf(wireModel("cloud/a"), wireModel("cloud/b"))),
                    ModelSection(title = "Local", models = listOf(wireModel("local/c"))),
                ),
            ),
        )

        assertTrue(f.client.refreshModels())

        assertEquals(listOf("cloud/a", "cloud/b", "local/c"), f.client.denebModels.value.map { it.id })
    }

    @Test
    fun refreshModelsDeduplicatesIdAcrossSectionsKeepingFirstDefinition() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            modelsPayload(
                sections = listOf(
                    ModelSection(models = listOf(wireModel("same", display = "first"))),
                    ModelSection(models = listOf(wireModel("same", display = "second"))),
                ),
            ),
        )

        f.client.refreshModels()

        assertEquals(1, f.client.denebModels.value.size)
        assertEquals("first", f.client.denebModels.value.single().display)
    }

    @Test
    fun refreshModelsDropsBlankModelIdentities() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            modelsPayload(
                sections = listOf(ModelSection(models = listOf(wireModel(""), wireModel("valid"), wireModel("   ")))),
            ),
        )

        f.client.refreshModels()

        assertEquals(listOf("valid"), f.client.denebModels.value.map { it.id })
    }

    @Test
    fun displayFallsBackFromDisplayToLabelToId() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            modelsPayload(
                sections = listOf(
                    ModelSection(
                        models = listOf(
                            wireModel("one", display = "Display", label = "Label"),
                            wireModel("two", display = "", label = "Label two"),
                            wireModel("three"),
                        ),
                    ),
                ),
            ),
        )

        f.client.refreshModels()

        assertEquals(listOf("Display", "Label two", "three"), f.client.denebModels.value.map { it.display })
    }

    @Test
    fun currentModelAndHealthMetadataArePreserved() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            modelsPayload(
                current = "model/current",
                sections = listOf(
                    ModelSection(
                        models = listOf(
                            wireModel(
                                id = "model/current",
                                health = "healthy",
                                custom = true,
                                deletable = true,
                                unhealthy = false,
                                note = "fast",
                            ),
                        ),
                    ),
                ),
            ),
        )

        f.client.refreshModels()
        val model = f.client.denebModels.value.single()

        assertTrue(model.current)
        assertEquals("healthy", model.health)
        assertTrue(model.custom)
        assertTrue(model.deletable)
        assertFalse(model.unhealthy)
        assertEquals("fast", model.note)
    }

    @Test
    fun roleModelsUseLastServerValueForDuplicateRole() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            modelsPayload(
                roles = listOf(
                    RoleModel(role = "main", model = "old"),
                    RoleModel(role = "fallback", model = "backup"),
                    RoleModel(role = "main", model = "new"),
                ),
            ),
        )

        f.client.refreshModels()

        assertEquals(mapOf("main" to "new", "fallback" to "backup"), f.client.denebRoleModels.value)
    }

    @Test
    fun advisoriesAndVisionFlagReplacePreviousSnapshot() = runTest {
        val f = gatewayClientFixture()
        f.client._denebModelAdvisories.value = listOf("old")
        f.client._denebMainHasVision.value = false
        f.transport.enqueueRpc(
            modelsPayload(advisories = listOf("a", "b"), mainHasVision = true),
        )

        f.client.refreshModels()

        assertEquals(listOf("a", "b"), f.client.denebModelAdvisories.value)
        assertTrue(f.client.denebMainHasVision.value)
    }

    @Test
    fun failedRefreshPreservesLastKnownModelsAndMetadata() = runTest {
        val f = gatewayClientFixture()
        f.client._denebModels.value = listOf(
            ModelOption(id = "cached", display = "Cached", current = true, health = "healthy"),
        )
        f.client._denebRoleModels.value = mapOf("main" to "cached")
        f.client._denebModelAdvisories.value = listOf("cached advice")
        f.client._denebMainHasVision.value = true
        f.transport.enqueueJson("bad")

        assertFalse(f.client.refreshModels())

        assertEquals(listOf("cached"), f.client.denebModels.value.map { it.id })
        assertEquals(mapOf("main" to "cached"), f.client.denebRoleModels.value)
        assertEquals(listOf("cached advice"), f.client.denebModelAdvisories.value)
        assertTrue(f.client.denebMainHasVision.value)
    }

    @Test
    fun setRoleModelSendsIdAndRoleThenReconcilesRegistry() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")
        f.transport.enqueueRpc(
            modelsPayload(
                current = "new-model",
                sections = listOf(ModelSection(models = listOf(wireModel("new-model")))),
            ),
        )

        val result = f.client.setRoleModel("new-model", "main")

        assertTrue(result)
        val params = f.transport.requests.first().requireRpc("miniapp.models.set")
        assertEquals("new-model", params["id"]?.jsonPrimitive?.content)
        assertEquals("main", params["role"]?.jsonPrimitive?.content)
        assertTrue(f.client.denebModels.value.single().current)
    }

    @Test
    fun failedRoleSwitchStillRefreshesRegistryToReconcileServerState() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("bad")
        f.transport.enqueueRpc(
            modelsPayload(sections = listOf(ModelSection(models = listOf(wireModel("server-current"))))),
        )

        val result = f.client.setMainModel("rejected")

        assertFalse(result)
        assertEquals(listOf("miniapp.models.set", "miniapp.models.list"), f.transport.requestMethods())
        assertEquals("server-current", f.client.denebModels.value.single().id)
    }

    @Test
    fun addCustomModelRefreshesOnlyAfterAcceptedWrite() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")
        f.transport.enqueueRpc(modelsPayload())

        assertTrue(f.client.addCustomModel("https://api.example/v1", "custom/model"))

        val params = f.transport.requests.first().requireRpc("miniapp.models.add_custom")
        assertEquals("https://api.example/v1", params["endpoint"]?.jsonPrimitive?.content)
        assertEquals("custom/model", params["model"]?.jsonPrimitive?.content)
        assertEquals("miniapp.models.list", f.transport.requests.last().rpcMethod)
    }

    @Test
    fun rejectedCustomModelAddDoesNotRefresh() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson("bad")

        assertFalse(f.client.addCustomModel("bad", "bad"))

        assertEquals(listOf("miniapp.models.add_custom"), f.transport.requestMethods())
    }

    @Test
    fun deleteCustomModelRefreshesOnlyOnSuccess() = runTest {
        val success = gatewayClientFixture()
        success.transport.enqueueRpc("{}")
        success.transport.enqueueRpc(modelsPayload())

        assertTrue(success.client.deleteCustomModel("custom/model"))

        assertEquals("custom/model", success.transport.requests.first().rpcParams?.get("id")?.jsonPrimitive?.content)
        assertEquals(2, success.transport.requests.size)

        val failure = gatewayClientFixture()
        failure.transport.enqueueJson("bad")
        assertFalse(failure.client.deleteCustomModel("custom/model"))
        assertEquals(1, failure.transport.requests.size)
    }

    @Test
    fun serviceEntriesPutCurrentModelFirstAndKeepRemainingOrder() {
        val f = gatewayClientFixture()
        f.client._denebModels.value = listOf(
            ModelOption(id = "one", display = "One", current = false, health = ""),
            ModelOption(id = "current", display = "Current", current = true, health = ""),
            ModelOption(id = "three", display = "Three", current = false, health = ""),
        )

        val entries = f.client.denebServiceEntries()

        assertEquals(listOf("current", "one", "three"), entries.map { it.modelId })
        assertEquals(listOf("deneb-model:current", "deneb-model:one", "deneb-model:three"), entries.map { it.instanceId })
        assertTrue(entries.all { it.serviceId == "deneb" })
    }

    @Test
    fun emptyModelRegistryProducesNoServiceEntries() {
        val f = gatewayClientFixture()

        assertTrue(f.client.denebServiceEntries().isEmpty())
    }

    @Test
    fun serviceEntriesPreferSessionOverrideOverGlobalCurrent() {
        val f = gatewayClientFixture()
        f.client._denebModels.value = listOf(
            ModelOption(id = "one", display = "One", current = false, health = ""),
            ModelOption(id = "current", display = "Current", current = true, health = ""),
            ModelOption(id = "three", display = "Three", current = false, health = ""),
        )
        f.client._sessionModels.value = mapOf("client:main:alpha" to "three")

        val entries = f.client.denebServiceEntries("client:main:alpha")

        assertEquals(listOf("three", "one", "current"), entries.map { it.modelId })
    }

    @Test
    fun setSessionModelSendsSessionKeyAndDoesNotRefreshRegistry() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")

        val result = f.client.setSessionModel(" client:main:alpha ", "kimi/kimi-k2.5")

        assertTrue(result)
        val params = f.transport.requests.single().requireRpc("miniapp.models.set")
        assertEquals("kimi/kimi-k2.5", params["id"]?.jsonPrimitive?.content)
        assertEquals("client:main:alpha", params["sessionKey"]?.jsonPrimitive?.content)
        assertEquals(null, params["role"])
        assertEquals("kimi/kimi-k2.5", f.client.sessionModels.value["client:main:alpha"])
        assertEquals(1, f.transport.requests.size)
    }

    @Test
    fun setSessionModelEmptyIdClearsOverride() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("{}")
        f.client._sessionModels.value = mapOf("client:main:alpha" to "kimi/kimi-k2.5")

        val result = f.client.setSessionModel("client:main:alpha", "  ")

        assertTrue(result)
        val params = f.transport.requests.single().requireRpc("miniapp.models.set")
        assertEquals("", params["id"]?.jsonPrimitive?.content)
        assertEquals("client:main:alpha", params["sessionKey"]?.jsonPrimitive?.content)
        assertFalse(f.client.sessionModels.value.containsKey("client:main:alpha"))
    }

    @Test
    fun refreshClientStatusMapsEntireHandshakePayload() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc(
            json.encodeToString(
                ClientHelloPayload(
                    version = "v2",
                    nativeApiVersion = 7,
                    model = "main/model",
                    capabilities = mapOf("fleet" to true, "mail" to false),
                    endpoints = mapOf("rpc" to "/api/rpc"),
                    tsMs = 99,
                ),
            ),
        )

        val status = f.client.refreshClientStatus()

        assertEquals("v2", status?.version)
        assertEquals(7, status?.nativeApiVersion)
        assertEquals("main/model", status?.model)
        assertEquals(mapOf("fleet" to true, "mail" to false), status?.capabilities)
        assertEquals(mapOf("rpc" to "/api/rpc"), status?.endpoints)
        assertEquals(99, status?.timestampMs)
        assertEquals(status, f.client.clientStatus.value)
    }

    @Test
    fun failedClientStatusRefreshClearsStaleHandshake() = runTest {
        val f = gatewayClientFixture()
        f.client._clientStatus.value = ClientStatus(
            version = "stale",
            nativeApiVersion = 1,
            model = "stale/model",
            capabilities = emptyMap(),
            endpoints = emptyMap(),
            timestampMs = 1,
        )
        f.transport.enqueueJson("bad")

        val result = f.client.refreshClientStatus()

        assertNull(result)
        assertNull(f.client.clientStatus.value)
    }

    @Test
    fun updateCheckRequiresConfiguredToken() = runTest {
        val f = gatewayClientFixture(token = "")

        assertNull(f.client.checkUpdate())

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun newerUpdateBuildReturnsEncodedDownloadUrl() = runTest {
        val f = gatewayClientFixture(token = "token with space", url = "https://gateway.example/")
        f.transport.enqueueJson(
            json.encodeToString(
                UpdateManifest(
                    code = DENEB_VERSION_CODE + 1,
                    file = "deneb build.apk",
                    notes = "security fixes",
                ),
            ),
        )

        val update = f.client.checkUpdate()

        assertEquals((DENEB_VERSION_CODE + 1).toString(), update?.buildLabel)
        assertEquals("security fixes", update?.notes)
        assertTrue(update?.apkUrl.orEmpty().contains("file=deneb+build.apk") || update?.apkUrl.orEmpty().contains("file=deneb%20build.apk"))
        assertTrue(update?.apkUrl.orEmpty().contains("clientToken=token"))
        assertEquals("https://gateway.example/api/v1/app/update/manifest", f.transport.singleRequest().url)
    }

    @Test
    fun updateWithDownloadTokenPrefersShortLivedUrlAuth() = runTest {
        val f = gatewayClientFixture(token = "long-lived-secret", url = "https://gateway.example/")
        f.transport.enqueueJson(
            json.encodeToString(
                UpdateManifest(
                    code = DENEB_VERSION_CODE + 1,
                    file = "deneb.apk",
                    downloadToken = "1234567890.abcdef",
                ),
            ),
        )

        val update = f.client.checkUpdate()

        val url = update?.apkUrl.orEmpty()
        assertTrue(url.contains("dl=1234567890.abcdef"))
        // The long-lived client token must never leak into the URL when the
        // gateway minted a short-lived one.
        assertTrue(!url.contains("long-lived-secret"))
    }

    @Test
    fun currentOlderAndBlankFileManifestsAreIgnored() = runTest {
        val current = gatewayClientFixture()
        current.transport.enqueueJson(json.encodeToString(UpdateManifest(code = DENEB_VERSION_CODE, file = "app.apk")))
        assertNull(current.client.checkUpdate())

        val older = gatewayClientFixture()
        older.transport.enqueueJson(json.encodeToString(UpdateManifest(code = DENEB_VERSION_CODE - 1, file = "app.apk")))
        assertNull(older.client.checkUpdate())

        val blank = gatewayClientFixture()
        blank.transport.enqueueJson(json.encodeToString(UpdateManifest(code = DENEB_VERSION_CODE + 1, file = "")))
        assertNull(blank.client.checkUpdate())
    }

    @Test
    fun updateCheckRejectsValidManifestOnHttpError() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueJson(
            json.encodeToString(UpdateManifest(code = DENEB_VERSION_CODE + 1, file = "app.apk")),
            status = HttpStatusCode.InternalServerError,
        )

        assertNull(f.client.checkUpdate())
    }

    @Test
    fun updateCheckPropagatesCancellation() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueFailure(CancellationException("cancel update"))

        val failure = assertFailsWith<CancellationException> { f.client.checkUpdate() }

        assertEquals("cancel update", failure.message)
    }

    @Test
    fun pushRegistrationRejectsBlankTokenWithoutNetwork() = runTest {
        val f = gatewayClientFixture()

        assertFalse(f.client.registerPushToken("  ", "android"))
        assertFalse(f.client.unregisterPushToken(""))

        assertTrue(f.transport.requests.isEmpty())
    }

    @Test
    fun pushRegisterAndUnregisterSerializeTokenParams() = runTest {
        val f = gatewayClientFixture()
        f.transport.enqueueRpc("""{"ok":true,"count":1,"delivery":true}""")
        f.transport.enqueueWrite(ok = true)

        assertTrue(f.client.registerPushToken("fcm-token", "android"))
        assertTrue(f.client.unregisterPushToken("fcm-token"))

        val register = f.transport.requests[0].requireRpc("miniapp.push.register")
        assertEquals("fcm-token", register["token"]?.jsonPrimitive?.content)
        assertEquals("android", register["platform"]?.jsonPrimitive?.content)
        val unregister = f.transport.requests[1].requireRpc("miniapp.push.unregister")
        assertEquals("fcm-token", unregister["token"]?.jsonPrimitive?.content)
    }

    @Test
    fun pushRegisterTracksGatewayDeliveryReadiness() = runTest {
        val f = gatewayClientFixture()

        // Gateway confirms the FCM sender is configured: flips ready + persists.
        f.transport.enqueueRpc("""{"ok":true,"count":1,"delivery":true}""")
        assertTrue(f.client.registerPushToken("fcm-token", "android"))
        assertTrue(f.client.fcmDeliveryReady.value)
        assertTrue(f.settings.settings.getBoolean(DenebGatewayClient.KEY_FCM_DELIVERY, false))

        // Older gateway omitting the field reads as not-ready (conservative:
        // unknown delivery must keep background SSE alive, not doze).
        f.transport.enqueueRpc("""{"ok":true,"count":1}""")
        assertTrue(f.client.registerPushToken("fcm-token", "android"))
        assertFalse(f.client.fcmDeliveryReady.value)
        assertFalse(f.settings.settings.getBoolean(DenebGatewayClient.KEY_FCM_DELIVERY, true))

        // No definitive response (RPC error) leaves the last answer untouched.
        f.transport.enqueueRpc(ok = false)
        assertFalse(f.client.registerPushToken("fcm-token", "android"))
        assertFalse(f.client.fcmDeliveryReady.value)
    }
}
