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
        registry.register(PingCommand(synthesizer: CoordinateSynthesizer()))

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
        XCTAssertEqual(resp.data["attached"] as? Bool, false)
    }

    func testLaunchAppNoResetSkipsLaunchWhenSameBundleAlreadyTracked() {
        let cmd = LaunchAppCommand()
        let bridge = MockXCUIBridge()
        let state = mockState(bundleId: "com.example.app")

        let req = Request(
            id: 1,
            type: "launchApp",
            args: ["bundleId": "com.example.app", "noReset": true]
        )
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.launchCalls, [])
        XCTAssertEqual(state.currentBundleId, "com.example.app")
        XCTAssertNotNil(state.currentApp)
        XCTAssertEqual(resp.data["attached"] as? Bool, true)
    }

    func testLaunchAppNoResetLaunchesWhenBundleIdDiffers() {
        let cmd = LaunchAppCommand()
        let bridge = MockXCUIBridge()
        let state = mockState(bundleId: "com.example.old")

        let req = Request(
            id: 1,
            type: "launchApp",
            args: ["bundleId": "com.example.new", "noReset": true]
        )
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.launchCalls, ["com.example.new"])
        XCTAssertEqual(state.currentBundleId, "com.example.new")
        XCTAssertEqual(resp.data["attached"] as? Bool, false)
    }

    func testLaunchAppNoResetLaunchesWhenNoCurrentApp() {
        let cmd = LaunchAppCommand()
        let bridge = MockXCUIBridge()
        let state = DriverState()

        let req = Request(
            id: 1,
            type: "launchApp",
            args: ["bundleId": "com.example.app", "noReset": true]
        )
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.launchCalls, ["com.example.app"])
        XCTAssertEqual(state.currentBundleId, "com.example.app")
        XCTAssertEqual(resp.data["attached"] as? Bool, false)
    }

    func testLaunchAppDefaultsToResetEvenWhenAlreadyTracked() {
        let cmd = LaunchAppCommand()
        let bridge = MockXCUIBridge()
        let state = mockState(bundleId: "com.example.app")

        let req = Request(id: 1, type: "launchApp", args: ["bundleId": "com.example.app"])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.launchCalls, ["com.example.app"])
        XCTAssertEqual(resp.data["attached"] as? Bool, false)
    }

    // MARK: - TapAtCommand

    func testTapAtRejectsMissingCoordinates() {
        let cmd = TapAtCommand(synthesizer: CoordinateSynthesizer())
        let req = Request(id: 1, type: "tapAt", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing x/y (numbers)")
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
        let cmd = SwipeCommand(synthesizer: CoordinateSynthesizer())
        let req = Request(id: 1, type: "swipe", args: ["fromX": 0])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("missing") == true)
    }

    // MARK: - LongPressAtCommand

    func testLongPressRejectsMissingCoordinates() {
        let cmd = LongPressAtCommand(synthesizer: CoordinateSynthesizer())
        let req = Request(id: 1, type: "longPressAt", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing x/y (numbers)")
    }

    // MARK: - PressKeyCommand

    /// Build a PressKeyCommand wired to a registry of `FakePressKeyStrategy`
    /// instances. Each test composes the registry it needs — no mocks
    /// on the bridge side (bridge.pressKey no longer exists).
    private func makePressKeyCommand(
        strategies: [FakePressKeyStrategy],
        fallback: FakePressKeyStrategy = FakePressKeyStrategy(
            key: "",
            requiresApp: true,
            result: PressKeyResult(affordanceFound: true, strategy: "typed_raw")
        )
    ) -> (PressKeyCommand, [String: FakePressKeyStrategy]) {
        let registry = PressKeyRegistry(fallback: fallback)
        var byKey: [String: FakePressKeyStrategy] = ["": fallback]
        for s in strategies {
            registry.register(s)
            byKey[s.key] = s
        }
        return (PressKeyCommand(registry: registry), byKey)
    }

    func testPressKeyRejectsMissingKey() {
        let (cmd, _) = makePressKeyCommand(strategies: [])
        let req = Request(id: 1, type: "pressKey", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "missing key")
    }

    func testPressKeyDeviceWideStrategyDoesNotRequireTrackedApp() {
        let home = FakePressKeyStrategy(
            key: "home",
            requiresApp: false,
            result: PressKeyResult(affordanceFound: true, strategy: "home")
        )
        let (cmd, byKey) = makePressKeyCommand(strategies: [home])

        let req = Request(id: 1, type: "pressKey", args: ["key": "home"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["affordanceFound"] as? Bool, true)
        XCTAssertEqual(resp.data["strategy"] as? String, "home")
        // Command invoked the strategy exactly once, with nil app.
        XCTAssertEqual(byKey["home"]?.callCount, 1)
        XCTAssertEqual(byKey["home"]?.lastAppWasNonNil, false)
    }

    func testPressKeyAppScopedStrategyRequiresTrackedApp() {
        let back = FakePressKeyStrategy(
            key: "back",
            requiresApp: true,
            result: PressKeyResult(affordanceFound: true, strategy: "nav_bar_back")
        )
        let (cmd, byKey) = makePressKeyCommand(strategies: [back])

        let req = Request(id: 1, type: "pressKey", args: ["key": "back"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
        // Strategy must NOT have been invoked when app is missing.
        XCTAssertEqual(byKey["back"]?.callCount, 0)
    }

    func testPressKeyForwardsAffordanceResultFromStrategy() {
        let back = FakePressKeyStrategy(
            key: "back",
            requiresApp: true,
            result: PressKeyResult(affordanceFound: true, strategy: "nav_bar_back")
        )
        let (cmd, byKey) = makePressKeyCommand(strategies: [back])

        let req = Request(id: 1, type: "pressKey", args: ["key": "back"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: mockState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["affordanceFound"] as? Bool, true)
        XCTAssertEqual(resp.data["strategy"] as? String, "nav_bar_back")
        XCTAssertEqual(byKey["back"]?.callCount, 1)
        XCTAssertEqual(byKey["back"]?.lastAppWasNonNil, true)
    }

    func testPressKeyForwardsAffordanceFalseForEdgeSwipeFallback() {
        // Exercises the `affordanceFound=false` wire response path.
        let back = FakePressKeyStrategy(
            key: "back",
            requiresApp: true,
            result: PressKeyResult(affordanceFound: false, strategy: "edge_swipe_best_effort")
        )
        let (cmd, _) = makePressKeyCommand(strategies: [back])

        let req = Request(id: 1, type: "pressKey", args: ["key": "back"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: mockState())

        XCTAssertTrue(resp.ok) // wire-level ok; affordance outcome is in data
        XCTAssertEqual(resp.data["affordanceFound"] as? Bool, false)
        XCTAssertEqual(resp.data["strategy"] as? String, "edge_swipe_best_effort")
    }

    func testPressKeyFallsBackToRegistryFallbackForUnknownKey() {
        let (cmd, byKey) = makePressKeyCommand(
            strategies: [],
            fallback: FakePressKeyStrategy(
                key: "",
                requiresApp: true,
                result: PressKeyResult(affordanceFound: true, strategy: "typed_raw")
            )
        )

        let req = Request(id: 1, type: "pressKey", args: ["key": "volume_up"])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: mockState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["strategy"] as? String, "typed_raw")
        // Fallback received the raw key so it could type it.
        XCTAssertEqual(byKey[""]?.lastKey, "volume_up")
    }

    // MARK: - PressKeyRegistry

    func testPressKeyRegistryResolvesRegisteredStrategy() {
        let home = FakePressKeyStrategy(
            key: "home",
            requiresApp: false,
            result: PressKeyResult(affordanceFound: true, strategy: "home")
        )
        let fallback = FakePressKeyStrategy(
            key: "",
            requiresApp: true,
            result: PressKeyResult(affordanceFound: true, strategy: "typed_raw")
        )
        let registry = PressKeyRegistry(fallback: fallback)
        registry.register(home)

        let resolved = registry.resolve("home")
        XCTAssertTrue(resolved is FakePressKeyStrategy)
        XCTAssertEqual(resolved.key, "home")
    }

    func testPressKeyRegistryRoutesUnknownKeyToFallback() {
        let fallback = FakePressKeyStrategy(
            key: "",
            requiresApp: true,
            result: PressKeyResult(affordanceFound: true, strategy: "typed_raw")
        )
        let registry = PressKeyRegistry(fallback: fallback)

        let resolved = registry.resolve("never_registered")
        XCTAssertEqual(resolved.key, "")
    }

    // MARK: - GetScreenSizeCommand

    func testGetScreenSizeFallsBackToSpringboardWithoutTrackedApp() {
        let cmd = GetScreenSizeCommand()
        let bridge = MockXCUIBridge()
        bridge.screenSizeBehavior = {
            CGSize(width: 390, height: 844)
        }
        let req = Request(id: 1, type: "getScreenSize", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: DriverState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["width"] as? Int, 390)
        XCTAssertEqual(resp.data["height"] as? Int, 844)
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

    // MARK: - ClearFocusedInputCommand

    func testClearFocusedInputRequiresTrackedApp() {
        let cmd = ClearFocusedInputCommand()
        let req = Request(id: 1, type: "clearFocusedInput", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    // MARK: - ClearFocusedInputCommand stages
    //
    // The command's two-stage clear — ⌘A + ⌫ (fast path) with an
    // exact-length delete-loop fallback — reads the focused value
    // directly from `app.snapshot()`. Our MockXCUIBridge doesn't
    // synthesize snapshots, so the mock path here always reports
    // `focusedValueLength == 0` → the command takes the
    // "already-empty" early-out. That's sufficient to verify the
    // dispatch wiring; the realistic integration test lives in the
    // host-side bench (`scripts/bench-ios-type.mjs`) which exercises
    // the full fast-path + fallback against a live simulator.

    func testClearFocusedInputShortCircuitsWhenFieldEmpty() {
        let cmd = ClearFocusedInputCommand()
        let bridge = MockXCUIBridge()
        let state = mockState()

        let req = Request(id: 1, type: "clearFocusedInput", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: state)

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["strategy"] as? String, "already-empty")
        // No keystrokes dispatched at all — no focus, no content.
        XCTAssertEqual(bridge.typeKeyCalls.count, 0)
        XCTAssertEqual(bridge.typeTextCalls.count, 0)
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
        XCTAssertTrue(resp.error?.contains("no app bound") == true)
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

    // MARK: - HideKeyboardCommand

    func testHideKeyboardRejectsWhenNoAppLaunched() {
        let cmd = HideKeyboardCommand()
        let req = Request(id: 1, type: "hideKeyboard", args: [:])
        let resp = cmd.handle(req, bridge: MockXCUIBridge(), state: DriverState())

        XCTAssertFalse(resp.ok)
        XCTAssertTrue(resp.error?.contains("no app launched") == true)
    }

    func testHideKeyboardReturnsNotVisibleWhenNoKeyboard() {
        let cmd = HideKeyboardCommand()
        let bridge = MockXCUIBridge()
        // Default hideKeyboardBehavior is .notVisible.
        let req = Request(id: 2, type: "hideKeyboard", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: mockState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(bridge.hideKeyboardCallCount, 1)
        XCTAssertEqual(resp.data["ok"] as? Bool, false)
        XCTAssertEqual(resp.data["strategy"] as? String, "not-visible")
    }

    func testHideKeyboardReportsBridgeStrategy() {
        let cmd = HideKeyboardCommand()
        let bridge = MockXCUIBridge()
        bridge.hideKeyboardBehavior = {
            HideKeyboardResult(ok: true, strategy: "dismiss-affordance:Done")
        }
        let req = Request(id: 3, type: "hideKeyboard", args: [:])
        let resp = cmd.handle(req, bridge: bridge, state: mockState())

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["ok"] as? Bool, true)
        XCTAssertEqual(resp.data["strategy"] as? String, "dismiss-affordance:Done")
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
        // ScreenshotCommand returns JPEG (resize + compress); mock
        // just round-trips a stub base64 string but the format
        // constant is fixed at "jpeg".
        XCTAssertEqual(resp.data["format"] as? String, "jpeg")
    }

    // MARK: - Wire protocol

    func testRequestDecodeRoundTrip() {
        let json = "{\"id\":7,\"type\":\"dumpRawTree\",\"args\":{\"limit\":50}}"
        let req = Request.decode(json)

        XCTAssertNotNil(req)
        XCTAssertEqual(req?.id, 7)
        XCTAssertEqual(req?.type, "dumpRawTree")
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

    // MARK: - PointerPatternClassifier

    private func wp(_ phase: Phase, _ x: CGFloat, _ y: CGFloat, _ t: Double) -> Waypoint {
        return Waypoint(
            phase: phase,
            point: CGPoint(x: x, y: y),
            atOffsetSeconds: t,
            pressure: nil
        )
    }

    private func path(_ waypoints: [Waypoint]) -> PointerPath {
        return PointerPath(id: "f1", waypoints: waypoints)
    }

    func testClassifyTapAtSamePointZeroDuration() throws {
        let p = path([wp(.down, 100, 200, 0), wp(.up, 100, 200, 0)])
        let result = try PointerPatternClassifier().classify(path: p)
        XCTAssertEqual(result, .tap(point: CGPoint(x: 100, y: 200)))
    }

    func testClassifyLongPressWhenDurationNonZero() throws {
        let p = path([wp(.down, 50, 60, 0), wp(.up, 50, 60, 0.75)])
        let result = try PointerPatternClassifier().classify(path: p)
        XCTAssertEqual(
            result,
            .longPress(point: CGPoint(x: 50, y: 60), durationSeconds: 0.75)
        )
    }

    func testClassifyDragFromDownMoveUp() throws {
        let p = path([
            wp(.down, 10, 20, 0),
            wp(.move, 30, 40, 0.2),
            wp(.up, 30, 40, 0.2),
        ])
        let result = try PointerPatternClassifier().classify(path: p)
        XCTAssertEqual(
            result,
            .drag(
                from: CGPoint(x: 10, y: 20),
                to: CGPoint(x: 30, y: 40),
                pressSeconds: 0.2
            )
        )
    }

    func testClassifyLongPressPlusDrag() throws {
        let p = path([
            wp(.down, 10, 20, 0),
            wp(.move, 300, 600, 0.8),
            wp(.up, 300, 600, 0.8),
        ])
        let result = try PointerPatternClassifier().classify(path: p)
        XCTAssertEqual(
            result,
            .drag(
                from: CGPoint(x: 10, y: 20),
                to: CGPoint(x: 300, y: 600),
                pressSeconds: 0.8
            )
        )
    }

    func testClassifyRejectsEmptyWaypoints() {
        let p = path([])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible(let reason) = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
            XCTAssertTrue(reason.contains("no waypoints"))
        }
    }

    func testClassifyRejectsSingleWaypoint() {
        let p = path([wp(.down, 0, 0, 0)])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
        }
    }

    func testClassifyRejectsNotOpeningWithDown() {
        let p = path([wp(.move, 0, 0, 0), wp(.up, 10, 10, 0)])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible(let reason) = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
            XCTAssertTrue(reason.contains("must open with a `down`"))
        }
    }

    func testClassifyRejectsNotClosingWithUp() {
        let p = path([wp(.down, 0, 0, 0), wp(.move, 10, 10, 0)])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible(let reason) = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
            XCTAssertTrue(reason.contains("must close with an `up`"))
        }
    }

    func testClassifyRejectsPressure() {
        let p = path([
            Waypoint(phase: .down, point: .zero, atOffsetSeconds: 0, pressure: 0.5),
            wp(.up, 0, 0, 0),
        ])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible(let reason) = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
            XCTAssertTrue(reason.contains("pressure"))
        }
    }

    func testClassifyRejectsTwoWaypointsAtDistinctPoints() {
        let p = path([wp(.down, 0, 0, 0), wp(.up, 10, 10, 0.1)])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible(let reason) = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
            XCTAssertTrue(reason.contains("distinct endpoints"))
        }
    }

    func testClassifyRejectsThreeWaypointsWithoutMove() {
        let p = path([
            wp(.down, 0, 0, 0),
            wp(.down, 0, 0, 0.1),
            wp(.up, 0, 0, 0.2),
        ])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible(let reason) = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
            XCTAssertTrue(reason.contains("[down, move, up]"))
        }
    }

    func testClassifyRejectsFourOrMoreWaypoints() {
        let p = path([
            wp(.down, 0, 0, 0),
            wp(.move, 10, 10, 0.1),
            wp(.move, 20, 20, 0.2),
            wp(.up, 20, 20, 0.2),
        ])
        XCTAssertThrowsError(try PointerPatternClassifier().classify(path: p)) { err in
            guard case SynthesizerError.patternNotExpressible(let reason) = err else {
                return XCTFail("expected patternNotExpressible, got \(err)")
            }
            XCTAssertTrue(reason.contains("2–3 waypoints"))
        }
    }

    func testCoordinateBackendAdvertisesNoExtendedCapabilities() {
        let caps = CoordinateSynthesizer().capabilities
        XCTAssertFalse(caps.canMultiPointer)
        XCTAssertFalse(caps.canPressure)
    }

    // MARK: - Command → PointerPath shape

    func testTapAtBuildsTwoWaypointPathAtSamePoint() {
        let path = TapAtCommand.buildPath(x: 100, y: 200)
        XCTAssertEqual(path.id, "tap")
        XCTAssertEqual(path.waypoints.count, 2)
        XCTAssertEqual(path.waypoints[0].phase, .down)
        XCTAssertEqual(path.waypoints[1].phase, .up)
        XCTAssertEqual(path.waypoints[0].point, CGPoint(x: 100, y: 200))
        XCTAssertEqual(path.waypoints[1].point, CGPoint(x: 100, y: 200))
        XCTAssertEqual(path.waypoints[0].atOffsetSeconds, 0)
        XCTAssertEqual(path.waypoints[1].atOffsetSeconds, 0)
    }

    func testLongPressBuildsPathWithDurationOffsetOnUp() {
        let path = LongPressAtCommand.buildPath(x: 50, y: 60, durationMs: 750)
        XCTAssertEqual(path.waypoints[0].phase, .down)
        XCTAssertEqual(path.waypoints[0].atOffsetSeconds, 0)
        XCTAssertEqual(path.waypoints[1].phase, .up)
        XCTAssertEqual(path.waypoints[1].atOffsetSeconds, 0.75)
    }

    func testLongPressEnforcesMinimumPress() {
        let path = LongPressAtCommand.buildPath(x: 0, y: 0, durationMs: 10)
        // 10ms → 0.01s but floor is 0.05s
        XCTAssertEqual(path.waypoints[1].atOffsetSeconds, 0.05)
    }

    func testSwipeBuildsThreeWaypointPathWithPressOffset() {
        let path = SwipeCommand.buildPath(
            fromX: 10, fromY: 20, toX: 300, toY: 600, durationMs: 800
        )
        XCTAssertEqual(path.id, "swipe")
        XCTAssertEqual(path.waypoints.count, 3)
        XCTAssertEqual(path.waypoints[0].phase, .down)
        XCTAssertEqual(path.waypoints[0].point, CGPoint(x: 10, y: 20))
        XCTAssertEqual(path.waypoints[0].atOffsetSeconds, 0)
        XCTAssertEqual(path.waypoints[1].phase, .move)
        XCTAssertEqual(path.waypoints[1].point, CGPoint(x: 300, y: 600))
        XCTAssertEqual(path.waypoints[1].atOffsetSeconds, 0.8)
        XCTAssertEqual(path.waypoints[2].phase, .up)
        XCTAssertEqual(path.waypoints[2].point, CGPoint(x: 300, y: 600))
        XCTAssertEqual(path.waypoints[2].atOffsetSeconds, 0.8)
    }

    func testSwipeEnforcesMinimumPress() {
        let path = SwipeCommand.buildPath(
            fromX: 0, fromY: 0, toX: 10, toY: 10, durationMs: 0
        )
        // 0ms → 0s but floor is 0.05s (matches CoordinateSynthesizer)
        XCTAssertEqual(path.waypoints[1].atOffsetSeconds, 0.05)
    }

    // MARK: - EventRecordSynthesizer (M2.3)

    func testEventRecordBackendAdvertisesFullCapabilities() {
        // Capability shape is a static property of the type —
        // does not exercise the probe. Verifies the backend
        // type-checks against the protocol and advertises the
        // full W3C surface.
        let caps = EventRecordSynthesizer().capabilities
        XCTAssertTrue(caps.canMultiPointer)
        XCTAssertTrue(caps.canPressure)
    }

    func testEventRecordBackendProbeWritesDiagnosticLog() {
        // Touch `isAvailable` to force the one-shot probe, then
        // verify the diagnostic log captured something. We do
        // NOT assert the probe result — unit tests may run in a
        // context where the XCUITest daemon can't dispatch
        // synthesized events; that's the point of having a
        // fallback path via `CoordinateSynthesizer`.
        _ = EventRecordSynthesizer.isAvailable
        XCTAssertFalse(
            EventRecordSynthesizer.lastProbeLog.isEmpty,
            "probe should have recorded at least one diagnostic line"
        )
    }

    // MARK: - AtomyxBlockHelper (ObjC bridge for synthesizeEvent)

    /// The bridge exists because the XCTest daemon's XPC reply
    /// invokes our completion block with sentinel pointer 0x1
    /// instead of nil/NSError*. A regression in the helper —
    /// removing `__unsafe_unretained` from the parameter, or
    /// reintroducing a Swift closure capture — re-crashes the
    /// agent. This test exercises the happy path so any change
    /// to the helper's signature breaks the build deterministically
    /// instead of waiting for a sim run.
    func testBlockHelperSignalsSemaphoreOnInvoke() {
        let semaphore = DispatchSemaphore(value: 0)
        let block = atomyxMakeSemaphoreSignalingBlock(semaphore)

        // Cast to the canonical ObjC block signature the daemon
        // invokes. Calling with `nil` exercises the branch that
        // previously crashed under Swift retain semantics.
        typealias CompletionBlock = @convention(block) (AnyObject?) -> Void
        let invoke = unsafeBitCast(block, to: CompletionBlock.self)
        invoke(nil)

        let outcome = semaphore.wait(timeout: .now() + 1)
        XCTAssertEqual(outcome, .success, "block must signal semaphore on invoke")
    }

    /// Invoking with a non-nil sentinel-shaped pointer still must
    /// not retain the argument. This test would crash under a
    /// regression that drops `__unsafe_unretained`.
    func testBlockHelperToleratesArbitraryArgPointer() {
        let semaphore = DispatchSemaphore(value: 0)
        let block = atomyxMakeSemaphoreSignalingBlock(semaphore)

        typealias CompletionBlock = @convention(block) (UnsafeRawPointer?) -> Void
        let invoke = unsafeBitCast(block, to: CompletionBlock.self)
        // Sentinel value the daemon was observed to send — not a
        // real ObjC object. The helper MUST NOT retain it.
        invoke(UnsafeRawPointer(bitPattern: 0x1))

        XCTAssertEqual(semaphore.wait(timeout: .now() + 1), .success)
    }

    // MARK: - EventSynthesizerFactory

    func testFactoryReturnsConcreteSynthesizer() {
        // `make()` is internal — no env var, no overrides. It
        // returns either backend depending on the runtime probe;
        // both implement EventSynthesizer.
        let syn = EventSynthesizerFactory.make()
        XCTAssertTrue(syn is CoordinateSynthesizer || syn is EventRecordSynthesizer)
    }

    func testBackendsExposeMechanismNameViaProtocol() {
        XCTAssertEqual(
            (CoordinateSynthesizer() as EventSynthesizer).mechanismName,
            "coordinate"
        )
        XCTAssertEqual(
            (EventRecordSynthesizer() as EventSynthesizer).mechanismName,
            "event-record"
        )
    }

    func testCoordinateBackendReportsNoProbeLog() {
        let syn: EventSynthesizer = CoordinateSynthesizer()
        XCTAssertNil(syn.probeLog)
    }

    func testEventRecordBackendExposesProbeLogViaProtocol() {
        let syn: EventSynthesizer = EventRecordSynthesizer()
        // The probe runs once per process. After the first
        // .isAvailable touch the log is populated for diagnostic
        // purposes; nil only if the probe code path was never hit.
        _ = EventRecordSynthesizer.isAvailable
        XCTAssertNotNil(syn.probeLog)
    }

    func testBuiltPathsClassifyOnCoordinateBackend() throws {
        // Round-trip: the shapes the 3 commands emit must all
        // classify cleanly through `PointerPatternClassifier` into
        // the expected `PublicPointerPattern`. Guards against the
        // commands and the classifier drifting apart.
        let classifier = PointerPatternClassifier()

        let tapPattern = try classifier.classify(
            path: TapAtCommand.buildPath(x: 100, y: 200)
        )
        XCTAssertEqual(tapPattern, .tap(point: CGPoint(x: 100, y: 200)))

        let longPressPattern = try classifier.classify(
            path: LongPressAtCommand.buildPath(x: 50, y: 60, durationMs: 500)
        )
        XCTAssertEqual(
            longPressPattern,
            .longPress(point: CGPoint(x: 50, y: 60), durationSeconds: 0.5)
        )

        let swipePattern = try classifier.classify(
            path: SwipeCommand.buildPath(
                fromX: 10, fromY: 20, toX: 300, toY: 600, durationMs: 800
            )
        )
        XCTAssertEqual(
            swipePattern,
            .drag(
                from: CGPoint(x: 10, y: 20),
                to: CGPoint(x: 300, y: 600),
                pressSeconds: 0.8
            )
        )
    }
}

/// Mock `XCUIBridge` for unit tests — records every call so tests
/// can assert command handlers invoked the bridge with the expected
/// args. Behavior of individual methods is configurable via the
/// `*Behavior` closures below.
///
/// `launchApp` returns an `XCUIApplication(bundleIdentifier:)` reference
/// WITHOUT calling `.launch()`. In unit-test context, the reference is
/// a pass-through object that command handlers store in
/// `state.currentApp`; tests that invoke subsequent bridge methods
/// with that ref are fine because `MockXCUIBridge` ignores the `app`
/// parameter on every method. Tests must NOT call `.launch()` on the
/// returned ref — that would require a real simulator.
final class MockXCUIBridge: XCUIBridge {
    var tapCalls: [(CGFloat, CGFloat)] = []
    var terminateCalls: [String] = []
    var longPressCalls: [(CGFloat, CGFloat, TimeInterval)] = []
    var swipeCalls: [(CGFloat, CGFloat, CGFloat, CGFloat, TimeInterval)] = []
    var screenshotCallCount = 0
    var typeTextCalls: [String] = []
    var launchCalls: [String] = []

    /// Override to simulate keyboard presence / layout. Default
    /// returns `.notVisible` so tests that don't care about the
    /// keyboard can ignore this.
    var keyboardBehavior: () -> KeyboardInfoResult = { .notVisible }

    /// Override to simulate `hideKeyboard` outcome. Default is
    /// notVisible — matches the common case where no keyboard is up.
    var hideKeyboardBehavior: () -> HideKeyboardResult = { .notVisible }
    var hideKeyboardCallCount = 0

    /// Override to simulate raw tree dump outcome. Default returns
    /// nil (snapshot failure) — tests set this to a preset nested
    /// dict when they want a happy path.
    var rawTreeBehavior: () -> [String: Any]? = { nil }

    /// Override to simulate screen size query. Default returns a
    /// plausible iPhone 16 Pro Max logical size.
    var screenSizeBehavior: () -> CGSize = {
        CGSize(width: 430, height: 932)
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

    func screenshot() -> String {
        screenshotCallCount += 1
        return "ZmFrZS1wbmc=" // base64("fake-png")
    }

    func typeText(app _: XCUIApplication, text: String) {
        typeTextCalls.append(text)
    }

    /// Records the `(key, modifiers)` sent to typeKey so tests can
    /// assert on the ⌘A + ⌫ clear sequence without needing a live
    /// simulator hardware keyboard.
    var typeKeyCalls: [(key: String, modifiers: XCUIElement.KeyModifierFlags)] = []
    func typeKey(app _: XCUIApplication, key: String, modifiers: XCUIElement.KeyModifierFlags) -> Bool {
        typeKeyCalls.append((key: key, modifiers: modifiers))
        return true
    }

    func getKeyboard(app _: XCUIApplication) -> KeyboardInfoResult {
        return keyboardBehavior()
    }

    func hideKeyboard(app _: XCUIApplication) -> HideKeyboardResult {
        hideKeyboardCallCount += 1
        return hideKeyboardBehavior()
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

/// Test double for `PressKeyStrategy`. Records call count, last `key`
/// passed, and whether the `app` parameter was non-nil — enough for
/// command-level tests to prove routing and guard behavior without
/// touching XCUI primitives. Returns a canned `PressKeyResult`.
final class FakePressKeyStrategy: PressKeyStrategy {
    let key: String
    let requiresApp: Bool
    private let result: PressKeyResult

    private(set) var callCount = 0
    private(set) var lastKey: String?
    private(set) var lastAppWasNonNil: Bool = false

    init(key: String, requiresApp: Bool, result: PressKeyResult) {
        self.key = key
        self.requiresApp = requiresApp
        self.result = result
    }

    func execute(key: String, app: XCUIApplication?) -> PressKeyResult {
        callCount += 1
        lastKey = key
        lastAppWasNonNil = app != nil
        return result
    }
}
