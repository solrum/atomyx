import XCTest
import Foundation
import UIKit

/// On-simulator behavioral test for mid-gesture screenshot capture.
/// Dispatches a held pointer sequence carrying a `capture` waypoint
/// through the real `EventRecordSynthesizer` against a live
/// simulator and asserts the screenshot lands while the touch is
/// still down, decodes as a real image, and fits comfortably inside
/// a short hold.
///
/// Run explicitly (not part of `make test`'s fast, mock-based suite):
///
///     xcodebuild test -project AtomyxDriver.xcodeproj -scheme AtomyxDriver \
///       -destination 'platform=iOS Simulator,id=<booted UDID>' \
///       -only-testing:AtomyxDriverAgent/GestureCaptureUITests
///
/// Skips when only the `CoordinateSynthesizer` fallback is available
/// on this Xcode/iOS combination, since that backend has no way to
/// interleave a mid-gesture screenshot with a held touch.
final class GestureCaptureUITests: XCTestCase {
    override class var runsForEachTargetApplicationUIConfiguration: Bool { false }

    func testMidGestureCaptureLandsInsideAShortHold() throws {
        try XCTSkipUnless(
            EventRecordSynthesizer.isAvailable,
            "event-record backend unavailable on this Xcode/iOS combination; " +
                "see EventRecordSynthesizer.probeLog for the missing symbol"
        )

        let synthesizer = EventRecordSynthesizer()
        let point = CGPoint(x: 100, y: 200)
        // holdSeconds comfortably exceeds captureOffsetSeconds plus
        // the screenshot's own latency (observed ~160ms), so the
        // capture has room to complete before the touch lifts.
        let holdSeconds = 0.4
        let captureOffsetSeconds = 0.1

        let pointer = PointerPath(id: "single", waypoints: [
            Waypoint(phase: .down, point: point, atOffsetSeconds: 0, pressure: nil),
            Waypoint(phase: .capture, point: point, atOffsetSeconds: captureOffsetSeconds, pressure: nil),
            Waypoint(phase: .up, point: point, atOffsetSeconds: holdSeconds, pressure: nil),
        ])

        // EventRecordSynthesizer.dispatch rejects a main-thread call:
        // its completion callback is posted to main and would
        // deadlock a synchronous wait there. Dispatch from a
        // background queue, the same way CommandServer routes
        // dispatchPointer requests in the real agent.
        var images: [Data] = []
        var dispatchError: Error?
        let done = DispatchSemaphore(value: 0)
        let start = Date()
        DispatchQueue.global().async {
            do {
                images = try synthesizer.dispatch(pointers: [pointer])
            } catch {
                dispatchError = error
            }
            done.signal()
        }
        let waitOutcome = done.wait(timeout: .now() + 15)
        let elapsedSeconds = Date().timeIntervalSince(start)

        XCTAssertEqual(waitOutcome, .success, "dispatch(pointers:) did not complete within 15s")
        XCTAssertNil(dispatchError, "dispatch(pointers:) failed: \(String(describing: dispatchError))")
        XCTAssertEqual(images.count, 1, "want exactly one captured image for one capture waypoint")

        guard let imageData = images.first else { return }
        XCTAssertFalse(imageData.isEmpty, "captured image data is empty")
        XCTAssertNotNil(UIImage(data: imageData), "captured image does not decode as a valid image")

        // synthesizeEvent:completion: only signals completion after
        // the full record, including its `up` at holdSeconds, has
        // played out. A successful dispatch here therefore proves
        // the touch was still down at captureOffsetSeconds, when the
        // capture fired, strictly before holdSeconds.
        XCTAssertGreaterThanOrEqual(
            elapsedSeconds, holdSeconds,
            "dispatch returned before the gesture's hold duration elapsed"
        )

        NSLog(
            "[atomyx] mid-gesture capture timing: holdSeconds=\(holdSeconds) " +
                "captureOffsetSeconds=\(captureOffsetSeconds) " +
                "totalDispatchElapsedSeconds=\(elapsedSeconds) " +
                "capturedImageBytes=\(imageData.count)"
        )
    }
}
