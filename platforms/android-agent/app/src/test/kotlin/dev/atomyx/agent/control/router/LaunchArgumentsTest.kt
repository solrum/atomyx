package dev.atomyx.agent.control.router

import com.google.gson.JsonParser
import org.junit.Assert.assertEquals
import org.junit.Test

/** Tests how a launch request's `arguments` object becomes intent extras. */
class LaunchArgumentsTest {

    private fun parse(json: String) = JsonParser.parseString(json).asJsonObject

    @Test
    fun absentArgumentsYieldNoExtras() {
        assertEquals(emptyMap<String, String>(), launchArgumentsFrom(parse("""{"packageName":"a.b"}""")))
    }

    @Test
    fun primitiveValuesBecomeText() {
        val body = parse("""{"packageName":"a.b","arguments":{"route":"/focus","retries":3,"debug":true}}""")
        assertEquals(
            mapOf("route" to "/focus", "retries" to "3", "debug" to "true"),
            launchArgumentsFrom(body),
        )
    }

    @Test
    fun nonPrimitiveValuesAreSkipped() {
        val body = parse("""{"arguments":{"route":"/focus","nested":{"a":1},"list":[1],"none":null}}""")
        assertEquals(mapOf("route" to "/focus"), launchArgumentsFrom(body))
    }

    @Test
    fun nonObjectArgumentsYieldNoExtras() {
        assertEquals(emptyMap<String, String>(), launchArgumentsFrom(parse("""{"arguments":"route=/focus"}""")))
    }
}
