package script

import (
	"strings"

	"dev.atomyx/internal/driver"
)

// normalizeSetOrientation accepts the orientation name as a bare string:
// portrait, landscapeLeft or landscapeRight.
func normalizeSetOrientation(value any) (ScriptStep, error) {
	text, ok := asString(value)
	if !ok {
		return nil, newParseError("setOrientation: expected one of %s, got %s", strings.Join(orientationNames(), ", "), goType(value))
	}
	orientation, known := driver.ParseOrientation(text)
	if !known {
		return nil, newParseError("setOrientation: %q is not an orientation, use one of: %s", text, strings.Join(orientationNames(), ", "))
	}
	return &SetOrientationStep{Command: CommandSetOrientation, Orientation: orientation}, nil
}

// orientationNames lists the accepted orientation names in a stable
// order.
func orientationNames() []string {
	all := driver.AllOrientations()
	names := make([]string, len(all))
	for i, o := range all {
		names[i] = string(o)
	}
	return names
}
