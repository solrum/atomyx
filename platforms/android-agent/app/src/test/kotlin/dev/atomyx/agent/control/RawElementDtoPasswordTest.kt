package dev.atomyx.agent.control

import com.google.gson.Gson
import com.google.gson.JsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Pins the wire shape of the password flag: present and true for a
 * secret-holding node, absent for every other node, so the host
 * decoder reads a missing field as false.
 */
class RawElementDtoPasswordTest {

    private val gson = Gson()

    private fun serialize(dto: RawElementDto): JsonObject =
        gson.toJsonTree(dto).asJsonObject

    @Test
    fun passwordFlagMapsToTrueOrNull() {
        assertEquals(true, passwordMarker(true))
        assertNull(passwordMarker(false))
    }

    @Test
    fun passwordNodeSerializesTrue() {
        val json = serialize(RawElementDto(elementId = "el_001", isPassword = passwordMarker(true)))
        assertTrue(json.has("isPassword"))
        assertTrue(json.get("isPassword").asBoolean)
    }

    @Test
    fun ordinaryNodeOmitsTheField() {
        val json = serialize(RawElementDto(elementId = "el_002", isPassword = passwordMarker(false)))
        assertFalse(json.has("isPassword"))
    }
}
