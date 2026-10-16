import Foundation

/// Rotate the device to one of the named orientations.
///
/// Request args:
///   - `orientation`: `portrait`, `landscapeLeft` or `landscapeRight`.
///     Required.
///
/// Response data:
///   - `orientation`: the name that was applied.
///
/// The change is requested from the system and recorded for the gesture
/// synthesizer, which names the interface orientation on every pointer
/// event. The reply does not wait for the interface to finish rotating, so
/// the host reads the screen size until it reflects the new orientation.
/// An app that does not support the orientation stays as it is, which the
/// host sees as a screen size that never changes.
final class SetOrientationCommand: CommandHandler {
    let type = "setOrientation"

    private let tracker: DeviceOrientationTracker

    init(tracker: DeviceOrientationTracker) {
        self.tracker = tracker
    }

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let raw = request.args["orientation"] as? String, !raw.isEmpty else {
            return .error(id: request.id, message: "missing orientation")
        }
        guard let orientation = DeviceOrientation(rawValue: raw) else {
            return .error(id: request.id, message: "unsupported orientation: \(raw)")
        }
        bridge.setDeviceOrientation(orientation)
        tracker.record(orientation)
        return .ok(id: request.id, data: ["orientation": orientation.rawValue])
    }
}
