enum ClearResult {
    /// Strategy is certain the field is already empty, so the chain skips the verify gate.
    case success(strategy: String)
    /// Strategy performed an action, so the chain must run the verify gate.
    case attempted
    /// Strategy cannot apply to this context, so the chain moves to the next strategy.
    case skipped
}
