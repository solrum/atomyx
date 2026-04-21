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
/// `testServeCommands` is intentionally a **blocking** test — it calls
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
/// The `Makefile` wraps both invocations as `make test` and `make serve`.
final class AtomyxDriverAgent: XCTestCase {
    private static let listenPort: UInt16 = 22087

    override class var runsForEachTargetApplicationUIConfiguration: Bool { false }

    override func setUp() {
        super.setUp()
        continueAfterFailure = true
    }

    func testServeCommands() throws {
        let bridge = DefaultXCUIBridge()
        let state = DriverState()

        // The factory probes for the multi-pointer-capable backend
        // and falls back to the coordinate backend on probe failure.
        // Backend selection is internal — no user-facing knob.
        let synthesizer = EventSynthesizerFactory.make()
        NSLog("[atomyx] gesture synthesizer mechanism = \(synthesizer.mechanismName)")

        // Press-key strategy composition root. Adding a new key =
        // drop in a new `PressKeyStrategy` file and register it here.
        let pressKeyRegistry = PressKeyRegistry(fallback: FallbackTypeStrategy())
        pressKeyRegistry.register(HomeKeyStrategy())
        pressKeyRegistry.register(EnterKeyStrategy())
        pressKeyRegistry.register(BackKeyStrategy())

        let registry = CommandRegistry()
        registry.register(PingCommand(synthesizer: synthesizer))
        registry.register(LaunchAppCommand())
        registry.register(ForceStopAppCommand())
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
            port: AtomyxDriverAgent.listenPort,
            handler: { request in
                registry.dispatch(request, bridge: bridge, state: state)
            },
            requiresMainThread: { request in
                registry.requiresMainThread(for: request.type)
            }
        )
        try server.start()
        NSLog("[atomyx] driver listening on 127.0.0.1:\(AtomyxDriverAgent.listenPort)")
        RunLoop.current.run()
    }
}
