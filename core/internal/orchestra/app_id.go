package orchestra

import (
	"fmt"

	"dev.atomyx/internal/diag"
)

// maxAppIDLength bounds the length validateAppID accepts. Real
// Android package names and iOS bundle ids stay well under this, so
// the limit exists only to reject pathological input before it
// reaches a device call.
const maxAppIDLength = 255

// validateAppID rejects any appID that is not a well-formed Android
// package name or iOS bundle id: non-empty, at most maxAppIDLength
// bytes, and built only from ASCII letters, digits, dot, underscore,
// and hyphen.
//
// Every Orchestra method that carries an appID to a device call runs
// it through this check first. A driver adapter turns appID into a
// shell argument (for example an adb shell command), so a character
// outside this set is a command-injection vector, not a valid
// identifier, and must never reach the adapter.
func validateAppID(appID string) error {
	if appID == "" {
		return diag.New(
			diag.CodeInvalidAppID,
			"app id is empty",
			"pass a non-empty Android package name or iOS bundle id",
			nil,
		)
	}
	if len(appID) > maxAppIDLength {
		return diag.New(
			diag.CodeInvalidAppID,
			fmt.Sprintf("app id is longer than %d characters", maxAppIDLength),
			"pass a valid Android package name or iOS bundle id",
			nil,
		)
	}
	for _, r := range appID {
		if !isValidAppIDRune(r) {
			return diag.New(
				diag.CodeInvalidAppID,
				fmt.Sprintf("app id %q contains the disallowed character %q", appID, r),
				"use only letters, digits, dot, underscore, and hyphen in an app id",
				nil,
			)
		}
	}
	return nil
}

func isValidAppIDRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '_', r == '-':
		return true
	default:
		return false
	}
}
