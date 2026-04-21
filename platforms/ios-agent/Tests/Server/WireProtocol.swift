import Foundation

/// Line-delimited JSON request/response types. Shared by CommandServer
/// and every CommandHandler.
///
/// Wire format (one per line):
///   → { "id": N, "type": "...", "args": { ... } }
///   ← { "id": N, "ok": true,  "data":  { ... } }
///   ← { "id": N, "ok": false, "error": "..."    }

struct Request {
    let id: Int
    let type: String
    let args: [String: Any]

    static func decode(_ json: String) -> Request? {
        guard let data = json.data(using: .utf8) else { return nil }
        guard let obj = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] else {
            return nil
        }
        guard let id = obj["id"] as? Int,
              let type = obj["type"] as? String else { return nil }
        let args = (obj["args"] as? [String: Any]) ?? [:]
        return Request(id: id, type: type, args: args)
    }
}

struct Response {
    let id: Int
    let ok: Bool
    let data: [String: Any]
    let error: String?

    static func ok(id: Int, data: [String: Any] = [:]) -> Response {
        Response(id: id, ok: true, data: data, error: nil)
    }

    static func error(id: Int, message: String) -> Response {
        Response(id: id, ok: false, data: [:], error: message)
    }

    func encode() -> Data {
        var obj: [String: Any] = ["id": id, "ok": ok]
        if ok {
            obj["data"] = data
        } else {
            obj["error"] = error ?? ""
        }
        return (try? JSONSerialization.data(withJSONObject: obj)) ?? Data("{\"ok\":false}".utf8)
    }
}
