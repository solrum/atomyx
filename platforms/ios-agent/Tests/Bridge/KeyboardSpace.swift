import CoreGraphics
import Foundation

/// Maps points from portrait device space into interface space, the
/// space app elements are reported in. This is the inverse of
/// `PortraitSpace.map` for the same interface orientation code.
///
/// With `W` and `H` the portrait width and height in points:
///
/// - Portrait: a point is unchanged.
/// - Interface landscape-right (code 3): `(x, y)` becomes `(y, W - x)`.
/// - Interface landscape-left (code 4): `(x, y)` becomes `(H - y, x)`.
///
/// Portrait, portrait upside-down, any other code, and an unknown screen
/// size leave points unchanged, matching `PortraitSpace`.
enum InterfaceSpace {
    static func map(
        _ point: CGPoint,
        interfaceOrientationCode code: Int,
        portraitSize size: CGSize?
    ) -> CGPoint {
        guard let size = size else { return point }
        let w = min(size.width, size.height)
        let h = max(size.width, size.height)
        switch code {
        case InterfaceOrientationCode.landscapeRight:
            return CGPoint(x: point.y, y: w - point.x)
        case InterfaceOrientationCode.landscapeLeft:
            return CGPoint(x: h - point.y, y: point.x)
        default:
            return point
        }
    }
}

/// Rewrites the software keyboard subtree of a raw hierarchy dump from
/// portrait device space into interface space.
///
/// The keyboard is reported in portrait device space in every orientation
/// while the rest of the app is reported in interface space. The keyboard
/// sits under a full-screen window that is itself in interface space, so
/// the rewritten subtree starts at the shallowest node on the path to a
/// node of element type `keyboard` whose frame does not fit inside the
/// interface screen, which is the node with a portrait-shaped frame. That
/// node and every descendant are rewritten, and nothing else.
///
/// Only the two landscape orientations are remapped. Portrait and portrait
/// upside-down return the tree unchanged. A path whose nodes all fit inside
/// the interface screen is left alone, because it is already in interface
/// space.
enum KeyboardSpace {
    private static let keyboardType = "keyboard"

    /// Returns `tree` with its keyboard subtree remapped. The tree is
    /// returned unchanged in portrait orientations, for an unknown screen size, and
    /// when it holds no keyboard.
    static func remap(
        _ tree: [String: Any],
        interfaceOrientationCode code: Int,
        portraitSize size: CGSize?
    ) -> [String: Any] {
        guard let size = size,
              code == InterfaceOrientationCode.landscapeLeft
                  || code == InterfaceOrientationCode.landscapeRight
        else { return tree }
        let screen = CGSize(
            width: max(size.width, size.height),
            height: min(size.width, size.height))
        return descend(tree, code: code, size: size, screen: screen)
    }

    private static func descend(
        _ node: [String: Any],
        code: Int,
        size: CGSize,
        screen: CGSize
    ) -> [String: Any] {
        guard let kids = node["children"] as? [[String: Any]] else { return node }
        var out = node
        out["children"] = kids.map { child -> [String: Any] in
            guard containsKeyboard(child) else { return child }
            if fitsInside(child, width: screen.width, height: screen.height) {
                return descend(child, code: code, size: size, screen: screen)
            }
            return remapSubtree(child, code: code, size: size, screen: screen)
        }
        return out
    }

    private static func containsKeyboard(_ node: [String: Any]) -> Bool {
        var pending = [node]
        while let current = pending.popLast() {
            if current["elementType"] as? String == keyboardType { return true }
            if let kids = current["children"] as? [[String: Any]] {
                pending.append(contentsOf: kids)
            }
        }
        return false
    }

    private static func fitsInside(_ node: [String: Any], width: CGFloat, height: CGFloat) -> Bool {
        guard let b = bounds(node["bounds"]) else { return false }
        return b.minX >= 0 && b.minY >= 0 && b.maxX <= width && b.maxY <= height
    }

    private static func remapSubtree(
        _ node: [String: Any],
        code: Int,
        size: CGSize,
        screen: CGSize
    ) -> [String: Any] {
        var out = node
        for key in ["bounds", "accessibilityFrame"] {
            if let rect = bounds(node[key]) {
                let mapped = mapRect(rect, code: code, size: size)
                out[key] = encode(mapped)
                if key == "bounds", node["visible"] is Bool {
                    out["visible"] = !mapped.isEmpty
                        && mapped.intersects(CGRect(origin: .zero, size: screen))
                }
            }
        }
        if let kids = node["children"] as? [[String: Any]] {
            out["children"] = kids.map {
                remapSubtree($0, code: code, size: size, screen: screen)
            }
        }
        return out
    }

    private static func mapRect(_ rect: CGRect, code: Int, size: CGSize) -> CGRect {
        let a = InterfaceSpace.map(
            CGPoint(x: rect.minX, y: rect.minY), interfaceOrientationCode: code, portraitSize: size)
        let b = InterfaceSpace.map(
            CGPoint(x: rect.maxX, y: rect.maxY), interfaceOrientationCode: code, portraitSize: size)
        return CGRect(
            x: min(a.x, b.x), y: min(a.y, b.y),
            width: abs(a.x - b.x), height: abs(a.y - b.y))
    }

    private static func bounds(_ raw: Any?) -> CGRect? {
        guard let b = raw as? [String: Int],
              let l = b["left"], let t = b["top"], let r = b["right"], let bt = b["bottom"]
        else { return nil }
        return CGRect(x: l, y: t, width: r - l, height: bt - t)
    }

    private static func encode(_ rect: CGRect) -> [String: Int] {
        return [
            "left": Int(rect.minX.rounded()),
            "top": Int(rect.minY.rounded()),
            "right": Int(rect.maxX.rounded()),
            "bottom": Int(rect.maxY.rounded()),
        ]
    }
}
