package dev.atomyx.agent.control.clear

import android.accessibilityservice.AccessibilityService
import dev.atomyx.agent.control.GestureDispatcher
import dev.atomyx.agent.control.UiTreeService

data class ClearContext(
    val service: AccessibilityService,
    val uiTree: UiTreeService,
    val gestures: GestureDispatcher,
    val hintText: CharSequence?,
    val initialText: String,
)
