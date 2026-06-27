package dev.atomyx.agent.control.clear

import dev.atomyx.agent.control.findBackspaceKey

class KeypadBackspaceStrategy : ClearTextStrategy {
    override val name = "keypad_backspace"

    companion object {
        private const val BACKSPACE_DELAY_MS = 80L
    }

    override fun attempt(context: ClearContext): ClearResult {
        context.uiTree.markDirty()
        val summary = context.uiTree.dumpCompact()
        val backspace = findBackspaceKey(summary)
            ?: return ClearResult.Failed("no on-screen backspace key found")
        // One extra tap as a safety margin — some inputs append a leading
        // character during focus animation.
        val taps = context.initialText.length + 1
        val cx = backspace.bounds.left + (backspace.bounds.right - backspace.bounds.left) / 2f
        val cy = backspace.bounds.top + (backspace.bounds.bottom - backspace.bounds.top) / 2f
        for (i in 0 until taps) {
            context.gestures.tapAt(cx, cy)
            try { Thread.sleep(BACKSPACE_DELAY_MS) } catch (_: InterruptedException) {}
        }
        return ClearResult.Success
    }
}
