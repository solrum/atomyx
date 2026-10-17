import XCTest

final class KeyboardSpaceTests: XCTestCase {
    private let size = CGSize(width: 440, height: 956)
    private let scriptLandscapeLeft = InterfaceOrientationCode.code(for: .landscapeLeft)
    private let scriptLandscapeRight = InterfaceOrientationCode.code(for: .landscapeRight)

    private func rect(_ l: Int, _ t: Int, _ r: Int, _ b: Int) -> [String: Int] {
        return ["left": l, "top": t, "right": r, "bottom": b]
    }

    private func node(
        _ type: String,
        _ bounds: [String: Int],
        visible: Bool? = nil,
        children: [[String: Any]] = []
    ) -> [String: Any] {
        var dict: [String: Any] = ["elementType": type, "bounds": bounds]
        if let visible = visible { dict["visible"] = visible }
        if !children.isEmpty { dict["children"] = children }
        return dict
    }

    private func bounds(_ node: [String: Any]) -> [String: Int]? {
        return node["bounds"] as? [String: Int]
    }

    private func children(_ node: [String: Any]) -> [[String: Any]] {
        return node["children"] as? [[String: Any]] ?? []
    }

    /// Application, then a full-screen window in interface space, then the
    /// portrait-shaped wrapper, with the keyboard five levels below the
    /// application and the key eight levels below it.
    private func dump(keyboard: [String: Int], key: [String: Int]) -> [String: Any] {
        let keyNode = node("button", key, visible: true)
        let inner = node("other", keyboard, visible: true, children: [
            node("other", keyboard, visible: true, children: [keyNode]),
        ])
        let keyboardNode = node("keyboard", keyboard, visible: true, children: [inner])
        let wrapper = node("other", rect(0, 0, 440, 956), visible: true, children: [
            node("other", rect(0, 0, 440, 956), visible: true, children: [
                node("other", rect(0, 0, 440, 956), visible: true, children: [keyboardNode]),
            ]),
        ])
        let window = node("window", rect(0, 0, 956, 440), visible: true, children: [wrapper])
        return node("application", rect(0, 0, 956, 440), visible: true, children: [window])
    }

    private func walk(_ tree: [String: Any], _ path: [Int]) -> [String: Any] {
        return path.reduce(tree) { children($0)[$1] }
    }

    private func assertDump(
        _ out: [String: Any],
        keyboard: [String: Int],
        key: [String: Int],
        file: StaticString = #filePath,
        line: UInt = #line
    ) {
        let window = walk(out, [0])
        XCTAssertEqual(bounds(window), rect(0, 0, 956, 440), file: file, line: line)
        XCTAssertEqual(bounds(walk(out, [0, 0])), rect(0, 0, 956, 440), file: file, line: line)
        let keyboardNode = walk(out, [0, 0, 0, 0, 0])
        XCTAssertEqual(keyboardNode["elementType"] as? String, "keyboard", file: file, line: line)
        XCTAssertEqual(bounds(keyboardNode), keyboard, file: file, line: line)
        let keyNode = walk(keyboardNode, [0, 0, 0])
        XCTAssertEqual(bounds(keyNode), key, file: file, line: line)
        XCTAssertEqual(keyNode["visible"] as? Bool, true, file: file, line: line)
    }

    // MARK: - Point mapping

    func testPortraitLeavesPointsUnchanged() {
        let p = CGPoint(x: 264, y: 766)
        XCTAssertEqual(
            InterfaceSpace.map(p, interfaceOrientationCode: InterfaceOrientationCode.portrait, portraitSize: size), p)
    }

    func testScriptLandscapeLeftMapsPortraitPointsToXFromYAndYFromWidthMinusX() {
        let code = scriptLandscapeLeft
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 140, y: 117), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 117, y: 300))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 176, y: 189), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 189, y: 264))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 2, y: 117), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 117, y: 438))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 181, y: 839), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 839, y: 259))
    }

    func testScriptLandscapeRightMapsPortraitPointsToHeightMinusYAndXFromX() {
        let code = scriptLandscapeRight
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 264, y: 766), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 190, y: 264))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 300, y: 839), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 117, y: 300))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 259, y: 117), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 839, y: 259))
        XCTAssertEqual(
            InterfaceSpace.map(CGPoint(x: 438, y: 839), interfaceOrientationCode: code, portraitSize: size),
            CGPoint(x: 117, y: 438))
    }

    func testPortraitUpsideDownLeavesPointsUnchanged() {
        let p = CGPoint(x: 264, y: 766)
        XCTAssertEqual(
            InterfaceSpace.map(
                p, interfaceOrientationCode: InterfaceOrientationCode.portraitUpsideDown, portraitSize: size), p)
    }

    func testUnknownSizeLeavesPointsUnchanged() {
        let p = CGPoint(x: 264, y: 766)
        XCTAssertEqual(InterfaceSpace.map(p, interfaceOrientationCode: scriptLandscapeLeft, portraitSize: nil), p)
    }

    func testEveryLandscapeCodeInvertsPortraitSpace() {
        let p = CGPoint(x: 478, y: 178)
        for code in [scriptLandscapeLeft, scriptLandscapeRight] {
            let portrait = PortraitSpace.map(p, interfaceOrientationCode: code, portraitSize: size)
            XCTAssertEqual(InterfaceSpace.map(portrait, interfaceOrientationCode: code, portraitSize: size), p)
        }
    }

    // MARK: - Tree remap

    func testScriptLandscapeLeftRemapsEverythingBelowTheWindow() {
        let tree = dump(keyboard: rect(2, 117, 181, 839), key: rect(140, 117, 176, 189))
        let out = KeyboardSpace.remap(tree, interfaceOrientationCode: scriptLandscapeLeft, portraitSize: size)
        assertDump(out, keyboard: rect(117, 259, 839, 438), key: rect(117, 264, 189, 300))
    }

    func testScriptLandscapeRightRemapsEverythingBelowTheWindow() {
        let tree = dump(keyboard: rect(259, 117, 438, 839), key: rect(264, 766, 300, 839))
        let out = KeyboardSpace.remap(tree, interfaceOrientationCode: scriptLandscapeRight, portraitSize: size)
        assertDump(out, keyboard: rect(117, 259, 839, 438), key: rect(117, 264, 190, 300))
    }

    func testAppElementsOutsideTheKeyboardPathAreUnchanged() {
        var tree = dump(keyboard: rect(259, 117, 438, 839), key: rect(264, 766, 300, 839))
        var window = walk(tree, [0])
        var kids = children(window)
        kids.append(node("button", rect(10, 20, 110, 60), visible: true))
        window["children"] = kids
        tree["children"] = [window]

        let out = KeyboardSpace.remap(tree, interfaceOrientationCode: scriptLandscapeRight, portraitSize: size)

        XCTAssertEqual(bounds(walk(out, [0, 1])), rect(10, 20, 110, 60))
    }

    func testPortraitUpsideDownReturnsTheTreeUnchanged() {
        let tree = dump(keyboard: rect(259, 117, 438, 839), key: rect(264, 766, 300, 839))
        let out = KeyboardSpace.remap(
            tree, interfaceOrientationCode: InterfaceOrientationCode.portraitUpsideDown, portraitSize: size)
        XCTAssertEqual(bounds(walk(out, [0, 0])), rect(0, 0, 440, 956))
    }

    func testPortraitReturnsTheTreeUnchanged() {
        let tree = dump(keyboard: rect(259, 117, 438, 839), key: rect(264, 766, 300, 839))
        let out = KeyboardSpace.remap(
            tree, interfaceOrientationCode: InterfaceOrientationCode.portrait, portraitSize: size)
        XCTAssertEqual(bounds(walk(out, [0, 0])), rect(0, 0, 440, 956))
    }

    func testTreeWithoutKeyboardIsUnchanged() {
        let tree = node("application", rect(0, 0, 956, 440), children: [
            node("window", rect(0, 0, 956, 440)),
        ])
        let out = KeyboardSpace.remap(tree, interfaceOrientationCode: scriptLandscapeLeft, portraitSize: size)
        XCTAssertEqual(bounds(children(out)[0]), rect(0, 0, 956, 440))
    }

    func testKeyboardAlreadyInsideTheInterfaceScreenIsUnchanged() {
        let keyboard = node("keyboard", rect(0, 259, 956, 440))
        let tree = node("application", rect(0, 0, 956, 440), children: [
            node("window", rect(0, 0, 956, 440), children: [keyboard]),
        ])
        let out = KeyboardSpace.remap(tree, interfaceOrientationCode: scriptLandscapeLeft, portraitSize: size)
        XCTAssertEqual(bounds(walk(out, [0, 0])), rect(0, 259, 956, 440))
    }
}
