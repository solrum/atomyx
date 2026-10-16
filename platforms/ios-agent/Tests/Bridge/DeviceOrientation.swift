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

/// Maps device orientations to the interface orientation code that a
/// synthesized pointer event record carries.
///
/// The system interprets event coordinates in the space of the interface
/// orientation named by the record, and element frames read from the
/// accessibility tree are already in that space. A record therefore needs
/// the true interface orientation and the frame coordinates unchanged.
/// A record that names portrait while the interface is in landscape lands
/// the touch on the wrong point.
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
}

/// Holds the last known device orientation so a gesture never has to ask
/// the system for it. The first read of `XCUIDevice.orientation` takes
/// several seconds, which is longer than a gesture call may wait, so the
/// value is seeded once on a background queue at startup and updated by
/// every orientation change the runner makes.
///
/// Safe to call from any thread. An orientation change made outside the
/// runner, such as turning a physical device by hand, is not observed
/// until the next change the runner makes.
final class DeviceOrientationTracker: InterfaceOrientationSource {
    private let lock = NSLock()
    private var code = InterfaceOrientationCode.portrait
    private var known = false

    var interfaceOrientationCode: Int {
        lock.lock()
        defer { lock.unlock() }
        return code
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

    /// Reads the device orientation on a background queue and seeds the
    /// tracker with it.
    func seed(
        on queue: DispatchQueue = DispatchQueue.global(qos: .utility),
        read: @escaping () -> Int
    ) {
        queue.async { [weak self] in
            self?.seedIfUnknown(deviceOrientationRaw: read())
        }
    }
}
