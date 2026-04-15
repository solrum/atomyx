import Foundation

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
final class TapAtCommand: CommandHandler {
    let type = "tapAt"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let x = Self.numeric(request.args["x"]),
              let y = Self.numeric(request.args["y"]) else {
            return .error(id: request.id, message: "missing x/y (numbers)")
        }
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }
        bridge.tapAt(app: app, x: CGFloat(x), y: CGFloat(y))
        return .ok(id: request.id, data: ["x": x, "y": y])
    }

    private static func numeric(_ v: Any?) -> Double? {
        if let d = v as? Double { return d }
        if let i = v as? Int { return Double(i) }
        if let s = v as? String { return Double(s) }
        return nil
    }
}
