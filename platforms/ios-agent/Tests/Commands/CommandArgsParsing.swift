import Foundation

/// Coerce a JSON-decoded request arg into a `Double`. Tolerant
/// of the three shapes we observe across language clients: a
/// boxed `Double`, a boxed `Int` (host serializers that drop the
/// fractional part for whole numbers), or a `String` (literal
/// numeric text passed without quoting on the host). Returns
/// `nil` for any other shape — callers raise the appropriate
/// "missing field" error so the host validator gets a precise
/// diagnostic.
enum CommandArgs {
    static func numeric(_ value: Any?) -> Double? {
        if let d = value as? Double { return d }
        if let i = value as? Int { return Double(i) }
        if let s = value as? String { return Double(s) }
        return nil
    }
}
