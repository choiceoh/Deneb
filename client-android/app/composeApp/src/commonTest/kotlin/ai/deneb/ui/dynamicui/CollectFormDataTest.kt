package ai.deneb.ui.dynamicui

import kotlinx.serialization.json.JsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class CollectFormDataTest {

    @Test
    fun emptyActionProducesEmptyPayload() {
        assertEquals(emptyMap(), collectFormData(CallbackAction(), emptyMap()))
        assertEquals(emptyMap(), collectFormData(CallbackAction(collectFrom = emptyList()), mapOf("x" to "1")))
    }

    @Test
    fun staticCallbackDataIsCoercedToStrings() {
        val action = CallbackAction(
            data = linkedMapOf(
                "text" to JsonPrimitive("hello"),
                "enabled" to JsonPrimitive(true),
                "count" to JsonPrimitive(42),
                "ratio" to JsonPrimitive(1.5),
            ),
        )

        assertEquals(
            linkedMapOf(
                "text" to "hello",
                "enabled" to "true",
                "count" to "42",
                "ratio" to "1.5",
            ),
            collectFormData(action, emptyMap()),
        )
    }

    @Test
    fun collectFromSelectsOnlyNamedExistingFields() {
        val action = CallbackAction(collectFrom = listOf("name", "missing", "choice"))
        val form = linkedMapOf("name" to "Kim", "choice" to "A", "unlisted" to "secret")

        val collected = collectFormData(action, form)

        assertEquals(mapOf("name" to "Kim", "choice" to "A"), collected)
        assertFalse("missing" in collected)
        assertFalse("unlisted" in collected)
    }

    @Test
    fun currentFormValueOverridesStaticDataForSameKey() {
        val action = CallbackAction(
            data = mapOf("choice" to JsonPrimitive("default"), "fixed" to JsonPrimitive("yes")),
            collectFrom = listOf("choice"),
        )

        val collected = collectFormData(action, mapOf("choice" to "user-selected"))

        assertEquals("user-selected", collected["choice"])
        assertEquals("yes", collected["fixed"])
    }

    @Test
    fun emptyFormValuesArePreservedAsExplicitSubmissions() {
        val action = CallbackAction(collectFrom = listOf("optional", "spaces"))

        val collected = collectFormData(action, mapOf("optional" to "", "spaces" to "   "))

        assertTrue("optional" in collected)
        assertEquals("", collected["optional"])
        assertEquals("   ", collected["spaces"])
    }

    @Test
    fun duplicateCollectIdsDoNotDuplicateOrReorderPayload() {
        val action = CallbackAction(collectFrom = listOf("a", "a", "b", "a"))

        val collected = collectFormData(action, linkedMapOf("a" to "1", "b" to "2"))

        assertEquals(listOf("a", "b"), collected.keys.toList())
        assertEquals(mapOf("a" to "1", "b" to "2"), collected)
    }

    @Test
    fun formStateIsNotMutatedDuringCollection() {
        val form = linkedMapOf("a" to "1", "b" to "2")
        val before = form.toMap()

        collectFormData(CallbackAction(collectFrom = listOf("a")), form)

        assertEquals(before, form)
    }

    @Test
    fun staticKeyOrderIsRetainedWhenCollectedValueOverridesIt() {
        val action = CallbackAction(
            data = linkedMapOf(
                "first" to JsonPrimitive("static"),
                "second" to JsonPrimitive("fixed"),
            ),
            collectFrom = listOf("third", "first"),
        )

        val collected = collectFormData(action, mapOf("first" to "form", "third" to "new"))

        assertEquals(listOf("first", "second", "third"), collected.keys.toList())
        assertEquals("form", collected["first"])
    }

    @Test
    fun unusualIdsRemainExactKeysWithoutNormalization() {
        val ids = listOf("with space", "한글", "a.b", "a/b", "")
        val form = ids.associateWith { "value:$it" }

        val collected = collectFormData(CallbackAction(collectFrom = ids), form)

        assertEquals(form, collected)
    }
}
