import Foundation

/// Move the device's reported position to a latitude and longitude.
///
/// Request args:
///   - `latitude`: degrees, required, from -90 to 90.
///   - `longitude`: degrees, required, from -180 to 180.
///
/// Response data:
///   - `set`: true once the position was applied.
///   - `reason`: present when `set` is false. `unsupported-os` means the
///     OS predates iOS 16.4, which is permanent for the device.
///
/// The command keeps the default threading contract and runs on the main
/// thread, because `XCUIDevice` asserts main-thread access.
final class SetLocationCommand: CommandHandler {
    let type = "setLocation"

    func handle(_ request: Request, bridge: XCUIBridge, state: DriverState) -> Response {
        guard let latitude = Self.number(request.args["latitude"]) else {
            return .error(id: request.id, message: "missing latitude")
        }
        guard let longitude = Self.number(request.args["longitude"]) else {
            return .error(id: request.id, message: "missing longitude")
        }
        guard (-90.0...90.0).contains(latitude), (-180.0...180.0).contains(longitude) else {
            return .error(id: request.id, message: "location is out of range")
        }
        switch bridge.setLocation(latitude: latitude, longitude: longitude) {
        case .unsupported:
            return .ok(id: request.id, data: ["set": false, "reason": "unsupported-os"])
        case .set:
            return .ok(id: request.id, data: ["set": true])
        }
    }

    private static func number(_ value: Any?) -> Double? {
        if let number = value as? NSNumber { return number.doubleValue }
        return value as? Double
    }
}
