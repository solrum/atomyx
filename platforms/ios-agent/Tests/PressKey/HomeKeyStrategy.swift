import Foundation
import XCTest

/// `home` is device-wide, so no tracked app is needed. Uses
/// `XCUIDevice.shared.press(.home)` which dispatches the hardware home
/// signal to the simulator (or physical device home button / indicator
/// gesture on Face ID devices).
///
/// Always reports `affordanceFound=true`: the press itself always
/// succeeds at the device level. Whether the foreground app honored it
/// (some apps intercept) is out of scope. Agents instead observe the
/// result via follow-up inspection.
final class HomeKeyStrategy: PressKeyStrategy {
    let key = "home"
    let requiresApp = false

    func execute(key _: String, app _: XCUIApplication?) -> PressKeyResult {
        XCUIDevice.shared.press(.home)
        return PressKeyResult(affordanceFound: true, strategy: "home")
    }
}
