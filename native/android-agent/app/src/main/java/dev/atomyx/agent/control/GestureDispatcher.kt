package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.content.Intent
import android.graphics.Path
import android.os.Bundle
import android.view.accessibility.AccessibilityNodeInfo

/**
 * Dispatches gestures and node actions through the AccessibilityService.
 *
 * Coordinate-first contract: the host-side TS adapter
 * (`@atomyx/core-driver-android`) runs selector resolution on the
 * canonical `TreeNode` it builds from `/tree`, then calls coordinate
 * primitives here. The APK never receives a `Selector` — it only
 * speaks points + the text-input helpers below.
 *
 * Keyboard / typing helpers (`typeViaKeyboard`, `clearFocusedInput`)
 * are kept on the device side because they need multiple tightly
 * sequenced reads of the focused-node state that would cost too
 * many adb roundtrips to orchestrate host-side.
 */
class GestureDispatcher(
    private val service: AccessibilityService,
    private val uiTree: UiTreeService,
) {

    fun tapAt(x: Float, y: Float) {
        require(x >= 0 && y >= 0) { "tap coordinates must be non-negative: ($x, $y)" }
        val path = Path().apply { moveTo(x, y) }
        val stroke = GestureDescription.StrokeDescription(path, 0L, 50L)
        service.dispatchGesture(GestureDescription.Builder().addStroke(stroke).build(), null, null)
    }

    fun swipe(fromX: Float, fromY: Float, toX: Float, toY: Float, durationMs: Long) {
        require(fromX >= 0 && fromY >= 0 && toX >= 0 && toY >= 0) {
            "swipe coordinates must be non-negative"
        }
        val path = Path().apply {
            moveTo(fromX, fromY)
            lineTo(toX, toY)
        }
        val stroke = GestureDescription.StrokeDescription(path, 0L, durationMs)
        service.dispatchGesture(GestureDescription.Builder().addStroke(stroke).build(), null, null)
    }

    fun typeViaKeyboard(text: String, perKeyDelayMs: Long = 80L, clearFirst: Boolean = true): TypeResult {
        // Phase 0: clear any existing text in the focused field. Default ON
        // because append-on-stale-state is a major source of test flake.
        // Caller can opt out with clearFirst=false for additive flows.
        if (clearFirst) {
            clearFocusedFieldBestEffort(perKeyDelayMs)
        }

        // Phase 1: poll until keyboard is actually visible and has keys.
        // The IME may still be animating in (focus just moved, password field
        // triggers a layout swap, etc) — failing immediately is the root cause
        // of agent fallback to brute-force tap-per-key.
        val kb = waitForKeyboardReady(maxWaitMs = 2000L)

        // Phase 1b: if no system IME, the app may render a CUSTOM in-app
        // keypad as plain Flutter / Compose / native views (common for
        // banking apps that need security-hardened input). Fall through to
        // an on-screen-keys scan that finds clickable elements with single-
        // character labels and taps them.
        if (kb == null) {
            return typeViaOnScreenKeys(text, perKeyDelayMs)
        }

        var currentKb: KeyboardInfo = kb
        var labelMap = buildLabelMap(currentKb)

        // Sanity check: if the keyboard reports visible but the labelMap is
        // empty (custom IMEs where getKeyboardInfo can't extract keys, like
        // Samsung Honeyboard on some layouts), fall through to the generic
        // on-screen-keys scanner which reads the full compact tree.
        if (labelMap.isEmpty()) {
            return typeViaOnScreenKeys(text, perKeyDelayMs)
        }

        var typed = 0
        for (ch in text) {
            val needle = ch.toString().lowercase()
            var key = labelMap[needle]

            // Phase 2: missing key may mean layout switch is needed
            // (numeric → qwerty or vice versa).
            if (key == null) {
                val switched = trySwitchLayoutFor(needle, currentKb)
                if (switched != null) {
                    currentKb = switched
                    labelMap = buildLabelMap(currentKb)
                    key = labelMap[needle]
                }
            }

            // Phase 3: still missing — fall through to on-screen keys scan
            // for the REMAINING chars (restart from 0 to be safe).
            if (key == null) {
                return typeViaOnScreenKeys(text, perKeyDelayMs)
            }

            tapAt(key.bounds.exactCenterX(), key.bounds.exactCenterY())
            typed++
            try { Thread.sleep(perKeyDelayMs) } catch (_: InterruptedException) {}
        }
        return TypeResult(true, typed, text.length, "ok")
    }

    /**
     * Best-effort clear of the currently focused input field. Two strategies:
     *
     *   1. ACTION_SET_TEXT(""): fastest, works for native Android EditText
     *      and Flutter inputs that proxy through Semantics. If the focused
     *      node accepts the action, we're done in <10ms.
     *
     *   2. Backspace via on-screen keypad: when ACTION_SET_TEXT is rejected
     *      (Flutter custom inputs without Semantics text-edit support), we
     *      read the focused node's text length and tap an on-screen
     *      backspace key that many character times. The backspace key is
     *      detected structurally by label match against a wide set of
     *      patterns (delete icon, "del", "back", "⌫", "削除", "✕", …).
     *
     * Returns silently. Failure is logged but does NOT abort the type.
     */
    private fun clearFocusedFieldBestEffort(perKeyDelayMs: Long) {
        val root = service.rootInActiveWindow ?: return
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT) ?: return
        try {
            // Strategy 1: ACTION_SET_TEXT
            try {
                val args = Bundle().apply {
                    putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, "")
                }
                if (focused.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) {
                    return
                }
            } catch (_: Exception) {}

            // Strategy 2: backspace via on-screen keypad
            val existing = focused.text?.toString() ?: ""
            if (existing.isEmpty()) return

            uiTree.markDirty()
            val summary = uiTree.dumpCompact()
            val backspace = findBackspaceKey(summary) ?: return
            // Tap backspace one extra time as a safety margin (some inputs
            // append a leading char during focus animation).
            val taps = existing.length + 1
            for (i in 0 until taps) {
                tapAt(
                    backspace.bounds.left + (backspace.bounds.right - backspace.bounds.left) / 2f,
                    backspace.bounds.top + (backspace.bounds.bottom - backspace.bounds.top) / 2f,
                )
                try { Thread.sleep(perKeyDelayMs) } catch (_: InterruptedException) {}
            }
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }

    /**
     * Locate a backspace / clear / delete key on the current screen by
     * matching the label / contentDesc against a wide set of language- and
     * icon-independent patterns. Structural — works on any locale, any
     * keypad style.
     */
    private fun findBackspaceKey(summary: List<CompactElement>): CompactElement? {
        val patterns = listOf(
            // Glyphs / icons
            "⌫", "⌦", "✕", "✖", "×", "x", "✗",
            // English
            "del", "delete", "back", "backspace", "clear", "erase",
            // Japanese banking apps
            "削除", "消去", "クリア", "戻る",
            // Korean
            "삭제", "지우기",
            // Vietnamese
            "xoá", "xóa",
        )
        for (el in summary) {
            if (!el.enabled) continue
            val needle = el.label.trim().lowercase()
            if (needle.isEmpty() || needle.length > 12) continue
            if (patterns.any { it.equals(needle, ignoreCase = true) }) {
                return el
            }
        }
        return null
    }

    /**
     * Type by tapping ON-SCREEN clickable elements whose label matches each
     * character. Used when no system IME is visible — typical for Flutter /
     * native banking apps that render a custom in-app numeric keypad as part
     * of their own view tree (digits exposed as clickable views with
     * contentDesc / text matching "0".."9").
     *
     * Builds a label→element map ONCE at start (single tree dump), then taps
     * keys without re-dumping for each character. Re-reads the tree only if
     * a key looks stale (bounds mismatch) — covers carousels / scrolling
     * keypads which are uncommon.
     */
    private fun typeViaOnScreenKeys(text: String, perKeyDelayMs: Long): TypeResult {
        uiTree.markDirty()
        val summary = uiTree.dumpCompact()

        // Build TWO label maps: one from elements inside the IME window
        // (system keyboard keys — Samsung Honeyboard etc), one from
        // non-IME elements (custom in-app keypads — Flutter banking apps).
        // Prefer IME keys when both exist; fall back to non-IME otherwise.
        val imeMap = mutableMapOf<String, CompactElement>()
        val nonImeMap = mutableMapOf<String, CompactElement>()
        for (el in summary) {
            if (!el.enabled) continue
            val label = el.label.trim()
            if (label.isEmpty() || label.length > 3) continue
            if (el.bounds.right <= el.bounds.left || el.bounds.bottom <= el.bounds.top) continue
            val key = label.lowercase()
            if (el.isInIme) imeMap.putIfAbsent(key, el)
            else nonImeMap.putIfAbsent(key, el)
        }
        // IME keys win when present. Otherwise use in-app keypad.
        val keyMap = if (imeMap.isNotEmpty()) imeMap else nonImeMap

        if (keyMap.isEmpty()) {
            return TypeResult(
                success = false,
                typed = 0,
                total = text.length,
                reason = "no system IME visible AND no on-screen keypad detected (no clickable " +
                    "elements with short labels). Focus the target field by tapping it first, " +
                    "or verify the app actually exposes a keyboard.",
            )
        }

        var typed = 0
        for (ch in text) {
            val needle = ch.toString().lowercase()
            val key = keyMap[needle]
                ?: return TypeResult(
                    false,
                    typed,
                    text.length,
                    "no on-screen key for '$ch'. Available keys: " +
                        keyMap.keys.take(30).joinToString(","),
                )
            tapAt(key.bounds.left + (key.bounds.right - key.bounds.left) / 2f,
                  key.bounds.top + (key.bounds.bottom - key.bounds.top) / 2f)
            typed++
            try { Thread.sleep(perKeyDelayMs) } catch (_: InterruptedException) {}
        }
        return TypeResult(true, typed, text.length, "ok via on-screen keys (custom keypad)")
    }

    private fun waitForKeyboardReady(maxWaitMs: Long): KeyboardInfo? {
        val deadline = System.currentTimeMillis() + maxWaitMs
        // Fast path: check immediately — if the IME is already up (common
        // when fill_input_at_coordinates just tapped the field), skip sleep.
        uiTree.markDirty()
        val immediate = uiTree.getKeyboardInfo()
        if (immediate.visible && immediate.keys.isNotEmpty()) return immediate

        var attempt = 0
        while (System.currentTimeMillis() < deadline) {
            val waitMs = if (attempt == 0) 300L else minOf(500L, 100L * (attempt + 1))
            try { Thread.sleep(waitMs) } catch (_: InterruptedException) {}
            uiTree.markDirty()
            val kb = uiTree.getKeyboardInfo()
            if (kb.visible && kb.keys.isNotEmpty()) return kb
            attempt++
        }
        return null
    }

    private fun buildLabelMap(kb: KeyboardInfo): Map<String, KeyboardKey> {
        val map = mutableMapOf<String, KeyboardKey>()
        for (k in kb.keys) {
            val label = k.label.trim()
            if (label.isNotEmpty()) map.putIfAbsent(label.lowercase(), k)
        }
        return map
    }

    /**
     * If the current keyboard does not contain the needed character, try
     * tapping a layout-switch key and re-read. Most Android soft keyboards
     * expose:
     *   - "?123" / "123" / "12!?"  → switch from letters to numbers/symbols
     *   - "ABC" / "abc"            → switch from numbers back to letters
     *   - "あいう" / "ABC" / "ｱｲｳ"  → switch input mode (Japanese IMEs)
     *
     * We pick the switch key based on what `needle` is — digit needs a numeric
     * layout, letter needs an alphabet layout. Returns the new keyboard info
     * after switch, or null if no useful switch key was found.
     */
    private fun trySwitchLayoutFor(needle: String, current: KeyboardInfo): KeyboardInfo? {
        val isDigit = needle.length == 1 && needle[0].isDigit()
        val isLetter = needle.length == 1 && needle[0].isLetter()

        val switchCandidates = if (isDigit) {
            listOf("?123", "123", "12!?", "12#")
        } else if (isLetter) {
            listOf("abc", "ABC", "あいう")
        } else {
            // Symbol or other — try the symbol page.
            listOf("?123", "123", "=\\<", "@#&")
        }

        for (k in current.keys) {
            val label = k.label.trim()
            if (label.isEmpty()) continue
            if (switchCandidates.any { it.equals(label, ignoreCase = true) }) {
                tapAt(k.bounds.exactCenterX(), k.bounds.exactCenterY())
                try { Thread.sleep(250) } catch (_: InterruptedException) {}
                uiTree.markDirty()
                val next = uiTree.getKeyboardInfo()
                if (next.visible && next.keys.isNotEmpty()) return next
                return null
            }
        }
        return null
    }

    /**
     * Dispatch a long-press gesture at the given screen coordinates. The
     * gesture is a zero-movement stroke with a configurable duration so the
     * OS interprets it as a long press — typically used to focus an input and
     * surface the text-selection menu, or to trigger a context menu.
     */
    fun longPressAt(x: Float, y: Float, durationMs: Long = 800L) {
        require(x >= 0 && y >= 0) { "long press coordinates must be non-negative: ($x, $y)" }
        val d = durationMs.coerceIn(300L, 5000L)
        val path = Path().apply { moveTo(x, y) }
        val stroke = GestureDescription.StrokeDescription(path, 0L, d)
        service.dispatchGesture(GestureDescription.Builder().addStroke(stroke).build(), null, null)
    }

    /**
     * Clear the currently focused input by finding the focused editable node
     * via the accessibility API and performing ACTION_SET_TEXT with an empty
     * string. Works even when the field has no stable selector — which is the
     * common case for Flutter apps where text fields are exposed as plain
     * views without resourceId / contentDesc. Caller must focus the target
     * first with a tap.
     */
    fun clearFocusedInput(): TapResult {
        val root = service.rootInActiveWindow
            ?: return TapResult(false, "no active window — is the device unlocked?")
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
            ?: return TapResult(false, "no input-focused node — tap the target field first")
        try {
            // Strategy 1: ACTION_SET_TEXT — fast path for native EditText
            // and Flutter inputs that proxy through Semantics.
            val args = Bundle().apply {
                putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, "")
            }
            if (focused.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) {
                return TapResult(true, "ok via ACTION_SET_TEXT")
            }

            // Strategy 2: backspace via on-screen keypad — for Flutter custom
            // inputs that don't accept ACTION_SET_TEXT.
            val existing = focused.text?.toString() ?: ""
            if (existing.isEmpty()) return TapResult(true, "ok (already empty)")
            uiTree.markDirty()
            val summary = uiTree.dumpCompact()
            val backspace = findBackspaceKey(summary)
                ?: return TapResult(
                    false,
                    "ACTION_SET_TEXT rejected and no on-screen backspace key found " +
                        "(custom keypad must expose a delete/clear button labeled with " +
                        "⌫, del, 削除, etc.). Existing text length: ${existing.length}",
                )
            val taps = existing.length + 1
            for (i in 0 until taps) {
                tapAt(
                    backspace.bounds.left + (backspace.bounds.right - backspace.bounds.left) / 2f,
                    backspace.bounds.top + (backspace.bounds.bottom - backspace.bounds.top) / 2f,
                )
                try { Thread.sleep(80L) } catch (_: InterruptedException) {}
            }
            return TapResult(true, "ok via on-screen backspace ($taps taps)")
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
    }

    fun pressKey(key: String) {
        when (key) {
            "back" -> service.performGlobalAction(AccessibilityService.GLOBAL_ACTION_BACK)
            "home" -> service.performGlobalAction(AccessibilityService.GLOBAL_ACTION_HOME)
            "enter" -> error("press_key('enter') not supported on non-rooted devices")
            else -> error("unknown key: $key")
        }
    }

    fun launchApp(packageName: String) {
        val intent = service.packageManager.getLaunchIntentForPackage(packageName)
            ?: error("no launch intent for $packageName")
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        service.startActivity(intent)
    }

    /**
     * Force-stop an app package. Equivalent to `adb shell am force-stop <pkg>`.
     * ActivityManager.killBackgroundProcesses only kills background processes;
     * we reflectively invoke the hidden `forceStopPackage` which is the
     * closest equivalent available from within an app process. If the
     * reflection fails (system policy), we fall back to killing background
     * processes which handles most stale-state scenarios.
     */
    fun forceStopApp(packageName: String) {
        val am = service.getSystemService(android.content.Context.ACTIVITY_SERVICE)
            as android.app.ActivityManager
        try {
            val m = am.javaClass.getMethod("forceStopPackage", String::class.java)
            m.invoke(am, packageName)
        } catch (_: Exception) {
            am.killBackgroundProcesses(packageName)
        }
    }

    data class TapResult(val success: Boolean, val reason: String)
    data class TypeResult(val success: Boolean, val typed: Int, val total: Int, val reason: String)
}
