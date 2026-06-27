import Foundation
import CoreGraphics
import XCTest

/// Canonical gesture-dispatch abstraction for the iOS driver. Two
/// concrete backends:
///
///   - `EventRecordSynthesizer` — XCSynthesizedEventRecord +
///     XCPointerEventPath. Full multi-pointer + pressure
///     surface. Probed at startup; selected when present.
///   - `CoordinateSynthesizer` — XCUICoordinate primitives.
///     Single-pointer fallback, used only when the event-record
///     probe fails (older Xcode, SDK drift, etc.).
///
/// The host TS compiler emits time-ordered waypoints and the
/// synthesizer maps them to the underlying primitive. The
/// waypoint shape is identical for both backends so the command
/// layer carries one wire protocol regardless of which synthesizer
/// the factory picked.

/// Capabilities the active synthesizer can dispatch. Propagated
/// back to the host via the `ping` response so the YAML validator
/// can reject unsupported sequences before they reach Swift.
struct EventCapabilities: Equatable {
    let canMultiPointer: Bool
    let canPressure: Bool
}

/// Errors surfaced by the synthesizer. Each maps to a
/// `POINTER_*` error code on the host side.
enum SynthesizerError: Error, Equatable {
    /// The sequence is syntactically valid but cannot be
    /// expressed using the primitives this backend owns
    /// (e.g. multi-pointer paths on the coordinate fallback).
    case patternNotExpressible(reason: String)
    /// An XCSynthesizedEventRecord / XCPointerEventPath symbol
    /// went missing at runtime. Only raised by
    /// `EventRecordSynthesizer`.
    case privateSymbolMissing(symbol: String)
    /// The underlying dispatch call threw.
    case dispatchFailed(reason: String)
}

/// Waypoint phase. `wait` is represented by the `atOffsetSeconds`
/// delta between adjacent waypoints, not as its own phase.
enum Phase: String, Equatable {
    case down
    case move
    case up
}

/// One waypoint in a pointer's time-ordered action list.
/// `atOffsetSeconds` is measured from the first `down` across all
/// pointers (time zero is when the first finger touches down).
struct Waypoint: Equatable {
    let phase: Phase
    let point: CGPoint
    let atOffsetSeconds: Double
    let pressure: Double?
}

/// One pointer's full waypoint list. `id` is the author-chosen
/// string from the YAML (e.g. "finger1"); used only for
/// diagnostics and error messages.
struct PointerPath: Equatable {
    let id: String
    let waypoints: [Waypoint]
}

/// Dispatches a gesture. The coordinate backend accepts single-
/// pointer paths only; the event-record backend accepts any number
/// of parallel pointers aligned on a shared clock.
///
/// Gestures are device-global: coordinates address the active
/// scene, not an owning app. Callers do NOT have to launch or
/// attach to an app before dispatching — the XCUITest daemon
/// routes the synthesized events to whichever app is currently
/// frontmost. The event-record backend bypasses XCUIApplication
/// entirely; the coordinate backend constructs a Springboard
/// handle internally so `XCUICoordinate` arithmetic resolves
/// against full-screen frame geometry.
protocol EventSynthesizer {
    var capabilities: EventCapabilities { get }

    /// Short identifier for telemetry / log correlation. Stable
    /// across releases — host-side code MUST NOT branch on it.
    var mechanismName: String { get }

    /// Optional probe log. Backends that probe runtime symbols
    /// expose the probe outcome here so contributors can see what
    /// went missing when capabilities don't match expectations.
    /// Returns `nil` when the backend does not probe.
    var probeLog: String? { get }

    /// Dispatch the gesture synchronously — returns when the
    /// underlying XCUITest call completes. Throws
    /// `SynthesizerError` on refusal or dispatch failure.
    func dispatch(pointers: [PointerPath]) throws
}
