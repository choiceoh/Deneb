package ai.deneb.sensing

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class GeofencingTest {

    @Test
    fun codecRoundTripsDefaultAndCustomRadiusEntries() {
        val expected = listOf(
            DenebGeofence("home", "집", 37.5665, 126.9780),
            DenebGeofence("work", "직장", -33.8688, 151.2093, radiusM = 275.5f),
        )

        val encoded = encodeGeofences(expected)

        assertEquals(expected, decodeGeofences(encoded))
        assertTrue(encoded.contains("home"))
        assertTrue(encoded.contains("275.5"))
    }

    @Test
    fun emptyListRoundTripsWithoutSpecialCase() {
        assertEquals("[]", encodeGeofences(emptyList()))
        assertEquals(emptyList(), decodeGeofences("[]"))
    }

    @Test
    fun malformedOrWrongShapeStorageFallsBackToEmpty() {
        for (raw in listOf("", "broken", "{}", "null", "[1,2]", "[{\"id\":\"home\"}]")) {
            assertEquals(emptyList(), decodeGeofences(raw), raw)
        }
    }

    @Test
    fun parserBuildsGeofenceFromNumericLocationFix() {
        val result = parseLocationToGeofence(
            id = "home",
            label = "집",
            locationJson = """{"latitude":37.5665,"longitude":126.9780}""",
        )

        assertEquals(DenebGeofence("home", "집", 37.5665, 126.9780, 150f), result)
    }

    @Test
    fun parserAcceptsIntegerCoordinatesAndCustomRadius() {
        val result = parseLocationToGeofence(
            id = "origin",
            label = "원점",
            locationJson = """{"latitude":0,"longitude":0,"accuracy":3.2}""",
            radiusM = 25f,
        )

        assertEquals(DenebGeofence("origin", "원점", 0.0, 0.0, 25f), result)
    }

    @Test
    fun coordinateRangeBoundariesAreAccepted() {
        assertEquals(
            DenebGeofence("nw", "NW", 90.0, -180.0, 1f),
            parseLocationToGeofence("nw", "NW", """{"latitude":90,"longitude":-180}""", 1f),
        )
        assertEquals(
            DenebGeofence("se", "SE", -90.0, 180.0, 10_000f),
            parseLocationToGeofence("se", "SE", """{"latitude":-90,"longitude":180}""", 10_000f),
        )
    }

    @Test
    fun missingNullAndNonNumericCoordinatesAreRejected() {
        val invalid = listOf(
            "{}",
            """{"latitude":37.0}""",
            """{"longitude":127.0}""",
            """{"latitude":null,"longitude":127.0}""",
            """{"latitude":"north","longitude":127.0}""",
            """{"latitude":true,"longitude":127.0}""",
            "[]",
            "not-json",
        )

        invalid.forEach { raw ->
            assertNull(parseLocationToGeofence("id", "label", raw), raw)
        }
    }

    @Test
    fun outOfRangeCoordinatesAreRejectedBeforeOsRegistration() {
        val invalid = listOf(
            """{"latitude":90.0001,"longitude":0}""",
            """{"latitude":-90.0001,"longitude":0}""",
            """{"latitude":0,"longitude":180.0001}""",
            """{"latitude":0,"longitude":-180.0001}""",
        )

        invalid.forEach { raw ->
            assertNull(parseLocationToGeofence("id", "label", raw), raw)
        }
    }

    @Test
    fun nonFiniteCoordinatesAreRejected() {
        for (token in listOf("NaN", "Infinity", "-Infinity")) {
            assertNull(
                parseLocationToGeofence(
                    "id",
                    "label",
                    """{"latitude":"$token","longitude":0}""",
                ),
                token,
            )
            assertNull(
                parseLocationToGeofence(
                    "id",
                    "label",
                    """{"latitude":0,"longitude":"$token"}""",
                ),
                token,
            )
        }
    }

    @Test
    fun nonPositiveAndNonFiniteRadiusAreRejected() {
        val location = """{"latitude":37.0,"longitude":127.0}"""

        for (radius in listOf(0f, -1f, Float.NaN, Float.POSITIVE_INFINITY, Float.NEGATIVE_INFINITY)) {
            assertNull(parseLocationToGeofence("id", "label", location, radius), radius.toString())
        }
    }

    @Test
    fun parserPreservesCallerIdentityAndUnicodeLabel() {
        val result = requireNotNull(
            parseLocationToGeofence(
                id = "custom-place-1",
                label = "부모님 댁 🏠",
                locationJson = """ { "longitude" : 128.5, "latitude" : 35.1 } """,
                radiusM = 99.25f,
            ),
        )

        assertEquals("custom-place-1", result.id)
        assertEquals("부모님 댁 🏠", result.label)
        assertEquals(99.25f, result.radiusM)
    }
}
