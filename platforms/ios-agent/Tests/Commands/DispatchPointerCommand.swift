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
///         { phase: "down"|"move"|"up", x: number, y: number,
///           atOffsetSeconds: number, pressure?: number }
///       ]
///     }
///   ]
/// }
/// ```
///
/// Rejection paths:
///   - Missing / malformed `pointers` → 400-style error response.
///   - Out-of-range `pressure` (must be in `[0.0, 1.0]`) →
///     400-style error response.
///   - Capability mismatch (multi-pointer or pressure on a
///     backend without the capability) → surfaced via
///     `SynthesizerError` and returned as a structured error
///     to the host so the YAML validator can map it to the
///     right POINTER_* code.
///
/// Coordinates are in device-global point space. The synthesizer
/// dispatches events through the XCUITest daemon to whichever app
/// is currently frontmost — no `launchApp` required.
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
            return .error(id: request.id, message: "pointer parse error: \(error)")
        }

        do {
            try synthesizer.dispatch(pointers: pointers)
        } catch let err as SynthesizerError {
            return .error(id: request.id, message: "dispatch rejected: \(Self.describe(err))")
        } catch {
            return .error(id: request.id, message: "dispatch failed: \(error)")
        }

        return .ok(id: request.id, data: [
            "pointers": pointers.count,
            "mechanism": synthesizer.mechanismName,
        ])
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

    private static func describe(_ err: SynthesizerError) -> String {
        switch err {
        case .patternNotExpressible(let reason):
            return "pattern not expressible: \(reason)"
        case .privateSymbolMissing(let sym):
            return "private symbol missing: \(sym)"
        case .dispatchFailed(let reason):
            return "dispatch failed: \(reason)"
        }
    }
}
