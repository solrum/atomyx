import XCTest

final class KeyboardSpaceTests: XCTestCase {
    private let size = CGSize(width: 440, height: 956)
    private let landscapeLeft = InterfaceOrientationCode.landscapeLeft
    private let landscapeRight = InterfaceOrientationCode.landscapeRight

    private func rect(_ l: Int, _ t: Int, _ r: Int, _ b: Int) -> [String: Int] {
        return ["left": l, "top": t, "right": r, "bottom": b]
    }

    private func node(
        _ type: String,
        _ bounds: [String: Int],
        children: [[String: Any]] = []
    ) -> [String: Any] {
        var dict: [String: Any] = ["elementType": type, "bounds": bounds]
        if !children.isEmpty { dict["children"] = children }
        return dict
    }

    private func bounds(_ node: [String: Any]) -> [String: Int]? {
        return node["bounds"] as? [String: Int]
    }

    private func children(_ node: [String: Any]) -> [[String: Any]] {
        return node["children"] as? [[String: Any]] ?? []
    }

    private func keyboardTree() -> [String: Any] {
        let key = node("button", rect(264, 766, 300, 839))
        let keyboard = node("keyboard", rect(259, 0, 440, 956), children: [key])
        let wrapper = node("other", rect(259, 0, 440, 956), children: [keyboard])
        let appWindow = node("window", rect(0, 0, 956, 440), children: [
            node("button", rect(10, 20, 110, 60)),
        ])
        return node("application", rect(0, 0, 956, 440), children: [appWindow, wrapper])
    }

    // MARK: - Point mapping

    func testPortraitLeavesPointsUnchanged() {
        let p = CGPoint(x: 264, y: 766)
        XCTAssertEqual(
            InterfaceSpace.map(p, interfaceOrientationCode: InterfaceOrientationCode.portrait, portraitSize: size), p)
    }

    func testLandscapeLeftMapsMeasuredKeyboardCorners() {
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 264, y: 766), interfaceOrientationCode: landscapeLeft, portraitSize: size),
            CGPoint(x: 190, y: 264))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 300, y: 839), interfaceOrientationCode: landscapeLeft, portraitSize: size),
            CGPoint(x: 117, y: 300))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 259, y: 0), interfaceOrientationCode: landscapeLeft, portraitSize: size),
            CGPoint(x: 956, y: 259))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 440, y: 956), interfaceOrientationCode: landscapeLeft, portraitSize: size),
            CGPoint(x: 0, y: 440))
    }

    func testLandscapeRightMirrorsLandscapeLeft() {
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 264, y: 766), interfaceOrientationCode: landscapeRight, portraitSize: size),
            CGPoint(x: 766, y: 176))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 440, y: 956), interfaceOrientationCode: landscapeRight, portraitSize: size),
            CGPoint(x: 956, y: 0))
    }

    func testPortraitUpsideDownRotatesHalfATurn() {
        XCTAssertEqual(
            InterfaceSpace.map(
                CGPoint(x: 264, y: 766),
                interfaceOrientationCode: InterfaceOrientationCode.portraitUpsideDown,
                portraitSize: size),
            CGPoint(x: 176, y: 190))
    }

    func testUnknownSizeLeavesPointsUnchanged() {
        let p = CGPoint(x: 264, y: 766)
        XCTAssertEqual(InterfaceSpace.map(p, interfaceOrientationCode: landscapeLeft, portraitSize: nil), p)
    }

    func testEveryLandscapeCodeInvertsPortraitSpace() {
        let p = CGPoint(x: 478, y: 178)
        for code in [landscapeLeft, landscapeRight] {
            let portrait = PortraitSpace.map(p, interfaceOrientationCode: code, portraitSize: size)
            XCTAssertEqual(InterfaceSpace.map(portrait, interfaceOrientationCode: code, portraitSize: size), p)
        }
    }

    // MARK: - Tree remap

    func testLandscapeLeftRemapsOnlyTheKeyboardWindow() throws {
        let out = KeyboardSpace.remap(keyboardTree(), interfaceOrientationCode: landscapeLeft, portraitSize: size)
        let kids = children(out)
        XCTAssertEqual(bounds(kids[0]), rect(0, 0, 956, 440))
        XCTAssertEqual(bounds(children(kids[0])[0]), rect(10, 20, 110, 60))
        XCTAssertEqual(bounds(kids[1]), rect(0, 259, 956, 440))
        let keyboard = children(kids[1])[0]
        XCTAssertEqual(bounds(keyboard), rect(0, 259, 956, 440))
        XCTAssertEqual(bounds(children(keyboard)[0]), rect(117, 264, 190, 300))
    }

    func testLandscapeRightRemapsTheKeyboardWindow() {
        let out = KeyboardSpace.remap(keyboardTree(), interfaceOrientationCode: landscapeRight, portraitSize: size)
        let wrapper = children(out)[1]
        XCTAssertEqual(bounds(wrapper), rect(0, 0, 956, 181))
        XCTAssertEqual(bounds(children(children(wrapper)[0])[0]), rect(766, 140, 839, 176))
    }

    func testPortraitUpsideDownRemapsTheKeyboardWindow() {
        let out = KeyboardSpace.remap(
            keyboardTree(),
            interfaceOrientationCode: InterfaceOrientationCode.portraitUpsideDown,
            portraitSize: size)
        XCTAssertEqual(bounds(children(out)[1]), rect(0, 0, 181, 956))
    }

    func testPortraitReturnsTheTreeUnchanged() {
        let out = KeyboardSpace.remap(
            keyboardTree(), interfaceOrientationCode: InterfaceOrientationCode.portrait, portraitSize: size)
        XCTAssertEqual(bounds(children(out)[1]), rect(259, 0, 440, 956))
    }

    func testTreeWithoutKeyboardIsUnchanged() {
        let tree = node("application", rect(0, 0, 956, 440), children: [
            node("window", rect(0, 0, 956, 440)),
        ])
        let out = KeyboardSpace.remap(tree, interfaceOrientationCode: landscapeLeft, portraitSize: size)
        XCTAssertEqual(bounds(children(out)[0]), rect(0, 0, 956, 440))
    }

    func testKeyboardAlreadyInsideTheInterfaceScreenIsUnchanged() {
        let keyboard = node("keyboard", rect(0, 259, 956, 440))
        let tree = node("application", rect(0, 0, 956, 440), children: [
            node("window", rect(0, 0, 956, 440), children: [keyboard]),
        ])
        let out = KeyboardSpace.remap(tree, interfaceOrientationCode: landscapeLeft, portraitSize: size)
        XCTAssertEqual(bounds(children(children(out)[0])[0]), rect(0, 259, 956, 440))
    }
}
