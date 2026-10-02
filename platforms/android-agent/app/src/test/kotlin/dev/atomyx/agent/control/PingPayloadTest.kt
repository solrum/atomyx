package dev.atomyx.agent.control

import dev.atomyx.agent.control.router.pingPayload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PingPayloadTest {

    @Test
    fun announcesTheSecureFieldsCapability() {
        val payload = pingPayload(canMultiPointer = true, canPressure = false, sdkInt = 34)

        @Suppress("UNCHECKED_CAST")
        val capabilities = payload["capabilities"] as Map<String, Any>
        assertEquals(true, capabilities["secureFields"])
    }

    @Test
    fun keepsTheExistingFieldsAHostReads() {
        val payload = pingPayload(canMultiPointer = true, canPressure = false, sdkInt = 34)

        @Suppress("UNCHECKED_CAST")
        val capabilities = payload["capabilities"] as Map<String, Any>
        assertEquals(true, payload["ok"])
        assertEquals(34, payload["sdkInt"])
        assertEquals("accessibility", payload["mechanism"])
        assertEquals(true, capabilities["canMultiPointer"])
        assertEquals(false, capabilities["canPressure"])
        assertTrue(capabilities.keys.containsAll(listOf("canMultiPointer", "canPressure", "secureFields")))
    }
}
