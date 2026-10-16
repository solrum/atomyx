import Foundation

/// Open a URL, deep link or universal link through the system's default
/// handler, or in one named app.
///
/// Request args:
///   - `url`: the link to open. Required and must parse as a URL.
///   - `bundleId`: optional. When set, the link opens in that app, and
///     the driver tracks the app as the app under test so later
///     inspect and action commands run against it.
///
/// Response data:
///   - `opened`: true once the system accepted the link.
///   - `reason`: present when `opened` is false. `unsupported-os` means
///     the OS predates iOS 16.4, which is permanent for the device.
///
/// Without a `bundleId` the tracked app does not change, because the
/// handler of a link is not known to XCUITest. A caller that needs the
/// handler tracked names it.
final class OpenLinkCommand: CommandHandler {
    let type = "openLink"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let raw = request.args["url"] as? String, !raw.isEmpty else {
            return .error(id: request.id, message: "missing url")
        }
        guard let url = URL(string: raw) else {
            return .error(id: request.id, message: "url is not a valid URL")
        }
        let bundleId = (request.args["bundleId"] as? String).flatMap { $0.isEmpty ? nil : $0 }
        switch bridge.openLink(url: url, bundleId: bundleId) {
        case .unsupported:
            return .ok(id: request.id, data: ["opened": false, "reason": "unsupported-os"])
        case .opened(let app):
            if let app = app, let bundleId = bundleId {
                state.currentApp = app
                state.currentBundleId = bundleId
            }
            return .ok(id: request.id, data: ["opened": true])
        }
    }
}
