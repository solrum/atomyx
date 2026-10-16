import Foundation

/// Platform-neutral device orientation, as named on the wire.
///
/// `landscapeLeft` is the device turned a quarter turn counterclockwise,
/// which leaves the home indicator edge on the right. `landscapeRight`
/// is the device turned a quarter turn clockwise. Upside-down portrait
/// is not offered, because phones do not support it.
enum DeviceOrientation: String, CaseIterable {
    case portrait
    case landscapeLeft
    case landscapeRight

    /// Raw value of the matching `UIDeviceOrientation` case, which is
    /// what `XCUIDevice.orientation` takes.
    var deviceOrientationRaw: Int {
        switch self {
        case .portrait: return 1
        case .landscapeLeft: return 3
        case .landscapeRight: return 4
        }
    }
}

/// Maps device orientations to interface orientation codes.
///
/// `UIInterfaceOrientation` swaps its two landscape cases relative to
/// `UIDeviceOrientation`, because the interface rotates against the
/// device. The same physical state therefore has the same raw number in
/// both enumerations: device landscape-left is interface landscape-right,
/// both 3, and device landscape-right is interface landscape-left, both 4.
enum InterfaceOrientationCode {
    static let portrait = 1
    static let portraitUpsideDown = 2
    static let landscapeRight = 3
    static let landscapeLeft = 4

    /// Interface orientation code for a raw `UIDeviceOrientation` value.
    /// Face-up, face-down and unknown device orientations carry no
    /// interface information, so they map to portrait.
    static func code(forDeviceOrientationRaw raw: Int) -> Int {
        switch raw {
        case 1: return portrait
        case 2: return portraitUpsideDown
        case 3: return landscapeRight
        case 4: return landscapeLeft
        default: return portrait
        }
    }

    /// Interface orientation code for a named orientation.
    static func code(for orientation: DeviceOrientation) -> Int {
        return code(forDeviceOrientationRaw: orientation.deviceOrientationRaw)
    }
}

/// Supplies the interface orientation code for the gesture synthesizer.
protocol InterfaceOrientationSource: AnyObject {
    var interfaceOrientationCode: Int { get }

    /// Screen size in points with the shorter side as width, or nil
    /// until it has been read.
    var portraitSize: CGSize? { get }
}

/// Maps points from hierarchy space, the space element frames are read
/// in, into portrait device space, the space a synthesized pointer event
/// record interprets its coordinates in whatever orientation it names.
///
/// With `W` and `H` the portrait width and height in points:
///
/// - Portrait: a point is unchanged.
/// - Device landscape-left (interface code 3): `(x, y)` becomes `(W - y, x)`.
/// - Device landscape-right (interface code 4): `(x, y)` becomes `(y, H - x)`.
///
/// Any other code, and an unknown screen size, leave points unchanged.
enum PortraitSpace {
    static func map(
        _ point: CGPoint,
        interfaceOrientationCode code: Int,
        portraitSize size: CGSize?
    ) -> CGPoint {
        guard let size = size else { return point }
        let w = min(size.width, size.height)
        let h = max(size.width, size.height)
        switch code {
        case InterfaceOrientationCode.landscapeRight:
            return CGPoint(x: w - point.y, y: point.x)
        case InterfaceOrientationCode.landscapeLeft:
            return CGPoint(x: point.y, y: h - point.x)
        default:
            return point
        }
    }

    /// Maps every waypoint of every pointer path.
    static func map(
        _ pointers: [PointerPath],
        interfaceOrientationCode code: Int,
        portraitSize size: CGSize?
    ) -> [PointerPath] {
        return pointers.map { pointer in
            PointerPath(id: pointer.id, waypoints: pointer.waypoints.map { wp in
                Waypoint(
                    phase: wp.phase,
                    point: map(wp.point, interfaceOrientationCode: code, portraitSize: size),
                    atOffsetSeconds: wp.atOffsetSeconds,
                    pressure: wp.pressure
                )
            })
        }
    }
}

/// Holds the last known device orientation so a gesture never has to ask
/// the system for it. The first read of `XCUIDevice.orientation` takes
/// several seconds, which is longer than a gesture call may wait, so the
/// value is seeded once at startup and updated by every orientation change
/// the runner makes.
///
/// Safe to call from any thread. An orientation change made outside the
/// runner, such as turning a physical device by hand, is not observed
/// until the next change the runner makes.
final class DeviceOrientationTracker: InterfaceOrientationSource {
    private let lock = NSLock()
    private var code = InterfaceOrientationCode.portrait
    private var known = false
    private var size: CGSize?

    var interfaceOrientationCode: Int {
        lock.lock()
        defer { lock.unlock() }
        return code
    }

    var portraitSize: CGSize? {
        lock.lock()
        defer { lock.unlock() }
        return size
    }

    /// Reads the screen size on the main queue and caches it with the
    /// shorter side as width. The call returns immediately, and the read
    /// runs once the main run loop is serving.
    func seedPortraitSize(
        on queue: DispatchQueue = DispatchQueue.main,
        read: @escaping () -> CGSize
    ) {
        queue.async { [weak self] in
            let raw = read()
            guard raw.width > 0, raw.height > 0, let self = self else { return }
            self.lock.lock()
            defer { self.lock.unlock() }
            self.size = CGSize(
                width: min(raw.width, raw.height),
                height: max(raw.width, raw.height)
            )
        }
    }

    /// Records the orientation the runner just set, replacing any seed.
    func record(_ orientation: DeviceOrientation) {
        lock.lock()
        defer { lock.unlock() }
        code = InterfaceOrientationCode.code(for: orientation)
        known = true
    }

    /// Records a raw device orientation read from the system, unless an
    /// orientation change already set a newer value.
    func seedIfUnknown(deviceOrientationRaw raw: Int) {
        lock.lock()
        defer { lock.unlock() }
        guard !known else { return }
        code = InterfaceOrientationCode.code(forDeviceOrientationRaw: raw)
        known = true
    }

    /// Reads the device orientation on the main queue and seeds the tracker
    /// with it. XCUIAutomation requires the read to happen on the main
    /// thread. The call returns immediately, and the read runs once the
    /// main run loop is serving.
    func seed(
        on queue: DispatchQueue = DispatchQueue.main,
        read: @escaping () -> Int
    ) {
        queue.async { [weak self] in
            self?.seedIfUnknown(deviceOrientationRaw: read())
        }
    }
}
