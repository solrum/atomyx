import Foundation
import XCTest

/// Outcome of resolving the app the driver tracks from `launchApp`.
enum TrackedApp {
    /// No app was ever launched or attached.
    case none
    /// An app is tracked but its process is gone (crash, uninstall,
    /// external termination, or a stop from outside the driver).
    case notRunning
    /// The tracked app is alive, in the foreground or the background.
    /// With `requireForeground`, a backgrounded app is reported as
    /// `notRunning` instead.
    case running(XCUIApplication)
}

extension DriverState {
    /// Springboard proxy whose frame matches the full screen, used when
    /// no live app is available.
    static func springboard() -> XCUIApplication {
        XCUIApplication(bundleIdentifier: "com.apple.springboard")
    }

    /// Classify the tracked app with exactly one `state` query on the
    /// app proxy. The tracked reference is left untouched, so a later
    /// `launchApp` with `noReset` sees a dead app and launches fresh
    /// instead of attaching to it.
    ///
    /// `requireForeground` is for reads that need the app on screen,
    /// such as a hierarchy snapshot, which fails for a backgrounded
    /// process. Actions keep the default so a backgrounded app still
    /// counts as live.
    func resolveTrackedApp(bridge: XCUIBridge, requireForeground: Bool = false) -> TrackedApp {
        guard let app = currentApp else { return .none }
        switch bridge.appState(app: app) {
        case .runningForeground:
            return .running(app)
        case .runningBackground where !requireForeground:
            return .running(app)
        default:
            return .notRunning
        }
    }
}

extension CommandHandler {
    /// Guard for commands that act on the app. Returns the live app, or
    /// the error response to send back when none is available.
    func requireRunningApp(
        _ request: Request,
        bridge: XCUIBridge,
        state: DriverState
    ) -> (app: XCUIApplication?, failure: Response?) {
        switch state.resolveTrackedApp(bridge: bridge) {
        case .running(let app):
            return (app, nil)
        case .notRunning:
            return (nil, .error(id: request.id, message: "the app is not running, launch it first"))
        case .none:
            return (nil, .error(id: request.id, message: "no app launched, call launchApp first"))
        }
    }
}
