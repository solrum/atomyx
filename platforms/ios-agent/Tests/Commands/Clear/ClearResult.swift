enum ClearResult {
    /// Strategy is certain the field is already empty — chain skips the verify gate.
    case success(strategy: String)
    /// Strategy performed an action — chain must run the verify gate.
    case attempted
    /// Strategy cannot apply to this context — chain moves to the next strategy.
    case skipped
}
