import XCTest

struct ClearTextChain {
    private let strategies: [ClearTextStrategy]

    init(strategies: [ClearTextStrategy]) {
        self.strategies = strategies
    }

    /// Run strategies in order. Returns the name of the winning strategy.
    func run(context: ClearContext) throws -> String {
        var tried: [String] = []
        for strategy in strategies {
            let result = try strategy.attempt(context: context)
            switch result {
            case .success(let strategyName):
                return strategyName
            case .skipped:
                continue
            case .attempted:
                tried.append(strategy.name)
                if verifyCleared(context: context) {
                    return strategy.name
                }
            }
        }
        let (lastValue, elementType) = readFocusedInfo(context: context)
        // Hardware keyboard is absent when the on-screen system keyboard is visible.
        let hasHardwareKeyboard = !context.app.keyboards.firstMatch.exists
        throw ClearTextChainError.allStrategiesFailed(
            ClearTextDiagnostic(
                strategiesTried: tried,
                lastValue: lastValue,
                focusedElementType: elementType,
                hasHardwareKeyboard: hasHardwareKeyboard
            )
        )
    }

    // Poll up to 500 ms for the focused field to report empty.
    private func verifyCleared(context: ClearContext) -> Bool {
        let pollInterval: TimeInterval = 0.05
        for _ in 0..<10 {
            guard let snapshot = try? context.app.snapshot() else {
                // App navigated away — field gone counts as cleared.
                return true
            }
            if let focused = findFocused(in: snapshot) {
                let value = focused.value as? String ?? ""
                let placeholder = focused.label
                let isEmpty = value.isEmpty || (!placeholder.isEmpty && value == placeholder)
                if isEmpty { return true }
            } else {
                return true
            }
            Thread.sleep(forTimeInterval: pollInterval)
        }
        return false
    }

    private func readFocusedInfo(context: ClearContext) -> (String?, String) {
        guard let snapshot = try? context.app.snapshot(),
              let focused = findFocused(in: snapshot) else {
            return (nil, "unknown")
        }
        return (focused.value as? String, String(describing: focused.elementType))
    }
}

/// Iterative DFS over an XCUIElementSnapshot tree to find the node with keyboard focus.
/// Iterative rather than recursive to avoid stack overflow on pathological Flutter/RN trees.
func findFocused(in snapshot: XCUIElementSnapshot) -> XCUIElementSnapshot? {
    var stack: [XCUIElementSnapshot] = [snapshot]
    while let node = stack.popLast() {
        if node.hasFocus { return node }
        for child in node.children.reversed() {
            stack.append(child)
        }
    }
    return nil
}
