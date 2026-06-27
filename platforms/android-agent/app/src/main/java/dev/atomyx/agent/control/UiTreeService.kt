package dev.atomyx.agent.control

import android.accessibilityservice.AccessibilityService
import android.graphics.Rect
import android.view.accessibility.AccessibilityNodeInfo
import android.view.accessibility.AccessibilityWindowInfo

/**
 * Stateful UI tree service for AI-driven exploratory testing (Atomyx).
 *
 * Design: METADATA-ONLY registry. We do NOT retain AccessibilityNodeInfo
 * references — those become stale when the underlying view tree changes
 * (keyboard appears, focus shift, RecyclerView recycle, animations).
 *
 * Instead, at dump time, we capture EVERYTHING we need to act on the element:
 *   - screen bounds (for tap)
 *   - resourceId / text / contentDesc (for re-finding fresh node at input time)
 *
 * For tap: use cached bounds directly via dispatchGesture.
 * For input: re-resolve a fresh AccessibilityNodeInfo from the live tree by
 *            stable selector at input time, then performAction(SET_TEXT).
 *
 * Performance optimizations:
 *   - Skip system UI windows (status bar, nav bar, IME header chrome)
 *   - Skip subtrees with no interesting nodes
 *   - Skip elements with invalid/zero bounds (off-screen)
 *   - Single getBoundsInScreen call per node, single read of each property
 *   - Event-driven cache invalidation (AccessibilityEvent marks the tree
 *     dirty) with a 5s TTL safety net — see `maxCacheAgeMs` below.
 */
class UiTreeService(
    private val service: AccessibilityService,
) {

    data class RegistryEntry(
        val resourceId: String?,
        val text: String?,
        val contentDesc: String?,
        val className: String?,
        val bounds: Rect,
        val clickable: Boolean,
        val enabled: Boolean,
        val focused: Boolean,
        val isInImeWindow: Boolean,
    )

    private val registry: MutableMap<String, RegistryEntry> = mutableMapOf()
    private var lastDumpAt: Long = 0
    private var cachedDto: RawElementDto? = null
    private var cachedKeyboard: KeyboardInfo? = null

    // Cache validity: invalidated by accessibility events (window state/content
    // changed) instead of pure TTL. While UI is static, cache is reused
    // indefinitely. This eliminates redundant tree walks.
    @Volatile private var dirty: Boolean = true
    private val maxCacheAgeMs = 5000L  // safety net — force re-dump every 5s anyway

    /** Called by AtomyxAccessibilityService when relevant events fire. */
    fun markDirty() {
        dirty = true
    }

    @Synchronized
    fun dumpTree(forceFresh: Boolean = false): RawElementDto {
        val now = System.currentTimeMillis()
        val ageMs = now - lastDumpAt
        if (!forceFresh && !dirty && cachedDto != null && ageMs < maxCacheAgeMs) {
            return cachedDto!!
        }

        registry.clear()
        cachedKeyboard = null
        dirty = false

        val rootDto = RawElementDto(
            elementId = "el_root",
            className = "Root",
            children = mutableListOf(),
        )

        val windows = try { service.windows ?: emptyList() } catch (_: Exception) { emptyList() }
        // Sort by layer descending — top-most window first
        val sortedWindows = windows.sortedByDescending { it.layer }

        var counter = 0
        var imeKeys = mutableListOf<KeyboardKey>()
        var imeBounds: Rect? = null
        var imePackage: String? = null

        for (win in sortedWindows) {
            val root = try { win.root } catch (_: Exception) { null } ?: continue

            val isImeWindow = win.type == AccessibilityWindowInfo.TYPE_INPUT_METHOD
            val isSystemUi = isSystemUiPackage(root.packageName?.toString())

            // Skip status bar / navigation bar — never useful for testing
            if (isSystemUi && !isImeWindow) {
                root.recycle()
                continue
            }

            val dto = walk(
                node = root,
                depthLeft = 60,
                isInIme = isImeWindow,
                imeKeys = imeKeys,
                nextId = { ++counter },
            )
            if (dto != null) {
                if (isImeWindow) dto.isIme = true
                rootDto.children.add(dto)
            }

            if (isImeWindow) {
                val rect = Rect()
                root.getBoundsInScreen(rect)
                imeBounds = rect
                imePackage = root.packageName?.toString()
            }

            root.recycle()
        }

        cachedDto = rootDto
        cachedKeyboard = if (imeBounds != null) {
            KeyboardInfo(
                visible = true,
                packageName = imePackage,
                bounds = imeBounds,
                layout = classifyLayout(imeKeys),
                keys = imeKeys,
            )
        } else {
            KeyboardInfo(visible = false, packageName = null, bounds = null, layout = "none", keys = emptyList())
        }
        lastDumpAt = now
        return rootDto
    }

    @Synchronized
    fun getKeyboardInfo(): KeyboardInfo {
        if (cachedKeyboard == null || dirty) {
            dumpTree(forceFresh = true)
        }
        return cachedKeyboard ?: KeyboardInfo(false, null, null, "none", emptyList())
    }

    @Synchronized
    fun recycleRegistry() {
        registry.clear()
        cachedDto = null
        cachedKeyboard = null
        lastDumpAt = 0
        dirty = true
    }

    /**
     * Compact tree representation — flat list of actionable elements only.
     * Drops layout wrappers, includes stable selectors for direct lookup.
     * Used by Claude exploration for token-efficient screen reading.
     */
    @Synchronized
    fun dumpCompact(): List<CompactElement> {
        if (dirty || cachedDto == null) dumpTree()
        val out = mutableListOf<CompactElement>()
        for ((_, entry) in registry) {
            // Keep an element if it has ANY addressable signal:
            //   - clickable flag (native interactive)
            //   - text or contentDesc (label / accessibility name)
            //   - resourceId (Flutter / Compose / RN often expose ids
            //     without setting clickable or any label)
            if (
                !entry.clickable &&
                entry.text.isNullOrBlank() &&
                entry.contentDesc.isNullOrBlank() &&
                entry.resourceId.isNullOrBlank()
            ) continue
            out.add(
                CompactElement(
                    selector = bestSelector(entry),
                    label = entry.text ?: entry.contentDesc ?: "",
                    role = roleOf(entry.className),
                    clickable = entry.clickable,
                    enabled = entry.enabled,
                    bounds = BoundsDto(entry.bounds.left, entry.bounds.top, entry.bounds.right, entry.bounds.bottom),
                    isInIme = entry.isInImeWindow,
                ),
            )
        }
        return out
    }

    private fun bestSelector(e: RegistryEntry): Map<String, String> {
        return when {
            !e.resourceId.isNullOrBlank() -> mapOf("resourceId" to e.resourceId)
            !e.contentDesc.isNullOrBlank() -> mapOf("contentDesc" to e.contentDesc)
            !e.text.isNullOrBlank() -> mapOf("text" to e.text)
            else -> emptyMap()  // no stable selector — caller must use bounds
        }
    }

    private fun roleOf(className: String?): String {
        if (className == null) return "view"
        val last = className.split(".").lastOrNull() ?: return "view"
        return when {
            last.contains("Button", ignoreCase = true) -> "button"
            last.contains("EditText", ignoreCase = true) || last.contains("TextField", ignoreCase = true) -> "input"
            last.contains("TextView", ignoreCase = true) -> "text"
            last.contains("Image", ignoreCase = true) -> "image"
            last.contains("CheckBox", ignoreCase = true) -> "checkbox"
            last.contains("Switch", ignoreCase = true) -> "switch"
            else -> last.lowercase()
        }
    }

    // ────────────────────────────────────────────────────────────────────
    // Walk
    // ────────────────────────────────────────────────────────────────────

    private fun walk(
        node: AccessibilityNodeInfo,
        depthLeft: Int,
        isInIme: Boolean,
        imeKeys: MutableList<KeyboardKey>,
        nextId: () -> Int,
    ): RawElementDto? {
        if (depthLeft <= 0) return null

        // Single read of bounds + properties
        val rect = Rect()
        node.getBoundsInScreen(rect)

        // Skip elements with invalid bounds (off-screen, zero-size)
        val validBounds = rect.left >= 0 && rect.top >= 0 && rect.width() > 0 && rect.height() > 0

        val text = node.text?.toString()
        val desc = node.contentDescription?.toString()
        val rid = node.viewIdResourceName
        val cls = node.className?.toString()
        val clickable = node.isClickable
        val enabled = node.isEnabled
        // Input focus — the element accepting keystrokes right now.
        // Distinct from accessibility focus (isAccessibilityFocused).
        val focused = node.isFocused
        // Selection state — surfaces tab selection and segmented-
        // control current item.
        val selected = node.isSelected
        // Toggle / checkbox state for any node that exposes it.
        val checkable = node.isCheckable
        val checked = node.isChecked
        // Accessibility-runtime visibility — true when the
        // accessibility framework considers the node viewable on
        // screen (accounts for offscreen scroll position even when
        // bounds are non-zero).
        val visible = node.isVisibleToUser
        // Placeholder text on an empty field — API 26+. Lets host
        // code pick an input whose only identifier is its placeholder
        // (e.g. "Email", "Search products"). Cross-platform selector
        // key: `hint`.
        val hint = node.hintText?.toString()

        // Walk children first so we can decide whether to retain non-interesting parents
        val children = mutableListOf<RawElementDto>()
        val childCount = node.childCount
        for (i in 0 until childCount) {
            val child = try { node.getChild(i) } catch (_: Exception) { null } ?: continue
            try {
                val childDto = walk(child, depthLeft - 1, isInIme, imeKeys, nextId)
                if (childDto != null) children.add(childDto)
            } finally {
                child.recycle()
            }
        }

        // `focused` is counted as interesting so a focused empty input
        // survives pruning — host-side focus observers need to see the
        // node even when it has no resourceId/text/desc yet. `hint` is
        // interesting too: placeholder-only inputs (empty EditText
        // whose only identifier is its placeholder) must reach the
        // host so the inspector can emit a `hint:` selector.
        val interesting = clickable || !text.isNullOrBlank() ||
            !desc.isNullOrBlank() || !rid.isNullOrBlank() ||
            !hint.isNullOrBlank() || focused

        // Aggressive pruning: drop non-interesting leaves AND non-interesting wrappers
        // with 0 or 1 child (the child will be promoted)
        if (!interesting) {
            if (children.isEmpty()) return null
            if (children.size == 1) return children[0]  // flatten wrapper
        }

        if (!validBounds && !interesting) return null

        val id = "el_${"%03d".format(nextId())}"
        registry[id] = RegistryEntry(
            resourceId = rid,
            text = text,
            contentDesc = desc,
            className = cls,
            bounds = Rect(rect),
            clickable = clickable,
            enabled = enabled,
            focused = focused,
            isInImeWindow = isInIme,
        )

        // Track keyboard keys for IME classification
        if (isInIme && clickable && (text != null || desc != null)) {
            val label = text ?: desc!!
            imeKeys.add(KeyboardKey(elementId = id, label = label, bounds = Rect(rect)))
        }

        return RawElementDto(
            elementId = id,
            className = cls,
            resourceId = rid,
            text = text,
            contentDesc = desc,
            hintText = hint,
            bounds = BoundsDto(rect.left, rect.top, rect.right, rect.bottom),
            clickable = clickable,
            enabled = enabled,
            focused = focused,
            selected = selected,
            checkable = checkable,
            checked = checked,
            visible = visible,
            children = children,
        )
    }

    // ────────────────────────────────────────────────────────────────────
    // Helpers
    // ────────────────────────────────────────────────────────────────────

    private fun isSystemUiPackage(pkg: String?): Boolean {
        if (pkg == null) return false
        return pkg == "com.android.systemui" || pkg == "android"
    }

    private fun classifyLayout(keys: List<KeyboardKey>): String {
        if (keys.isEmpty()) return "none"
        val labels = keys.map { it.label.lowercase() }.toSet()

        // Phone/PIN dialer: 0-9 + delete + maybe special chars
        val digits = setOf("0", "1", "2", "3", "4", "5", "6", "7", "8", "9")
        val foundDigits = labels.intersect(digits)
        val hasLetters = labels.any { it.length == 1 && it[0] in 'a'..'z' }

        return when {
            foundDigits.size >= 9 && !hasLetters -> "numeric_pad"
            foundDigits.size >= 8 && hasLetters -> "phone_alpha"  // T9-style
            hasLetters && labels.contains("q") -> "qwerty"
            hasLetters -> "alpha"
            foundDigits.size >= 5 -> "numeric_partial"
            else -> "unknown"
        }
    }
}

data class RawElementDto(
    val elementId: String,
    val className: String? = null,
    val resourceId: String? = null,
    val text: String? = null,
    val contentDesc: String? = null,
    val hintText: String? = null,
    val bounds: BoundsDto? = null,
    val clickable: Boolean = false,
    val enabled: Boolean = true,
    val focused: Boolean = false,
    val selected: Boolean = false,
    // `checkable` distinguishes "this node can carry a checked
    // state" (true for CheckBox, Switch, ToggleButton) from
    // "this node is checked right now". A non-checkable node has
    // `checked = false` because the AccessibilityNodeInfo default
    // is false even when the property is meaningless — host code
    // reads `checked` only when `checkable` is true.
    val checkable: Boolean = false,
    val checked: Boolean = false,
    val visible: Boolean = true,
    // Set on the TOP-LEVEL DTO of an IME window subtree by dumpTree().
    // Lets host-side `findKeyboardNode(tree)` locate the keyboard
    // without a separate RPC. Descendants of the IME root inherit
    // semantically — they're in the same subtree.
    var isIme: Boolean = false,
    val children: MutableList<RawElementDto> = mutableListOf(),
)

data class BoundsDto(val left: Int, val top: Int, val right: Int, val bottom: Int)

data class ElementMatchDto(
    val elementId: String,
    val confidence: String,
)

data class KeyboardKey(
    val elementId: String,
    val label: String,
    val bounds: Rect,
)

data class KeyboardInfo(
    val visible: Boolean,
    val packageName: String?,
    val bounds: Rect?,
    val layout: String,  // "numeric_pad" | "qwerty" | "alpha" | "phone_alpha" | "unknown" | "none"
    val keys: List<KeyboardKey>,
)

data class CompactElement(
    val selector: Map<String, String>,
    val label: String,
    val role: String,
    val clickable: Boolean,
    val enabled: Boolean,
    val bounds: BoundsDto,
    val isInIme: Boolean,
)
