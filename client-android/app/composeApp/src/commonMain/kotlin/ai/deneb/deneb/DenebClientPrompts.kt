package ai.deneb.deneb

import ai.deneb.deneb.generated.PromptDetailOut
import ai.deneb.deneb.generated.PromptListResponse
import ai.deneb.deneb.generated.PromptRow
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put

suspend fun DenebGatewayClient.fetchPrompts(): List<PromptRow>? = callRpc<PromptListResponse>("miniapp.prompts.list", buildJsonObject {})?.prompts

suspend fun DenebGatewayClient.fetchPrompt(id: String): PromptDetailOut? = callRpc<PromptDetailOut>("miniapp.prompts.get", promptIdParams(id))

suspend fun DenebGatewayClient.updatePrompt(id: String, text: String): PromptDetailOut? = callRpc<PromptDetailOut>(
    "miniapp.prompts.update",
    buildJsonObject {
        put("id", id)
        put("text", text)
    },
)

suspend fun DenebGatewayClient.resetPrompt(id: String): PromptDetailOut? = callRpc<PromptDetailOut>("miniapp.prompts.reset", promptIdParams(id))

private fun promptIdParams(id: String): JsonObject = buildJsonObject {
    put("id", id)
}
