package runfolder

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	// MaxLabelLen caps the label part of a run id.
	MaxLabelLen = 24

	// DefaultLabel stands in for a label that reduces to nothing.
	DefaultLabel = "mcp"

	// MaxIDAttempts bounds how many random suffixes a Starter tries
	// when the folder it wants already exists.
	MaxIDAttempts = 8

	idTimeLayout = "20060102-150405"
	suffixBytes  = 2
)

// Label reduces text to the label part of a run id. The text is
// lowercased, every character outside a to z, 0 to 9 maps to a dash,
// runs of dashes collapse to one, leading and trailing dashes are
// dropped, and the result is cut to MaxLabelLen characters with a dash
// left at the cut dropped too. A label that ends up empty becomes
// DefaultLabel.
func Label(text string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	label := b.String()
	if len(label) > MaxLabelLen {
		label = label[:MaxLabelLen]
	}
	label = strings.TrimRight(label, "-")
	if label == "" {
		return DefaultLabel
	}
	return label
}

// newRunID builds an id of the form <yyyymmdd-hhmmss>-<label>-<4 hex>.
// The time is formatted in UTC so ids sort the same on every machine,
// and the four hex characters come from rnd.
func newRunID(at time.Time, label string, rnd io.Reader) (string, error) {
	var suffix [suffixBytes]byte
	if _, err := io.ReadFull(rnd, suffix[:]); err != nil {
		return "", fmt.Errorf("drawing run id suffix: %w", err)
	}
	return at.UTC().Format(idTimeLayout) + "-" + Label(label) + "-" + hex.EncodeToString(suffix[:]), nil
}
