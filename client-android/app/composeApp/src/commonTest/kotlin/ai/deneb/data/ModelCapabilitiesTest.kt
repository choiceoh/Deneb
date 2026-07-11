package ai.deneb.data

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class ModelCapabilitiesTest {

    @Test
    fun everyDeclaredLimitedModelDisablesTools() {
        LIMITED_MODELS.forEach { model ->
            assertFalse(supportsTools(model), model)
        }
    }

    @Test
    fun matchingIsCaseInsensitive() {
        val variants = listOf(
            "LLAMA3.2:1B",
            "Gemma2",
            "GEMMA-4-E2B",
            "Phi3:Mini",
            "TINYLLAMA",
            "DeepSeek-Coder:6.7B",
        )

        variants.forEach { model -> assertFalse(supportsTools(model), model) }
    }

    @Test
    fun taggedAndQuantizedVariantsInheritTheLimitedPrefix() {
        val variants = listOf(
            "llama3.2:3b-instruct-q4_K_M",
            "llama3.1:8b/latest",
            "gemma2:latest",
            "gemma3:4b-it-qat",
            "phi3:mini-128k-instruct",
            "tinyllama:1.1b-chat-v1.0",
            "codellama:7b-code-q8_0",
            "deepseek-coder:1.3b-base",
        )

        variants.forEach { model -> assertFalse(supportsTools(model), model) }
    }

    @Test
    fun strongerSiblingModelsRemainToolCapable() {
        val models = listOf(
            "llama3.1:70b",
            "llama3.3:70b-instruct",
            "phi3:medium",
            "deepseek-coder:33b",
            "deepseek-r1:32b",
            "qwen2.5:32b",
            "mistral-large",
            "command-r-plus",
        )

        models.forEach { model -> assertTrue(supportsTools(model), model) }
    }

    @Test
    fun hostedProviderIdsAreNotConfusedWithLocalLimitedPrefixes() {
        val models = listOf(
            "openai/gpt-5",
            "anthropic/claude-sonnet-4",
            "google/gemini-2.5-pro",
            "ollama/qwen3:30b",
            "vendor/llama3.1:8b",
        )

        models.forEach { model -> assertTrue(supportsTools(model), model) }
    }

    @Test
    fun nearMatchesDoNotDisableUnlistedFamilies() {
        val models = listOf(
            "llama3.2",
            "llama3.1:80b",
            "gemma",
            "phi3",
            "stable",
            "deepseek-coder-v2:16b",
        )

        models.forEach { model -> assertTrue(supportsTools(model), model) }
    }
}
