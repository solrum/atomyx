package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.os.Bundle
import android.view.accessibility.AccessibilityNodeInfo
import dev.atomyx.agent.control.clear.ClearTextChain

/**
 * Types text into the currently focused input field using a three-strategy
 * chain. Lives device-side because every path here needs multiple tightly
 * sequenced reads of focused-node state that would cost too many adb
 * roundtrips to orchestrate from the host.
 *
 * Strategy chain for `typeViaKeyboard`:
 *
 *   1. `ACTION_SET_TEXT` — native EditText + Flutter Semantics text fields
 *      accept this atomically. Verified by a polling read so `obscureText:
 *      true` Flutter inputs (which accept the action but drop the write)
 *      fall through.
 *   2. System IME per-key tap — when the system keyboard is visible, tap
 *      each character's key via the IME's reported key bounds. Handles
 *      numeric↔qwerty layout switches.
 *   3. On-screen keypad — when no system IME is visible (custom Flutter
 *      banking / OTP keypads), scan the compact tree for clickable elements
 *      with short labels matching each char.
 */
class KeyboardTyper(
    private val service: AccessibilityService,
    private val uiTree: UiTreeService,
    private val gestures: GestureDispatcher,
) {

    fun typeViaKeyboard(
        text: String,
        perKeyDelayMs: Long = 80L,
        clearFirst: Boolean = true,
    ): TypeResult {
        // Optional clear: wipe any existing content first. Default ON
        // because append-on-stale-state is a major source of test flake;
        // caller opts out with clearFirst=false for additive flows.
        if (clearFirst) {
            clearFocusedFieldBestEffort(perKeyDelayMs)
        }

        // Fast path — ACTION_SET_TEXT. Bypasses keyboard layout
        // entirely and is reliable on native EditText + most Flutter
        // inputs. Returns null when the action is rejected OR when
        // verification confirms the write didn't land (obscureText
        // drop case); we then fall through to keyboard dispatch.
        val setTextResult = trySetText(text)
        if (setTextResult != null) return setTextResult

        // IME readiness wait: the keyboard may still be animating in
        // after focus moved, so a tight poll replaces a hardcoded
        // sleep. Failing before the IME renders is the root cause
        // of brute-force fallback, so we give it up to 2 s.
        val kb = waitForKeyboardReady(maxWaitMs = 2000L)

        // No system IME? The app may render a custom in-app keypad
        // (common for banking / OTP flows — a11y exposes the keys as
        // clickable Flutter / Compose / native views with short
        // labels). Scan and tap those directly.
        if (kb == null) {
            return typeViaOnScreenKeys(text, perKeyDelayMs)
        }

        var currentKb: KeyboardInfo = kb
        var labelMap = buildLabelMap(currentKb)

        // Keyboard reports visible but no keys extracted — some
        // custom system IMEs (e.g. Samsung Honeyboard on certain
        // layouts) surface keys in ways getKeyboardInfo can't
        // introspect. Fall back to the generic compact-tree scanner.
        if (labelMap.isEmpty()) {
            return typeViaOnScreenKeys(text, perKeyDelayMs)
        }

        var typed = 0
        for (ch in text) {
            val needle = ch.toString().lowercase()
            var key = labelMap[needle]

            // Missing key may mean a layout switch is needed
            // (numeric ↔ qwerty). Try to swap.
            if (key == null) {
                val switched = trySwitchLayoutFor(needle, currentKb)
                if (switched != null) {
                    currentKb = switched
                    labelMap = buildLabelMap(currentKb)
                    key = labelMap[needle]
                }
            }

            // Still missing — fall through to the on-screen-keys
            // scanner for the remaining characters. Restart from 0
            // to keep the contract "all or nothing per strategy".
            if (key == null) {
                return typeViaOnScreenKeys(text, perKeyDelayMs)
            }

            gestures.tapAt(key.bounds.exactCenterX(), key.bounds.exactCenterY())
            typed++
            try { Thread.sleep(perKeyDelayMs) } catch (_: InterruptedException) {}
        }
        return TypeResult(true, typed, text.length, "ok")
    }

    /**
     * Try to set text directly via ACTION_SET_TEXT on the focused node.
     * Bypasses keyboard layout entirely.
     *
     * Contract:
     *   - Returns a successful TypeResult when the action is accepted AND
     *     verification confirms the text landed.
     *   - Returns null when the action is rejected OR when verification
     *     misses, signaling the caller to fall through to per-key keyboard
     *     dispatch.
     *
     * Framework notes (expand this list when adding support):
     *
     *   - Native Android EditText: ACTION_SET_TEXT is the canonical
     *     programmatic-set path. Accepts and applies atomically.
     *
     *   - Flutter Semantics text fields: accept the action, but
     *     `obscureText: true` variants silently drop the write without
     *     updating the rendered value. The explicit verification loop below
     *     catches this case.
     */
    private fun trySetText(text: String): TypeResult? {
        val root = service.rootInActiveWindow ?: return null
        val focused = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT) ?: return null
        try {
            val args = Bundle().apply {
                putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, text)
            }
            if (focused.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args)) {
                // Verify the action actually took effect. Flutter
                // obscureText fields accept ACTION_SET_TEXT but don't
                // update the underlying text. Exponential backoff
                // polling (10/20/40/80 ms, ~150ms total budget) lets
                // the view hierarchy settle without the previous
                // hardcoded 100ms sleep that over-waited on fast
                // native paths and under-waited on slow custom views.
                val delays = longArrayOf(10L, 20L, 40L, 80L)
                var verified = false
                for (d in delays) {
                    try { Thread.sleep(d) } catch (_: InterruptedException) {}
                    val refreshed = service.rootInActiveWindow
                        ?.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
                    if (refreshed != null) {
                        val actual = refreshed.text?.toString() ?: ""
                        try { refreshed.recycle() } catch (_: Exception) {}
                        // Password fields render dots, so any non-empty
                        // content counts as success. Empty target text
                        // counts as verified by construction.
                        if (actual.isNotEmpty() || text.isEmpty()) {
                            verified = true
                            break
                        }
                    }
                }
                if (!verified) return null
                return TypeResult(true, text.length, text.length, "ok via ACTION_SET_TEXT")
            }
        } catch (_: Exception) {
        } finally {
            try { focused.recycle() } catch (_: Exception) {}
        }
        return null
    }

    /**
     * Best-effort clear of the currently focused input field before typing.
     * Delegates to ClearTextChain. Failure does NOT abort the type.
     */
    private fun clearFocusedFieldBestEffort(perKeyDelayMs: Long) {
        ClearTextChain(service, uiTree, gestures).clearBestEffort(perKeyDelayMs)
    }

    /**
     * Type by tapping ON-SCREEN clickable elements whose label matches each
     * character. Used when no system IME is visible — typical for Flutter /
     * native banking apps that render a custom in-app numeric keypad as part
     * of their own view tree (digits exposed as clickable views with
     * contentDesc / text matching "0".."9").
     *
     * Builds a label→element map ONCE at start (single tree dump), then taps
     * keys without re-dumping for each character.
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
            gestures.tapAt(
                key.bounds.left + (key.bounds.right - key.bounds.left) / 2f,
                key.bounds.top + (key.bounds.bottom - key.bounds.top) / 2f,
            )
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
     * We pick the switch key based on what `needle` is — digit needs a
     * numeric layout, letter needs an alphabet layout. Returns the new
     * keyboard info after switch, or null if no useful switch key was found.
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
                gestures.tapAt(k.bounds.exactCenterX(), k.bounds.exactCenterY())
                try { Thread.sleep(250) } catch (_: InterruptedException) {}
                uiTree.markDirty()
                val next = uiTree.getKeyboardInfo()
                if (next.visible && next.keys.isNotEmpty()) return next
                return null
            }
        }
        return null
    }
}
