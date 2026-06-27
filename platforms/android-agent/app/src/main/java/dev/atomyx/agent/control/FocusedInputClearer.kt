package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import dev.atomyx.agent.control.clear.ClearTextChain

/**
 * Clears the currently focused input field. Works even when the field has
 * no stable selector — which is the common case for Flutter apps where text
 * fields are exposed as plain views without resourceId / contentDesc. Caller
 * must focus the target first with a tap.
 *
 * Delegates to ClearTextChain, which tries four strategies in priority order
 * and verifies the field is empty after each attempt before falling through.
 */
class FocusedInputClearer(
    private val service: AccessibilityService,
    private val uiTree: UiTreeService,
    private val gestures: GestureDispatcher,
) {

    fun clearFocusedInput(): TapResult =
        ClearTextChain(service, uiTree, gestures).clear()
}
