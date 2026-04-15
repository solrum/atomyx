import Foundation

/// Resolve a platform-neutral selector to a concrete element. This is
/// the Batch 2 entry point for all selector-based tool actions —
/// `tap(selector)` / `inputText(selector)` on the host side compose
/// this command with `tapAt` / `typeText` to form the full operation.
///
/// Priority order (snapshot-based strategies):
///   resourceId > contentDesc > text > textContains > hint
///
/// iOS-native escape hatches:
///   - `predicate`: NSPredicate evaluated via XCUIElementQuery
///   - `classChain`: NOT supported (Appium extension); returns notFound
///
/// Request args: any subset of Selector fields plus optional `nth`.
/// Response data matches the `ResolvedElement` shape on the host port:
///   - `found`: bool. When false, no other fields are populated.
///   - `resolvedBy`: which strategy produced the match.
///   - `identifier`, `label`, `value`, `enabled`: element metadata.
///   - `x`, `y`, `w`, `h`: midpoint + dimensions in points.
final class ResolveSelectorCommand: CommandHandler {
    let type = "resolveSelector"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let app = state.currentApp else {
            return .error(id: request.id, message: "no app launched — call launchApp first")
        }
        let query = SelectorQuery.fromArgs(request.args)
        let result = bridge.resolveSelector(app: app, query: query)

        if !result.found {
            return .ok(id: request.id, data: ["found": false])
        }
        var data: [String: Any] = [
            "found": true,
            "resolvedBy": result.resolvedBy ?? "",
            "identifier": result.identifier,
            "label": result.label,
            "value": result.value ?? "",
            "enabled": result.enabled,
            "x": Int(result.frame.midX),
            "y": Int(result.frame.midY),
            "w": Int(result.frame.width),
            "h": Int(result.frame.height),
        ]
        if let obscuredBy = result.obscuredBy {
            data["obscuredBy"] = [
                "role": obscuredBy.role,
                "identifier": obscuredBy.identifier,
                "label": obscuredBy.label,
            ]
        }
        return .ok(id: request.id, data: data)
    }
}
