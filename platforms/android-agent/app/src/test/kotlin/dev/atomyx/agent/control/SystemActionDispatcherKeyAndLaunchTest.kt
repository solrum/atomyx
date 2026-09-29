package dev.atomyx.agent.control

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Pure-function tests for the caller-facing validation messages
 * `SystemActionDispatcher` builds for an unsupported press_key
 * request and a launch request naming a package with no launchable
 * activity.
 *
 * These tests run without an Android runtime: the functions under
 * test only format strings from their arguments and the supported-
 * key set, and never touch `AccessibilityService`.
 */
class SystemActionDispatcherKeyAndLaunchTest {

    @Test
    fun supportedKeysListsBackAndHome() {
        assertEquals(setOf("back", "home"), SystemActionDispatcher.SUPPORTED_KEYS)
    }

    @Test
    fun unsupportedKeyMessageNamesTheKeyAndTheSupportedList() {
        val message = SystemActionDispatcher.unsupportedKeyMessage("enter")
        assertEquals("Unsupported key: enter. Supported keys: back, home.", message)
    }

    @Test
    fun unsupportedKeyMessageCapsALongKeyButKeepsTheFullSupportedList() {
        val longKey = "a".repeat(200)
        val message = SystemActionDispatcher.unsupportedKeyMessage(longKey)
        val expectedKey = "a".repeat(32) + "..."
        assertEquals("Unsupported key: $expectedKey. Supported keys: back, home.", message)
    }

    @Test
    fun noLaunchableActivityMessageNamesThePackage() {
        val message = SystemActionDispatcher.noLaunchableActivityMessage("com.example.app")
        assertEquals("No launchable activity for package: com.example.app.", message)
    }

    @Test
    fun noLaunchableActivityMessageCapsALongPackageName() {
        val longPackageName = "com.example." + "a".repeat(200)
        val message = SystemActionDispatcher.noLaunchableActivityMessage(longPackageName)
        val expectedPackageName = longPackageName.take(32) + "..."
        assertEquals("No launchable activity for package: $expectedPackageName.", message)
    }
}
