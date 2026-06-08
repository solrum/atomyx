package dev.atomyx.agent.control

/** Result of a single-node tap or keyboard-dismiss operation. */
data class TapResult(val success: Boolean, val reason: String)

/** Result of a type operation: how many characters landed and why. */
data class TypeResult(
    val success: Boolean,
    val typed: Int,
    val total: Int,
    val reason: String,
)

/**
 * Locate a backspace / clear / delete key on the current screen by
 * matching the label / contentDesc against a wide set of language- and
 * icon-independent patterns. Structural — works on any locale, any
 * keypad style.
 *
 * Shared between [KeyboardTyper] (pre-type clear) and
 * [FocusedInputClearer] (explicit clear command).
 */
internal fun findBackspaceKey(summary: List<CompactElement>): CompactElement? {
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
