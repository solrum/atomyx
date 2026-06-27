import Foundation
import CoreGraphics

/// Long-press at raw point coordinates for a given duration, in
/// device-global point space. The synthesizer dispatches to the
/// currently frontmost app — no `launchApp` required.
///
/// Request args:
///   - `x`, `y` (required, numeric)
///   - `durationMs` (optional, default 800): press duration.
///     Emitted as the `atOffsetSeconds` delta between `down` and
///     `up` waypoints; the coordinate backend maps this to
///     `XCUICoordinate.press(forDuration:)`.
///
/// Gesture dispatch routes through the shared `EventSynthesizer`
/// so every backend consumes the same waypoint shape.
final class LongPressAtCommand: CommandHandler {
    let type = "longPressAt"

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
        _ = state
        guard let x = CommandArgs.numeric(request.args["x"]),
              let y = CommandArgs.numeric(request.args["y"]) else {
            return .error(id: request.id, message: "missing x/y (numbers)")
        }

        let durationMs = CommandArgs.numeric(request.args["durationMs"]) ?? 800
        let path = Self.buildPath(x: x, y: y, durationMs: durationMs)
        do {
            try synthesizer.dispatch(pointers: [path])
        } catch {
            return .error(id: request.id, message: "longPress dispatch failed: \(error)")
        }
        return .ok(id: request.id, data: [
            "x": x, "y": y, "durationMs": durationMs,
        ])
    }

    /// Build the pointer path a long-press at (x, y) emits. Press
    /// duration is floored at `GESTURE_MIN_PRESS_SECONDS` so very
    /// small values still look like a press rather than a tap on
    /// the coordinate backend.
    static func buildPath(x: Double, y: Double, durationMs: Double) -> PointerPath {
        let durationSeconds = max(GESTURE_MIN_PRESS_SECONDS, durationMs / 1000)
        let point = CGPoint(x: x, y: y)
        return PointerPath(
            id: "longPress",
            waypoints: [
                Waypoint(phase: .down, point: point, atOffsetSeconds: 0, pressure: nil),
                Waypoint(phase: .up, point: point, atOffsetSeconds: durationSeconds, pressure: nil),
            ]
        )
    }

}
