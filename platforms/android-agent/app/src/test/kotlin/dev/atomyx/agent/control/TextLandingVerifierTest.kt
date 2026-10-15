package dev.atomyx.agent.control

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Test

class TextLandingVerifierTest {

    private fun field(
        text: String,
        id: String = "email",
        secure: Boolean = false,
        hint: Boolean = false,
    ) = FieldSnapshot(id, text, secure, hint)

    private fun identity(
        className: String? = "android.widget.EditText",
        left: Int = 40,
        right: Int = 1040,
        width: Int = 1000,
        height: Int = 120,
    ) = fieldIdentity(className, false, left, right, width, height, null)

    @Test
    fun verticalShiftOfTheSameFieldKeepsItsIdentity() {
        val verdict = verifyLanding(
            "alice",
            5,
            field("", id = identity()),
            field("alice", id = identity()),
        )

        assertEquals(Landing.MATCH, verdict.landing)
    }

    @Test
    fun differentWidthOrClassChangesTheIdentity() {
        assertNotEquals(identity(), identity(width = 900, right = 940))
        assertNotEquals(identity(), identity(className = "android.widget.TextView"))
        assertEquals(
            Landing.MISMATCH,
            verifyLanding("a", 1, field("", id = identity()), field("a", id = identity(width = 900))).landing,
        )
    }

    @Test
    fun uniqueIdSeparatesOtherwiseIdenticalFields() {
        val first = fieldIdentity("c", false, 0, 10, 10, 10, "u1")
        val second = fieldIdentity("c", false, 0, 10, 10, 10, "u2")

        assertNotEquals(first, second)
    }

    @Test
    fun exactMatchOnTheSameFieldLands() {
        val verdict = verifyLanding("alice", 5, field(""), field("alice"))

        assertEquals(Landing.MATCH, verdict.landing)
        assertEquals(5, verdict.landed)
    }

    @Test
    fun appendedValueMatchesWhenPriorContentIsPartOfTheExpectedValue() {
        val verdict = verifyLanding("abcdef", 3, field("abc"), field("abcdef"))

        assertEquals(Landing.MATCH, verdict.landing)
    }

    @Test
    fun textOnAnotherFieldIsAMismatchEvenWhenItEqualsTheRequest() {
        val verdict = verifyLanding("alice", 5, field("", id = "email"), field("alice", id = "other"))

        assertEquals(Landing.MISMATCH, verdict.landing)
        assertEquals(0, verdict.landed)
    }

    @Test
    fun differentContentOnTheTargetIsAMismatchWithoutEchoingIt() {
        val verdict = verifyLanding("alice", 5, field(""), field("alicX"))

        assertEquals(Landing.MISMATCH, verdict.landing)
        assertEquals(4, verdict.landed)
        assertFalse(verdict.reason.contains("alic"))
    }

    @Test
    fun secureFieldMatchesByLengthWhenMasked() {
        val verdict = verifyLanding("hunter2", 7, field("", secure = true), field("•••••••", secure = true))

        assertEquals(Landing.MATCH, verdict.landing)
        assertEquals(7, verdict.landed)
    }

    @Test
    fun secureFieldWithWrongLengthIsAMismatchAndHidesLengths() {
        val verdict = verifyLanding("hunter2", 7, field("", secure = true), field("•••", secure = true))

        assertEquals(Landing.MISMATCH, verdict.landing)
        assertEquals(3, verdict.landed)
        assertFalse(verdict.reason.any { it.isDigit() })
    }

    @Test
    fun emptyFieldMeansNothingLanded() {
        assertEquals(Landing.NOTHING_LANDED, verifyLanding("alice", 5, field(""), field("")).landing)
    }

    @Test
    fun hintShownInsteadOfContentMeansNothingLanded() {
        val verdict = verifyLanding("alice", 5, field("", hint = true), field("Email", hint = true))

        assertEquals(Landing.NOTHING_LANDED, verdict.landing)
    }

    @Test
    fun emptyRequestMatchesAnEmptyFieldAndRejectsLeftoverContent() {
        assertEquals(Landing.MATCH, verifyLanding("", 0, field(""), field("")).landing)
        assertEquals(Landing.MISMATCH, verifyLanding("", 0, field(""), field("stale")).landing)
    }
}
