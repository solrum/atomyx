package dev.atomyx.agent.control

/**
 * What the typer can read from an input node without keeping the node
 * itself. [identity] names the field (resource id, class and screen
 * bounds) so a later read can tell whether it is the same field.
 */
data class FieldSnapshot(
    val identity: String,
    val text: String,
    val secure: Boolean,
    val showingHint: Boolean,
) {
    /** Text the user would see as content, empty while a placeholder hint shows. */
    val content: String get() = if (showingHint) "" else text
}

enum class Landing { MATCH, NOTHING_LANDED, MISMATCH }

/**
 * Outcome of comparing a field read after a set-text with what was
 * requested. [landed] counts requested characters known to be present
 * and never exceeds the requested length.
 */
data class LandingVerdict(val landing: Landing, val landed: Int, val reason: String)

/**
 * Judges whether a text write reached the field it was aimed at.
 *
 * [expected] is the full value the field must hold afterwards: the
 * requested text, preceded by the prior content when the route appends.
 * [requestedLength] is the length of the requested text alone.
 *
 * Rules:
 *   - A different field than [before] never matches, whatever it holds.
 *   - A plain field must hold [expected] exactly.
 *   - A secure field renders masked characters, so it matches when it
 *     holds [expected] or a value of the same length.
 *   - An empty field, or one showing only its hint, reports
 *     NOTHING_LANDED so the caller can try another input strategy.
 *
 * Reasons never include field contents, because the text may be a secret.
 */
fun verifyLanding(
    expected: String,
    requestedLength: Int,
    before: FieldSnapshot,
    after: FieldSnapshot,
): LandingVerdict {
    if (after.identity != before.identity) {
        return LandingVerdict(
            Landing.MISMATCH,
            0,
            "input focus moved to a different field while typing",
        )
    }
    val actual = after.content
    val priorLength = (expected.length - requestedLength).coerceAtLeast(0)
    if (expected.isEmpty()) {
        return if (actual.isEmpty()) {
            LandingVerdict(Landing.MATCH, 0, "ok")
        } else {
            LandingVerdict(Landing.MISMATCH, 0, "the field still holds content after clearing it")
        }
    }
    if (actual.isEmpty()) {
        return LandingVerdict(Landing.NOTHING_LANDED, 0, "the field is empty after typing")
    }
    if (actual == expected) {
        return LandingVerdict(Landing.MATCH, requestedLength, "ok")
    }
    if (before.secure || after.secure) {
        return if (actual.length == expected.length) {
            LandingVerdict(Landing.MATCH, requestedLength, "ok")
        } else {
            LandingVerdict(
                Landing.MISMATCH,
                (actual.length - priorLength).coerceIn(0, requestedLength),
                "the secure field length does not match the requested length",
            )
        }
    }
    val common = expected.commonPrefixWith(actual).length
    return LandingVerdict(
        Landing.MISMATCH,
        (common - priorLength).coerceIn(0, requestedLength),
        "the focused field does not hold the requested text",
    )
}
