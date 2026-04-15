import Foundation

/// Dump the accessibility tree as a nested dictionary. Returns
/// hierarchical `RawElement` shape (parent → children) unlike
/// `dumpTree` which returns a flat filtered list for selector
/// rendering.
///
/// Consumed by host-side `DeviceInspector.getUiTree()` which feeds
/// tool-layer strategies that need parent-child relationships —
/// notably `StructuralInputFinder`'s 4-strategy chain that walks
/// preceding-sibling / container-descendant patterns to locate
/// editable text fields by their semantic label.
///
/// Request args: none (always operates on tracked currentApp).
///
/// Response data:
///   - `root`: nested dict with fields
///     `{elementType, identifier, label, value?, enabled, bounds, children?}`
///     — mirrors `RawElement` TS type one-to-one
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
