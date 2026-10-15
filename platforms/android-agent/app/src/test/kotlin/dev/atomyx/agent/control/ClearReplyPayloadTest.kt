package dev.atomyx.agent.control

import dev.atomyx.agent.control.clear.ClearDiagnostic
import dev.atomyx.agent.control.clear.ClearOutcome
import dev.atomyx.agent.control.clear.clearReplyPayload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test

class ClearReplyPayloadTest {

    @Test
    fun failureCarriesEveryDiagnosticField() {
        val diagnostic = ClearDiagnostic(
            strategiesTried = listOf("set_text", "select_all_cut"),
            lastValue = "partial",
            focusedNodeDesc = "android.widget.EditText(text=partial, hint=null)",
            screenWidth = 1080,
            screenHeight = 2400,
        )

        val payload = clearReplyPayload(ClearOutcome.failed("clear_failed", diagnostic))

        assertEquals(false, payload["ok"])
        assertEquals("clear_failed", payload["reason"])
        assertEquals(listOf("set_text", "select_all_cut"), payload["strategiesTried"])
        assertEquals("partial", payload["lastValue"])
        assertEquals("android.widget.EditText(text=partial, hint=null)", payload["focusedNodeDesc"])
        assertEquals(1080, payload["screenWidth"])
        assertEquals(2400, payload["screenHeight"])
    }

    @Test
    fun successKeepsOnlyOkAndReason() {
        val payload = clearReplyPayload(ClearOutcome.succeeded("ok via set_text"))

        assertEquals(mapOf<String, Any>("ok" to true, "reason" to "ok via set_text"), payload)
        assertFalse(payload.containsKey("strategiesTried"))
    }
}
