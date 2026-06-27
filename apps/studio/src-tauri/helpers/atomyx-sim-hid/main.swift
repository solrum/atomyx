import Foundation
import Network
import AppKit
import ObjectiveC

// Dispatches touch events into the iOS Simulator via the
// SimDeviceLegacyHIDClient private API. Input arrives as
// newline-delimited JSON over a localhost WebSocket; each
// message maps to a gesture (tap / swipe / touch-down /
// touch-move / touch-up). The binary emits one handshake
// line to stdout when the server is ready, then is silent.
//
// On Xcode < 26 the Indigo HID symbols are absent from
// SimulatorKit; IndigoHIDClient.ensureWarm() returns false
// in that case. The WebSocket server still starts and replies
// with {"ok":false,"error":"..."} for every gesture so the
// orchestrator can detect the situation and fall back to
// XCUITest without losing the connection.
//
// Usage: atomyx-sim-hid --udid <UDID> [--max-clients <N>]

// MARK: - CLI parsing

struct CLIArgs {
    var udid: String
    var maxClients: Int = 4
}

func parseArgs() -> CLIArgs? {
    var udid: String?
    var maxClients = 4
    var i = 1
    let args = CommandLine.arguments
    while i < args.count {
        switch args[i] {
        case "--udid":
            i += 1
            if i < args.count { udid = args[i] }
        case "--max-clients":
            i += 1
            if i < args.count, let v = Int(args[i]) { maxClients = v }
        default:
            logErr("unknown argument: \(args[i])")
        }
        i += 1
    }
    guard let resolved = udid else { return nil }
    return CLIArgs(udid: resolved, maxClients: maxClients)
}

// MARK: - stderr logging

func logErr(_ line: String) {
    if let data = "LOG: \(line)\n".data(using: .utf8) {
        FileHandle.standardError.write(data)
    }
}

func fail(_ msg: String, code: Int32 = 1) -> Never {
    logErr("FATAL: \(msg)")
    exit(code)
}

// MARK: - Stdout handshake

private func emitHandshake(_ json: String) {
    if let data = (json + "\n").data(using: .utf8) {
        FileHandle.standardOutput.write(data)
    }
}

// MARK: - Developer directory resolution

/// Returns the active Xcode developer directory (e.g. /Applications/Xcode.app/Contents/Developer).
func developerDir() -> String {
    let task = Process()
    task.launchPath = "/usr/bin/xcode-select"
    task.arguments = ["-p"]
    let pipe = Pipe()
    task.standardOutput = pipe
    task.standardError = Pipe()
    do { try task.run() } catch { return "/Applications/Xcode.app/Contents/Developer" }
    let data = pipe.fileHandleForReading.readDataToEndOfFile()
    task.waitUntilExit()
    guard task.terminationStatus == 0 else {
        return "/Applications/Xcode.app/Contents/Developer"
    }
    return String(data: data, encoding: .utf8)?
        .trimmingCharacters(in: .whitespacesAndNewlines)
        ?? "/Applications/Xcode.app/Contents/Developer"
}

// MARK: - Private framework bootstrap

/// Loads CoreSimulator and SimulatorKit via dlopen. Fails hard if either
/// is absent — neither the device resolution nor the HID dispatch path
/// can operate without both frameworks.
func bootstrapPrivateFrameworks(devDir: String) {
    let coreSimPath = "/Library/Developer/PrivateFrameworks/CoreSimulator.framework/CoreSimulator"
    let simKitPath  = "\(devDir)/Library/PrivateFrameworks/SimulatorKit.framework/SimulatorKit"

    guard dlopen(coreSimPath, RTLD_NOW | RTLD_GLOBAL) != nil else {
        fail("dlopen CoreSimulator failed: \(String(cString: dlerror())) — is Xcode installed?")
    }
    logErr("CoreSimulator loaded from \(coreSimPath)")

    guard dlopen(simKitPath, RTLD_NOW | RTLD_GLOBAL) != nil else {
        fail("dlopen SimulatorKit failed: \(String(cString: dlerror())) — path: \(simKitPath)")
    }
    logErr("SimulatorKit loaded from \(simKitPath)")
}

// MARK: - SimDevice resolution

/// Resolves the SimDevice NSObject for the given UDID via SimServiceContext.
///
/// Resolution path:
///   SimServiceContext.sharedServiceContextForDeveloperDir(_:error:)
///     → SimDeviceSet.defaultDeviceSetWithError(_:)
///       → SimDeviceSet.availableDevices
///         → find device whose UDID uuidString matches target
func resolveSimDevice(udid: String, devDir: String) -> NSObject? {
    guard let ctxClass = NSClassFromString("SimServiceContext") else {
        logErr("NSClassFromString('SimServiceContext') returned nil — CoreSimulator not loaded")
        return nil
    }

    let ctxSel = NSSelectorFromString("sharedServiceContextForDeveloperDir:error:")
    guard ctxClass.responds(to: ctxSel) else {
        logErr("SimServiceContext does not respond to sharedServiceContextForDeveloperDir:error:")
        return nil
    }

    typealias CtxFn = @convention(c) (AnyClass, Selector, NSString, NSErrorPointer) -> NSObject?
    let ctxImp = class_getMethodImplementation(object_getClass(ctxClass), ctxSel)
    let ctxFn = unsafeBitCast(ctxImp, to: CtxFn.self)

    var ctxErr: NSError?
    guard let ctx = ctxFn(ctxClass, ctxSel, devDir as NSString, &ctxErr) else {
        logErr("SimServiceContext init failed: \(ctxErr?.localizedDescription ?? "unknown")")
        return nil
    }

    let setSel = NSSelectorFromString("defaultDeviceSetWithError:")
    guard let setResult = ctx.perform(setSel, with: nil),
          let deviceSet = setResult.takeUnretainedValue() as? NSObject else {
        logErr("defaultDeviceSetWithError failed")
        return nil
    }

    guard let devices = deviceSet.value(forKey: "availableDevices") as? [NSObject] else {
        logErr("availableDevices returned nil or unexpected type")
        return nil
    }

    for device in devices {
        guard let uuidObj = device.value(forKey: "UDID") as? NSUUID else { continue }
        if uuidObj.uuidString.uppercased() == udid.uppercased() {
            logErr("resolved SimDevice for UDID \(udid)")
            return device
        }
    }

    logErr("no SimDevice found with UDID \(udid) — is the simulator booted?")
    return nil
}

// MARK: - IOHIDEvent helpers (symbol types)

// IOHIDEvent creation lives in the dyld shared cache and is reachable
// via RTLD_DEFAULT. The trackpad wrapper is in SimulatorKit.
//
// Argument layout verified from baguette's IOHIDDigitizerDispatch:
//   IOHIDEventCreateDigitizerEvent — 15 args (allocator, ts, transducer,
//     index, identifier, eventMask, buttonMask, x, y, z,
//     tipPressure, barrelPressure, range, touch, options)
//   IOHIDEventCreateDigitizerFingerEvent — 13 args (allocator, ts,
//     index, identifier, eventMask, x, y, z,
//     tipPressure, twist, range, touch, options)
//   IOHIDEventAppendEvent — 3 args (parent, child, options)
//   IndigoHIDMessageForTrackpadEventFromHIDEventRef — 1 arg (eventRef raw ptr)

private typealias CreateDigitizerFn = @convention(c) (
    CFAllocator?, UInt64, UInt32,
    UInt32, UInt32, UInt32, UInt32,
    Double, Double, Double, Double, Double,
    Bool, Bool, UInt32
) -> Unmanaged<CFTypeRef>?

private typealias CreateFingerFn = @convention(c) (
    CFAllocator?, UInt64,
    UInt32, UInt32, UInt32,
    Double, Double, Double, Double, Double,
    Bool, Bool, UInt32
) -> Unmanaged<CFTypeRef>?

private typealias AppendEventFn  = @convention(c) (CFTypeRef, CFTypeRef, UInt32) -> Void
private typealias TrackpadWrapFn = @convention(c) (UnsafeRawPointer) -> UnsafeMutableRawPointer?
private typealias ServiceFn      = @convention(c) () -> UnsafeMutableRawPointer?

// MARK: - IndigoHIDClient

/// Wraps SimDeviceLegacyHIDClient and dispatches digitizer touch events
/// via the IOHIDDigitizerEvent recipe (baguette-derived).
///
/// All methods return false and log a clear message when a required
/// private symbol is absent. The WS server remains functional so the
/// orchestrator can detect unavailability and fall back to XCUITest.
final class IndigoHIDClient {
    private let device: NSObject
    private var hidClient: AnyObject?
    private var warm = false

    // Resolved C function pointers — nil when symbol is unavailable.
    private var createDigitizerFn: CreateDigitizerFn?
    private var createFingerFn:    CreateFingerFn?
    private var appendEventFn:     AppendEventFn?
    private var trackpadWrapFn:    TrackpadWrapFn?
    private var createPointerSvc:  ServiceFn?
    private var createMouseSvc:    ServiceFn?

    // Monotonic per-session touch identifier so iOS threads distinct
    // gesture sequences correctly through the HID stack.
    private var touchCounter: UInt32 = 0

    init(device: NSObject) {
        self.device = device
    }

    // MARK: - Warm-up

    /// Lazy-creates the SimDeviceLegacyHIDClient and registers pointer +
    /// mouse services. Returns false if any required symbol or class is
    /// absent; on false the WS server continues running but every dispatch
    /// returns an error reply.
    @discardableResult
    func ensureWarm() -> Bool {
        guard !warm else { return hidClient != nil }
        warm = true

        resolveSymbols()

        guard let cls = NSClassFromString("_TtC12SimulatorKit24SimDeviceLegacyHIDClient") else {
            logErr("IndigoHIDClient: SimDeviceLegacyHIDClient class not found — Xcode < 26?")
            return false
        }
        let initSel = NSSelectorFromString("initWithDevice:error:")
        guard let metaCls = object_getClass(cls),
              let allocImp = class_getMethodImplementation(metaCls, NSSelectorFromString("alloc")),
              let initImp  = class_getMethodImplementation(cls, initSel) else {
            logErr("IndigoHIDClient: could not resolve alloc or initWithDevice:error:")
            return false
        }

        typealias AllocFn = @convention(c) (AnyClass, Selector) -> AnyObject?
        typealias InitFn  = @convention(c) (AnyObject, Selector, AnyObject, AutoreleasingUnsafeMutablePointer<NSError?>) -> AnyObject?

        let allocFn = unsafeBitCast(allocImp, to: AllocFn.self)
        let initFn  = unsafeBitCast(initImp,  to: InitFn.self)

        guard let allocated = allocFn(cls, NSSelectorFromString("alloc")) else {
            logErr("IndigoHIDClient: alloc returned nil")
            return false
        }
        var err: NSError?
        guard let client = initFn(allocated, initSel, device, &err) else {
            logErr("IndigoHIDClient: initWithDevice:error: failed: \(err?.localizedDescription ?? "unknown")")
            return false
        }

        hidClient = client
        warmServices(on: client)
        logErr("IndigoHIDClient: warm — client ready")
        return true
    }

    // MARK: - Gesture dispatch

    /// Single-finger tap at a normalised point [0..1].
    func tap(point: CGPoint, holdMs: Int, identifier: UInt32) -> Bool {
        guard let client = hidClient else { return false }
        guard send(point: point, identifier: identifier, phase: .down, on: client) else { return false }
        let holdUs = UInt32(max(20, holdMs)) * 1000
        usleep(holdUs)
        return send(point: point, identifier: identifier, phase: .up, on: client)
    }

    /// Continuous swipe from `start` to `end` over `steps` interpolated moves.
    /// Optional `dwellMs` holds the finger at the endpoint before lift.
    func swipe(from start: CGPoint, to end: CGPoint,
               steps: Int, stepMs: Int, dwellMs: Int,
               identifier: UInt32) -> Bool {
        guard let client = hidClient else { return false }
        guard send(point: start, identifier: identifier, phase: .down, on: client) else { return false }
        var ok = 0
        for i in 1...max(1, steps) {
            usleep(UInt32(max(1, stepMs)) * 1000)
            let t  = Double(i) / Double(max(1, steps))
            let pt = CGPoint(x: start.x + (end.x - start.x) * t,
                             y: start.y + (end.y - start.y) * t)
            if send(point: pt, identifier: identifier, phase: .move, on: client) { ok += 1 }
        }
        if dwellMs > 0 {
            let pulses = max(1, dwellMs / 50)
            for _ in 0..<pulses {
                _ = send(point: end, identifier: identifier, phase: .move, on: client)
                usleep(50_000)
            }
        }
        usleep(UInt32(max(1, stepMs)) * 1000)
        return send(point: end, identifier: identifier, phase: .up, on: client) && ok >= max(1, steps) / 2
    }

    /// Phase-by-phase dispatch: finger press.
    func streamingTouchDown(point: CGPoint, identifier: UInt32) -> Bool {
        guard let client = hidClient else { return false }
        return send(point: point, identifier: identifier, phase: .down, on: client)
    }

    /// Phase-by-phase dispatch: finger move.
    func streamingTouchMove(point: CGPoint, identifier: UInt32) -> Bool {
        guard let client = hidClient else { return false }
        return send(point: point, identifier: identifier, phase: .move, on: client)
    }

    /// Phase-by-phase dispatch: finger lift.
    func streamingTouchUp(point: CGPoint, identifier: UInt32) -> Bool {
        guard let client = hidClient else { return false }
        return send(point: point, identifier: identifier, phase: .up, on: client)
    }

    // MARK: - Core dispatch (IOHIDDigitizerEvent recipe)

    private enum TouchPhase {
        case down, move, up

        // IOHIDDigitizerEventMask bits: Range=1, Touch=2, Position=4.
        // Sustained touch (down + move) carries all three; lift carries
        // Touch | Position so iOS sees the finger-lift state change.
        var eventMask: UInt32 {
            switch self { case .down, .move: return 0x07; case .up: return 0x06 }
        }
        var range: Bool { self != .up }
        var touch: Bool { self != .up }
    }

    /// Build a digitizer parent + finger child IOHIDEvent, run it through
    /// IndigoHIDMessageForTrackpadEventFromHIDEventRef, patch the two byte
    /// slots the wrapper leaves uninitialised, then dispatch via
    /// SimDeviceLegacyHIDClient. Returns false at the first failure.
    ///
    /// Byte-patch rationale (baguette recipe, empirically verified on
    /// iOS 26.4 / Xcode 26):
    ///   offset 0x6c + 0x10c → UInt32 0x32 (IndigoHIDTouchTarget, routing tag)
    ///   offset 0x3a/0x3b + 0xda/0xdb → edge bitmask (0x00 for interior touches)
    private func send(point: CGPoint, identifier: UInt32,
                      phase: TouchPhase, on client: AnyObject) -> Bool {
        guard let parent = makeDigitizerEvent(point: point,
                                              identifier: identifier,
                                              phase: phase) else {
            logErr("IndigoHIDClient: makeDigitizerEvent returned nil")
            return false
        }
        let raw: UnsafeMutableRawPointer? = withExtendedLifetime(parent) {
            guard let wrapFn = trackpadWrapFn else { return nil }
            let ptr = Unmanaged.passUnretained(parent as AnyObject).toOpaque()
            return wrapFn(ptr)
        }
        guard let raw else {
            logErr("IndigoHIDClient: trackpad wrapper returned nil")
            return false
        }
        patchMessage(raw)
        sendMessage(raw, to: client)
        return true
    }

    /// Build digitizer parent event with a finger child appended.
    /// iOS requires the parent+child structure; a bare finger event
    /// produces a truncated 192-byte stub the system ignores.
    private func makeDigitizerEvent(point: CGPoint, identifier: UInt32,
                                    phase: TouchPhase) -> CFTypeRef? {
        guard let createParent = createDigitizerFn,
              let createFinger = createFingerFn,
              let appendFn     = appendEventFn else {
            logErr("IndigoHIDClient: IOHIDEvent symbols not resolved")
            return nil
        }
        let mask              = phase.eventMask
        let range             = phase.range
        let touch             = phase.touch
        let now               = mach_absolute_time()
        let transducerFinger: UInt32 = 2

        guard let parentUM = createParent(
            nil, now, transducerFinger,
            0, identifier, mask, 0,
            point.x, point.y, 0.0,
            0.0, 0.0,
            range, touch, 0
        ) else { return nil }
        let parent = parentUM.takeRetainedValue()

        if let fingerUM = createFinger(
            nil, now,
            0, identifier, mask,
            point.x, point.y, 0.0,
            0.0, 0.0,
            range, touch, 0
        ) {
            let finger = fingerUM.takeRetainedValue()
            appendFn(parent, finger, 0)
        }
        return parent
    }

    /// Patch IndigoHIDTouchTarget at offsets 0x6c and 0x10c, and
    /// clear the edge bitmask slots at 0x3a/0x3b and 0xda/0xdb.
    /// Interior touches always use edge = 0x00.
    private func patchMessage(_ msg: UnsafeMutableRawPointer) {
        let target: UInt32 = 0x32
        msg.storeBytes(of: target, toByteOffset: 0x6c, as: UInt32.self)
        let size = malloc_size(msg)
        if size >= 0x110 {
            msg.storeBytes(of: target, toByteOffset: 0x10c, as: UInt32.self)
        }
        // Interior touch: edge bytes are 0x00.
        msg.storeBytes(of: UInt8(0), toByteOffset: 0x3a, as: UInt8.self)
        msg.storeBytes(of: UInt8(0), toByteOffset: 0x3b, as: UInt8.self)
        if size >= 0xdc {
            msg.storeBytes(of: UInt8(0), toByteOffset: 0xda, as: UInt8.self)
            msg.storeBytes(of: UInt8(0), toByteOffset: 0xdb, as: UInt8.self)
        }
    }

    /// Dispatch the patched Indigo message via SimDeviceLegacyHIDClient.
    private func sendMessage(_ message: UnsafeMutableRawPointer, to client: AnyObject) {
        let sel = NSSelectorFromString("sendWithMessage:freeWhenDone:completionQueue:completion:")
        guard let cls = object_getClass(client),
              let imp = class_getMethodImplementation(cls, sel) else {
            logErr("IndigoHIDClient: sendWithMessage:freeWhenDone:completionQueue:completion: not found")
            return
        }
        typealias Fn = @convention(c) (
            AnyObject, Selector, UnsafeMutableRawPointer, ObjCBool, AnyObject?, AnyObject?
        ) -> Void
        unsafeBitCast(imp, to: Fn.self)(client, sel, message, ObjCBool(true), nil, nil)
    }

    // MARK: - Symbol resolution

    private func resolveSymbols() {
        let dev     = developerDir()
        let kitPath = "\(dev)/Library/PrivateFrameworks/SimulatorKit.framework/SimulatorKit"
        guard let kit = dlopen(kitPath, RTLD_NOW) else {
            logErr("IndigoHIDClient: SimulatorKit dlopen failed — \(String(cString: dlerror()))")
            return
        }
        // IOHIDEvent creation functions live in the dyld shared cache.
        let dyld = UnsafeMutableRawPointer(bitPattern: -2)
        if let p = dlsym(dyld, "IOHIDEventCreateDigitizerEvent") {
            createDigitizerFn = unsafeBitCast(p, to: CreateDigitizerFn.self)
        }
        if let p = dlsym(dyld, "IOHIDEventCreateDigitizerFingerEvent") {
            createFingerFn = unsafeBitCast(p, to: CreateFingerFn.self)
        }
        if let p = dlsym(dyld, "IOHIDEventAppendEvent") {
            appendEventFn = unsafeBitCast(p, to: AppendEventFn.self)
        }
        if let p = dlsym(kit, "IndigoHIDMessageForTrackpadEventFromHIDEventRef") {
            trackpadWrapFn = unsafeBitCast(p, to: TrackpadWrapFn.self)
        }
        if let p = dlsym(kit, "IndigoHIDMessageToCreatePointerService") {
            createPointerSvc = unsafeBitCast(p, to: ServiceFn.self)
        }
        if let p = dlsym(kit, "IndigoHIDMessageToCreateMouseService") {
            createMouseSvc = unsafeBitCast(p, to: ServiceFn.self)
        }
        logErr("IndigoHIDClient: symbols — createDigitizer:\(createDigitizerFn != nil) createFinger:\(createFingerFn != nil) append:\(appendEventFn != nil) trackpadWrap:\(trackpadWrapFn != nil)")
    }

    private func warmServices(on client: AnyObject) {
        if let create = createPointerSvc, let msg = create() {
            sendMessage(msg, to: client)
            usleep(20_000)
        }
        if let create = createMouseSvc, let msg = create() {
            sendMessage(msg, to: client)
            usleep(20_000)
        }
    }

    // MARK: - Identifier generation

    func nextIdentifier() -> UInt32 {
        touchCounter &+= 1
        if touchCounter == 0 { touchCounter = 1 }
        return touchCounter
    }
}

// MARK: - JSON message parsing

private struct GestureMessage {
    enum Kind {
        case tap(x: Double, y: Double, holdMs: Int, id: UInt32)
        case swipe(x1: Double, y1: Double, x2: Double, y2: Double,
                   steps: Int, stepMs: Int, dwellMs: Int, id: UInt32)
        case touchDown(x: Double, y: Double, id: UInt32)
        case touchMove(x: Double, y: Double, id: UInt32)
        case touchUp(x: Double, y: Double, id: UInt32)
    }
    let id: UInt32
    let kind: Kind

    static func parse(json: [String: Any]) -> GestureMessage? {
        guard let type  = json["type"] as? String,
              let msgId = (json["id"] as? NSNumber).map({ UInt32($0.intValue) }) else { return nil }
        switch type {
        case "tap":
            guard let x = (json["x"] as? NSNumber)?.doubleValue,
                  let y = (json["y"] as? NSNumber)?.doubleValue else { return nil }
            let holdMs = (json["holdMs"] as? NSNumber)?.intValue ?? 50
            return GestureMessage(id: msgId, kind: .tap(x: x, y: y, holdMs: holdMs, id: msgId))
        case "swipe":
            guard let x1 = (json["x1"] as? NSNumber)?.doubleValue,
                  let y1 = (json["y1"] as? NSNumber)?.doubleValue,
                  let x2 = (json["x2"] as? NSNumber)?.doubleValue,
                  let y2 = (json["y2"] as? NSNumber)?.doubleValue else { return nil }
            let steps  = (json["steps"]  as? NSNumber)?.intValue ?? 10
            let stepMs = (json["stepMs"] as? NSNumber)?.intValue ?? 16
            let dwellMs = (json["dwellMs"] as? NSNumber)?.intValue ?? 0
            return GestureMessage(id: msgId, kind: .swipe(x1: x1, y1: y1, x2: x2, y2: y2,
                                                           steps: steps, stepMs: stepMs,
                                                           dwellMs: dwellMs, id: msgId))
        case "touch-down":
            guard let x = (json["x"] as? NSNumber)?.doubleValue,
                  let y = (json["y"] as? NSNumber)?.doubleValue else { return nil }
            return GestureMessage(id: msgId, kind: .touchDown(x: x, y: y, id: msgId))
        case "touch-move":
            guard let x = (json["x"] as? NSNumber)?.doubleValue,
                  let y = (json["y"] as? NSNumber)?.doubleValue else { return nil }
            return GestureMessage(id: msgId, kind: .touchMove(x: x, y: y, id: msgId))
        case "touch-up":
            guard let x = (json["x"] as? NSNumber)?.doubleValue,
                  let y = (json["y"] as? NSNumber)?.doubleValue else { return nil }
            return GestureMessage(id: msgId, kind: .touchUp(x: x, y: y, id: msgId))
        default:
            return nil
        }
    }
}

// MARK: - WebSocket server

/// Localhost-only WebSocket server that receives newline-delimited JSON
/// gesture messages and dispatches them to IndigoHIDClient. Replies with
/// a text frame per message indicating success or failure.
final class InputWebSocketServer {
    private let listener: NWListener
    private let queue    = DispatchQueue(label: "dev.atomyx.simhid.ws")
    private var clients: [NWConnection] = []
    private var _port: UInt16 = 0

    var localPort: UInt16 { _port }

    init() throws {
        let wsOpts = NWProtocolWebSocket.Options()
        wsOpts.autoReplyPing = true
        let params = NWParameters.tcp
        params.requiredLocalEndpoint = NWEndpoint.hostPort(host: "127.0.0.1", port: .any)
        params.defaultProtocolStack.applicationProtocols.insert(wsOpts, at: 0)
        listener = try NWListener(using: params)
        listener.newConnectionHandler = { [weak self] conn in
            self?.accept(conn)
        }
    }

    func start(client: IndigoHIDClient, onReady: @escaping (UInt16) -> Void) {
        listener.stateUpdateHandler = { [weak self] state in
            guard let self else { return }
            if case .ready = state, let port = self.listener.port {
                self._port = port.rawValue
                onReady(port.rawValue)
            }
        }
        listener.start(queue: queue)
        self.hidClient = client
    }

    private var hidClient: IndigoHIDClient?

    private func accept(_ conn: NWConnection) {
        conn.stateUpdateHandler = { [weak self, weak conn] state in
            guard let self, let conn else { return }
            switch state {
            case .ready:
                self.queue.async { self.clients.append(conn) }
                self.receive(on: conn)
            case .failed, .cancelled:
                self.queue.async { self.clients.removeAll { $0 === conn } }
            default:
                break
            }
        }
        conn.start(queue: queue)
    }

    private func receive(on conn: NWConnection) {
        conn.receiveMessage { [weak self, weak conn] data, ctx, complete, error in
            guard let self, let conn else { return }
            if let error {
                logErr("ws receive error: \(error)")
                return
            }
            if let data, !data.isEmpty {
                self.dispatchGestureJson(data: data, conn: conn)
            }
            self.receive(on: conn)
        }
    }

    /// Parse incoming JSON, dispatch to IndigoHIDClient, reply with result.
    /// Maps each "type" field to the matching IndigoHIDClient method.
    private func dispatchGestureJson(data: Data, conn: NWConnection) {
        guard let client = hidClient,
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let msg   = GestureMessage.parse(json: json) else {
            reply(conn: conn, id: 0, ok: false, error: "invalid json")
            return
        }
        var ok = false
        switch msg.kind {
        case let .tap(x, y, holdMs, id):
            ok = client.tap(point: CGPoint(x: x, y: y), holdMs: holdMs, identifier: id)
        case let .swipe(x1, y1, x2, y2, steps, stepMs, dwellMs, id):
            ok = client.swipe(from: CGPoint(x: x1, y: y1), to: CGPoint(x: x2, y: y2),
                              steps: steps, stepMs: stepMs, dwellMs: dwellMs, identifier: id)
        case let .touchDown(x, y, id):
            ok = client.streamingTouchDown(point: CGPoint(x: x, y: y), identifier: id)
        case let .touchMove(x, y, id):
            ok = client.streamingTouchMove(point: CGPoint(x: x, y: y), identifier: id)
        case let .touchUp(x, y, id):
            ok = client.streamingTouchUp(point: CGPoint(x: x, y: y), identifier: id)
        }
        let errorMsg: String? = ok ? nil : "dispatch failed — check helper log for details"
        reply(conn: conn, id: msg.id, ok: ok, error: errorMsg)
    }

    private func reply(conn: NWConnection, id: UInt32, ok: Bool, error: String?) {
        var body = "{\"ok\":\(ok),\"id\":\(id)"
        if let error {
            let escaped = error.replacingOccurrences(of: "\"", with: "\\\"")
            body += ",\"error\":\"\(escaped)\""
        }
        body += "}"
        guard let data = body.data(using: .utf8) else { return }
        let wsCtx = NWConnection.ContentContext(
            identifier: "textFrame",
            metadata: [NWProtocolWebSocket.Metadata(opcode: .text)]
        )
        conn.send(content: data, contentContext: wsCtx, isComplete: true, completion: .idempotent)
    }
}

// MARK: - Main

let cliArgs = parseArgs() ?? { fail("missing --udid <UDID>") }()

_ = NSApplication.shared
NSApp.setActivationPolicy(.prohibited)

var liveServer: InputWebSocketServer?

Task {
    do {
        let devDir = developerDir()
        logErr("developer dir: \(devDir)")

        bootstrapPrivateFrameworks(devDir: devDir)

        guard let simDevice = resolveSimDevice(udid: cliArgs.udid, devDir: devDir) else {
            fail("could not resolve SimDevice for UDID \(cliArgs.udid) — is the simulator booted?")
        }

        let hidClient = IndigoHIDClient(device: simDevice)
        // Warm eagerly so the first gesture has no cold-start latency.
        // Returns false on Xcode < 26; we log and continue — the server
        // still starts and callers receive {"ok":false} replies.
        let warmed = hidClient.ensureWarm()
        if !warmed {
            logErr("IndigoHIDClient warm-up failed — private symbols unavailable (Xcode < 26?); server will start but gestures will return errors")
        }

        let server = try InputWebSocketServer()
        liveServer = server

        let assignedPort: UInt16 = await withCheckedContinuation { continuation in
            server.start(client: hidClient) { port in
                continuation.resume(returning: port)
            }
        }

        // Rust reads this single line to discover the WS port.
        // transport tag "ws-input" distinguishes this binary from
        // atomyx-sim-capture which uses "ws".
        emitHandshake("{\"event\":\"listen\",\"port\":\(assignedPort),\"transport\":\"ws-input\"}")
        logErr("WS server listening on 127.0.0.1:\(assignedPort) udid=\(cliArgs.udid)")
    } catch {
        fail("setup failed: \(error.localizedDescription)")
    }
}

signal(SIGINT)  { _ in exit(0) }
signal(SIGTERM) { _ in exit(0) }
NSApp.run()
