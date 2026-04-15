import Foundation

/// Drag from one point to another, app-relative coordinates.
///
/// Request args:
///   - `fromX`, `fromY` (required, numeric)
///   - `toX`, `toY` (required, numeric)
///   - `durationMs` (optional, default 100): press-and-hold time
///     before the drag starts. Maps to `XCUICoordinate.press(
///     forDuration:thenDragTo:)`. The drag motion itself runs at
///     XCUITest's internal speed — not directly parameterizable via
///     this XCUITest API. Total gesture duration ≈ durationMs + drag.
///
/// Requires a tracked app (for coordinate space).
final class SwipeCommand: CommandHandler {
    let type = "swipe"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let fromX = Self.numeric(request.args["fromX"]),
              let fromY = Self.numeric(request.args["fromY"]),
              let toX = Self.numeric(request.args["toX"]),
              let toY = Self.numeric(request.args["toY"]) else {
            return .error(id: request.id, message: "missing fromX/fromY/toX/toY (numbers)")
        }
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let durationMs = Self.numeric(request.args["durationMs"]) ?? 100
        let durationSeconds = max(0.01, durationMs / 1000)

        bridge.swipe(
            app: app,
            fromX: CGFloat(fromX),
            fromY: CGFloat(fromY),
            toX: CGFloat(toX),
            toY: CGFloat(toY),
            durationSeconds: durationSeconds
        )
        return .ok(id: request.id, data: [
            "fromX": fromX, "fromY": fromY,
            "toX": toX, "toY": toY,
            "durationMs": durationMs,
        ])
    }

    private static func numeric(_ v: Any?) -> Double? {
        if let d = v as? Double { return d }
        if let i = v as? Int { return Double(i) }
        if let s = v as? String { return Double(s) }
        return nil
    }
}
