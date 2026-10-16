import XCTest

/// Unit tests for the orientation mapping that the gesture synthesizer
/// relies on, the tracker that caches it, and the command that changes it.
/// None of them needs a simulator.
final class DeviceOrientationTests: XCTestCase {

    // MARK: - Interface orientation code

    func testPortraitMapsToInterfacePortrait() {
        XCTAssertEqual(InterfaceOrientationCode.code(for: .portrait), 1)
    }

    func testDeviceLandscapeLeftMapsToInterfaceLandscapeRight() {
        XCTAssertEqual(InterfaceOrientationCode.code(for: .landscapeLeft), InterfaceOrientationCode.landscapeRight)
        XCTAssertEqual(InterfaceOrientationCode.code(for: .landscapeLeft), 3)
    }

    func testDeviceLandscapeRightMapsToInterfaceLandscapeLeft() {
        XCTAssertEqual(InterfaceOrientationCode.code(for: .landscapeRight), InterfaceOrientationCode.landscapeLeft)
        XCTAssertEqual(InterfaceOrientationCode.code(for: .landscapeRight), 4)
    }

    func testEveryNamedOrientationHasADistinctCode() {
        let codes = DeviceOrientation.allCases.map { InterfaceOrientationCode.code(for: $0) }
        XCTAssertEqual(Set(codes).count, DeviceOrientation.allCases.count)
    }

    func testRawDeviceOrientationsMapToInterfaceCodes() {
        XCTAssertEqual(InterfaceOrientationCode.code(forDeviceOrientationRaw: 1), InterfaceOrientationCode.portrait)
        XCTAssertEqual(InterfaceOrientationCode.code(forDeviceOrientationRaw: 2), InterfaceOrientationCode.portraitUpsideDown)
        XCTAssertEqual(InterfaceOrientationCode.code(forDeviceOrientationRaw: 3), InterfaceOrientationCode.landscapeRight)
        XCTAssertEqual(InterfaceOrientationCode.code(forDeviceOrientationRaw: 4), InterfaceOrientationCode.landscapeLeft)
    }

    func testFlatAndUnknownDeviceOrientationsFallBackToPortrait() {
        for raw in [0, 5, 6, 99, -1] {
            XCTAssertEqual(InterfaceOrientationCode.code(forDeviceOrientationRaw: raw), InterfaceOrientationCode.portrait, "raw \(raw)")
        }
    }

    func testNamedOrientationsUseTheDeviceRawValuesXCUIDeviceTakes() {
        XCTAssertEqual(DeviceOrientation.portrait.deviceOrientationRaw, 1)
        XCTAssertEqual(DeviceOrientation.landscapeLeft.deviceOrientationRaw, 3)
        XCTAssertEqual(DeviceOrientation.landscapeRight.deviceOrientationRaw, 4)
    }

    func testWireNamesParse() {
        XCTAssertEqual(DeviceOrientation(rawValue: "portrait"), .portrait)
        XCTAssertEqual(DeviceOrientation(rawValue: "landscapeLeft"), .landscapeLeft)
        XCTAssertEqual(DeviceOrientation(rawValue: "landscapeRight"), .landscapeRight)
        XCTAssertNil(DeviceOrientation(rawValue: "portraitUpsideDown"))
        XCTAssertNil(DeviceOrientation(rawValue: "sideways"))
    }

    // MARK: - Tracker

    func testTrackerStartsInPortrait() {
        XCTAssertEqual(DeviceOrientationTracker().interfaceOrientationCode, InterfaceOrientationCode.portrait)
    }

    func testTrackerRecordsEachOrientation() {
        let tracker = DeviceOrientationTracker()
        for orientation in DeviceOrientation.allCases {
            tracker.record(orientation)
            XCTAssertEqual(tracker.interfaceOrientationCode, InterfaceOrientationCode.code(for: orientation))
        }
    }

    func testSeedReadsTheOrientationOnTheMainThread() {
        let tracker = DeviceOrientationTracker()
        let done = expectation(description: "seed read")
        let lock = NSLock()
        var readOnMain: Bool?
        DispatchQueue.global(qos: .utility).async {
            tracker.seed {
                lock.lock()
                readOnMain = Thread.isMainThread
                lock.unlock()
                done.fulfill()
                return 4
            }
        }
        wait(for: [done], timeout: 5)
        lock.lock()
        defer { lock.unlock() }
        XCTAssertEqual(readOnMain, true)
    }

    func testSeedSetsTheCodeWhenNothingWasRecorded() {
        let tracker = DeviceOrientationTracker()
        tracker.seedIfUnknown(deviceOrientationRaw: 4)
        XCTAssertEqual(tracker.interfaceOrientationCode, InterfaceOrientationCode.landscapeLeft)
    }

    func testSeedNeverOverwritesARecordedOrientation() {
        let tracker = DeviceOrientationTracker()
        tracker.record(.landscapeLeft)
        tracker.seedIfUnknown(deviceOrientationRaw: 1)
        XCTAssertEqual(tracker.interfaceOrientationCode, InterfaceOrientationCode.landscapeRight)
    }

    func testBackgroundSeedReadsTheSystemOrientationOnTheGivenQueue() {
        let tracker = DeviceOrientationTracker()
        let queue = DispatchQueue(label: "orientation-seed-test")
        tracker.seed(on: queue) { 3 }
        queue.sync {}
        XCTAssertEqual(tracker.interfaceOrientationCode, InterfaceOrientationCode.landscapeRight)
    }

    // MARK: - SetOrientationCommand

    func testSetOrientationAppliesAndRecordsTheOrientation() {
        let tracker = DeviceOrientationTracker()
        let bridge = MockXCUIBridge()
        let cmd = SetOrientationCommand(tracker: tracker)

        let resp = cmd.handle(
            Request(id: 1, type: "setOrientation", args: ["orientation": "landscapeLeft"]),
            bridge: bridge,
            state: DriverState()
        )

        XCTAssertTrue(resp.ok)
        XCTAssertEqual(resp.data["orientation"] as? String, "landscapeLeft")
        XCTAssertEqual(bridge.setOrientationCalls, [.landscapeLeft])
        XCTAssertEqual(tracker.interfaceOrientationCode, InterfaceOrientationCode.landscapeRight)
    }

    func testSetOrientationRotatesBackToPortrait() {
        let tracker = DeviceOrientationTracker()
        let bridge = MockXCUIBridge()
        let cmd = SetOrientationCommand(tracker: tracker)

        for name in ["landscapeRight", "portrait"] {
            let resp = cmd.handle(
                Request(id: 1, type: "setOrientation", args: ["orientation": name]),
                bridge: bridge,
                state: DriverState()
            )
            XCTAssertTrue(resp.ok)
        }

        XCTAssertEqual(bridge.setOrientationCalls, [.landscapeRight, .portrait])
        XCTAssertEqual(tracker.interfaceOrientationCode, InterfaceOrientationCode.portrait)
    }

    func testSetOrientationRejectsAMissingName() {
        let bridge = MockXCUIBridge()
        let cmd = SetOrientationCommand(tracker: DeviceOrientationTracker())

        let missing = cmd.handle(Request(id: 1, type: "setOrientation", args: [:]), bridge: bridge, state: DriverState())
        let empty = cmd.handle(Request(id: 2, type: "setOrientation", args: ["orientation": ""]), bridge: bridge, state: DriverState())

        XCTAssertFalse(missing.ok)
        XCTAssertEqual(missing.error, "missing orientation")
        XCTAssertFalse(empty.ok)
        XCTAssertTrue(bridge.setOrientationCalls.isEmpty)
    }

    func testSetOrientationRejectsAnUnknownNameWithoutChangingAnything() {
        let tracker = DeviceOrientationTracker()
        let bridge = MockXCUIBridge()
        let cmd = SetOrientationCommand(tracker: tracker)

        let resp = cmd.handle(
            Request(id: 1, type: "setOrientation", args: ["orientation": "sideways"]),
            bridge: bridge,
            state: DriverState()
        )

        XCTAssertFalse(resp.ok)
        XCTAssertEqual(resp.error, "unsupported orientation: sideways")
        XCTAssertTrue(bridge.setOrientationCalls.isEmpty)
        XCTAssertEqual(tracker.interfaceOrientationCode, InterfaceOrientationCode.portrait)
    }

    // MARK: - Portrait space mapping

    private let portraitSize = CGSize(width: 440, height: 956)
    private let landscapePoint = CGPoint(x: 478, y: 178)

    func testPortraitLeavesPointsUnchanged() {
        let mapped = PortraitSpace.map(
            landscapePoint,
            interfaceOrientationCode: InterfaceOrientationCode.portrait,
            portraitSize: portraitSize
        )
        XCTAssertEqual(mapped, landscapePoint)
    }

    func testDeviceLandscapeLeftMapsIntoPortraitSpace() {
        let code = InterfaceOrientationCode.code(for: .landscapeLeft)
        let mapped = PortraitSpace.map(landscapePoint, interfaceOrientationCode: code, portraitSize: portraitSize)
        XCTAssertEqual(mapped, CGPoint(x: 262, y: 478))
    }

    func testDeviceLandscapeRightMapsIntoPortraitSpace() {
        let code = InterfaceOrientationCode.code(for: .landscapeRight)
        let mapped = PortraitSpace.map(landscapePoint, interfaceOrientationCode: code, portraitSize: portraitSize)
        XCTAssertEqual(mapped, CGPoint(x: 178, y: 478))
    }

    func testUnknownSizeLeavesPointsUnchanged() {
        let mapped = PortraitSpace.map(
            landscapePoint,
            interfaceOrientationCode: InterfaceOrientationCode.landscapeRight,
            portraitSize: nil
        )
        XCTAssertEqual(mapped, landscapePoint)
    }

    func testEveryWaypointOfEveryPointerIsMapped() {
        let pointers = [
            PointerPath(id: "a", waypoints: [
                Waypoint(phase: .down, point: CGPoint(x: 478, y: 178), atOffsetSeconds: 0, pressure: nil),
                Waypoint(phase: .up, point: CGPoint(x: 100, y: 50), atOffsetSeconds: 0.2, pressure: 0.5),
            ]),
            PointerPath(id: "b", waypoints: [
                Waypoint(phase: .down, point: CGPoint(x: 10, y: 20), atOffsetSeconds: 0, pressure: nil),
            ]),
        ]

        let mapped = PortraitSpace.map(
            pointers,
            interfaceOrientationCode: InterfaceOrientationCode.landscapeRight,
            portraitSize: portraitSize
        )

        XCTAssertEqual(mapped.map { $0.id }, ["a", "b"])
        XCTAssertEqual(mapped[0].waypoints.map { $0.point }, [CGPoint(x: 262, y: 478), CGPoint(x: 390, y: 100)])
        XCTAssertEqual(mapped[1].waypoints.map { $0.point }, [CGPoint(x: 420, y: 10)])
        XCTAssertEqual(mapped[0].waypoints[1].phase, .up)
        XCTAssertEqual(mapped[0].waypoints[1].atOffsetSeconds, 0.2)
        XCTAssertEqual(mapped[0].waypoints[1].pressure, 0.5)
    }

    func testSeedPortraitSizeCachesTheShorterSideAsWidth() {
        let tracker = DeviceOrientationTracker()
        XCTAssertNil(tracker.portraitSize)
        let queue = DispatchQueue(label: "size-seed-test")
        tracker.seedPortraitSize(on: queue) { CGSize(width: 956, height: 440) }
        queue.sync {}
        XCTAssertEqual(tracker.portraitSize, CGSize(width: 440, height: 956))
    }

    // MARK: - Synthesizer wiring

    func testEventRecordSynthesizerAcceptsAnyOrientationSource() {
        let tracker = DeviceOrientationTracker()
        tracker.record(.landscapeRight)
        let synthesizer: EventSynthesizer = EventRecordSynthesizer(orientationSource: tracker)
        XCTAssertEqual(synthesizer.mechanismName, "event-record")
    }
}
