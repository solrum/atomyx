import Foundation
import XCTest

/// `back` fallback chain, in order of verifiability:
///
///   1. Nav bar back button (VERIFIABLE). UIKit `UINavigationController`
///      and SwiftUI `NavigationStack` both expose a leftmost button in
///      `navigationBars` that pops the stack. `waitForExistence(0.5)`
///      gives the XCUITest daemon a bounded window to answer, with the
///      same RPC cost as `.exists` but with a timeout safety net when the
///      simulator is slow. Tapping it sets `affordanceFound=true`.
///
///   2. Top-bar back button (VERIFIABLE). Apps that draw their own app
///      bar, such as Flutter, expose no `navigationBars` element. Their
///      back control is a plain button labelled or identified "Back"
///      near the top of the window. Only matches whose frame lies in the
///      top band of the window count, so a "Back" button in page content
///      is never tapped. Tapping it sets `affordanceFound=true`.
///
///   3. Edge-swipe fallback (UNVERIFIABLE). iOS's pop gesture recognizer
///      activates only from the screen's left edge with a fast drag.
///      We CAN dispatch the gesture via normalized coordinates, but
///      cannot verify the app popped anything, because some apps intercept
///      or ignore the gesture. Reports `affordanceFound=false` so the
///      agent knows to fall back to a screen-specific "Cancel"/"Done"/
///      "X" affordance via `find_element`.
///
/// Future strategies (not this one's job): a modal dismiss button search
/// ("Cancel" / "Done" / "Close"). This would be its own `ModalDismissKey`
/// strategy alongside this one, registered under a different key name
/// so agents opt in explicitly.
final class BackKeyStrategy: PressKeyStrategy {
    private static let topBandFraction: CGFloat = 0.15
    private static let topBarExistenceTimeout: TimeInterval = 0.5

    let key = "back"
    let requiresApp = true

    func execute(key _: String, app: XCUIApplication?) -> PressKeyResult {
        let app = app!
        let navBarBackButton = app.navigationBars.buttons.element(boundBy: 0)
        if navBarBackButton.waitForExistence(timeout: 0.5) {
            navBarBackButton.tap()
            return PressKeyResult(affordanceFound: true, strategy: "nav_bar_back")
        }
        if let topBarBack = topBarBackButton(in: app) {
            topBarBack.tap()
            return PressKeyResult(affordanceFound: true, strategy: "top_bar_back")
        }
        let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.0, dy: 0.5))
        let end = app.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5))
        start.press(
            forDuration: 0.0,
            thenDragTo: end,
            withVelocity: .fast,
            thenHoldForDuration: 0.0
        )
        return PressKeyResult(affordanceFound: false, strategy: "edge_swipe_best_effort")
    }

    private func topBarBackButton(in app: XCUIApplication) -> XCUIElement? {
        let matches = app.buttons.matching(
            NSPredicate(format: "label == 'Back' OR identifier == 'Back'")
        )
        guard matches.firstMatch.waitForExistence(timeout: Self.topBarExistenceTimeout) else {
            return nil
        }
        let window = app.windows.element(boundBy: 0).frame
        let bandBottom = window.minY + window.height * Self.topBandFraction
        for index in 0..<matches.count {
            let candidate = matches.element(boundBy: index)
            let frame = candidate.frame
            if !frame.isEmpty && frame.minY >= window.minY && frame.maxY <= bandBottom {
                return candidate
            }
        }
        return nil
    }
}
