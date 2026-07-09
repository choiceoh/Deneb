package ai.deneb.ui.dynamicui

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

/**
 * The lenient JSON→value coercion primitives the dynamic-UI node builders lean on.
 * LLMs send ints for floats, "yes" for bools, single values where arrays are
 * expected — these readers are the tolerance layer, so their edge rules are pinned
 * here. (The labeled-HTML grammar itself is covered by DenebUiHtmlTest.)
 */
class DenebUiNodeReadersTest {

    private fun obj(build: kotlinx.serialization.json.JsonObjectBuilder.() -> Unit) = buildJsonObject(build)

    // --- toStringLike ----------------------------------------------------

    @Test
    fun `toStringLike coerces primitives, arrays, and null`() {
        assertEquals("hi", JsonPrimitive("hi").toStringLike())
        assertEquals("42", JsonPrimitive(42).toStringLike())
        assertEquals("", JsonNull.toStringLike())
        val arr = buildJsonArray {
            add(JsonPrimitive(1))
            add(JsonPrimitive("a"))
            add(JsonPrimitive(true))
        }
        assertEquals("1, a, true", arr.toStringLike())
    }

    // --- readString / readNullableString ---------------------------------

    @Test
    fun `readString returns the default for a missing key`() {
        assertEquals("", obj {}.readString("k"))
        assertEquals("fallback", obj {}.readString("k", default = "fallback"))
        assertEquals("v", obj { put("k", "v") }.readString("k"))
    }

    @Test
    fun `readNullableString is null for absent and explicit null`() {
        assertNull(obj {}.readNullableString("k"))
        assertNull(obj { put("k", JsonNull) }.readNullableString("k"))
        assertEquals("v", obj { put("k", "v") }.readNullableString("k"))
    }

    // --- readNullableBoolean ---------------------------------------------

    @Test
    fun `readNullableBoolean accepts bools, numbers, and word strings`() {
        assertEquals(true, obj { put("k", true) }.readNullableBoolean("k"))
        assertEquals(false, obj { put("k", false) }.readNullableBoolean("k"))
        assertEquals(true, obj { put("k", 1) }.readNullableBoolean("k"))
        assertEquals(false, obj { put("k", 0) }.readNullableBoolean("k"))
        assertEquals(true, obj { put("k", "YES") }.readNullableBoolean("k"))
        assertEquals(false, obj { put("k", "no") }.readNullableBoolean("k"))
        assertNull(obj { put("k", 2) }.readNullableBoolean("k")) // out-of-range number
        assertNull(obj { put("k", "maybe") }.readNullableBoolean("k"))
        assertNull(obj {}.readNullableBoolean("k"))
    }

    // --- readNullableInt / readInt ---------------------------------------

    @Test
    fun `readNullableInt coerces ints, truncates floats, parses strings`() {
        assertEquals(5, obj { put("k", 5) }.readNullableInt("k"))
        assertEquals(5, obj { put("k", 5.9) }.readNullableInt("k")) // toInt truncates
        assertEquals(7, obj { put("k", "7") }.readNullableInt("k"))
        assertNull(obj { put("k", "abc") }.readNullableInt("k"))
        assertNull(obj {}.readNullableInt("k"))
    }

    @Test
    fun `readInt falls back to the default when uncoercible`() {
        assertEquals(0, obj {}.readInt("k"))
        assertEquals(-1, obj { put("k", "nope") }.readInt("k", default = -1))
        assertEquals(9, obj { put("k", 9) }.readInt("k", default = -1))
    }

    // --- readNullableFloat -----------------------------------------------

    @Test
    fun `readNullableFloat accepts int-shaped and string numbers`() {
        assertEquals(75f, obj { put("k", 75) }.readNullableFloat("k"))
        assertEquals(1.5f, obj { put("k", 1.5) }.readNullableFloat("k"))
        assertEquals(2.5f, obj { put("k", "2.5") }.readNullableFloat("k"))
        assertNull(obj { put("k", "x") }.readNullableFloat("k"))
    }

    // --- collection readers ----------------------------------------------

    @Test
    fun `readStringList handles arrays, a single value, and absence`() {
        val arr = obj {
            put(
                "k",
                buildJsonArray {
                    add(JsonPrimitive(1))
                    add(JsonPrimitive("a"))
                },
            )
        }
        assertEquals(listOf("1", "a"), arr.readStringList("k").toList())
        assertEquals(listOf("solo"), obj { put("k", "solo") }.readStringList("k").toList())
        assertEquals(emptyList(), obj {}.readStringList("k").toList())
    }

    @Test
    fun `readTableRows normalizes arrays, objects, and bare primitives to string rows`() {
        val rows = obj {
            put(
                "rows",
                buildJsonArray {
                    add(
                        buildJsonArray {
                            add(JsonPrimitive(1))
                            add(JsonPrimitive(2))
                        },
                    )
                    add(
                        buildJsonObject {
                            put("a", 3)
                            put("b", 4)
                        },
                    )
                    add(JsonPrimitive("solo"))
                },
            )
        }.readTableRows("rows").map { it.toList() }
        assertEquals(listOf(listOf("1", "2"), listOf("3", "4"), listOf("solo")), rows)
    }

    @Test
    fun `readChipList wraps bare strings and defaults value to label`() {
        val chips = obj {
            put(
                "chips",
                buildJsonArray {
                    add(JsonPrimitive("plain"))
                    add(buildJsonObject { put("label", "L") })
                    add(
                        buildJsonObject {
                            put("label", "L2")
                            put("value", "V2")
                        },
                    )
                },
            )
        }.readChipList("chips").toList()
        assertEquals(ChipItem(label = "plain", value = "plain"), chips[0])
        assertEquals(ChipItem(label = "L", value = "L"), chips[1])
        assertEquals(ChipItem(label = "L2", value = "V2"), chips[2])
    }
}
