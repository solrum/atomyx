import Foundation
import XCTest

/// Abstraction over XCUITest API calls for command handlers. Hides the
/// `XCUIApplication` / `XCUIElement` surface behind a protocol so:
///
///   1. **Version drift isolation.** When iOS ships a new accessibility
///      API, we add a second bridge implementation; `DefaultXCUIBridge`
///      stays as the stable-API path.
///
///   2. **Unit testability.** `MockXCUIBridge` (in the test file) lets
///      command handlers run under `node:test`-style assertions without
///      touching a simulator.
///
/// Press-key dispatch is NOT on this protocol — it lives in
/// `PressKeyStrategy` under `Tests/PressKey/`. The bridge is a thin
/// adapter over XCUI primitives; anything with its own strategy chain
/// belongs in a dedicated registry.

/// Per-key descriptor inside a keyboard. `label` is the visible key
/// text (e.g. "Q", "return", "shift"), `frame` is the tap target.
struct KeyDescriptor {
    let label: String
    let frame: CGRect
}

/// Result of `getKeyboard`. `visible=false` means no system keyboard
/// is on screen; the other fields are zero-initialized. `layout` is a
/// heuristic classification from the key labels — see
/// `DefaultXCUIBridge.detectKeyboardLayout`.
struct KeyboardInfoResult {
    let visible: Bool
    let frame: CGRect
    let keys: [KeyDescriptor]
    let layout: String

    static let notVisible = KeyboardInfoResult(
        visible: false, frame: .zero, keys: [], layout: "none"
    )
}

/// Result of `hideKeyboard`. `ok=false, strategy="not-visible"` is
/// the common no-op case; `ok=true, strategy=<name>` indicates which
/// dismissal path fired.
struct HideKeyboardResult {
    let ok: Bool
    let strategy: String

    static let notVisible = HideKeyboardResult(ok: false, strategy: "not-visible")
}

protocol XCUIBridge {
    /// Launch an app by bundle id and return its `XCUIApplication`
    /// reference. Idempotent — if the app is already running, this
    /// activates it.
    func launchApp(bundleId: String) -> XCUIApplication

    /// Terminate an app by bundle id. Creates a fresh reference
    /// internally — the caller does not need to hold one. Safe to call
    /// on an app that isn't running.
    func terminateApp(bundleId: String)

    /// Tap at raw point coordinates (app-relative). Uses
    /// `XCUICoordinate.tap()` which dispatches real UI events.
    func tapAt(app: XCUIApplication, x: CGFloat, y: CGFloat)

    /// Long-press at raw point coordinates for the given duration.
    /// Uses `XCUICoordinate.press(forDuration:)`.
    func longPressAt(app: XCUIApplication, x: CGFloat, y: CGFloat, durationSeconds: TimeInterval)

    /// Drag from one point to another. `durationSeconds` is the
    /// press-and-hold time before the drag starts (passed to
    /// `press(forDuration:thenDragTo:)`). The drag motion itself runs
    /// at XCUITest's internal speed — not parameterizable through this
    /// API.
    func swipe(
        app: XCUIApplication,
        fromX: CGFloat,
        fromY: CGFloat,
        toX: CGFloat,
        toY: CGFloat,
        durationSeconds: TimeInterval
    )

    /// Capture a JPEG screenshot of the full screen, resized to fit
    /// within `maxDim` on the longest side. Returns base64-encoded
    /// JPEG bytes. Quality 80 + max 900px keeps the payload under
    /// ~40KB for typical mobile screens while remaining readable
    /// for visual debugging.
    func screenshot() -> String

    /// Type raw text into the currently-focused input field. iOS
    /// handles this natively via `XCUIApplication.typeText(_:)` — the
    /// system keyboard accepts the string in one call and dispatches
    /// per-character events internally. No per-key delay simulation
    /// needed; XCUITest handles IME layout switching transparently for
    /// system-provided keyboards.
    func typeText(app: XCUIApplication, text: String)

    /// Dispatch a hardware-keyboard key press with modifier flags.
    /// Used for cross-platform keyboard shortcuts that the on-screen
    /// IME can't represent (⌘A select-all, ⌘C/V copy-paste, etc.).
    /// Requires the simulator to have Connect Hardware Keyboard on
    /// (`xcrun simctl keyboard <udid> enable hardware` or the
    /// I/O → Keyboard menu); without it, the modifier flags are
    /// silently dropped by the software keyboard and the key is
    /// typed as a plain character.
    ///
    /// Returns true when the dispatch is accepted, false when the
    /// XCUITest API throws — most commonly because no element has
    /// keyboard focus or the simulator keyboard configuration
    /// rejects the modifier combo.
    func typeKey(app: XCUIApplication, key: String, modifiers: XCUIElement.KeyModifierFlags) -> Bool

    /// Query the system keyboard state. Returns `visible=false` when
    /// no `.keyboard` elementType is present in the snapshot tree.
    /// Custom in-app keyboards (Flutter `GestureDetector` grids, React
    /// Native `TouchableOpacity` keys) are NOT detected — they're
    /// regular app views, not system keyboards. The host tool layer
    /// handles the "visible=false but field is focused" case via
    /// hierarchy inspection.
    func getKeyboard(app: XCUIApplication) -> KeyboardInfoResult

    /// Dismiss the on-screen keyboard if one is visible. Strategies
    /// tried in order until one succeeds:
    ///
    ///   1. Tap a keyboard dismiss affordance ("Hide keyboard" or
    ///      "done"/"return" button within the keyboard frame).
    ///   2. Tap just above the keyboard frame — resigns first
    ///      responder on most apps.
    ///
    /// Dispatches the gesture and returns immediately; does NOT
    /// wait for the dismiss animation. Host-side `waitForKeyboard`
    /// confirms the keyboard is gone by polling `hierarchy()`.
    ///
    /// Returns `ok: false` with `strategy: "not-visible"` when no
    /// keyboard was visible to begin with — idempotent no-op.
    func hideKeyboard(app: XCUIApplication) -> HideKeyboardResult

    /// Dump the accessibility tree as a nested dictionary. This is the
    /// canonical tree source — the host adapter (`@atomyx/core-driver-ios`)
    /// reshapes it into the framework's `TreeNode` and runs selector
    /// resolution, obscurement detection, and element filtering on the
    /// host side. The driver just returns raw `app.snapshot()` bytes.
    ///
    /// Returns `nil` when the snapshot RPC fails (simulator hung,
    /// XCUITest daemon crashed). Caller treats nil as an error.
    func dumpRawTree(app: XCUIApplication) -> [String: Any]?

    /// Return the tracked app's screen frame in points. Used by the
    /// host adapter to decide whether a resolved element is inside the
    /// visible viewport (long lists expose off-screen elements in the
    /// accessibility tree but coordinate taps to off-screen points
    /// fail silently).
    func getScreenSize(app: XCUIApplication) -> CGSize
}

/// Production implementation using the stable XCUITest API subset.
/// Works on iOS 15+ / Xcode 14+. Bleeding-edge APIs belong in a
/// separate bridge class when they appear — do not leak them here.
final class DefaultXCUIBridge: XCUIBridge {
    func launchApp(bundleId: String) -> XCUIApplication {
        let app = XCUIApplication(bundleIdentifier: bundleId)
        app.launch()
        return app
    }

    func terminateApp(bundleId: String) {
        let app = XCUIApplication(bundleIdentifier: bundleId)
        app.terminate()
    }

    func tapAt(app: XCUIApplication, x: CGFloat, y: CGFloat) {
        coordinate(in: app, x: x, y: y).tap()
    }

    func longPressAt(app: XCUIApplication, x: CGFloat, y: CGFloat, durationSeconds: TimeInterval) {
        coordinate(in: app, x: x, y: y).press(forDuration: durationSeconds)
    }

    func swipe(
        app: XCUIApplication,
        fromX: CGFloat,
        fromY: CGFloat,
        toX: CGFloat,
        toY: CGFloat,
        durationSeconds: TimeInterval
    ) {
        let start = coordinate(in: app, x: fromX, y: fromY)
        let end = coordinate(in: app, x: toX, y: toY)
        start.press(forDuration: durationSeconds, thenDragTo: end)
    }

    func screenshot() -> String {
        let shot = XCUIScreen.main.screenshot()
        let image = shot.image
        let resized = Self.resizeImage(image, maxDim: 540)
        guard let jpeg = resized.jpegData(compressionQuality: 0.8) else {
            // Fallback: return raw PNG if JPEG conversion fails.
            return shot.pngRepresentation.base64EncodedString()
        }
        return jpeg.base64EncodedString()
    }

    /// Down-scale a UIImage so its longest side fits within `maxDim`.
    /// Returns the original image if it already fits.
    private static func resizeImage(_ image: UIImage, maxDim: CGFloat) -> UIImage {
        let w = image.size.width
        let h = image.size.height
        guard w > maxDim || h > maxDim else { return image }
        let scale = maxDim / max(w, h)
        let newSize = CGSize(width: w * scale, height: h * scale)
        let renderer = UIGraphicsImageRenderer(size: newSize)
        return renderer.image { _ in
            image.draw(in: CGRect(origin: .zero, size: newSize))
        }
    }

    func typeText(app: XCUIApplication, text: String) {
        app.typeText(text)
    }

    func typeKey(app: XCUIApplication, key: String, modifiers: XCUIElement.KeyModifierFlags) -> Bool {
        // `typeKey` is non-throwing in the XCUI API; a missing focus
        // or a disabled simulator hardware keyboard produces a
        // silent no-op rather than a failure. Caller verifies by
        // reading post-call state, so we just forward and return
        // true unconditionally.
        app.typeKey(key, modifierFlags: modifiers)
        return true
    }

    func getScreenSize(app: XCUIApplication) -> CGSize {
        return app.frame.size
    }

    func dumpRawTree(app: XCUIApplication) -> [String: Any]? {
        guard let root = try? app.snapshot() else { return nil }
        return Self.buildRawTree(root: root, screen: app.frame)
    }

    /// Iterative pre-order walk producing a nested `[String: Any]`
    /// mirror of the XCUIElementSnapshot tree. Iterative (not recursive)
    /// for consistency with `getKeyboard`'s walk and to avoid Swift's
    /// ~512KB main-thread stack on pathological Flutter/RN trees that
    /// nest 30+ wrapper layers. Memory is O(tree size), same as
    /// recursive, but bounded by heap instead of the call stack.
    ///
    /// Two-pass approach:
    ///
    ///   Pass 1 — DFS walk pushing nodes onto a flat `entries` array in
    ///   document order, recording each node's parent index in
    ///   `childrenOf`. Depth-bounded at 100 levels.
    ///
    ///   Pass 2 — iterate `entries` from last to first (bottom-up). At
    ///   that point every descendant has already been materialized, so
    ///   assembling a parent's `children` array is just mapping child
    ///   indices to the finalized child dicts.
    ///
    /// Entry 0 is always the root. `maxDepth` of 100 is a generous
    /// ceiling — real iOS/Flutter apps rarely exceed 20 levels.
    private static func buildRawTree(root: XCUIElementSnapshot, screen: CGRect) -> [String: Any] {
        let maxDepth = 100
        struct WorkItem {
            let node: XCUIElementSnapshot
            let parentIndex: Int
            let depth: Int
        }

        var entries: [[String: Any]] = []
        var childrenOf: [Int: [Int]] = [:]

        var stack: [WorkItem] = [WorkItem(node: root, parentIndex: -1, depth: 0)]
        while let item = stack.popLast() {
            let myIndex = entries.count
            entries.append(Self.nodeDict(item.node, screen: screen))

            if item.parentIndex >= 0 {
                childrenOf[item.parentIndex, default: []].append(myIndex)
            }

            if item.depth < maxDepth {
                // Push children in reverse so pop order preserves
                // document order (parent → child[0] subtree → child[1]
                // subtree → ...). Stable order matters for diff-based
                // walkers on the host side.
                for child in item.node.children.reversed() {
                    stack.append(WorkItem(
                        node: child,
                        parentIndex: myIndex,
                        depth: item.depth + 1
                    ))
                }
            }
        }

        // Bottom-up assembly: walk entries last → first so every
        // child dict is already final before its parent consumes it.
        for i in (0 ..< entries.count).reversed() {
            if let childIndices = childrenOf[i], !childIndices.isEmpty {
                entries[i]["children"] = childIndices.map { entries[$0] }
            }
        }

        return entries[0]
    }

    /// Build the flat per-node `[String: Any]` (no children key yet).
    /// Called by `buildRawTree` once per node as the walk descends.
    /// `screen` is the host app's window frame; used to decide an
    /// on-screen / off-screen `visible` flag.
    private static func nodeDict(_ node: XCUIElementSnapshot, screen: CGRect) -> [String: Any] {
        let frame = node.frame
        var dict: [String: Any] = [
            "elementType": elementTypeName(node.elementType),
            "identifier": node.identifier,
            "label": node.label,
            "enabled": node.isEnabled,
            // XCUIElementSnapshot exposes `hasFocus` (not
            // `hasKeyboardFocus`, which lives only on XCUIElement).
            // For text inputs on iOS 13+ these are equivalent — the
            // snapshot carries whichever affordance receives
            // keystrokes.
            "focused": node.hasFocus,
            "bounds": [
                "left": Int(frame.minX),
                "top": Int(frame.minY),
                "right": Int(frame.maxX),
                "bottom": Int(frame.maxY),
            ],
        ]
        if let value = node.value as? String, !value.isEmpty {
            dict["value"] = value
        }
        if let placeholder = node.placeholderValue, !placeholder.isEmpty {
            // Placeholder text on an empty field. Cross-platform
            // selector key: `hint`. Lets host code pick an input
            // whose only identifier is its placeholder ("Email",
            // "Search products") — typical when developers forget
            // to set `accessibilityIdentifier`.
            dict["placeholderValue"] = placeholder
        }
        // `isSelected` carries Apple's accessibility "selected" bit
        // — used by tabs, segmented controls, and any custom
        // surface that calls `accessibilityTraits.insert(.selected)`.
        // Cross-platform consumers read `node.selected`.
        dict["selected"] = node.isSelected
        // Window / view title. Useful for navigation surfaces where
        // the visible heading is exposed via the title attribute
        // rather than `label`.
        if !node.title.isEmpty {
            dict["title"] = node.title
        }
        // Accessibility traits bitmask, decoded into a string array
        // of the canonical UIAccessibility trait names. The bitmask
        // is the closest objective signal to "what kind of element
        // is this really" and survives Flutter's accessibility-node
        // merging that loses fidelity in `elementType` alone.
        //
        // `XCUIElementSnapshot.traits` is exposed as a public
        // property on iOS 17+ but reachable on earlier versions via
        // KVC; wrap in `value(forKey:)` so absent keys fail silently.
        if let rawTraits = (node as AnyObject).value(forKey: "traits") as? UInt64,
           rawTraits != 0 {
            let names = decodeTraits(rawTraits)
            if !names.isEmpty {
                dict["traits"] = names
            }
        }
        // Device-coordinate frame for the accessibility region. On
        // most surfaces this matches `frame`; when it differs, it's
        // the element's hit area as opposed to its layout area.
        // `accessibilityFrame` is exposed on `XCUIElement` but not
        // declared on the `XCUIElementSnapshot` protocol — reach via
        // KVC so older Xcode SDKs and protocol-typed values keep
        // compiling. Snapshots returned by `app.snapshot()` typically
        // carry CGRect.zero for this key (the property is element-
        // facing, not snapshot-facing); skip emitting when the rect
        // is empty or matches `frame` to avoid noise downstream.
        if let aFrame = (node as AnyObject).value(forKey: "accessibilityFrame") as? CGRect,
           !aFrame.isEmpty,
           aFrame != frame {
            dict["accessibilityFrame"] = [
                "left": Int(aFrame.minX),
                "top": Int(aFrame.minY),
                "right": Int(aFrame.maxX),
                "bottom": Int(aFrame.maxY),
            ]
        }
        // Visibility + accessibility-leaf flag from the XCTest
        // accessibility daemon. Reads the snapshot's
        // `additionalAttributes` using integer keys resolved at
        // process load via `AccessibilityAttrSymbols`. This is the
        // same path WDA takes for `wdVisible` / `wdAccessible` —
        // when the dictionary is populated, it is the only signal
        // that accounts for occlusion (modal sheets, covering
        // siblings) which a pure frame-intersect cannot catch.
        //
        // Caveat: `app.snapshot()` does not pre-fetch the daemon
        // attributes today, so the dictionary is typically empty
        // and the fallback path runs. A future change will switch
        // to a snapshot variant that requests these attributes
        // explicitly; the resolver is in place so callers light up
        // automatically once that path lands.
        //
        // Fallbacks: `frame ∩ screen` for visibility, KVC
        // `isAccessibilityElement` for leaf detection. Both miss
        // occlusion but keep the wire schema slot populated.
        let additional = (node as AnyObject).value(forKey: "additionalAttributes") as? [AnyHashable: Any]
        if let visKey = AtomyxXCAXAIsVisibleAttribute(),
           let raw = additional?[visKey] as? NSNumber {
            dict["visible"] = raw.boolValue
        } else {
            dict["visible"] = !frame.isEmpty && frame.intersects(screen)
        }
        if let elKey = AtomyxXCAXAIsElementAttribute(),
           let raw = additional?[elKey] as? NSNumber {
            dict["accessible"] = raw.boolValue
        } else if let accessible = (node as AnyObject).value(forKey: "isAccessibilityElement") as? Bool {
            dict["accessible"] = accessible
        }
        return dict
    }

    /// Decode a `UIAccessibilityTraits` bitmask into an array of
    /// canonical trait names. Mirrors the public trait constants;
    /// callers that consume the wire JSON match against these
    /// strings. Unknown bits are dropped silently — the host can
    /// still observe them by reading the raw bitmask if it cares.
    private static func decodeTraits(_ bits: UInt64) -> [String] {
        var out: [String] = []
        if bits & UIAccessibilityTraits.button.rawValue != 0 { out.append("button") }
        if bits & UIAccessibilityTraits.link.rawValue != 0 { out.append("link") }
        if bits & UIAccessibilityTraits.image.rawValue != 0 { out.append("image") }
        if bits & UIAccessibilityTraits.searchField.rawValue != 0 { out.append("searchField") }
        if bits & UIAccessibilityTraits.staticText.rawValue != 0 { out.append("staticText") }
        if bits & UIAccessibilityTraits.header.rawValue != 0 { out.append("header") }
        if bits & UIAccessibilityTraits.tabBar.rawValue != 0 { out.append("tabBar") }
        if bits & UIAccessibilityTraits.selected.rawValue != 0 { out.append("selected") }
        if bits & UIAccessibilityTraits.notEnabled.rawValue != 0 { out.append("notEnabled") }
        if bits & UIAccessibilityTraits.adjustable.rawValue != 0 { out.append("adjustable") }
        if bits & UIAccessibilityTraits.summaryElement.rawValue != 0 { out.append("summaryElement") }
        if bits & UIAccessibilityTraits.keyboardKey.rawValue != 0 { out.append("keyboardKey") }
        if bits & UIAccessibilityTraits.causesPageTurn.rawValue != 0 { out.append("causesPageTurn") }
        if bits & UIAccessibilityTraits.playsSound.rawValue != 0 { out.append("playsSound") }
        if bits & UIAccessibilityTraits.startsMediaSession.rawValue != 0 { out.append("startsMediaSession") }
        if bits & UIAccessibilityTraits.allowsDirectInteraction.rawValue != 0 { out.append("allowsDirectInteraction") }
        if bits & UIAccessibilityTraits.updatesFrequently.rawValue != 0 { out.append("updatesFrequently") }
        return out
    }

    func getKeyboard(app: XCUIApplication) -> KeyboardInfoResult {
        // One `app.snapshot()` RPC materializes the entire accessibility
        // tree including keyboard windows, then we walk locally for the
        // first `.keyboard` node and collect its `.key`/`.button`
        // descendants. Same pattern as `dumpRawTree`.
        //
        // Previous implementation polled `app.keyboards.element(boundBy: 0)
        // .waitForExistence(timeout: 0.2)` which produced false negatives
        // against Flutter hosts where the keyboard WAS visible but the
        // XCUIElementQuery returned no match inside the window.
        let appSnapshot: XCUIElementSnapshot
        do {
            appSnapshot = try app.snapshot()
        } catch {
            return .notVisible
        }

        // Walk to find the first .keyboard container
        var stack: [XCUIElementSnapshot] = [appSnapshot]
        var keyboardNode: XCUIElementSnapshot?
        while let node = stack.popLast() {
            if node.elementType == .keyboard {
                keyboardNode = node
                break
            }
            for child in node.children.reversed() {
                stack.append(child)
            }
        }

        guard let kb = keyboardNode else {
            return .notVisible
        }

        // Collect key descriptors from the keyboard subtree. System
        // keys are `.key` elementType; function keys (return, shift,
        // delete, space) are sometimes `.button` depending on layout.
        var keys: [KeyDescriptor] = []
        var kbStack: [XCUIElementSnapshot] = [kb]
        while let node = kbStack.popLast() {
            if node.elementType == .key || node.elementType == .button {
                let label = node.label
                if !label.isEmpty {
                    keys.append(KeyDescriptor(label: label, frame: node.frame))
                }
            }
            for child in node.children.reversed() {
                kbStack.append(child)
            }
        }

        let layout = Self.detectKeyboardLayout(keys: keys.map { $0.label })
        return KeyboardInfoResult(
            visible: true,
            frame: kb.frame,
            keys: keys,
            layout: layout
        )
    }

    func hideKeyboard(app: XCUIApplication) -> HideKeyboardResult {
        // Snapshot-walk to locate the keyboard without the
        // XCUIElementQuery flakiness we worked around in `getKeyboard`.
        let appSnapshot: XCUIElementSnapshot
        do {
            appSnapshot = try app.snapshot()
        } catch {
            return .notVisible
        }

        var stack: [XCUIElementSnapshot] = [appSnapshot]
        var keyboardNode: XCUIElementSnapshot?
        while let node = stack.popLast() {
            if node.elementType == .keyboard {
                keyboardNode = node
                break
            }
            for child in node.children.reversed() {
                stack.append(child)
            }
        }

        guard let kb = keyboardNode else {
            return .notVisible
        }

        // Strategy 1: look for an explicit dismiss button inside the
        // keyboard frame. iPad and numeric keypads commonly expose one
        // labeled "Hide keyboard", "Done", or "return".
        let dismissLabels: Set<String> = [
            "Hide keyboard", "hide keyboard",
            "Done", "done",
            "Return", "return",
        ]
        var keyStack: [XCUIElementSnapshot] = [kb]
        while let node = keyStack.popLast() {
            if (node.elementType == .key || node.elementType == .button)
                && dismissLabels.contains(node.label) {
                // Tap the affordance at its center.
                let frame = node.frame
                let cx = frame.midX
                let cy = frame.midY
                coordinate(in: app, x: cx, y: cy).tap()
                return HideKeyboardResult(ok: true, strategy: "dismiss-affordance:\(node.label)")
            }
            for child in node.children.reversed() {
                keyStack.append(child)
            }
        }

        // Strategy 2: tap just above the keyboard frame to resign
        // first responder. 10pt above the top edge, horizontally
        // centered on the screen. Avoids hitting the keyboard itself
        // (which would dispatch a key press).
        let kbFrame = kb.frame
        let appFrame = app.frame
        let tapY = max(0, kbFrame.minY - 10)
        let tapX = appFrame.midX
        coordinate(in: app, x: tapX, y: tapY).tap()
        return HideKeyboardResult(ok: true, strategy: "tap-above-keyboard")
    }

    // MARK: - Helpers

    /// Heuristic layout classifier from key labels. iOS does not expose
    /// keyboard type directly — we infer from the keys present. Not
    /// exact for locale-specific layouts, but accurate for English
    /// system keyboards which is the common case.
    ///
    /// Numeric-pad detection uses a **count-based** rule: at least 5
    /// single-digit labels present. This tolerates punctuation keys
    /// (".", ",", "⌫") and non-digit accessibility labels interspersed
    /// in the list, which would break an all-match rule. A full
    /// numeric pad has 10 digits; 5 is a safe floor that still rejects
    /// QWERTY's digit row (which has letters as the majority).
    static func detectKeyboardLayout(keys: [String]) -> String {
        if keys.isEmpty { return "unknown" }

        // QWERTY: any top-row letter key. Checked first so QWERTY with
        // a digit row doesn't match numeric pad.
        let qwertyMarkers: Set<String> = [
            "q", "w", "e", "r", "t", "y",
            "Q", "W", "E", "R", "T", "Y",
        ]
        if keys.contains(where: { qwertyMarkers.contains($0) }) {
            return "qwerty"
        }

        // Numeric pad: ≥5 single-digit keys. Full pad has 0–9; partial
        // match proves intent.
        let digitCount = keys.filter { label in
            label.count == 1 && Int(label) != nil
        }.count
        if digitCount >= 5 {
            return "numeric_pad"
        }

        return "unknown"
    }

    private func coordinate(in app: XCUIApplication, x: CGFloat, y: CGFloat) -> XCUICoordinate {
        let origin = app.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 0))
        return origin.withOffset(CGVector(dx: x, dy: y))
    }
}
