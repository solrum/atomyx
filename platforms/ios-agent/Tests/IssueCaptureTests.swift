import XCTest

final class IssueCaptureTests: XCTestCase {
    private let request = Request(id: 7, type: "tap", args: [:])

    func testIssueOutsideCommandIsNotCaptured() {
        XCTAssertFalse(IssueCapture().offer("boom"))
    }

    func testCleanCommandKeepsItsReply() {
        let capture = IssueCapture()
        let response = capture.guarded(request) { .ok(id: 7, data: ["x": 1]) }
        XCTAssertTrue(response.ok)
        XCTAssertEqual(response.data["x"] as? Int, 1)
    }

    func testIssueDuringCommandBecomesErrorReply() {
        let capture = IssueCapture()
        let response = capture.guarded(request) {
            XCTAssertTrue(capture.offer("failed - Unable to find element"))
            return .ok(id: 7)
        }
        XCTAssertFalse(response.ok)
        XCTAssertEqual(response.id, 7)
        XCTAssertEqual(response.error, "Unable to find element")
    }

    func testCaptureEndsWithTheCommand() {
        let capture = IssueCapture()
        _ = capture.guarded(request) { .ok(id: 7) }
        XCTAssertFalse(capture.offer("late"))
    }

    func testIssueFromAnotherQueueIsCaptured() {
        let capture = IssueCapture()
        let response = capture.guarded(request) {
            let done = expectation(description: "offered")
            DispatchQueue.global().async {
                XCTAssertTrue(capture.offer("from background"))
                done.fulfill()
            }
            wait(for: [done], timeout: 5)
            return .ok(id: 7)
        }
        XCTAssertEqual(response.error, "from background")
    }

    func testSanitizeStripsInternalNamesAndAddresses() {
        let message = IssueCapture.sanitize(
            "failed - <XCUIElement: 0x6000> bad XCUIApplication state\nsecond line")
        XCTAssertEqual(message, "bad element state")
    }

    func testSanitizeBoundsLengthAndFallsBack() {
        XCTAssertEqual(IssueCapture.sanitize(String(repeating: "a", count: 500)).count, 160)
        XCTAssertEqual(IssueCapture.sanitize(""), "ui action failed")
    }
}
