import Foundation

/// Picks the gesture synthesizer Atomyx uses on this process.
/// Probes for the multi-pointer-capable backend at startup; falls
/// back to the single-pointer XCUICoordinate backend only when the
/// probe fails (older Xcode, SDK drift, runtime symbol removal).
/// Selection is fully internal — callers do not choose, do not
/// override, and do not see backend names in the wire surface.
///
/// Adding a new backend: implement `EventSynthesizer` (with its
/// own `mechanismName`), give it an `isAvailable` probe, and add
/// one line to `make()` ahead of the existing fallback chain.
/// Mechanism naming is owned by the backend itself — this factory
/// does not branch on concrete types.
enum EventSynthesizerFactory {
    static func make() -> EventSynthesizer {
        if EventRecordSynthesizer.isAvailable {
            return EventRecordSynthesizer()
        }
        return CoordinateSynthesizer()
    }
}
