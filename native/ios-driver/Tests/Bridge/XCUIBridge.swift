import Foundation
import XCTest

/// Abstraction over XCUITest API calls. Command handlers depend on this
/// protocol, never directly on `XCUIApplication` / `XCUIElement`. Two
/// reasons:
///
///   1. **Version drift isolation.** When iOS introduces a new
///      accessibility API (expected in iOS 17+ / 18+), we add a new
///      bridge implementation that uses it. `DefaultXCUIBridge` stays
///      as the stable-API path.
///
///   2. **Unit testability.** `MockXCUIBridge` in the unit test target
///      lets command handlers run under `node:test`-style assertions
///      without a simulator.
///
/// The Week 2 baseline has only `DefaultXCUIBridge`. A separate
/// `XCUIBridgeModern` implementation will be added when a concrete
/// version-specific need appears (Phase 3 or later). Creating an empty
/// stub now is premature abstraction.

struct ElementDescriptor {
    let type: String
    let identifier: String
    let label: String
    let value: String?
    let enabled: Bool
    let hittable: Bool
    let frame: CGRect
}

/// Summary of a dumpElements call. `total` is the unfiltered count so
/// the caller can report truncation.
struct DumpResult {
    let total: Int
    let elements: [ElementDescriptor]
}

/// Result of a `pressKey` attempt. `strategy` names which affordance
/// was used so the host adapter can surface it as `reason` text.
/// `affordanceFound = false` means NO verifiable control was tapped —
/// either because we fell back to an unverifiable gesture (edge swipe)
/// or because no strategy applied at all. See `PressKeyCommand` docs.
struct PressKeyResult {
    let affordanceFound: Bool
    let strategy: String
}

/// Platform-neutral selector query. Mirrors the TypeScript
/// `Selector` port exactly so the wire protocol is a direct
/// pass-through. `nth` defaults to 0 (first match).
struct SelectorQuery {
    let resourceId: String?
    let contentDesc: String?
    let text: String?
    let textContains: String?
    let hint: String?
    let predicate: String?
    let classChain: String?
    let nth: Int

    static func fromArgs(_ args: [String: Any]) -> SelectorQuery {
        SelectorQuery(
            resourceId: nonEmptyString(args["resourceId"]),
            contentDesc: nonEmptyString(args["contentDesc"]),
            text: nonEmptyString(args["text"]),
            textContains: nonEmptyString(args["textContains"]),
            hint: nonEmptyString(args["hint"]),
            predicate: nonEmptyString(args["predicate"]),
            classChain: nonEmptyString(args["classChain"]),
            nth: (args["nth"] as? Int) ?? 0
        )
    }

    private static func nonEmptyString(_ v: Any?) -> String? {
        guard let s = v as? String, !s.isEmpty else { return nil }
        return s
    }
}

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

/// Obscuring element reference — populated when the resolved
/// target's midpoint is covered by a different element in z-order.
/// Host adapter uses this to surface an actionable error on tap
/// instead of dispatching a coordinate tap that would hit the
/// overlay.
struct ObscurerInfo {
    let role: String
    let identifier: String
    let label: String
}

/// Result of `resolveSelector`. Mirrors the TS `ResolvedElement`
/// shape so the host adapter can map without reshaping.
struct ResolvedResult {
    let found: Bool
    let resolvedBy: String?
    let frame: CGRect
    let identifier: String
    let label: String
    let value: String?
    let enabled: Bool
    /// Non-nil when the target element is visually covered by a
    /// different element at its midpoint in z-order. Only populated
    /// for the snapshot-based selector path — the native NSPredicate
    /// escape hatch does not run the z-order walk (no snapshot to
    /// walk from the query API).
    let obscuredBy: ObscurerInfo?

    static let notFound = ResolvedResult(
        found: false,
        resolvedBy: nil,
        frame: .zero,
        identifier: "",
        label: "",
        value: nil,
        enabled: false,
        obscuredBy: nil
    )
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

    /// Dump the accessibility tree of an app, filtered to elements with
    /// at least one stable signal. Mirrors Android's
    /// `UiTreeService.dumpCompact` behavior.
    func dumpElements(app: XCUIApplication, limit: Int) -> DumpResult

    /// Tap at raw point coordinates (app-relative). Uses
    /// `XCUICoordinate.tap()` which dispatches real UI events, same
    /// semantics as Android's `GestureDispatcher.tap()`.
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

    /// Press a system or keyboard key. Returns which strategy was
    /// used and whether it was a verifiable affordance (`nav_bar_back`,
    /// `home`, `enter`) vs a best-effort gesture (`edge_swipe`) vs
    /// nothing applied (`none`). See `PressKeyResult`.
    func pressKey(app: XCUIApplication, key: String) -> PressKeyResult

    /// Capture a PNG screenshot of the simulator screen. Returns the
    /// base64-encoded PNG bytes — the host adapter re-encodes as
    /// needed for its wire protocol.
    func screenshot() -> String

    /// Type raw text into the currently-focused input field. On iOS
    /// this is handled natively by `XCUIApplication.typeText(_:)` —
    /// the system keyboard accepts the string in one call and
    /// dispatches per-character events internally. No per-key delay
    /// simulation is needed; iOS XCUITest handles IME layout switching
    /// transparently for system-provided keyboards.
    func typeText(app: XCUIApplication, text: String)

    /// Resolve a platform-neutral selector to a concrete element
    /// snapshot. Priority order for non-native strategies:
    /// `resourceId` > `contentDesc` > `text` > `textContains` > `hint`.
    /// `predicate` uses iOS-native `NSPredicate` via XCUIElementQuery.
    /// `classChain` is an Appium extension not supported by XCUITest
    /// natively — returns `.notFound` today, may be re-enabled in a
    /// future batch if we implement a class-chain parser.
    func resolveSelector(app: XCUIApplication, query: SelectorQuery) -> ResolvedResult

    /// Query the system keyboard state. Returns `visible=false` when
    /// no `UIKeyboard` window is present. Custom in-app keyboards
    /// (Flutter `GestureDetector` grids, React Native `TouchableOpacity`
    /// keys) are NOT detected by this — they're regular app views,
    /// not `UIKeyboard` instances. Host-side tool layer must handle
    /// the "visible=false but field is focused" case via dumpTree
    /// inspection as a Phase 6 hardening.
    func getKeyboard(app: XCUIApplication) -> KeyboardInfoResult

    /// Dump the accessibility tree as a nested dictionary. Used by
    /// host-side `DeviceInspector.getUiTree()` which returns the
    /// hierarchical `RawElement` shape. Consumed by tool-layer
    /// strategies that walk parent-child relationships
    /// (`StructuralInputFinder`, any future tree diff work).
    ///
    /// Different from `dumpElements` which returns a flattened list
    /// optimized for selector rendering. Both use `app.snapshot()`
    /// under the hood.
    ///
    /// Returns `nil` when the snapshot RPC fails (simulator hung,
    /// XCUITest daemon crashed). Caller treats nil as an error.
    func dumpRawTree(app: XCUIApplication) -> [String: Any]?

    /// Return the tracked app's screen frame in points. Used by the
    /// host adapter to determine whether a resolved element is
    /// within the visible viewport (Settings-style long lists expose
    /// off-screen elements in the accessibility tree but coordinate
    /// taps to off-screen points fail silently).
    func getScreenSize(app: XCUIApplication) -> CGSize
}

/// Production implementation using the stable XCUITest API subset.
/// Works on iOS 15+ / Xcode 14+. Do not use iOS 17-only APIs here;
/// bleeding-edge paths belong in a separate bridge class when they
/// appear.
final class DefaultXCUIBridge: XCUIBridge {
    func launchApp(bundleId: String) -> XCUIApplication {
        let app = XCUIApplication(bundleIdentifier: bundleId)
        app.launch()
        return app
    }

    func dumpElements(app: XCUIApplication, limit: Int) -> DumpResult {
        // PERFORMANCE: use `XCUIApplication.snapshot()` instead of
        // `descendants(matching: .any)` + `element(boundBy: i)` iteration.
        //
        // Why: XCUIElementQuery is lazy and every property access
        // (`elementType`, `identifier`, `isHittable`, `frame`) triggers
        // an RPC to the XCUITest daemon. On Settings root (~200
        // elements) this costs 10–20 seconds end-to-end because the
        // daemon is remote-process.
        //
        // `snapshot()` takes ONE RPC to materialize the full tree, then
        // walks a local `XCUIElementSnapshot` tree in memory. Property
        // access on snapshot nodes is O(1). Typical latency: ~200–500ms
        // for the whole dump.
        //
        // Trade-off: `XCUIElementSnapshot` does NOT have `isHittable`
        // (hittable is a live screen-state query). We set `hittable:
        // false` always. The host adapter's `clickable` derivation
        // already prefers `INTERACTIVE_ROLES.has(type)` and only falls
        // back to `hittable && id != ""` — which is the path we can't
        // serve anymore. Week 1 finding #7 already flagged `isHittable`
        // as a poor clickable proxy, so losing it is net positive.
        let snapshot: XCUIElementSnapshot
        do {
            snapshot = try app.snapshot()
        } catch {
            return DumpResult(total: 0, elements: [])
        }

        // Iterative pre-order walk using an explicit stack. Avoids
        // unbounded recursion on deeply-nested trees — Flutter / RN
        // apps routinely nest 30+ `other` wrapper layers, and Swift's
        // default ~512KB stack with local variables per frame can hit
        // stack overflow on pathological cases. Memory is O(tree size),
        // same as recursive, but bounded by heap instead of call stack.
        var collected: [ElementDescriptor] = []
        var total = 0
        var stack: [XCUIElementSnapshot] = [snapshot]

        while let node = stack.popLast() {
            total += 1

            let type = node.elementType
            let identifier = node.identifier
            let label = node.label
            let value = node.value as? String

            if collected.count < limit,
               hasStableSignal(
                   type: type,
                   identifier: identifier,
                   label: label,
                   value: value
               )
            {
                collected.append(ElementDescriptor(
                    type: Self.elementTypeName(type),
                    identifier: identifier,
                    label: label,
                    value: value,
                    enabled: node.isEnabled,
                    hittable: false, // not available on XCUIElementSnapshot — see comment above
                    frame: node.frame
                ))
            }

            // Push children in reverse so child[0] pops next — preserves
            // pre-order traversal (parent → child[0] → child[0]'s subtree
            // → child[1] → ...). Matters for stable rendering order.
            for child in node.children.reversed() {
                stack.append(child)
            }
        }
        return DumpResult(total: total, elements: collected)
    }

    func tapAt(app: XCUIApplication, x: CGFloat, y: CGFloat) {
        coordinate(in: app, x: x, y: y).tap()
    }

    func terminateApp(bundleId: String) {
        let app = XCUIApplication(bundleIdentifier: bundleId)
        app.terminate()
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

    func pressKey(app: XCUIApplication, key: String) -> PressKeyResult {
        switch key {
        case "home":
            XCUIDevice.shared.press(.home)
            return PressKeyResult(affordanceFound: true, strategy: "home")

        case "enter":
            // Typed into whatever is focused. If no field is focused,
            // typeText is a silent no-op on iOS — we still report
            // affordanceFound=true because that's the documented
            // semantics (enter = best-effort type) shared with Android.
            app.typeText("\n")
            return PressKeyResult(affordanceFound: true, strategy: "enter")

        case "back":
            // Three-strategy fallback chain, in order of verifiability:
            //
            // 1. Nav bar back button (VERIFIABLE). UIKit navigation
            //    controllers and SwiftUI NavigationStack both surface
            //    a leftmost button in `navigationBars` that pops the
            //    stack. `.exists` triggers one RPC to check; worth
            //    the cost for the honest success signal.
            //
            // 2. Edge swipe (UNVERIFIABLE). iOS pop gesture recognizer
            //    activates only from the very left edge and requires
            //    a fast drag. We CAN dispatch the gesture, but cannot
            //    verify it actually popped the stack — apps may
            //    intercept or ignore it. Returns affordanceFound=false
            //    so the agent can fall back to `find_element` for
            //    screen-specific affordances (Cancel/Done/X buttons).
            //
            // Not implemented here but scheduled for Batch 2:
            //   3. Modal dismiss buttons in nav bar / toolbar — search
            //      for labels IN {"Cancel", "Done", "Close"}. Requires
            //      the `resolveSelector` command which lands with
            //      Batch 2's full selector resolution path.
            // Use `waitForExistence(timeout:)` instead of `.exists`.
            // Both go through the XCUITest daemon RPC, but
            // `waitForExistence` has a bounded polling contract — if
            // the daemon is slow or the app is frozen, we cap at
            // 500ms rather than potentially blocking the main thread
            // for seconds on `.exists` (which has no timeout semantic).
            let navBarBackButton = app.navigationBars.buttons.element(boundBy: 0)
            if navBarBackButton.waitForExistence(timeout: 0.5) {
                navBarBackButton.tap()
                return PressKeyResult(affordanceFound: true, strategy: "nav_bar_back")
            }
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.0, dy: 0.5))
            let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5))
            start.press(
                forDuration: 0.0,
                thenDragTo: end,
                withVelocity: .fast,
                thenHoldForDuration: 0.0
            )
            return PressKeyResult(affordanceFound: false, strategy: "edge_swipe_best_effort")

        default:
            // Unknown key — best effort: type the raw string.
            app.typeText(key)
            return PressKeyResult(affordanceFound: true, strategy: "typed_raw")
        }
    }

    func screenshot() -> String {
        let shot = XCUIScreen.main.screenshot()
        return shot.pngRepresentation.base64EncodedString()
    }

    func typeText(app: XCUIApplication, text: String) {
        app.typeText(text)
    }

    func resolveSelector(app: XCUIApplication, query: SelectorQuery) -> ResolvedResult {
        // iOS-native predicate escape-hatch: use XCUIElementQuery with
        // NSPredicate. One RPC to evaluate the query. Accept the
        // agent's predicate string as-is — if it's malformed the
        // NSPredicate(format:) init will trap, which is the right
        // signal (agents should test their predicates).
        //
        // Obscurement detection is NOT run for the predicate path —
        // we'd need a separate snapshot RPC to walk the z-order, and
        // the predicate escape hatch is already an advanced-user
        // opt-in. Document as known limitation. Callers needing
        // obscurement detection should use the snapshot-based
        // resourceId/contentDesc/text/textContains/hint strategies.
        if let predicateString = query.predicate {
            let ns = NSPredicate(format: predicateString)
            let elementQuery = app.descendants(matching: .any).matching(ns)
            let count = elementQuery.count
            if count == 0 { return .notFound }
            let idx = max(0, min(query.nth, count - 1))
            let el = elementQuery.element(boundBy: idx)
            return ResolvedResult(
                found: true,
                resolvedBy: "predicate",
                frame: el.frame,
                identifier: el.identifier,
                label: el.label,
                value: el.value as? String,
                enabled: el.isEnabled,
                obscuredBy: nil
            )
        }

        // classChain is an Appium extension, not native XCUITest. We
        // do not parse it — return notFound. A future batch could
        // implement a minimal class-chain subset if synapse requires.
        if query.classChain != nil {
            return .notFound
        }

        // Snapshot-based path for the 5 non-native strategies. One
        // RPC to materialize the tree, then a local walk with the
        // same iterative stack pattern as `dumpElements` — avoids
        // recursion-depth issues on deeply-nested Flutter/RN trees.
        let snapshot: XCUIElementSnapshot
        do {
            snapshot = try app.snapshot()
        } catch {
            return .notFound
        }

        // Collect matches per strategy in a single walk. Each strategy
        // has its own array so we can preserve priority order at the
        // end without re-walking.
        var resourceIdMatches: [XCUIElementSnapshot] = []
        var contentDescMatches: [XCUIElementSnapshot] = []
        var textMatches: [XCUIElementSnapshot] = []
        var textContainsMatches: [XCUIElementSnapshot] = []
        var hintMatches: [XCUIElementSnapshot] = []

        var stack: [XCUIElementSnapshot] = [snapshot]
        while let node = stack.popLast() {
            let identifier = node.identifier
            let label = node.label
            let value = (node.value as? String) ?? ""

            if let rid = query.resourceId, identifier == rid {
                resourceIdMatches.append(node)
            }
            if let cd = query.contentDesc, label == cd {
                contentDescMatches.append(node)
            }
            if let txt = query.text, label == txt || value == txt {
                textMatches.append(node)
            }
            if let sub = query.textContains,
               label.localizedCaseInsensitiveContains(sub)
                || value.localizedCaseInsensitiveContains(sub)
            {
                textContainsMatches.append(node)
            }
            if let h = query.hint,
               identifier.localizedCaseInsensitiveContains(h)
                || label.localizedCaseInsensitiveContains(h)
                || value.localizedCaseInsensitiveContains(h)
            {
                hintMatches.append(node)
            }

            for child in node.children.reversed() {
                stack.append(child)
            }
        }

        // Priority: resourceId > contentDesc > text > textContains > hint
        let chain: [(String, [XCUIElementSnapshot])] = [
            ("resourceId", resourceIdMatches),
            ("contentDesc", contentDescMatches),
            ("text", textMatches),
            ("textContains", textContainsMatches),
            ("hint", hintMatches),
        ]
        for (strategy, matches) in chain where !matches.isEmpty {
            let idx = max(0, min(query.nth, matches.count - 1))
            let matched = matches[idx]
            let obscuredBy = Self.findObscurer(root: snapshot, target: matched)
            return ResolvedResult(
                found: true,
                resolvedBy: strategy,
                frame: matched.frame,
                identifier: matched.identifier,
                label: matched.label,
                value: matched.value as? String,
                enabled: matched.isEnabled,
                obscuredBy: obscuredBy
            )
        }
        return .notFound
    }

    /// Walk the snapshot in pre-order DFS; return the LAST element
    /// whose frame contains the target's midpoint. Later-visited
    /// elements render on top in iOS view hierarchy (sibling index
    /// N+1 draws over sibling N), so "last match" == topmost in
    /// z-order at that point.
    ///
    /// If the topmost is the target itself, returns nil (not
    /// obscured). Otherwise returns the obscuring element's
    /// identity.
    ///
    /// Identity comparison: XCUIElementSnapshot conforms to
    /// NSObjectProtocol so `===` works for reference equality. We
    /// also guard with frame + identifier + label equality as a
    /// safety net in case the snapshot tree returns distinct
    /// wrapper objects for logically-identical nodes (possible in
    /// some Xcode versions' snapshot serialization).
    static func findObscurer(
        root: XCUIElementSnapshot,
        target: XCUIElementSnapshot
    ) -> ObscurerInfo? {
        let point = CGPoint(x: target.frame.midX, y: target.frame.midY)

        var topmost: XCUIElementSnapshot = root
        var found = false
        var stack: [XCUIElementSnapshot] = [root]
        while let node = stack.popLast() {
            if node.frame.contains(point) {
                topmost = node
                found = true
            }
            for child in node.children.reversed() {
                stack.append(child)
            }
        }

        // If walk found no containing element (shouldn't happen for
        // a valid target, but be defensive), treat as not obscured.
        if !found { return nil }

        // Reference equality first; fall back to structural match.
        if topmost === target { return nil }
        if topmost.frame == target.frame
            && topmost.identifier == target.identifier
            && topmost.label == target.label
            && topmost.elementType == target.elementType
        {
            return nil
        }

        // Ancestor-vs-obscurer disambiguation. Pre-order DFS returns
        // the last containing node — which can legitimately be an
        // ancestor container (UICollectionView, generic "Other" group)
        // whose frame encloses the target. Those are NOT obscurers:
        // they contain the point because they contain the target.
        //
        // To tell ancestors from real obscurers, walk topmost's
        // subtree looking for the target by reference equality. If
        // reachable, topmost is an ancestor — not obscured. Otherwise
        // topmost sits in a separate subtree that happens to overlap
        // the target's midpoint — a real obscurer candidate (modal,
        // sheet, alert, floating button).
        if containsNode(root: topmost, target: target) {
            return nil
        }

        // Generic containers with no identifier/label are almost
        // never real blockers (they're unstyled grouping views that
        // pass hit-testing through). Suppress to avoid false
        // positives on pages with overlapping UICollectionView /
        // UIStackView layers. Real obscurers — modal Sheet, Alert,
        // Button, NavigationBar — have non-empty identifiers or
        // labels OR distinctive element types.
        let role = elementTypeName(topmost.elementType)
        if (role == "other" || role == "group")
            && topmost.identifier.isEmpty
            && topmost.label.isEmpty
        {
            return nil
        }

        return ObscurerInfo(
            role: role,
            identifier: topmost.identifier,
            label: topmost.label
        )
    }

    /// Walk `root`'s subtree iteratively looking for `target` by
    /// reference equality. Used by `findObscurer` to distinguish
    /// ancestor containers (target is inside) from real obscurers
    /// (target is in a sibling subtree).
    private static func containsNode(
        root: XCUIElementSnapshot,
        target: XCUIElementSnapshot
    ) -> Bool {
        var stack: [XCUIElementSnapshot] = [root]
        while let node = stack.popLast() {
            if node === target { return true }
            for child in node.children {
                stack.append(child)
            }
        }
        return false
    }

    func getScreenSize(app: XCUIApplication) -> CGSize {
        return app.frame.size
    }

    func dumpRawTree(app: XCUIApplication) -> [String: Any]? {
        guard let snapshot = try? app.snapshot() else { return nil }
        return Self.buildRawNode(snapshot, depth: 0)
    }

    private static func buildRawNode(_ node: XCUIElementSnapshot, depth: Int) -> [String: Any] {
        let frame = node.frame
        var dict: [String: Any] = [
            "elementType": elementTypeName(node.elementType),
            "identifier": node.identifier,
            "label": node.label,
            "enabled": node.isEnabled,
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

        // Recursive tree build — bounded by `maxDepth` to avoid stack
        // overflow on pathological Flutter/RN trees that nest 30+
        // wrapper layers. 100 is a generous ceiling; real iOS apps
        // and Flutter scaffolds rarely exceed 20 levels.
        let maxDepth = 100
        if depth < maxDepth, !node.children.isEmpty {
            let childDicts = node.children.map { buildRawNode($0, depth: depth + 1) }
            dict["children"] = childDicts
        }
        return dict
    }

    func getKeyboard(app: XCUIApplication) -> KeyboardInfoResult {
        // Previous implementation used
        //     app.keyboards.element(boundBy: 0).waitForExistence(timeout: 0.2)
        // which was flaky — verified false negatives against
        // kabuappStation Flutter app where the keyboard WAS visible
        // (`UIKeyboardLayoutStar Preview` present in the snapshot
        // tree) but the XCUIElementQuery returned no match inside the
        // 200ms window.
        //
        // Snapshot-based walk is reliable: one `app.snapshot()` RPC
        // materializes the entire accessibility tree including
        // keyboard windows, then we walk locally for the first
        // `.keyboard` node and collect its `.key`/`.button`
        // descendants. Same pattern as `dumpElements` + `resolveSelector`.
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

        // Collect key descriptors from the keyboard subtree
        var keys: [KeyDescriptor] = []
        var kbStack: [XCUIElementSnapshot] = [kb]
        while let node = kbStack.popLast() {
            // Keys on iOS system keyboard are `.key` elementType.
            // Function keys (return, shift, delete, space) are
            // typically also `.key` but some layouts use `.button`.
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

    // MARK: - Helpers

    /// Heuristic layout classifier from key labels. iOS does NOT
    /// expose keyboard type directly — we infer from the keys present.
    /// Not exact (e.g. locale-specific layouts won't match "q") but
    /// accurate for English system keyboards which is the common case.
    ///
    /// Numeric-pad detection uses a **count-based** rule: at least 5
    /// single-digit labels present. This tolerates punctuation keys
    /// (".", ",", "⌫") and non-digit accessibility labels
    /// interspersed in the list, which would break an all-match rule.
    /// A full numeric pad has 10 digits; 5 is a safe floor that still
    /// rejects QWERTY's digit row (which has letters as the majority).
    static func detectKeyboardLayout(keys: [String]) -> String {
        if keys.isEmpty { return "unknown" }

        // QWERTY: presence of any of the top-row letter keys. Checked
        // first so QWERTY with a digit row doesn't match numeric pad.
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

    private func hasStableSignal(
        type: XCUIElement.ElementType,
        identifier: String,
        label: String,
        value: String?
    ) -> Bool {
        if !identifier.isEmpty { return true }
        if !label.isEmpty { return true }
        if let v = value, !v.isEmpty { return true }
        switch type {
        case .button, .cell, .link, .textField, .secureTextField,
             .switch, .slider, .picker, .searchField:
            return true
        default:
            return false
        }
    }

    static func elementTypeName(_ t: XCUIElement.ElementType) -> String {
        switch t {
        case .any: return "any"
        case .other: return "other"
        case .application: return "application"
        case .group: return "group"
        case .window: return "window"
        case .sheet: return "sheet"
        case .drawer: return "drawer"
        case .alert: return "alert"
        case .dialog: return "dialog"
        case .button: return "button"
        case .radioButton: return "radioButton"
        case .radioGroup: return "radioGroup"
        case .checkBox: return "checkBox"
        case .disclosureTriangle: return "disclosureTriangle"
        case .popUpButton: return "popUpButton"
        case .comboBox: return "comboBox"
        case .menuButton: return "menuButton"
        case .toolbarButton: return "toolbarButton"
        case .popover: return "popover"
        case .keyboard: return "keyboard"
        case .key: return "key"
        case .navigationBar: return "navigationBar"
        case .tabBar: return "tabBar"
        case .tabGroup: return "tabGroup"
        case .toolbar: return "toolbar"
        case .statusBar: return "statusBar"
        case .table: return "table"
        case .tableRow: return "tableRow"
        case .tableColumn: return "tableColumn"
        case .outline: return "outline"
        case .outlineRow: return "outlineRow"
        case .browser: return "browser"
        case .collectionView: return "collectionView"
        case .slider: return "slider"
        case .pageIndicator: return "pageIndicator"
        case .progressIndicator: return "progressIndicator"
        case .activityIndicator: return "activityIndicator"
        case .segmentedControl: return "segmentedControl"
        case .picker: return "picker"
        case .pickerWheel: return "pickerWheel"
        case .switch: return "switch"
        case .toggle: return "toggle"
        case .link: return "link"
        case .image: return "image"
        case .icon: return "icon"
        case .searchField: return "searchField"
        case .scrollView: return "scrollView"
        case .scrollBar: return "scrollBar"
        case .staticText: return "staticText"
        case .textField: return "textField"
        case .secureTextField: return "secureTextField"
        case .datePicker: return "datePicker"
        case .textView: return "textView"
        case .menu: return "menu"
        case .menuItem: return "menuItem"
        case .menuBar: return "menuBar"
        case .menuBarItem: return "menuBarItem"
        case .map: return "map"
        case .webView: return "webView"
        case .incrementArrow: return "incrementArrow"
        case .decrementArrow: return "decrementArrow"
        case .timeline: return "timeline"
        case .ratingIndicator: return "ratingIndicator"
        case .valueIndicator: return "valueIndicator"
        case .splitGroup: return "splitGroup"
        case .splitter: return "splitter"
        case .relevanceIndicator: return "relevanceIndicator"
        case .colorWell: return "colorWell"
        case .helpTag: return "helpTag"
        case .matte: return "matte"
        case .dockItem: return "dockItem"
        case .ruler: return "ruler"
        case .rulerMarker: return "rulerMarker"
        case .grid: return "grid"
        case .levelIndicator: return "levelIndicator"
        case .cell: return "cell"
        case .layoutArea: return "layoutArea"
        case .layoutItem: return "layoutItem"
        case .handle: return "handle"
        case .stepper: return "stepper"
        case .tab: return "tab"
        case .touchBar: return "touchBar"
        case .statusItem: return "statusItem"
        @unknown default: return "type\(t.rawValue)"
        }
    }
}
