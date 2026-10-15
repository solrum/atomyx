import Foundation

/// Turns issues that XCTest records while a command runs into an error
/// reply, so one failed UI action cannot end the serving test.
///
/// The serving test case forwards every recorded issue to `offer(_:)`.
/// While a command is active the issue is captured and the caller must
/// not pass it on to XCTest. Outside a command, `offer(_:)` returns
/// false and XCTest keeps its normal behavior.
///
/// Thread safety: issues arrive on whichever queue the failing XCTest
/// call ran on, which may differ from the queue that started the
/// command. All state is guarded by one lock.
final class IssueCapture {
    private static let maxMessageLength = 160
    private static let fallbackMessage = "ui action failed"

    private let lock = NSLock()
    private var active = false
    private var messages: [String] = []

    /// Returns true when the issue was captured for the active command.
    func offer(_ description: String) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        guard active else { return false }
        messages.append(description)
        return true
    }

    /// Runs `body` as one command and replaces its reply with an error
    /// when XCTest recorded an issue meanwhile.
    func guarded(_ request: Request, _ body: () -> Response) -> Response {
        begin()
        let response = body()
        let captured = end()
        guard let first = captured.first else { return response }
        return .error(id: request.id, message: Self.sanitize(first))
    }

    /// Reduces an XCTest issue description to a short message that names
    /// no framework types and carries no object addresses.
    static func sanitize(_ raw: String) -> String {
        var text = raw.split(whereSeparator: \.isNewline).first.map(String.init) ?? raw
        text = text.replacingOccurrences(
            of: #"<[^>]*0x[0-9a-fA-F]+[^>]*>"#, with: "", options: .regularExpression)
        text = text.replacingOccurrences(
            of: #"0x[0-9a-fA-F]+"#, with: "", options: .regularExpression)
        text = text.replacingOccurrences(
            of: #"^failed - "#, with: "", options: .regularExpression)
        text = text.replacingOccurrences(
            of: #"\b(XCUI|XCT|XC)[A-Za-z]*"#, with: "element", options: .regularExpression)
        text = text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        if text.isEmpty { return fallbackMessage }
        if text.count > maxMessageLength {
            text = String(text.prefix(maxMessageLength))
        }
        return text
    }

    private func begin() {
        lock.lock()
        active = true
        messages.removeAll()
        lock.unlock()
    }

    private func end() -> [String] {
        lock.lock()
        defer { lock.unlock() }
        active = false
        let captured = messages
        messages.removeAll()
        return captured
    }
}
