struct ClearTextDiagnostic {
    let strategiesTried: [String]
    let lastValue: String?
    let focusedElementType: String
    let hasHardwareKeyboard: Bool
}

enum ClearTextChainError: Error {
    case allStrategiesFailed(ClearTextDiagnostic)
}
