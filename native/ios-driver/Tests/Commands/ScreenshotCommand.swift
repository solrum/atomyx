import Foundation

/// Capture a full-screen screenshot, resized and JPEG-compressed for
/// the MCP channel. Returns the base64-encoded JPEG bytes in
/// `data.base64` along with the format marker.
///
/// Does NOT require a tracked app — `XCUIScreen.main.screenshot()` is
/// system-wide. Useful for debugging transitions where `dumpTree`
/// comes back empty.
final class ScreenshotCommand: CommandHandler {
    let type = "screenshot"

    func handle(_ request: Request, bridge: XCUIBridge, state _: DriverState) -> Response {
        let base64 = bridge.screenshot()
        return .ok(id: request.id, data: [
            "base64": base64,
            "format": "jpeg",
        ])
    }
}
