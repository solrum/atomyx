import Foundation
import CoreGraphics

/// Dispatch an arbitrary W3C-Actions gesture. Accepts the raw
/// waypoint list the host TypeScript `pointer` compiler emits.
/// All `EventSynthesizer` backends consume the same waypoint
/// shape so the wire protocol does not depend on which backend
/// the factory picked.
///
/// Request args:
/// ```
/// {
///   pointers: [
///     {
///       id: string,
///       waypoints: [
///         { phase: "down"|"move"|"up"|"capture", x: number, y: number,
///           atOffsetSeconds: number, pressure?: number }
///       ]
///     }
///   ]
/// }
/// ```
///
/// Response data adds `capturedImages: [{ base64: string, mimeType:
/// string }]` when the dispatched pointers carried at least one
/// `capture` waypoint, one entry per waypoint in dispatch order. A
/// sequence with no `capture` waypoint omits the field entirely.
///
/// Rejection paths:
///   - Missing / malformed `pointers` results in a 400-style error response.
///   - Out-of-range `pressure` (must be in `[0.0, 1.0]`)
///     results in a 400-style error response.
///   - Capability mismatch (multi-pointer or pressure on a
///     backend without the capability) is surfaced via
///     `SynthesizerError` and returned as a structured error
///     to the host so the YAML validator can map it to the
///     right POINTER_* code.
///
/// Coordinates are in device-global point space. The synthesizer
/// dispatches events through the XCUITest daemon to whichever app
/// is currently frontmost, so no `launchApp` is required.
final class DispatchPointerCommand: CommandHandler {
    let type = "dispatchPointer"

    /// Must run on a background queue so the main queue stays
    /// free to execute `synthesizeEvent:completion:`'s
    /// completion block. Running on main would deadlock.
    let requiresMainThread = false

    private let synthesizer: EventSynthesizer

    init(synthesizer: EventSynthesizer) {
        self.synthesizer = synthesizer
    }

    func handle(_ request: Request, bridge _: XCUIBridge, state _: DriverState) -> Response {
        guard let pointersRaw = request.args["pointers"] as? [[String: Any]], !pointersRaw.isEmpty else {
            return .error(id: request.id, message: "missing or empty `pointers` array")
        }

        let pointers: [PointerPath]
        do {
            pointers = try pointersRaw.map { try Self.parsePointer($0) }
        } catch let err as DispatchPointerCommand.ParseError {
            return .error(id: request.id, message: err.description)
        } catch {
            NSLog("[atomyx] dispatchPointer parse failed: \(error)")
            return .error(id: request.id, message: "The pointer description could not be parsed.")
        }

        let capturedImages: [Data]
        do {
            capturedImages = try synthesizer.dispatch(pointers: pointers)
        } catch let err as SynthesizerError {
            NSLog("[atomyx] dispatchPointer dispatch rejected: \(err)")
            return .error(id: request.id, message: Self.describe(err))
        } catch {
            NSLog("[atomyx] dispatchPointer dispatch failed: \(error)")
            return .error(id: request.id, message: "The gesture could not be dispatched on this device.")
        }

        var data: [String: Any] = [
            "pointers": pointers.count,
            "mechanism": synthesizer.mechanismName,
        ]
        if !capturedImages.isEmpty {
            data["capturedImages"] = capturedImages.map { image in
                ["base64": image.base64EncodedString(), "mimeType": "image/png"]
            }
        }
        return .ok(id: request.id, data: data)
    }

    // MARK: - Parsing

    private enum ParseError: Error, CustomStringConvertible {
        case missingField(String)
        case invalidPhase(String)
        case pressureOutOfRange(Double)

        var description: String {
            switch self {
            case .missingField(let name):
                return "missing field: \(name)"
            case .invalidPhase(let raw):
                return "invalid phase \"\(raw)\" (expected down/move/up)"
            case .pressureOutOfRange(let value):
                return "pressure \(value) is outside the valid range [0.0, 1.0]"
            }
        }
    }

    private static func parsePointer(_ dict: [String: Any]) throws -> PointerPath {
        let id = dict["id"] as? String ?? "pointer"
        guard let waypointsRaw = dict["waypoints"] as? [[String: Any]] else {
            throw ParseError.missingField("pointers[].waypoints")
        }
        let waypoints = try waypointsRaw.map { try parseWaypoint($0) }
        return PointerPath(id: id, waypoints: waypoints)
    }

    private static func parseWaypoint(_ dict: [String: Any]) throws -> Waypoint {
        guard let phaseRaw = dict["phase"] as? String,
              let phase = Phase(rawValue: phaseRaw) else {
            let raw = (dict["phase"] as? String) ?? "<missing>"
            throw ParseError.invalidPhase(raw)
        }
        guard let x = CommandArgs.numeric(dict["x"]) else { throw ParseError.missingField("waypoint.x") }
        guard let y = CommandArgs.numeric(dict["y"]) else { throw ParseError.missingField("waypoint.y") }
        guard let offset = CommandArgs.numeric(dict["atOffsetSeconds"]) else {
            throw ParseError.missingField("waypoint.atOffsetSeconds")
        }
        let pressure = CommandArgs.numeric(dict["pressure"])
        if let p = pressure, !(0.0...1.0).contains(p) {
            throw ParseError.pressureOutOfRange(p)
        }
        return Waypoint(
            phase: phase,
            point: CGPoint(x: x, y: y),
            atOffsetSeconds: offset,
            pressure: pressure
        )
    }

    /// Maps a synthesizer failure to a fixed, human-readable message
    /// safe to return to a tool caller. A private XCTest symbol name
    /// or an internal dispatch reason must never cross the tool
    /// boundary, so only `patternNotExpressible` forwards its detail,
    /// because that detail is a field-level validation message this
    /// file writes itself, never a reflection of a Swift type or an
    /// Apple private symbol. The call site above already logs the
    /// full error for every case.
    private static func describe(_ err: SynthesizerError) -> String {
        switch err {
        case .patternNotExpressible(let reason):
            return "The pointer description is invalid: \(reason)"
        case .privateSymbolMissing, .dispatchFailed:
            return "The gesture could not be dispatched on this device."
        }
    }
}
