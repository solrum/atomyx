import XCTest
// Types from the same bundle resolve directly — no import needed.
// `@testable import` doesn't apply to UI test bundles.

/// Unit tests for command handlers. Use `MockXCUIBridge` to run without
/// a real simulator. These tests run under the default `xcodebuild test`
/// invocation (no `ADET_SERVE` set) so the serving test is skipped and
/// these execute normally.
///
/// Goal: exercise dispatch logic, error paths, state mutation. NOT to
/// verify XCUITest semantics (that's the serving driver's job).
final class CommandHandlerUnitTests: XCTestCase {

    // MARK: - Registry

    func testRegistryDispatchesKnownCommand() {
        let registry = CommandRegistry()
        registry.register(PingCommand())

        let req = Request(id: 1, type: "ping", args: [:])
        let resp = registry.dispatch(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.id, 1)
        XCTAssertEqual(resp.data["pong"] as? Bool, true)
    }

    func testRegistryReturnsErrorForUnknownCommand() {
        let registry = CommandRegistry()
        let req = Request(id: 42, type: "nope", args: [:])
        let resp = registry.dispatch(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.id, 42)
        XCTAssertEqual(resp.error, "unknown command: nope")
    }

    // MARK: - LaunchAppCommand

    func testLaunchAppRejectsMissingBundleId() {
        let cmd = LaunchAppCommand()
        let req = Request(id: 1, type: "launchApp", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing bundleId")
    }

    func testLaunchAppRejectsEmptyBundleId() {
        let cmd = LaunchAppCommand()
        let req = Request(id: 1, type: "launchApp", args: ["bundleId": ""])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing bundleId")
    }

    func testLaunchAppHappyPathUpdatesTrackedState() {
        let cmd = LaunchAppCommand()
        let bridge = MockXCUIBridge()
        let state = DriverState()

        let req = Request(id: 1, type: "launchApp", args: ["bundleId": "com.example.app"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.launchCalls, ["com.example.app"])
        XCTAssertEqual(state.currentBundleId, "com.example.app")
        XCTAssertNotNil(state.currentApp)
        XCTAssertEqual(resp.data["bundleId"] as? String, "com.example.app")
    }

    // MARK: - DumpTreeCommand

    func testDumpTreeRejectsWhenNoAppLaunched() {
        let cmd = DumpTreeCommand()
        let req = Request(id: 1, type: "dumpTree", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    // MARK: - TapAtCommand

    func testTapAtRejectsMissingCoordinates() {
        let cmd = TapAtCommand()
        let req = Request(id: 1, type: "tapAt", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing x/y (numbers)")
    }

    func testTapAtRejectsWhenNoAppLaunched() {
        let cmd = TapAtCommand()
        let req = Request(id: 1, type: "tapAt", args: ["x": 100, "y": 200])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    // MARK: - ForceStopAppCommand

    func testForceStopAppRejectsMissingBundleId() {
        let cmd = ForceStopAppCommand()
        let req = Request(id: 1, type: "forceStopApp", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing bundleId")
    }

    func testForceStopAppCallsBridgeAndClearsTrackedState() {
        let cmd = ForceStopAppCommand()
        let bridge = MockXCUIBridge()
        let state = DriverState()
        state.currentBundleId = "com.example.app"
        // currentApp left nil for unit test (cannot construct XCUIApplication stub)

        let req = Request(id: 1, type: "forceStopApp", args: ["bundleId": "com.example.app"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.terminateCalls, ["com.example.app"])
        XCTAssertEqual(state.currentBundleId, "")
    }

    func testForceStopAppDoesNotClearUnrelatedTrackedState() {
        let cmd = ForceStopAppCommand()
        let bridge = MockXCUIBridge()
        let state = DriverState()
        state.currentBundleId = "com.example.app"

        let req = Request(id: 1, type: "forceStopApp", args: ["bundleId": "com.other.app"])
        _ = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertEqual(state.currentBundleId, "com.example.app")
    }

    // MARK: - SwipeCommand

    func testSwipeRejectsMissingCoordinates() {
        let cmd = SwipeCommand()
        let req = Request(id: 1, type: "swipe", args: ["fromX": 0])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("missing") == true)
    }

    func testSwipeRejectsWhenNoAppLaunched() {
        let cmd = SwipeCommand()
        let req = Request(id: 1, type: "swipe", args: [
            "fromX": 0, "fromY": 0, "toX": 100, "toY": 100,
        ])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    // MARK: - LongPressAtCommand

    func testLongPressRejectsMissingCoordinates() {
        let cmd = LongPressAtCommand()
        let req = Request(id: 1, type: "longPressAt", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing x/y (numbers)")
    }

    func testLongPressRejectsWhenNoAppLaunched() {
        let cmd = LongPressAtCommand()
        let req = Request(id: 1, type: "longPressAt", args: ["x": 10, "y": 20])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    // MARK: - PressKeyCommand

    func testPressKeyRejectsMissingKey() {
        let cmd = PressKeyCommand()
        let req = Request(id: 1, type: "pressKey", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing key")
    }

    func testPressKeyHomeDoesNotRequireTrackedApp() {
        let cmd = PressKeyCommand()
        let bridge = MockXCUIBridge()
        let req = Request(id: 1, type: "pressKey", args: ["key": "home"])
        let resp = cmd.handle(req, bridge: bridge, state: DriverState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.pressKeyCalls, ["home"])
        XCTAssertEqual(resp.data["affordanceFound"] as? Bool, true)
        XCTAssertEqual(resp.data["strategy"] as? String, "home")
    }

    func testPressKeyBackRequiresTrackedApp() {
        let cmd = PressKeyCommand()
        let req = Request(id: 1, type: "pressKey", args: ["key": "back"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testPressKeyBackReportsAffordanceFoundFromNavBar() {
        let cmd = PressKeyCommand()
        let bridge = MockXCUIBridge()
        // Default behavior: back → nav_bar_back, affordanceFound=true
        let state = mockState()

        let req = Request(id: 1, type: "pressKey", args: ["key": "back"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["affordanceFound"] as? Bool, true)
        XCTAssertEqual(resp.data["strategy"] as? String, "nav_bar_back")
        XCTAssertEqual(bridge.pressKeyCalls, ["back"])
    }

    func testPressKeyBackReportsAffordanceNotFoundOnEdgeSwipeFallback() {
        let cmd = PressKeyCommand()
        let bridge = MockXCUIBridge()
        // Override: simulate nav bar absent → edge-swipe fallback.
        // Exercises the `affordanceFound=false` wire response path and
        // proves PressKeyCommand faithfully passes the bridge's result
        // through without rewriting it.
        bridge.pressKeyBehavior = { _ in
            PressKeyResult(affordanceFound: false, strategy: "edge_swipe_best_effort")
        }
        let state = mockState()

        let req = Request(id: 1, type: "pressKey", args: ["key": "back"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok) // wire-level ok; affordance outcome is in data
        XCTAssertEqual(resp.data["affordanceFound"] as? Bool, false)
        XCTAssertEqual(resp.data["strategy"] as? String, "edge_swipe_best_effort")
    }

    // MARK: - ResolveSelectorCommand

    func testResolveSelectorRequiresTrackedApp() {
        let cmd = ResolveSelectorCommand()
        let req = Request(id: 1, type: "resolveSelector", args: ["resourceId": "foo"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testResolveSelectorReturnsNotFoundByDefault() {
        let cmd = ResolveSelectorCommand()
        let bridge = MockXCUIBridge() // default resolveBehavior = .notFound
        let state = mockState()

        let req = Request(id: 1, type: "resolveSelector", args: ["resourceId": "nope"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["found"] as? Bool, false)
    }

    func testResolveSelectorPassesQueryThroughAndEmitsFullResponse() {
        let cmd = ResolveSelectorCommand()
        let bridge = MockXCUIBridge()
        bridge.resolveBehavior = { _ in
            ResolvedResult(
                found: true,
                resolvedBy: "resourceId",
                frame: CGRect(x: 20, y: 100, width: 400, height: 44),
                identifier: "com.example.row",
                label: "Settings",
                value: nil,
                enabled: true,
                obscuredBy: nil
            )
        }
        let state = mockState()

        let req = Request(id: 1, type: "resolveSelector", args: [
            "resourceId": "com.example.row",
            "nth": 0,
        ])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["found"] as? Bool, true)
        XCTAssertEqual(resp.data["resolvedBy"] as? String, "resourceId")
        XCTAssertEqual(resp.data["identifier"] as? String, "com.example.row")
        XCTAssertEqual(resp.data["label"] as? String, "Settings")
        XCTAssertEqual(resp.data["enabled"] as? Bool, true)
        // Midpoint: x=220, y=122; size 400×44
        XCTAssertEqual(resp.data["x"] as? Int, 220)
        XCTAssertEqual(resp.data["y"] as? Int, 122)
        XCTAssertEqual(resp.data["w"] as? Int, 400)
        XCTAssertEqual(resp.data["h"] as? Int, 44)
        // Not obscured → no obscuredBy field in response
        XCTAssertNil(resp.data["obscuredBy"])
        // Bridge recorded the query with the resourceId passed through
        XCTAssertEqual(bridge.resolveCalls.count, 1)
        XCTAssertEqual(bridge.resolveCalls.first?.resourceId, "com.example.row")
    }

    func testResolveSelectorPassesObscuredByThrough() {
        let cmd = ResolveSelectorCommand()
        let bridge = MockXCUIBridge()
        bridge.resolveBehavior = { _ in
            ResolvedResult(
                found: true,
                resolvedBy: "resourceId",
                frame: CGRect(x: 20, y: 400, width: 400, height: 44),
                identifier: "com.example.row",
                label: "Settings",
                value: nil,
                enabled: true,
                obscuredBy: ObscurerInfo(
                    role: "other",
                    identifier: "modal-sheet",
                    label: "Notification Settings"
                )
            )
        }
        let state = mockState()

        let req = Request(id: 1, type: "resolveSelector", args: ["resourceId": "com.example.row"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        let obscuredBy = resp.data["obscuredBy"] as? [String: Any]
        XCTAssertNotNil(obscuredBy)
        XCTAssertEqual(obscuredBy?["role"] as? String, "other")
        XCTAssertEqual(obscuredBy?["identifier"] as? String, "modal-sheet")
        XCTAssertEqual(obscuredBy?["label"] as? String, "Notification Settings")
    }

    // MARK: - GetScreenSizeCommand

    func testGetScreenSizeRequiresTrackedApp() {
        let cmd = GetScreenSizeCommand()
        let req = Request(id: 1, type: "getScreenSize", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testGetScreenSizeReturnsBridgeValue() {
        let cmd = GetScreenSizeCommand()
        let bridge = MockXCUIBridge()
        bridge.screenSizeBehavior = {
            CGSize(width: 430, height: 932)
        }
        let state = mockState()

        let req = Request(id: 1, type: "getScreenSize", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["width"] as? Int, 430)
        XCTAssertEqual(resp.data["height"] as? Int, 932)
    }

    func testSelectorQueryIgnoresEmptyStrings() {
        // Empty strings must NOT populate the query — otherwise the
        // snapshot walker treats empty-matches as "match any element
        // with empty identifier", which would return every wrapper.
        let query = SelectorQuery.fromArgs([
            "resourceId": "",
            "contentDesc": "General",
            "text": "",
        ])
        XCTAssertNil(query.resourceId)
        XCTAssertEqual(query.contentDesc, "General")
        XCTAssertNil(query.text)
    }

    // MARK: - ClearFocusedInputCommand

    func testClearFocusedInputRequiresTrackedApp() {
        let cmd = ClearFocusedInputCommand()
        let req = Request(id: 1, type: "clearFocusedInput", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testClearFocusedInputSendsDefaultDeleteCount() {
        let cmd = ClearFocusedInputCommand()
        let bridge = MockXCUIBridge()
        let state = mockState()

        let req = Request(id: 1, type: "clearFocusedInput", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["deleted"] as? Int, 100)
        XCTAssertEqual(bridge.typeTextCalls.count, 1)
        XCTAssertEqual(bridge.typeTextCalls.first?.count, 100)
    }

    func testClearFocusedInputCapsRunawayMaxDeletes() {
        let cmd = ClearFocusedInputCommand()
        let bridge = MockXCUIBridge()
        let state = mockState()

        let req = Request(id: 1, type: "clearFocusedInput", args: ["maxDeletes": 99999])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["deleted"] as? Int, 500) // capped
    }

    // MARK: - GetKeyboardCommand

    func testGetKeyboardRequiresTrackedApp() {
        let cmd = GetKeyboardCommand()
        let req = Request(id: 1, type: "getKeyboard", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testGetKeyboardReturnsNotVisibleByDefault() {
        let cmd = GetKeyboardCommand()
        let bridge = MockXCUIBridge() // default = .notVisible
        let state = mockState()

        let req = Request(id: 1, type: "getKeyboard", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["visible"] as? Bool, false)
        XCTAssertEqual(resp.data["layout"] as? String, "none")
        XCTAssertTrue(resp.data["bounds"] is NSNull)
        XCTAssertEqual((resp.data["keys"] as? [Any])?.count, 0)
    }

    func testGetKeyboardReturnsVisibleWithKeys() {
        let cmd = GetKeyboardCommand()
        let bridge = MockXCUIBridge()
        bridge.keyboardBehavior = {
            KeyboardInfoResult(
                visible: true,
                frame: CGRect(x: 0, y: 500, width: 390, height: 300),
                keys: [
                    KeyDescriptor(label: "Q", frame: CGRect(x: 0, y: 500, width: 35, height: 40)),
                    KeyDescriptor(label: "W", frame: CGRect(x: 40, y: 500, width: 35, height: 40)),
                ],
                layout: "qwerty"
            )
        }
        let state = mockState()

        let req = Request(id: 1, type: "getKeyboard", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["visible"] as? Bool, true)
        XCTAssertEqual(resp.data["layout"] as? String, "qwerty")
        let keys = resp.data["keys"] as? [[String: Any]]
        XCTAssertEqual(keys?.count, 2)
        XCTAssertEqual(keys?.first?["label"] as? String, "Q")
    }

    func testDetectKeyboardLayoutReturnsQwertyForLetterKeys() {
        let layout = DefaultXCUIBridge.detectKeyboardLayout(
            keys: ["q", "w", "e", "r", "t", "y", "space"]
        )
        XCTAssertEqual(layout, "qwerty")
    }

    func testDetectKeyboardLayoutReturnsNumericPadForDigitsOnly() {
        let layout = DefaultXCUIBridge.detectKeyboardLayout(
            keys: ["1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "."]
        )
        XCTAssertEqual(layout, "numeric_pad")
    }

    func testDetectKeyboardLayoutReturnsUnknownForEmpty() {
        let layout = DefaultXCUIBridge.detectKeyboardLayout(keys: [])
        XCTAssertEqual(layout, "unknown")
    }

    // MARK: - DumpRawTreeCommand

    func testDumpRawTreeRequiresTrackedApp() {
        let cmd = DumpRawTreeCommand()
        let req = Request(id: 1, type: "dumpRawTree", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testDumpRawTreeReportsSnapshotFailure() {
        let cmd = DumpRawTreeCommand()
        let bridge = MockXCUIBridge() // default rawTreeBehavior = nil
        let state = mockState()

        let req = Request(id: 1, type: "dumpRawTree", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("snapshot failed") == true)
    }

    func testDumpRawTreeReturnsHierarchicalRoot() {
        let cmd = DumpRawTreeCommand()
        let bridge = MockXCUIBridge()
        bridge.rawTreeBehavior = {
            [
                "elementType": "application",
                "identifier": "",
                "label": "MyApp",
                "enabled": true,
                "bounds": ["left": 0, "top": 0, "right": 440, "bottom": 956],
                "children": [
                    [
                        "elementType": "textField",
                        "identifier": "login.username",
                        "label": "Username",
                        "enabled": true,
                        "bounds": ["left": 20, "top": 100, "right": 420, "bottom": 140],
                    ] as [String: Any],
                ],
            ]
        }
        let state = mockState()

        let req = Request(id: 1, type: "dumpRawTree", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        let root = resp.data["root"] as? [String: Any]
        XCTAssertNotNil(root)
        XCTAssertEqual(root?["elementType"] as? String, "application")
        let children = root?["children"] as? [[String: Any]]
        XCTAssertEqual(children?.count, 1)
        XCTAssertEqual(children?.first?["elementType"] as? String, "textField")
        XCTAssertEqual(children?.first?["identifier"] as? String, "login.username")
    }

    // MARK: - TypeTextCommand

    func testTypeTextRejectsMissingText() {
        let cmd = TypeTextCommand()
        let req = Request(id: 1, type: "typeText", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing text")
    }

    func testTypeTextRejectsWhenNoAppLaunched() {
        let cmd = TypeTextCommand()
        let req = Request(id: 1, type: "typeText", args: ["text": "hello"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testTypeTextHappyPathCallsBridgeAndReportsCount() {
        let cmd = TypeTextCommand()
        let bridge = MockXCUIBridge()
        let state = mockState()

        let req = Request(id: 1, type: "typeText", args: ["text": "hello"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.typeTextCalls, ["hello"])
        XCTAssertEqual(resp.data["typed"] as? Int, 5)
        XCTAssertEqual(resp.data["total"] as? Int, 5)
        XCTAssertEqual(resp.data["success"] as? Bool, true)
        XCTAssertEqual(resp.data["reason"] as? String, "ok")
    }

    // MARK: - ScreenshotCommand

    func testScreenshotReturnsBase64AndFormat() {
        let cmd = ScreenshotCommand()
        let bridge = MockXCUIBridge()
        let req = Request(id: 1, type: "screenshot", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: DriverState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.screenshotCallCount, 1)
        XCTAssertEqual(resp.data["base64"] as? String, "ZmFrZS1wbmc=")
        XCTAssertEqual(resp.data["format"] as? String, "png")
    }

    // MARK: - Wire protocol

    func testRequestDecodeRoundTrip() {
        let json = "{\"id\":7,\"type\":\"dumpTree\",\"args\":{\"limit\":50}}"
        let req = Request.decode(json)

        XCTAssertNotNil(req)
        XCTAssertEqual(req?.id, 7)
        XCTAssertEqual(req?.type, "dumpTree")
        XCTAssertEqual(req?.args["limit"] as? Int, 50)
    }

    func testRequestDecodeRejectsMissingFields() {
        XCTAssertNil(Request.decode("{\"id\":1}"))
        XCTAssertNil(Request.decode("{\"type\":\"ping\"}"))
        XCTAssertNil(Request.decode("not json"))
    }

    func testResponseOkEncode() {
        let resp = Response.ok(id: 3, data: ["pong": true])
        let encoded = String(data: resp.encode(), encoding: .utf8)!
        // Order-independent check — JSON key ordering is not guaranteed.
        XCTAssertTrue(encoded.contains("\"ok\":true"))
        XCTAssertTrue(encoded.contains("\"id\":3"))
        XCTAssertTrue(encoded.contains("\"pong\":true"))
    }

    func testResponseErrorEncode() {
        let resp = Response.error(id: 4, message: "bad")
        let encoded = String(data: resp.encode(), encoding: .utf8)!
        XCTAssertTrue(encoded.contains("\"ok\":false"))
        XCTAssertTrue(encoded.contains("\"error\":\"bad\""))
    }
}

/// Stub `XCUIBridge` for unit tests. Records calls so tests can assert
/// that command handlers invoked the bridge with the expected args.
///
/// `launchApp` returns an `XCUIApplication(bundleIdentifier:)` reference
/// WITHOUT calling `.launch()`. In unit-test context, the reference is
/// a pass-through object that command handlers store in
/// `state.currentApp`; tests that invoke subsequent bridge methods
/// with that ref are fine because `MockXCUIBridge` ignores the `app`
/// parameter on every method. Tests must NOT call `.launch()` on the
/// returned ref — that would require a real simulator.
///
/// `pressKey` uses a closure-based behavior override
/// (`pressKeyBehavior`) so tests can simulate `affordanceFound=false`
/// without adding mock subclasses.
final class MockXCUIBridge: XCUIBridge {
    var tapCalls: [(CGFloat, CGFloat)] = []
    var terminateCalls: [String] = []
    var longPressCalls: [(CGFloat, CGFloat, TimeInterval)] = []
    var swipeCalls: [(CGFloat, CGFloat, CGFloat, CGFloat, TimeInterval)] = []
    var pressKeyCalls: [String] = []
    var screenshotCallCount = 0
    var typeTextCalls: [String] = []
    var launchCalls: [String] = []
    var resolveCalls: [SelectorQuery] = []

    /// Override to simulate selector resolution outcomes. Default
    /// returns `.notFound` — tests set this to an explicit
    /// `ResolvedResult` when they want a happy path.
    var resolveBehavior: (SelectorQuery) -> ResolvedResult = { _ in .notFound }

    /// Override to simulate keyboard presence / layout. Default
    /// returns `.notVisible` so tests that don't care about the
    /// keyboard can ignore this.
    var keyboardBehavior: () -> KeyboardInfoResult = { .notVisible }

    /// Override to simulate raw tree dump outcome. Default returns
    /// nil (snapshot failure) — tests set this to a preset nested
    /// dict when they want a happy path.
    var rawTreeBehavior: () -> [String: Any]? = { nil }

    /// Override to simulate screen size query. Default returns a
    /// plausible iPhone 16 Pro Max logical size.
    var screenSizeBehavior: () -> CGSize = {
        CGSize(width: 430, height: 932)
    }

    /// Override to simulate alternate affordance outcomes (e.g.
    /// edge-swipe-fallback). Default returns `affordanceFound=true`
    /// with a plausible strategy per key.
    var pressKeyBehavior: (String) -> PressKeyResult = { key in
        switch key {
        case "home":
            return PressKeyResult(affordanceFound: true, strategy: "home")
        case "enter":
            return PressKeyResult(affordanceFound: true, strategy: "enter")
        case "back":
            return PressKeyResult(affordanceFound: true, strategy: "nav_bar_back")
        default:
            return PressKeyResult(affordanceFound: true, strategy: "typed_raw")
        }
    }

    func launchApp(bundleId: String) -> XCUIApplication {
        launchCalls.append(bundleId)
        // Returns a reference WITHOUT launching. Safe for unit tests
        // that only use the ref as a pass-through to other mock methods.
        return XCUIApplication(bundleIdentifier: bundleId)
    }

    func terminateApp(bundleId: String) {
        terminateCalls.append(bundleId)
    }

    func dumpElements(app _: XCUIApplication, limit _: Int) -> DumpResult {
        DumpResult(total: 0, elements: [])
    }

    func tapAt(app _: XCUIApplication, x: CGFloat, y: CGFloat) {
        tapCalls.append((x, y))
    }

    func longPressAt(app _: XCUIApplication, x: CGFloat, y: CGFloat, durationSeconds: TimeInterval) {
        longPressCalls.append((x, y, durationSeconds))
    }

    func swipe(
        app _: XCUIApplication,
        fromX: CGFloat, fromY: CGFloat,
        toX: CGFloat, toY: CGFloat,
        durationSeconds: TimeInterval
    ) {
        swipeCalls.append((fromX, fromY, toX, toY, durationSeconds))
    }

    func pressKey(app _: XCUIApplication, key: String) -> PressKeyResult {
        pressKeyCalls.append(key)
        return pressKeyBehavior(key)
    }

    func screenshot() -> String {
        screenshotCallCount += 1
        return "ZmFrZS1wbmc=" // base64("fake-png")
    }

    func typeText(app _: XCUIApplication, text: String) {
        typeTextCalls.append(text)
    }

    func resolveSelector(app _: XCUIApplication, query: SelectorQuery) -> ResolvedResult {
        resolveCalls.append(query)
        return resolveBehavior(query)
    }

    func getKeyboard(app _: XCUIApplication) -> KeyboardInfoResult {
        return keyboardBehavior()
    }

    func dumpRawTree(app _: XCUIApplication) -> [String: Any]? {
        return rawTreeBehavior()
    }

    func getScreenSize(app _: XCUIApplication) -> CGSize {
        return screenSizeBehavior()
    }
}

/// Helper: build a DriverState with a pre-set `currentApp` reference
/// for tests that need an app without going through launchApp.
private func mockState(bundleId: String = "com.example.mock") -> DriverState {
    let state = DriverState()
    state.currentApp = XCUIApplication(bundleIdentifier: bundleId)
    state.currentBundleId = bundleId
    return state
}
