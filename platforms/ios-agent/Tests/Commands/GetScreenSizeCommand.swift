import Foundation

/// Return the tracked app's screen frame in points.
///
/// Used by the host adapter to determine whether a resolved element
/// is inside the visible viewport before dispatching a coordinate
/// tap. iOS accessibility snapshot trees contain off-screen elements
/// (e.g. rows scrolled below the fold in a UITableView), and a raw
/// coordinate tap to an off-screen point fails silently — the tap
/// lands on empty space or outside the device bounds.
///
/// The adapter caches this result per session. Screen size is
/// stable between calls unless the device rotates; callers that
/// want rotation-safe values must invalidate the cache on the
/// rotation event before relying on subsequent reads.
///
/// Response data: `{width: Int, height: Int}` in points.
final class GetScreenSizeCommand: CommandHandler {
    let type = "getScreenSize"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }
        let size = bridge.getScreenSize(app: app)
        return .ok(id: request.id, data: [
            "width": Int(size.width),
            "height": Int(size.height),
        ])
    }
}
