import XCTest
import Foundation

/// Driver entry point. This XCTestCase is intentionally tiny: it wires
/// up the command registry + bridge + state, starts the TCP server,
/// and blocks the main thread so the XCUITest process stays alive
/// serving commands until `xcodebuild test` is terminated.
///
/// Unit tests for command handlers live in
/// `CommandHandlerUnitTests.swift` and use a `MockXCUIBridge` so they
/// run without touching a simulator.
///
/// `testServeCommands` is intentionally a **blocking** test. It calls
/// `RunLoop.current.run()` and never returns. Mixing it with
/// non-blocking unit tests in the same `xcodebuild test` invocation
/// would deadlock. We solve this by driving class selection at the
/// `xcodebuild -only-testing` level instead of via an env var:
///
///     # Unit tests only (fast, no simulator UI interaction)
///     xcodebuild test -only-testing:AtomyxDriverAgent/CommandHandlerUnitTests
///
///     # Serve mode (blocks)
///     xcodebuild test -only-testing:AtomyxDriverAgent/AtomyxDriverAgent/testServeCommands
///
/// The listen port defaults to 22087 and is overridden by the
/// `ATOMYX_PORT` environment variable (see `RunnerEnvironment`).
///
/// The `Makefile` wraps both invocations as `make test` and `make serve`.
final class AtomyxDriverAgent: XCTestCase {
    override class var runsForEachTargetApplicationUIConfiguration: Bool { false }

    override func setUp() {
        super.setUp()
        continueAfterFailure = true
    }

    private let issueCapture = IssueCapture()

    /// Issues recorded while a command runs become that command's error
    /// reply instead of ending the serving test. Issues recorded at any
    /// other time follow XCTest's normal handling.
    override func record(_ issue: XCTIssue) {
        if issueCapture.offer(issue.compactDescription) { return }
        super.record(issue)
    }

    func testServeCommands() throws {
        let environment = RunnerEnvironment()
        let orientation = DeviceOrientationTracker()
        let bridge = DefaultXCUIBridge(orientationSource: orientation)
        let state = DriverState()

        // The factory probes for the multi-pointer-capable backend
        // and falls back to the coordinate backend on probe failure.
        // Backend selection is internal. No user-facing knob.
        orientation.seed { bridge.deviceOrientationRaw() }
        orientation.seedPortraitSize { DriverState.springboard().frame.size }
        let synthesizer = EventSynthesizerFactory.make(orientationSource: orientation)
        NSLog("[atomyx] gesture synthesizer mechanism = \(synthesizer.mechanismName)")

        // Press-key strategy composition root. Adding a new key =
        // drop in a new `PressKeyStrategy` file and register it here.
        let pressKeyRegistry = PressKeyRegistry(fallback: FallbackTypeStrategy())
        pressKeyRegistry.register(HomeKeyStrategy())
        pressKeyRegistry.register(EnterKeyStrategy())
        pressKeyRegistry.register(BackKeyStrategy())

        let registry = CommandRegistry()
        registry.register(PingCommand(synthesizer: synthesizer, environment: environment))
        registry.register(LaunchAppCommand())
        registry.register(ForceStopAppCommand())
        registry.register(OpenLinkCommand())
        registry.register(SetLocationCommand())
        registry.register(SetOrientationCommand(tracker: orientation))
        registry.register(TapAtCommand(synthesizer: synthesizer))
        registry.register(LongPressAtCommand(synthesizer: synthesizer))
        registry.register(SwipeCommand(synthesizer: synthesizer))
        registry.register(DispatchPointerCommand(synthesizer: synthesizer))
        registry.register(PressKeyCommand(registry: pressKeyRegistry))
        registry.register(ScreenshotCommand())
        registry.register(TypeTextCommand())
        registry.register(ClearFocusedInputCommand())
        registry.register(GetKeyboardCommand())
        registry.register(HideKeyboardCommand())
        registry.register(DumpRawTreeCommand())
        registry.register(GetScreenSizeCommand())

        let server = CommandServer(
            port: environment.port,
            handler: { [issueCapture] request in
                issueCapture.guarded(request) {
                    registry.dispatch(request, bridge: bridge, state: state)
                }
            },
            requiresMainThread: { request in
                registry.requiresMainThread(for: request.type)
            }
        )
        try server.start()
        NSLog("[atomyx] driver listening on 127.0.0.1:\(environment.port)")
        RunLoop.current.run()
    }
}
