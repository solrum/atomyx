import Foundation
import CoreGraphics

/// Tap at raw point coordinates, app-relative (origin = app frame
/// top-left). Requires a tracked app.
///
/// Request args: `x` and `y` as Double | Int | String (parsed).
///
/// Coordinate space is POINTS, not pixels. iPhone 16 Pro Max reports
/// 440×956 via `XCUIApplication.frame` — slightly larger than the
/// nominal 430×932 logical size, likely because the reported frame
/// includes the status bar / home indicator safe areas. Callers should
/// compute target coordinates from the element midpoints returned by
/// `dumpTree`, which share the same coordinate space.
///
/// Dispatch routes through `EventSynthesizer` rather than
/// calling `XCUICoordinate.tap()` directly. The shared waypoint
/// shape means a tap is just a single-waypoint pointer path —
/// the same backend that dispatches a pinch dispatches a tap.
final class TapAtCommand: CommandHandler {
    let type = "tapAt"

    /// Gesture dispatch posts its completion block to the main
    /// queue and waits on a semaphore. Running this on main would
    /// deadlock — so tapAt joins dispatchPointer on the background
    /// accept queue.
    let requiresMainThread = false

    private let synthesizer: EventSynthesizer

    init(synthesizer: EventSynthesizer) {
        self.synthesizer = synthesizer
    }

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        _ = bridge  // gesture dispatch routes through `synthesizer`; bridge is unused here
        guard let x = CommandArgs.numeric(request.args["x"]),
              let y = CommandArgs.numeric(request.args["y"]) else {
            return .error(id: request.id, message: "missing x/y (numbers)")
        }
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let path = Self.buildPath(x: x, y: y)
        do {
            try synthesizer.dispatch(pointers: [path], in: app)
        } catch {
            return .error(id: request.id, message: "tap dispatch failed: \(error)")
        }
        return .ok(id: request.id, data: ["x": x, "y": y])
    }

    /// Build the pointer path a tap at (x, y) emits. Exposed so
    /// unit tests can verify the waypoint shape without needing
    /// a live `XCUIApplication`.
    static func buildPath(x: Double, y: Double) -> PointerPath {
        let point = CGPoint(x: x, y: y)
        return PointerPath(
            id: "tap",
            waypoints: [
                Waypoint(phase: .down, point: point, atOffsetSeconds: 0, pressure: nil),
                Waypoint(phase: .up, point: point, atOffsetSeconds: 0, pressure: nil),
            ]
        )
    }

}
