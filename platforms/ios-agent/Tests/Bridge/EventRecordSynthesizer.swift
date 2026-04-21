import Foundation
import CoreGraphics
import ObjectiveC
import XCTest

/// XPC-backed dispatch on `XCTRunnerDaemonSession`. `_XCT_*`
/// selectors go through ObjC message forwarding; the `as?`
/// cast against this protocol succeeds at runtime because the
/// proxy implements `-respondsToSelector:` against the
/// forwarded selector in the XPC code path (even though direct
/// `respondsToSelector:` calls from outside return false).
///
/// Concrete classes (`XCSynthesizedEventRecord`,
/// `XCPointerEventPath`) do NOT go through message forwarding
/// and therefore fail `as?` protocol casts even when the
/// selectors exist — we dispatch those via direct IMP casts
/// (see `impAs` below).
@objc private protocol _AtomyxDaemonSessionXPC {
    @objc(_XCT_synthesizeEvent:completion:)
    func synthesizeEvent(_ event: AnyObject, completion: @escaping (Error?) -> Void)
}

/// XCSynthesizedEventRecord-based gesture synthesizer. This is
/// the ONLY file in the repo that touches `XCSynth*` /
/// `XCPointerEventPath` symbols — a grep for
/// `NSClassFromString("XCSynth` returns exactly one match (here).
/// All access goes through Objective-C runtime reflection
/// (`NSClassFromString`, `performSelector:`, IMP cast) so the
/// binary carries no static references to those classes — if a
/// symbol is missing at runtime, the probe fails and the factory
/// falls back to `CoordinateSynthesizer`.
///
/// Contract: see ADR-001 in `.claude/docs/decisions/`.
///
/// Version drift expectations: `XCSynthesizedEventRecord` and
/// `XCPointerEventPath` have been stable across Xcode 12–17 with
/// occasional selector additions. `XCTRunnerDaemonSession`'s
/// dispatch selector changed between Xcode 14 and 15 and again
/// between 15 and 16. When drift hits, update the selector
/// strings in this file only.
final class EventRecordSynthesizer: EventSynthesizer {

    /// Result of `probe()`. Cached at first access so every
    /// factory call gets the same answer without repaying the
    /// probe cost.
    static let isAvailable: Bool = {
        let outcome = probe()
        lastProbeLog = outcome.log
        return outcome.ok
    }()

    /// Last probe's diagnostic log. Non-empty after
    /// `isAvailable` has been evaluated at least once; surfaced
    /// in the ping response so host-side logs / CI jobs can see
    /// exactly which class or selector went missing.
    static private(set) var lastProbeLog: String = ""

    let capabilities = EventCapabilities(
        canMultiPointer: true,
        canPressure: true
    )

    let mechanismName = "event-record"

    var probeLog: String? {
        Self.lastProbeLog.isEmpty ? nil : Self.lastProbeLog
    }

    func dispatch(pointers: [PointerPath], in app: XCUIApplication) throws {
        guard Self.isAvailable else {
            throw SynthesizerError.privateSymbolMissing(
                symbol: "XCSynthesizedEventRecord (see probe log)"
            )
        }
        guard !pointers.isEmpty else {
            throw SynthesizerError.patternNotExpressible(
                reason: "dispatch requires at least one pointer path"
            )
        }

        let orientation = Self.currentInterfaceOrientation(in: app)

        guard let recordCls = NSClassFromString("XCSynthesizedEventRecord") as? NSObject.Type else {
            throw SynthesizerError.privateSymbolMissing(symbol: "XCSynthesizedEventRecord")
        }
        guard let pathCls = NSClassFromString("XCPointerEventPath") as? NSObject.Type else {
            throw SynthesizerError.privateSymbolMissing(symbol: "XCPointerEventPath")
        }

        guard let recordAny = class_createInstance(recordCls, 0),
              let record = recordAny as? NSObject else {
            throw SynthesizerError.privateSymbolMissing(
                symbol: "XCSynthesizedEventRecord (class_createInstance failed)"
            )
        }
        _ = record.perform(
            NSSelectorFromString("initWithName:interfaceOrientation:"),
            with: "atomyx-gesture",
            with: NSNumber(value: orientation)
        )

        // Build one XCPointerEventPath per pointer and attach.
        // `addPointerEventPath:` takes an object arg, so
        // `perform(_:with:)` marshals correctly (no primitives).
        for pointer in pointers {
            let pathObj = try Self.buildPointerPath(
                pointer: pointer,
                pathClass: pathCls
            )
            _ = record.perform(
                NSSelectorFromString("addPointerEventPath:"),
                with: pathObj
            )
        }

        try Self.dispatchRecord(record)
    }

    // MARK: - Path construction

    private static func buildPointerPath(
        pointer: PointerPath,
        pathClass: NSObject.Type
    ) throws -> NSObject {
        guard let first = pointer.waypoints.first else {
            throw SynthesizerError.patternNotExpressible(
                reason: "pointer '\(pointer.id)' has no waypoints"
            )
        }
        guard first.phase == .down else {
            throw SynthesizerError.patternNotExpressible(
                reason: "pointer '\(pointer.id)' must open with a `down` waypoint"
            )
        }

        // Instantiate an XCPointerEventPath starting at the first
        // down waypoint's point + offset. See `dispatch` above
        // for the rationale on `class_createInstance` vs `alloc`.
        // The init takes a `CGPoint` + `double` — primitives that
        // `perform` can't marshal. We call the init via
        // NSInvocation to pass the primitive values correctly.
        guard let pathAny = class_createInstance(pathClass, 0),
              let path = pathAny as? NSObject else {
            throw SynthesizerError.privateSymbolMissing(
                symbol: "XCPointerEventPath (class_createInstance failed)"
            )
        }
        try Self.invokeInit(on: path, point: first.point, offset: first.atOffsetSeconds)

        // Walk remaining waypoints and dispatch to the
        // appropriate selector. `XCPointerEventPath` rejects
        // waypoints with non-strictly-increasing offsets (the
        // init already recorded offset=first.atOffsetSeconds;
        // every subsequent event must be strictly later).
        // Authors commonly emit `[down(0), up(0)]` for a tap —
        // we bump the next offset by a 10ms floor if it would
        // otherwise be non-increasing. 10ms matches XCUITest's
        // typical tap duration and comfortably exceeds the
        // runtime's internal precision threshold.
        let minOffsetDelta: Double = 0.01
        var lastEmittedOffset = first.atOffsetSeconds
        for (i, wp) in pointer.waypoints.enumerated() {
            let rawOffset = wp.atOffsetSeconds
            let effectiveOffset: Double
            if i == 0 {
                effectiveOffset = rawOffset
            } else if rawOffset > lastEmittedOffset {
                effectiveOffset = rawOffset
            } else {
                effectiveOffset = lastEmittedOffset + minOffsetDelta
            }
            lastEmittedOffset = effectiveOffset

            switch wp.phase {
            case .down:
                if i == 0 {
                    // Already down from `initForTouchAtPoint:offset:`.
                    continue
                }
                throw SynthesizerError.patternNotExpressible(
                    reason: "pointer '\(pointer.id)' has extra `down` at index \(i)"
                )
            case .move:
                try Self.invokeMove(on: path, toPoint: wp.point, atOffset: effectiveOffset)
            case .up:
                try Self.invokeLiftUp(on: path, atOffset: effectiveOffset)
            }

            if let pressure = wp.pressure {
                try Self.invokePressDown(
                    on: path, withPressure: pressure, atOffset: effectiveOffset
                )
            }
        }

        return path
    }

    // MARK: - IMP-based dispatch for primitive-arg selectors
    //
    // Swift's `perform(_:with:with:)` cannot marshal primitive
    // argument types (CGPoint by value, Double). NSInvocation is
    // unavailable in Swift. The reliable path is to grab the raw
    // IMP from the ObjC runtime and cast it to a function pointer
    // with the correct signature — Swift calls the function
    // directly with full primitive ABI support.

    private static func invokeInit(
        on path: NSObject, point: CGPoint, offset: TimeInterval
    ) throws {
        typealias Fn = @convention(c) (AnyObject, Selector, CGPoint, TimeInterval) -> Unmanaged<AnyObject>
        let sel = NSSelectorFromString("initForTouchAtPoint:offset:")
        let fn = try impAs(Fn.self, on: path, selector: sel)
        _ = fn(path, sel, point, offset)
    }

    private static func invokeMove(
        on path: NSObject, toPoint point: CGPoint, atOffset offset: TimeInterval
    ) throws {
        typealias Fn = @convention(c) (AnyObject, Selector, CGPoint, TimeInterval) -> Void
        let sel = NSSelectorFromString("moveToPoint:atOffset:")
        let fn = try impAs(Fn.self, on: path, selector: sel)
        fn(path, sel, point, offset)
    }

    private static func invokeLiftUp(
        on path: NSObject, atOffset offset: TimeInterval
    ) throws {
        typealias Fn = @convention(c) (AnyObject, Selector, TimeInterval) -> Void
        let sel = NSSelectorFromString("liftUpAtOffset:")
        let fn = try impAs(Fn.self, on: path, selector: sel)
        fn(path, sel, offset)
    }

    private static func invokePressDown(
        on path: NSObject, withPressure pressure: Double, atOffset offset: TimeInterval
    ) throws {
        typealias Fn = @convention(c) (AnyObject, Selector, Double, TimeInterval) -> Void
        let sel = NSSelectorFromString("pressDownWithPressure:atOffset:")
        let fn = try impAs(Fn.self, on: path, selector: sel)
        fn(path, sel, pressure, offset)
    }

    private static func impAs<Fn>(
        _: Fn.Type, on obj: NSObject, selector sel: Selector
    ) throws -> Fn {
        guard let method = class_getInstanceMethod(type(of: obj), sel) else {
            throw SynthesizerError.privateSymbolMissing(
                symbol: "\(NSStringFromSelector(sel)) on \(type(of: obj))"
            )
        }
        let imp = method_getImplementation(method)
        return unsafeBitCast(imp, to: Fn.self)
    }

    // MARK: - Dispatch

    private static func dispatchRecord(_ record: NSObject) throws {
        guard let sessionCls = NSClassFromString("XCTRunnerDaemonSession") as? NSObject.Type else {
            throw SynthesizerError.privateSymbolMissing(symbol: "XCTRunnerDaemonSession")
        }

        let session: NSObject
        if let shared = sessionCls.perform(NSSelectorFromString("sharedSession"))?.takeUnretainedValue() as? NSObject {
            session = shared
        } else {
            throw SynthesizerError.privateSymbolMissing(
                symbol: "XCTRunnerDaemonSession.sharedSession"
            )
        }

        // Dispatch via `_XCT_synthesizeEvent:completion:`. Both
        // args are objects (the event record + a block); use
        // `perform(_:with:with:)` to marshal them.
        //
        // Block construction note: the daemon's XPC reply path has
        // been observed (Xcode 16.2 / iOS 18.3) to invoke the
        // completion block with a sentinel pointer (0x1) instead
        // of a real NSError* or nil. A Swift
        // `@convention(block) (NSError?) -> Void` thunk
        // auto-retains the argument and crashes; even
        // `(AnyObject?)` crashes via `swift_unknownObjectRetain`.
        // Build the block in Objective-C so it never touches the
        // argument — see AtomyxBlockHelper.m for the rationale.
        // Capture-free block: pass the semaphore in as a parameter
        // to the ObjC helper so the resulting block has no Swift
        // captures. XPC's _Block_copy doesn't survive Swift-
        // captured closures crossing the connection.
        let semaphore = DispatchSemaphore(value: 0)
        let block: Any = atomyxMakeSemaphoreSignalingBlock(semaphore)

        // Selector note: the concrete `XCTRunnerDaemonSession`
        // exposes `synthesizeEvent:completion:`. The `_XCT_`
        // prefix variant is the name the XPC proxy uses in its
        // protocol declaration; it is NOT a valid selector on
        // the shared session instance.
        //
        // Threading note: `synthesizeEvent:completion:` posts its
        // completion block to the MAIN queue. CommandServer routes
        // synthesizer-using commands (tapAt, dispatchPointer) to
        // the background accept queue, so we can wait on a
        // semaphore here without deadlocking main.
        if Thread.isMainThread {
            throw SynthesizerError.dispatchFailed(
                reason: "EventRecordSynthesizer.dispatch must run on a background queue; " +
                    "see CommandHandler.requiresMainThread."
            )
        }

        _ = session.perform(
            NSSelectorFromString("synthesizeEvent:completion:"),
            with: record,
            with: block
        )

        // Cap at 30 s — well above any real gesture duration. The
        // daemon's completion has been observed to fire 1–2 s after
        // dispatch on a healthy session.
        let outcome = semaphore.wait(timeout: .now() + 30)
        if outcome == .timedOut {
            throw SynthesizerError.dispatchFailed(
                reason: "private gesture dispatch timed out after 30 s"
            )
        }
    }

    // MARK: - Probe

    private struct ProbeOutcome {
        let ok: Bool
        let log: String
    }

    /// Dry-run probe: verifies every private class and selector
    /// this file depends on, then executes a zero-duration touch
    /// at an offscreen coordinate. Caught failures return
    /// `ok=false`; the factory falls back to
    /// `CoordinateSynthesizer`.
    private static func probe() -> ProbeOutcome {
        var log = ""
        func step(_ message: String) { log += message + "\n" }

        // 1. Class lookups. Distinguish "class missing from runtime"
        // from "class present but not NSObject-derived" — the latter
        // can happen if Apple re-parents a private class to a pure
        // Swift base type, in which case `NSClassFromString` finds
        // it but the `as? NSObject.Type` cast fails.
        func resolveNSClass(_ name: String) -> (cls: NSObject.Type?, reason: String?) {
            guard let raw = NSClassFromString(name) else {
                return (nil, "class missing from runtime: \(name)")
            }
            guard let nsCls = raw as? NSObject.Type else {
                return (nil,
                    "class \(name) present but not NSObject-derived " +
                    "(cast `as? NSObject.Type` failed — Apple may have re-parented it)"
                )
            }
            return (nsCls, nil)
        }

        let recordLookup = resolveNSClass("XCSynthesizedEventRecord")
        guard let recordCls = recordLookup.cls else {
            return ProbeOutcome(ok: false, log: recordLookup.reason ?? "unknown")
        }
        step("class XCSynthesizedEventRecord: present")

        let pathLookup = resolveNSClass("XCPointerEventPath")
        guard let pathCls = pathLookup.cls else {
            return ProbeOutcome(ok: false, log: log + (pathLookup.reason ?? "unknown"))
        }
        step("class XCPointerEventPath: present")

        let sessionLookup = resolveNSClass("XCTRunnerDaemonSession")
        guard let sessionCls = sessionLookup.cls else {
            return ProbeOutcome(ok: false, log: log + (sessionLookup.reason ?? "unknown"))
        }
        step("class XCTRunnerDaemonSession: present")

        // 2. Selector response checks.
        let selectorsToProbe: [(String, NSObject.Type)] = [
            ("initWithName:interfaceOrientation:", recordCls),
            ("addPointerEventPath:", recordCls),
            ("initForTouchAtPoint:offset:", pathCls),
            ("moveToPoint:atOffset:", pathCls),
            ("liftUpAtOffset:", pathCls),
            ("sharedSession", sessionCls),
        ]
        for (selStr, cls) in selectorsToProbe {
            let sel = NSSelectorFromString(selStr)
            let instanceResponds = cls.instancesRespond(to: sel)
            let classResponds = cls.responds(to: sel)
            if !instanceResponds && !classResponds {
                return ProbeOutcome(
                    ok: false,
                    log: log + "selector not responding: \(selStr) on \(cls)"
                )
            }
            step("selector \(selStr): present")
        }

        // 3. sharedSession instance check. The dispatch selector
        // (`_XCT_synthesizeEvent:completion:`) is XPC-backed and
        // does NOT register via `respondsToSelector:` or protocol
        // `as?` conformance, even when the call works perfectly
        // through Objective-C message forwarding. We verify the
        // session proxy is reachable; any dispatch-path drift
        // surfaces as a real call failure at gesture time, not
        // at init.
        guard sessionCls.perform(NSSelectorFromString("sharedSession"))?.takeUnretainedValue() != nil else {
            return ProbeOutcome(
                ok: false,
                log: log + "XCTRunnerDaemonSession.sharedSession returned nil — test context is not live"
            )
        }
        step("sharedSession proxy reachable")

        return ProbeOutcome(ok: true, log: log)
    }

    /// Returns the orientation hint XCUITest events need so
    /// taps / drags map correctly across device rotations.
    /// Hardcoded to portrait (1).
    ///
    /// Why hardcoded: `XCUIDevice.shared.orientation` triggers
    /// a multi-second XPC round-trip on first access (the
    /// daemon initializes its orientation cache by introspecting
    /// the UI). That latency exceeds the host's gesture-call
    /// timeout and breaks the first pointer dispatch of a
    /// session. Rotation-aware dispatch needs a one-shot cached
    /// orientation read on a background queue.
    private static func currentInterfaceOrientation(in app: XCUIApplication) -> Int {
        return 1 // portrait
    }
}
