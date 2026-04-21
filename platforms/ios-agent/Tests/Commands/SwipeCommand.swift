import Foundation
import CoreGraphics

/// Drag from one point to another, app-relative coordinates.
///
/// Request args:
///   - `fromX`, `fromY` (required, numeric)
///   - `toX`, `toY` (required, numeric)
///   - `durationMs` (optional, default 100): press-and-hold time
///     before the drag starts. Emitted as the `atOffsetSeconds`
///     of the `move` waypoint; the coordinate backend maps this
///     to `XCUICoordinate.press(forDuration:thenDragTo:)`. The
///     drag motion itself runs at XCUITest's internal speed —
///     not directly parameterizable via this XCUITest API.
///     Total gesture duration ≈ durationMs + drag.
///
/// Requires a tracked app (for coordinate space). Gesture
/// dispatch routes through the shared `EventSynthesizer` so
/// every backend consumes the same waypoint shape.
final class SwipeCommand: CommandHandler {
    let type = "swipe"

    /// Synthesizer dispatch needs the main queue free for its
    /// completion block; this command joins the gesture commands
    /// on the background accept queue.
    let requiresMainThread = false

    private let synthesizer: EventSynthesizer

    init(synthesizer: EventSynthesizer) {
        self.synthesizer = synthesizer
    }

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        _ = bridge
        guard let fromX = CommandArgs.numeric(request.args["fromX"]),
              let fromY = CommandArgs.numeric(request.args["fromY"]),
              let toX = CommandArgs.numeric(request.args["toX"]),
              let toY = CommandArgs.numeric(request.args["toY"]) else {
            return .error(id: request.id, message: "missing fromX/fromY/toX/toY (numbers)")
        }
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let durationMs = CommandArgs.numeric(request.args["durationMs"]) ?? 100
        let path = Self.buildPath(
            fromX: fromX, fromY: fromY, toX: toX, toY: toY, durationMs: durationMs
        )
        do {
            try synthesizer.dispatch(pointers: [path], in: app)
        } catch {
            return .error(id: request.id, message: "swipe dispatch failed: \(error)")
        }
        return .ok(id: request.id, data: [
            "fromX": fromX, "fromY": fromY,
            "toX": toX, "toY": toY,
            "durationMs": durationMs,
        ])
    }

    /// Build the pointer path a swipe emits. Press-before-drag
    /// duration is floored at `GESTURE_MIN_PRESS_SECONDS` so
    /// zero-duration requests still produce a drag (not a tap)
    /// on the coordinate backend. The same floor is enforced
    /// defensively in `CoordinateSynthesizer`; keeping both
    /// aligned means the waypoint shape matches what the
    /// classifier actually sees.
    static func buildPath(
        fromX: Double, fromY: Double, toX: Double, toY: Double, durationMs: Double
    ) -> PointerPath {
        let pressSeconds = max(GESTURE_MIN_PRESS_SECONDS, durationMs / 1000)
        let fromPoint = CGPoint(x: fromX, y: fromY)
        let toPoint = CGPoint(x: toX, y: toY)
        return PointerPath(
            id: "swipe",
            waypoints: [
                Waypoint(phase: .down, point: fromPoint, atOffsetSeconds: 0, pressure: nil),
                Waypoint(phase: .move, point: toPoint, atOffsetSeconds: pressSeconds, pressure: nil),
                Waypoint(phase: .up, point: toPoint, atOffsetSeconds: pressSeconds, pressure: nil),
            ]
        )
    }

}
