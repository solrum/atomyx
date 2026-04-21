import Foundation

/// Dump the accessibility tree as a nested dictionary. Preserves
/// parent → child structure so host-side consumers that need
/// structural context (ancestor-descendant walks, preceding-
/// sibling lookups, layout-aware matchers) can operate on it.
///
/// Request args: none (always operates on tracked currentApp).
///
/// Response data:
///   - `root`: nested dict with fields
///     `{elementType, identifier, label, value?, enabled, bounds, children?}`
///     — maps one-to-one onto the host-side raw-element wire type.
final class DumpRawTreeCommand: CommandHandler {
    let type = "dumpRawTree"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }

        guard let root = bridge.dumpRawTree(app: app) else {
            return .error(id: request.id, message: "snapshot failed — driver or simulator may be unresponsive")
        }

        return .ok(id: request.id, data: ["root": root])
    }
}
