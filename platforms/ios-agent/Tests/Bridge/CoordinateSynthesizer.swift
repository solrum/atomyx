import Foundation
import CoreGraphics
import XCTest

/// One of the three gesture primitives the public path can
/// dispatch. Exposed at bundle scope so `PointerPatternClassifier`
/// can be unit-tested without an `XCUIApplication`.
enum PublicPointerPattern: Equatable {
    case tap(point: CGPoint)
    case longPress(point: CGPoint, durationSeconds: Double)
    case drag(from: CGPoint, to: CGPoint, pressSeconds: Double)
}

/// Minimum press-hold before XCUITest will accept a drag. Below
/// this the runtime can reinterpret the gesture as a tap, which
/// makes `[down, move, up]` with atOffset=0 silently misbehave.
/// Used by both the coordinate backend and the legacy
/// long-press / swipe commands that share the same XCUITest
/// floor.
let GESTURE_MIN_PRESS_SECONDS: Double = 0.05

/// Pure classifier from a pointer path to a `PublicPointerPattern`.
/// Has no XCUITest dependency — unit-tested directly.
struct PointerPatternClassifier {

    func classify(path: PointerPath) throws -> PublicPointerPattern {
        let pts = path.waypoints
        guard let first = pts.first else {
            throw SynthesizerError.patternNotExpressible(
                reason: "pointer '\(path.id)' has no waypoints"
            )
        }
        guard let last = pts.last, pts.count >= 2 else {
            throw SynthesizerError.patternNotExpressible(
                reason: "pointer '\(path.id)' needs at least `down` and `up` waypoints"
            )
        }
        guard first.phase == .down else {
            throw SynthesizerError.patternNotExpressible(
                reason: "pointer '\(path.id)' must open with a `down` waypoint"
            )
        }
        guard last.phase == .up else {
            throw SynthesizerError.patternNotExpressible(
                reason: "pointer '\(path.id)' must close with an `up` waypoint"
            )
        }
        for (i, wp) in pts.enumerated() where wp.pressure != nil {
            throw SynthesizerError.patternNotExpressible(
                reason: "pointer '\(path.id)' waypoint[\(i)] carries pressure; not supported on the public path"
            )
        }

        switch pts.count {
        case 2:
            return try classifyTwoWaypoints(down: first, up: last, pathId: path.id)

        case 3:
            let mid = pts[1]
            guard mid.phase == .move else {
                throw SynthesizerError.patternNotExpressible(
                    reason: "pointer '\(path.id)' three-waypoint sequence must be [down, move, up]"
                )
            }
            let pressSeconds = max(0.0, mid.atOffsetSeconds - first.atOffsetSeconds)
            return .drag(from: first.point, to: mid.point, pressSeconds: pressSeconds)

        default:
            throw SynthesizerError.patternNotExpressible(
                reason: "coordinate backend accepts 2–3 waypoints per pointer (got \(pts.count) on '\(path.id)'); multi-waypoint paths require a driver with multi-pointer capability"
            )
        }
    }

    private func classifyTwoWaypoints(
        down: Waypoint,
        up: Waypoint,
        pathId: String
    ) throws -> PublicPointerPattern {
        let holdSeconds = max(0.0, up.atOffsetSeconds - down.atOffsetSeconds)
        if down.point == up.point {
            if holdSeconds == 0 {
                return .tap(point: down.point)
            }
            return .longPress(point: down.point, durationSeconds: holdSeconds)
        }
        throw SynthesizerError.patternNotExpressible(
            reason: "pointer '\(pathId)' [down, up] has distinct endpoints — add an explicit `move` between them"
        )
    }
}

/// XCUICoordinate-based gesture synthesizer. Uses
/// `XCUICoordinate.tap`, `press(forDuration:)`, and
/// `press(forDuration:thenDragTo:)` and works on any Xcode / iOS
/// version that ships XCUITest. Single-pointer only — pinch,
/// rotate, and multi-finger custom gestures return
/// `.patternNotExpressible`. Atomyx falls back to this backend
/// when `EventRecordSynthesizer.isAvailable` is false.
///
/// Pattern mapping:
///
/// | Waypoints                               | Same point? | Maps to                                   |
/// |-----------------------------------------|-------------|-------------------------------------------|
/// | `[down(t=0), up(t=0)]`                  | yes         | `XCUICoordinate.tap()`                    |
/// | `[down(t=0), up(t=T)]`  with T > 0      | yes         | `press(forDuration: T)`                   |
/// | `[down(t=0), move(t=P), up(t=T)]`       | —           | `press(forDuration: P, thenDragTo: end)`  |
final class CoordinateSynthesizer: EventSynthesizer {
    let capabilities = EventCapabilities(
        canMultiPointer: false,
        canPressure: false
    )

    let mechanismName = "coordinate"
    let probeLog: String? = nil

    private let classifier = PointerPatternClassifier()

    func dispatch(pointers: [PointerPath], in app: XCUIApplication) throws {
        guard pointers.count == 1 else {
            throw SynthesizerError.patternNotExpressible(
                reason: "coordinate backend accepts exactly one pointer, got \(pointers.count)"
            )
        }
        let pattern = try classifier.classify(path: pointers[0])
        try execute(pattern: pattern, in: app)
    }

    private func execute(pattern: PublicPointerPattern, in app: XCUIApplication) throws {
        // XCUICoordinate primitives must run on the main thread.
        // Hop only when not already on main — nesting
        // `DispatchQueue.main.sync` aborts under libdispatch.
        if Thread.isMainThread {
            runOnMain(pattern: pattern, in: app)
        } else {
            DispatchQueue.main.sync { runOnMain(pattern: pattern, in: app) }
        }
    }

    private func runOnMain(pattern: PublicPointerPattern, in app: XCUIApplication) {
        switch pattern {
        case .tap(let p):
            coordinate(in: app, point: p).tap()

        case .longPress(let p, let durationSeconds):
            coordinate(in: app, point: p).press(forDuration: durationSeconds)

        case .drag(let from, let to, let pressSeconds):
            let start = coordinate(in: app, point: from)
            let end = coordinate(in: app, point: to)
            let effectivePress = max(pressSeconds, GESTURE_MIN_PRESS_SECONDS)
            start.press(forDuration: effectivePress, thenDragTo: end)
        }
    }

    private func coordinate(in app: XCUIApplication, point: CGPoint) -> XCUICoordinate {
        let origin = app.coordinate(
            withNormalizedOffset: CGVector(dx: 0, dy: 0)
        )
        return origin.withOffset(CGVector(dx: point.x, dy: point.y))
    }
}
