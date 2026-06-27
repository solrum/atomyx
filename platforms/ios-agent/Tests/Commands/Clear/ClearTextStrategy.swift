protocol ClearTextStrategy {
    var name: String { get }
    func attempt(context: ClearContext) throws -> ClearResult
}
