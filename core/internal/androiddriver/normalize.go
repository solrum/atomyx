package androiddriver

import (
	"fmt"
	"strings"

	"dev.atomyx/internal/transport"
	"dev.atomyx/internal/wire"
)

// classNameToRole maps an Android accessibility class name to a
// canonical cross-platform role. The mapping is deliberately coarse:
// it distinguishes only the roles the cross-platform framework treats
// differently, and an unrecognized class falls through to "other".
//
// Order matters: a more specific substring is checked before a more
// generic one that would also match it, for example "radiobutton" and
// "imagebutton" both contain "button".
func classNameToRole(className string) string {
	if className == "" {
		return "other"
	}
	cls := strings.ToLower(className)
	switch {
	case strings.Contains(cls, "radiobutton"):
		return "radio-button"
	case strings.Contains(cls, "checkbox"):
		return "checkbox"
	case strings.Contains(cls, "edittext"), strings.Contains(cls, "textinput"):
		return "text-field"
	case strings.Contains(cls, "searchview"):
		return "search-field"
	case strings.Contains(cls, "imagebutton"):
		return "button"
	case strings.Contains(cls, "button"):
		return "button"
	case strings.Contains(cls, "imageview"):
		return "image"
	case strings.Contains(cls, "textview"):
		return "text"
	case strings.Contains(cls, "switch"):
		return "switch"
	case strings.Contains(cls, "seekbar"), strings.Contains(cls, "slider"):
		return "slider"
	case strings.Contains(cls, "recyclerview"), strings.Contains(cls, "listview"):
		return "list"
	case strings.Contains(cls, "webview"):
		return "other"
	case strings.Contains(cls, "framelayout"),
		strings.Contains(cls, "linearlayout"),
		strings.Contains(cls, "relativelayout"),
		strings.Contains(cls, "constraintlayout"),
		strings.Contains(cls, "viewgroup"),
		strings.Contains(cls, "box"),
		strings.Contains(cls, "column"),
		strings.Contains(cls, "row"):
		return "container"
	default:
		return "other"
	}
}

// deriveRoleFromSignals recovers a role from observable state when
// the class-name table cannot identify the node, which happens for
// bare android.view.View instances that Flutter Semantics, Jetpack
// Compose, and custom UIKit-style stacks ship without a recognizable
// widget subclass. It returns "" when no signal is strong enough,
// leaving the caller to settle for "other" rather than guess: a wrong
// role misleads a selector more than a missing one does.
func deriveRoleFromSignals(raw transport.AndroidTreeNode) string {
	hasText := raw.Text != ""
	hasLabel := raw.ContentDesc != ""
	clickable := raw.Clickable != nil && *raw.Clickable
	if clickable && (hasText || hasLabel) {
		return "button"
	}
	if hasText && !clickable {
		return "text"
	}
	return ""
}

// formatBounds serializes a rectangle to the canonical "l,t,r,b"
// string the wire schema uses for the bounds attribute.
func formatBounds(b transport.AndroidBounds) string {
	return fmt.Sprintf("%d,%d,%d,%d", b.Left, b.Top, b.Right, b.Bottom)
}

// NormalizeAndroidTree translates one Android agent tree node,
// recursively, into the canonical wire.TreeNodeWire shape. The
// synthetic ElementID the agent assigns is not carried over; nothing
// in the canonical shape corresponds to it.
func NormalizeAndroidTree(raw transport.AndroidTreeNode) wire.TreeNodeWire {
	attributes := make(map[string]string)

	if raw.ResourceID != "" {
		attributes["id"] = raw.ResourceID
	}
	if raw.ContentDesc != "" {
		attributes["label"] = raw.ContentDesc
	}
	if raw.Text != "" {
		attributes["text"] = raw.Text
	}
	if raw.HintText != "" {
		attributes["hint"] = raw.HintText
	}
	if raw.ClassName != "" {
		attributes["class"] = raw.ClassName
	}

	role := classNameToRole(raw.ClassName)
	if role == "other" {
		if derived := deriveRoleFromSignals(raw); derived != "" {
			role = derived
		}
	}
	attributes["role"] = role

	if raw.Bounds != nil {
		attributes["bounds"] = formatBounds(*raw.Bounds)
	}
	if raw.IsIme {
		attributes["ext:isIme"] = "true"
	}

	children := make([]wire.TreeNodeWire, 0, len(raw.Children))
	for _, c := range raw.Children {
		children = append(children, NormalizeAndroidTree(c))
	}

	node := wire.TreeNodeWire{
		Attributes: attributes,
		Children:   children,
	}
	if raw.Clickable != nil {
		node.Clickable = raw.Clickable
	}
	if raw.Enabled != nil {
		node.Enabled = raw.Enabled
	}
	if raw.Focused != nil {
		node.Focused = raw.Focused
	}
	if raw.Selected != nil {
		node.Selected = raw.Selected
	}
	if raw.Visible != nil {
		node.Visible = raw.Visible
	}
	// checked is only meaningful on a checkable node (CheckBox,
	// Switch, ToggleButton); leaving it nil when the node is not
	// checkable, or when the agent did not report a checked state at
	// all, keeps the wire payload aligned with iOS, where checked is
	// never populated and nil correctly signals "not applicable".
	if raw.Checkable != nil && *raw.Checkable && raw.Checked != nil {
		node.Checked = raw.Checked
	}

	return node
}
