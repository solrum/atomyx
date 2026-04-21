import Foundation
import Darwin

/// TCP command server. Single responsibility: accept connections on
/// 127.0.0.1:<port>, read line-delimited JSON requests, dispatch each
/// to the provided handler closure, write line-delimited JSON responses.
///
/// Runs accept loop on a background queue. Each incoming client is
/// served sequentially — one connection at a time, no parallel
/// dispatch. Agents drive the session through a single TCP connection.
///
/// Threading policy:
///   - Public XCUITest gesture primitives (`tap`, `press`, `swipe`)
///     work safely from background queues on modern Xcode (they
///     internally serialize to main when they need to).
///   - The private-path `dispatchPointer` must NOT run on the main
///     thread: it calls `synthesizeEvent:completion:` whose
///     completion block is posted to the main queue, and we wait
///     on a semaphore for it — if the handler itself were on main,
///     main would be blocked and the completion could never fire
///     (classic main-queue deadlock).
///
/// Per-command threading: each `CommandHandler` declares
/// `requiresMainThread`. Handlers that need main (XCUI tree
/// queries, `XCUIApplication.launch`, etc.) set it to `true` —
/// the server wraps their handler call in
/// `DispatchQueue.main.sync`. Handlers that need main to stay
/// FREE (gesture dispatch waiting on a main-queue completion)
/// set it to `false` so they run on `acceptQueue` directly.
final class CommandServer {
    private let port: UInt16
    private let handler: (Request) -> Response
    private let requiresMainThread: (Request) -> Bool
    private var listenFd: Int32 = -1
    private let acceptQueue = DispatchQueue(label: "atomyx.driver.accept", qos: .userInitiated)

    /// `requiresMainThread` defaults to `{ _ in true }` — the
    /// conservative safe default for any handler whose threading
    /// preference is unknown. The serve entry point in
    /// `AtomyxDriverAgent` overrides it with a closure that
    /// reads each registered handler's own
    /// `CommandHandler.requiresMainThread`.
    init(
        port: UInt16,
        handler: @escaping (Request) -> Response,
        requiresMainThread: @escaping (Request) -> Bool = { _ in true }
    ) {
        self.port = port
        self.handler = handler
        self.requiresMainThread = requiresMainThread
    }

    func start() throws {
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else {
            throw NSError(domain: "atomyx.driver", code: 1, userInfo: [
                NSLocalizedDescriptionKey: "socket() failed: \(String(cString: strerror(errno)))",
            ])
        }

        var yes: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_REUSEADDR, &yes, socklen_t(MemoryLayout<Int32>.size))

        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = in_port_t(port).bigEndian
        addr.sin_addr = in_addr(s_addr: inet_addr("127.0.0.1"))
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)

        let bindResult = withUnsafePointer(to: &addr) { ptr -> Int32 in
            ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                Darwin.bind(fd, sa, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        guard bindResult == 0 else {
            let msg = String(cString: strerror(errno))
            close(fd)
            throw NSError(domain: "atomyx.driver", code: 2, userInfo: [
                NSLocalizedDescriptionKey: "bind() failed on port \(port): \(msg)",
            ])
        }

        guard Darwin.listen(fd, 4) == 0 else {
            let msg = String(cString: strerror(errno))
            close(fd)
            throw NSError(domain: "atomyx.driver", code: 3, userInfo: [
                NSLocalizedDescriptionKey: "listen() failed: \(msg)",
            ])
        }

        listenFd = fd
        acceptQueue.async { [weak self] in self?.acceptLoop() }
    }

    private func acceptLoop() {
        while listenFd >= 0 {
            var clientAddr = sockaddr_in()
            var len = socklen_t(MemoryLayout<sockaddr_in>.size)
            let clientFd = withUnsafeMutablePointer(to: &clientAddr) { ptr -> Int32 in
                ptr.withMemoryRebound(to: sockaddr.self, capacity: 1) { sa in
                    Darwin.accept(listenFd, sa, &len)
                }
            }
            if clientFd < 0 {
                if errno == EINTR { continue }
                NSLog("[atomyx] accept() failed: \(String(cString: strerror(errno)))")
                return
            }
            serveClient(clientFd)
            close(clientFd)
        }
    }

    private func serveClient(_ fd: Int32) {
        var buffer = Data()
        let chunkSize = 8192
        var chunk = [UInt8](repeating: 0, count: chunkSize)

        while true {
            let n = recv(fd, &chunk, chunkSize, 0)
            if n <= 0 { return }
            buffer.append(chunk, count: n)

            while let newlineIdx = buffer.firstIndex(of: 0x0A) {
                let line = buffer.subdata(in: buffer.startIndex..<newlineIdx)
                buffer.removeSubrange(buffer.startIndex...newlineIdx)

                guard let text = String(data: line, encoding: .utf8),
                      let request = Request.decode(text) else {
                    let errBytes = Array("{\"ok\":false,\"error\":\"bad json\"}\n".utf8)
                    _ = Darwin.send(fd, errBytes, errBytes.count, 0)
                    continue
                }

                // Per-request routing: most commands need the
                // main thread (XCUIApplication.launch, XCUIElement
                // queries, etc.). `DispatchPointerCommand` does
                // NOT — it needs main to stay free for its
                // synthesized-event completion block.
                let response: Response
                if requiresMainThread(request) {
                    if Thread.isMainThread {
                        response = handler(request)
                    } else {
                        var r: Response = .error(id: request.id, message: "dispatch failed")
                        DispatchQueue.main.sync { r = handler(request) }
                        response = r
                    }
                } else {
                    response = handler(request)
                }

                var payload = response.encode()
                payload.append(0x0A)
                _ = payload.withUnsafeBytes { raw -> Int in
                    Darwin.send(fd, raw.baseAddress, raw.count, 0)
                }
            }
        }
    }
}
