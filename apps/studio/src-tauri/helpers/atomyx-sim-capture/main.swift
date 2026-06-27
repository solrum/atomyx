import Foundation
import Network
import AVFoundation
import VideoToolbox
import CoreMedia
import CoreVideo
import AppKit
import IOSurface

// Captures the iOS Simulator's raw framebuffer via SimulatorKit private
// framework and streams encoded H.264 NAL units to connected WebSocket clients.
//
// Every captured frame IS exactly the device screen pixels — no window chrome,
// no bezel, no aspect-ratio guessing. SimulatorKit registers a framebuffer
// callback directly against the simulator's display IOSurface.
//
// On startup the helper binds a localhost WebSocket server on an OS-assigned
// port, then emits a single JSON handshake line to stdout:
//   {"event":"listen","port":<port>,"transport":"ws"}
// After that, stdout is silent. All frame data flows over WS binary frames
// with a 1-byte tag prefix (0x01=SPS/PPS, 0x02=IDR, 0x03=delta).
//
// ATOMYX_MIRROR_BACKEND env var: formerly selected the fMP4 stdout path.
// That backend has been removed. The env var is ignored; the WS path is
// always used. It is kept here only to avoid breaking callers that set it.
//
// Usage: atomyx-sim-capture --udid <UDID> [--max-size <N>] [--bitrate <bps>]

// MARK: - CLI parsing

struct CLIArgs {
    var udid: String
    var maxSize: Int = 1080
    var bitrate: Int = 8_000_000
}

func parseArgs() -> CLIArgs? {
    var udid: String?
    var maxSize = 1080
    var bitrate = 8_000_000
    var i = 1
    let args = CommandLine.arguments
    while i < args.count {
        switch args[i] {
        case "--udid":
            i += 1
            if i < args.count { udid = args[i] }
        case "--max-size":
            i += 1
            if i < args.count, let v = Int(args[i]) { maxSize = v }
        case "--bitrate":
            i += 1
            if i < args.count, let v = Int(args[i]) { bitrate = v }
        default:
            break
        }
        i += 1
    }
    guard let resolved = udid else { return nil }
    return CLIArgs(udid: resolved, maxSize: maxSize, bitrate: bitrate)
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

// MARK: - Stdout write helpers

let stdoutHandle = FileHandle.standardOutput

/// Emit the single handshake line to stdout and flush.
private func emitHandshake(_ json: String) {
    if let data = (json + "\n").data(using: .utf8) {
        stdoutHandle.write(data)
    }
}

// MARK: - Developer directory resolution

/// Returns the active Xcode developer directory (e.g. /Applications/Xcode.app/Contents/Developer).
/// Required for locating SimulatorKit, which lives inside the Xcode bundle.
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

/// Loads CoreSimulator and SimulatorKit via dlopen. Both must succeed —
/// the framebuffer capture path has no fallback when either framework is absent.
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
/// Returns nil and logs a clear message if any step fails.
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

    // defaultDeviceSetWithError: takes an NSError** argument but NSObject.perform
    // doesn't support inout. Passing nil suppresses the error pointer; a nil
    // return value is the failure signal.
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

// MARK: - WebSocket broadcast server

/// Localhost-only WebSocket server. Binds to 127.0.0.1 on an OS-assigned
/// port, broadcasts binary frames to every connected client.
///
/// Wire format per message: [1-byte tag][NAL bytes]
///   tag 0x01 — SPS+PPS (Annex-B, sent when format description changes)
///   tag 0x02 — IDR keyframe NAL
///   tag 0x03 — delta (P/B-frame) NAL
final class FrameWebSocketServer {
    private let listener: NWListener
    private var clients: [NWConnection] = []
    private let queue = DispatchQueue(label: "dev.atomyx.simcapture.ws")
    private var _port: UInt16 = 0
    // Cached SPS/PPS payload (tag 0x01). Replayed to each new client on
    // connect so the decoder can configure even if it missed the first emit.
    private var lastConfig: Data?

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

    /// Start listening. Calls `onReady` once the port is known.
    func start(onReady: @escaping (UInt16) -> Void) {
        listener.stateUpdateHandler = { [weak self] state in
            guard let self else { return }
            if case .ready = state, let port = self.listener.port {
                self._port = port.rawValue
                onReady(port.rawValue)
            }
        }
        listener.start(queue: queue)
    }

    private func accept(_ conn: NWConnection) {
        conn.stateUpdateHandler = { [weak self, weak conn] state in
            guard let self, let conn else { return }
            switch state {
            case .ready:
                self.queue.async {
                    self.clients.append(conn)
                    if let config = self.lastConfig {
                        var msg = Data([0x01])
                        msg.append(config)
                        let wsCtx = NWConnection.ContentContext(
                            identifier: "binaryFrame",
                            metadata: [NWProtocolWebSocket.Metadata(opcode: .binary)]
                        )
                        conn.send(content: msg, contentContext: wsCtx,
                                  isComplete: true, completion: .idempotent)
                    }
                }
                self.receive(on: conn)
            case .failed, .cancelled:
                self.queue.async {
                    self.clients.removeAll { $0 === conn }
                }
            default:
                break
            }
        }
        conn.start(queue: queue)
    }

    /// Keep the receive loop alive so the WS stack can process control frames.
    private func receive(on conn: NWConnection) {
        conn.receiveMessage { [weak self, weak conn] _, ctx, _, error in
            guard let self, let conn else { return }
            if let error {
                logErr("ws client receive error: \(error)")
                return
            }
            if let ctx, !ctx.isFinal { return }
            self.receive(on: conn)
        }
    }

    /// Broadcast [tag][payload] to all connected clients as a binary WS frame.
    func broadcast(tag: UInt8, payload: Data) {
        var msg = Data([tag])
        msg.append(payload)
        let wsCtx = NWConnection.ContentContext(
            identifier: "binaryFrame",
            metadata: [NWProtocolWebSocket.Metadata(opcode: .binary)]
        )
        queue.async { [weak self] in
            guard let self else { return }
            if tag == 0x01 { self.lastConfig = payload }
            for conn in self.clients {
                conn.send(
                    content: msg,
                    contentContext: wsCtx,
                    isComplete: true,
                    completion: .idempotent
                )
            }
        }
    }
}

// MARK: - NAL emitter (VT path)

/// Wraps a VTCompressionSession and broadcasts Annex-B NAL units over
/// the provided WebSocket server. Created lazily on the first IOSurface
/// arrival so the session dimensions match the actual surface size.
final class NalEmitter {
    private var session: VTCompressionSession?
    private var lastFormatDesc: CMFormatDescription?
    private var frameCount = 0
    private let wsServer: FrameWebSocketServer

    init(width: Int, height: Int, bitrate: Int, wsServer: FrameWebSocketServer) {
        self.wsServer = wsServer
        var vtSession: VTCompressionSession?
        let status = VTCompressionSessionCreate(
            allocator: nil,
            width: Int32(width),
            height: Int32(height),
            codecType: kCMVideoCodecType_H264,
            encoderSpecification: nil,
            imageBufferAttributes: nil,
            compressedDataAllocator: nil,
            outputCallback: nalOutputCallback,
            refcon: Unmanaged.passUnretained(self).toOpaque(),
            compressionSessionOut: &vtSession
        )
        guard status == noErr, let s = vtSession else {
            logErr("VTCompressionSessionCreate failed: \(status)")
            return
        }
        VTSessionSetProperty(s, key: kVTCompressionPropertyKey_RealTime, value: kCFBooleanTrue)
        VTSessionSetProperty(s, key: kVTCompressionPropertyKey_AllowFrameReordering, value: kCFBooleanFalse)
        VTSessionSetProperty(s, key: kVTCompressionPropertyKey_ProfileLevel,
                             value: kVTProfileLevel_H264_High_AutoLevel)
        let bitrateRef = bitrate as CFTypeRef
        VTSessionSetProperty(s, key: kVTCompressionPropertyKey_AverageBitRate, value: bitrateRef)
        VTCompressionSessionPrepareToEncodeFrames(s)
        session = s
        logErr("VTCompressionSession created \(width)x\(height) bitrate=\(bitrate)")
    }

    func encodePixelBuffer(_ pixelBuffer: CVPixelBuffer, pts: CMTime) {
        guard let s = session else { return }
        VTCompressionSessionEncodeFrame(
            s,
            imageBuffer: pixelBuffer,
            presentationTimeStamp: pts,
            duration: .invalid,
            frameProperties: nil,
            sourceFrameRefcon: nil,
            infoFlagsOut: nil
        )
    }

    func invalidate() {
        if let s = session {
            VTCompressionSessionInvalidate(s)
        }
        session = nil
    }

    /// Called from the VT output callback. Extracts SPS/PPS when the
    /// format description changes, converts AVCC payload to Annex-B,
    /// and broadcasts tagged binary frames over the WebSocket server.
    ///
    /// Tag protocol:
    ///   0x01 — SPS+PPS description (broadcast when format-desc changes)
    ///   0x02 — IDR keyframe NAL (NAL unit type 5 present)
    ///   0x03 — delta frame NAL
    fileprivate func handleOutput(
        sampleBuffer: CMSampleBuffer,
        flags: VTEncodeInfoFlags
    ) {
        guard let dataBuffer = CMSampleBufferGetDataBuffer(sampleBuffer) else { return }

        let currentDesc = CMSampleBufferGetFormatDescription(sampleBuffer)
        let descChanged = currentDesc != nil && currentDesc !== lastFormatDesc
        lastFormatDesc = currentDesc

        if descChanged, let desc = currentDesc {
            var paramCount = 0
            CMVideoFormatDescriptionGetH264ParameterSetAtIndex(
                desc, parameterSetIndex: 0, parameterSetPointerOut: nil,
                parameterSetSizeOut: nil, parameterSetCountOut: &paramCount,
                nalUnitHeaderLengthOut: nil
            )
            var paramBlob = Data()
            for idx in 0..<paramCount {
                var ptr: UnsafePointer<UInt8>?
                var size = 0
                let rc = CMVideoFormatDescriptionGetH264ParameterSetAtIndex(
                    desc, parameterSetIndex: idx,
                    parameterSetPointerOut: &ptr,
                    parameterSetSizeOut: &size,
                    parameterSetCountOut: nil,
                    nalUnitHeaderLengthOut: nil
                )
                if rc == noErr, let p = ptr, size > 0 {
                    paramBlob.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
                    paramBlob.append(p, count: size)
                }
            }
            if !paramBlob.isEmpty {
                wsServer.broadcast(tag: 0x01, payload: paramBlob)
            }
        }

        let totalLength = CMBlockBufferGetDataLength(dataBuffer)
        var rawData = Data(count: totalLength)
        rawData.withUnsafeMutableBytes { ptr in
            guard let base = ptr.baseAddress else { return }
            CMBlockBufferCopyDataBytes(dataBuffer, atOffset: 0,
                                       dataLength: totalLength, destination: base)
        }

        var nalBlob = Data()
        var isKeyframe = false
        var offset = 0
        while offset + 4 <= totalLength {
            let nalLength = Int(rawData[offset]) << 24
                          | Int(rawData[offset + 1]) << 16
                          | Int(rawData[offset + 2]) << 8
                          | Int(rawData[offset + 3])
            offset += 4
            guard nalLength > 0, offset + nalLength <= totalLength else { break }
            nalBlob.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
            let nalStart = offset
            nalBlob.append(rawData[nalStart..<(nalStart + nalLength)])
            let nalType = (rawData[nalStart]) & 0x1F
            if nalType == 5 { isKeyframe = true }
            offset += nalLength
        }

        guard !nalBlob.isEmpty else { return }

        frameCount += 1
        if frameCount <= 3 || frameCount % 60 == 0 {
            logErr("nal frame #\(frameCount) \(nalBlob.count) bytes keyframe=\(isKeyframe)")
        }

        wsServer.broadcast(tag: isKeyframe ? 0x02 : 0x03, payload: nalBlob)
    }
}

// C-compatible VT output callback.
private func nalOutputCallback(
    outputCallbackRefCon: UnsafeMutableRawPointer?,
    sourceFrameRefCon: UnsafeMutableRawPointer?,
    status: OSStatus,
    infoFlags: VTEncodeInfoFlags,
    sampleBuffer: CMSampleBuffer?
) {
    guard status == noErr, let sb = sampleBuffer else {
        if status != noErr { logErr("VT encode error: \(status)") }
        return
    }
    guard let refcon = outputCallbackRefCon else { return }
    let emitter = Unmanaged<NalEmitter>.fromOpaque(refcon).takeUnretainedValue()
    emitter.handleOutput(sampleBuffer: sb, flags: infoFlags)
}

// MARK: - Framebuffer capture coordinator

/// Drives the SimulatorKit framebuffer callback loop. On first IOSurface
/// arrival it creates the VT compression session (dimensions come from the
/// surface itself, so the session size is always exact). Subsequent frames
/// convert the IOSurface to a CVPixelBuffer and submit to VT without copying
/// pixel data; VT holds a reference until encoding completes.
///
/// The idle keep-alive re-submits the last CVPixelBuffer with a bumped PTS
/// whenever the framebuffer has been silent for more than `idleThresholdMs`.
/// SimulatorKit only fires the frame callback when the screen changes, so an
/// idle simulator produces zero frames without this keep-alive.
final class FramebufferCapture {
    private let device: NSObject
    private let wsServer: FrameWebSocketServer
    private let bitrate: Int
    private let maxSize: Int
    private let screenQueue = DispatchQueue(label: "dev.atomyx.simcapture.screen")

    private var emitter: NalEmitter?
    private var callbackUUID: UUID?
    private var descriptors: [NSObject] = []

    private var lastPixelBuffer: CVPixelBuffer?
    private var lastPts: CMTime = .zero
    private var lastFrameTime: TimeInterval = 0
    private var frameCount: Int = 0

    private let idleThresholdMs: Int = 80
    private var idleTimer: DispatchSourceTimer?

    init(device: NSObject, wsServer: FrameWebSocketServer, bitrate: Int, maxSize: Int) {
        self.device = device
        self.wsServer = wsServer
        self.bitrate = bitrate
        self.maxSize = maxSize
    }

    /// Wires up the framebuffer callback via SimulatorKit. Calls `onReady`
    /// (on an arbitrary queue) when the first surface descriptor is registered
    /// so the caller knows callbacks are active.
    func start(onReady: @escaping () -> Void) {
        screenQueue.async { [self] in
            self.wireCallbacks(onReady: onReady)
        }
    }

    private func wireCallbacks(onReady: @escaping () -> Void) {
        // Activate device I/O ports so framebuffer descriptors become visible.
        guard let ioResult = device.perform(NSSelectorFromString("io")),
              let io = ioResult.takeUnretainedValue() as? NSObject else {
            fail("device.io returned nil — cannot access framebuffer")
        }
        io.perform(NSSelectorFromString("updateIOPorts"))

        guard let ports = io.value(forKey: "deviceIOPorts") as? [NSObject] else {
            fail("deviceIOPorts is nil — SimulatorKit framebuffer not accessible")
        }

        let framebufferID = "com.apple.framebuffer.display"
        var found: [NSObject] = []
        for port in ports {
            guard let idResult = port.perform(NSSelectorFromString("portIdentifier")),
                  let idStr = idResult.takeUnretainedValue() as? String else { continue }
            if idStr == framebufferID {
                if let descResult = port.perform(NSSelectorFromString("descriptor")),
                   let desc = descResult.takeUnretainedValue() as? NSObject {
                    found.append(desc)
                }
            }
        }

        if found.isEmpty {
            fail("no framebuffer port '\(framebufferID)' found — is the simulator's display active?")
        }
        descriptors = found
        logErr("found \(found.count) framebuffer descriptor(s)")

        let uuid = UUID()
        callbackUUID = uuid

        let regSel = NSSelectorFromString(
            "registerScreenCallbacksWithUUID:callbackQueue:frameCallback:" +
            "surfacesChangedCallback:propertiesChangedCallback:"
        )

        // frameCallback: fires when a new rendered frame is available.
        let frameBlock: @convention(block) () -> Void = { [weak self] in
            self?.captureLatest()
        }
        // surfacesChangedCallback: fires when the IOSurface backing changes
        // (e.g. device rotation). Trigger a fresh capture to avoid a stale ref.
        let surfacesBlock: @convention(block) () -> Void = { [weak self] in
            self?.captureLatest()
        }
        // propertiesChangedCallback: fires on display-property changes; unused.
        let propsBlock: @convention(block) () -> Void = {}

        let frameObj = frameBlock as AnyObject
        let surfacesObj = surfacesBlock as AnyObject
        let propsObj = propsBlock as AnyObject

        typealias RegFn = @convention(c) (
            NSObject, Selector,
            NSUUID, DispatchQueue,
            AnyObject, AnyObject, AnyObject
        ) -> Void

        guard let descClass = object_getClass(found[0]) else {
            fail("could not determine class of framebuffer descriptor")
        }
        let regImp = class_getMethodImplementation(descClass, regSel)
        let regFn = unsafeBitCast(regImp, to: RegFn.self)

        for desc in found {
            regFn(desc, regSel,
                  uuid as NSUUID,
                  screenQueue,
                  frameObj, surfacesObj, propsObj)
        }
        logErr("framebuffer callbacks registered uuid=\(uuid.uuidString)")
        onReady()

        startIdleKeepAlive()
    }

    /// Picks the largest IOSurface from all registered descriptors (largest
    /// by pixel area = main display; secondary planes / overlays are smaller).
    /// Converts to CVPixelBuffer without copying pixels, then submits to VT.
    /// Creates the NalEmitter lazily on the first frame using the surface's
    /// actual dimensions, guaranteeing the VT session size is exact.
    private func captureLatest() {
        let surfSel = NSSelectorFromString("framebufferSurface")

        var bestSurface: IOSurface?
        var bestArea = 0

        for desc in descriptors {
            guard let result = desc.perform(surfSel),
                  let obj = result.takeUnretainedValue() as? NSObject else { continue }
            // Bridge the ObjC object to Swift's IOSurface value type.
            let surf = unsafeDowncast(obj, to: IOSurface.self)
            let area = IOSurfaceGetWidth(surf) * IOSurfaceGetHeight(surf)
            if area > bestArea {
                bestArea = area
                bestSurface = surf
            }
        }

        guard let surf = bestSurface, bestArea > 0 else { return }

        let surfW = IOSurfaceGetWidth(surf)
        let surfH = IOSurfaceGetHeight(surf)

        // Create the emitter on the first frame using the surface's reported
        // dimensions. This guarantees the VT session matches the actual pixels.
        if emitter == nil {
            let scale = Double(maxSize) / Double(max(surfW, surfH))
            let encW = max(2, Int((Double(surfW) * min(1.0, scale)).rounded()))
            let encH = max(2, Int((Double(surfH) * min(1.0, scale)).rounded()))
            logErr("framebuffer surface \(surfW)x\(surfH) encode \(encW)x\(encH) bitrate=\(bitrate)")
            let pixFmt = IOSurfaceGetPixelFormat(surf)
            logErr("IOSurface pixel format: \(String(format: "0x%08X", pixFmt)) (32BGRA=\(String(format: "0x%08X", kCVPixelFormatType_32BGRA)))")
            emitter = NalEmitter(width: encW, height: encH, bitrate: bitrate, wsServer: wsServer)
        }

        guard let enc = emitter else { return }

        // Wrap the IOSurface in a CVPixelBuffer without copying pixel data.
        // VT holds a reference until the encode callback fires; the IOSurface
        // is owned by SimulatorKit, so we must not release it before then.
        // CVPixelBufferCreateWithIOSurface retains the surface for us.
        let attrs: [String: Any] = [
            kCVPixelBufferIOSurfacePropertiesKey as String: [:] as [String: Any]
        ]
        // CVPixelBufferCreateWithIOSurface wraps the IOSurface without copying
        // pixel data. The resulting CVPixelBuffer retains the IOSurface until
        // VT finishes encoding and releases its reference.
        var unmanagedBuffer: Unmanaged<CVPixelBuffer>?
        let cvStatus = CVPixelBufferCreateWithIOSurface(
            kCFAllocatorDefault,
            surf,
            attrs as CFDictionary,
            &unmanagedBuffer
        )
        guard cvStatus == kCVReturnSuccess, let pb = unmanagedBuffer?.takeRetainedValue() else {
            logErr("CVPixelBufferCreateWithIOSurface failed: \(cvStatus)")
            return
        }

        let now = CMClockGetTime(CMClockGetHostTimeClock())
        enc.encodePixelBuffer(pb, pts: now)

        lastPixelBuffer = pb
        lastPts = now
        lastFrameTime = CACurrentMediaTime()
        frameCount += 1
        if frameCount <= 3 || frameCount % 120 == 0 {
            logErr("framebuffer frame #\(frameCount) \(surfW)x\(surfH)")
        }
    }

    private func startIdleKeepAlive() {
        let intervalMs = idleThresholdMs
        let interval = DispatchTimeInterval.milliseconds(intervalMs)
        let timer = DispatchSource.makeTimerSource(queue: screenQueue)
        timer.schedule(deadline: .now() + interval, repeating: interval)
        timer.setEventHandler { [weak self] in self?.tickIdle() }
        timer.resume()
        idleTimer = timer
    }

    private func tickIdle() {
        guard let pb = lastPixelBuffer, let enc = emitter else { return }
        let now = CACurrentMediaTime()
        if now - lastFrameTime < Double(idleThresholdMs) / 1000.0 { return }

        // Re-submit the last pixel buffer with a bumped PTS. VT produces a
        // tiny P-frame with no pixel diff, keeping the decoder pipeline warm.
        let increment = CMTime(value: CMTimeValue(idleThresholdMs), timescale: 1000)
        let nextPts = CMTimeAdd(lastPts, increment)
        enc.encodePixelBuffer(pb, pts: nextPts)
        lastPts = nextPts
    }

    func stop() {
        idleTimer?.cancel()
        idleTimer = nil

        if let uuid = callbackUUID {
            let unregSel = NSSelectorFromString("unregisterScreenCallbacksWithUUID:")
            for desc in descriptors {
                desc.perform(unregSel, with: uuid as NSUUID)
            }
        }
        emitter?.invalidate()
        emitter = nil
    }
}

// MARK: - Main

let args = parseArgs() ?? { fail("missing --udid <UDID>") }()

_ = NSApplication.shared
NSApp.setActivationPolicy(.prohibited)

// ARC retention for objects whose lifetime must span the run loop.
var liveWsServer: FrameWebSocketServer?
var liveCapture: FramebufferCapture?

Task {
    do {
        let devDir = developerDir()
        logErr("developer dir: \(devDir)")

        bootstrapPrivateFrameworks(devDir: devDir)

        guard let simDevice = resolveSimDevice(udid: args.udid, devDir: devDir) else {
            fail("could not resolve SimDevice for UDID \(args.udid) — is the simulator booted?")
        }

        // Start the WS server before registering framebuffer callbacks so the
        // handshake line appears on stdout before any frames arrive.
        let wsServer = try FrameWebSocketServer()
        liveWsServer = wsServer

        let assignedPort: UInt16 = await withCheckedContinuation { continuation in
            wsServer.start { port in
                continuation.resume(returning: port)
            }
        }

        // Handshake: Rust reads this line to learn the WS port.
        emitHandshake("{\"event\":\"listen\",\"port\":\(assignedPort),\"transport\":\"ws\"}")
        logErr("WS server listening on 127.0.0.1:\(assignedPort)")

        let capture = FramebufferCapture(
            device: simDevice,
            wsServer: wsServer,
            bitrate: args.bitrate,
            maxSize: args.maxSize
        )
        liveCapture = capture

        // Register framebuffer callbacks. The closure fires when the first
        // descriptor is wired so we can log readiness.
        await withCheckedContinuation { (continuation: CheckedContinuation<Void, Never>) in
            capture.start {
                logErr("framebuffer capture active")
                continuation.resume()
            }
        }

        logErr("capture started for UDID \(args.udid)")
    } catch {
        fail("setup failed: \(error.localizedDescription)")
    }
}

signal(SIGINT) { _ in
    liveCapture?.stop()
    exit(0)
}
signal(SIGTERM) { _ in
    liveCapture?.stop()
    exit(0)
}
NSApp.run()
