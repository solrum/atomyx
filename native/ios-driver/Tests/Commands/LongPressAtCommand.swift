import Foundation

/// Long-press at raw point coordinates for a given duration.
///
/// Request args:
///   - `x`, `y` (required, numeric)
///   - `durationMs` (optional, default 800): press duration. Maps
///     directly to `XCUICoordinate.press(forDuration:)`.
///
/// Requires a tracked app (for coordinate space).
final class LongPressAtCommand: CommandHandler {
    let type = "longPressAt"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let x = Self.numeric(request.args["x"]),
              let y = Self.numeric(request.args["y"]) else {
            return .error(id: request.id, message: "missing x/y (numbers)")
        }
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        let durationMs = Self.numeric(request.args["durationMs"]) ?? 800
        let durationSeconds = max(0.05, durationMs / 1000)

        bridge.longPressAt(
            app: app,
            x: CGFloat(x),
            y: CGFloat(y),
            durationSeconds: durationSeconds
        )
        return .ok(id: request.id, data: [
            "x": x, "y": y, "durationMs": durationMs,
        ])
    }

    private static func numeric(_ v: Any?) -> Double? {
        if let d = v as? Double { return d }
        if let i = v as? Int { return Double(i) }
        if let s = v as? String { return Double(s) }
        return nil
    }
}
