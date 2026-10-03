package runner

import (
	"errors"
	"fmt"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
	"dev.atomyx/internal/wire"
)

// readTimeoutMs is read's own wait budget for locating its element
// before reading a field, independent of any other command's
// timeout default.
const readTimeoutMs = 5000

// ReadCommand resolves a selector and reads one attribute or state
// field from it into a runtime variable.
type ReadCommand struct{}

func (ReadCommand) Command() string { return script.CommandRead }

func (ReadCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.ReadStep)
	writtenAs := authored[*script.ReadStep](step).As
	selector := compileScriptSelector(s.Selector)
	field := "value"
	if s.Field != nil {
		field = *s.Field
	}

	matches, err := ctx.Orchestra.WaitFor(ctx.Context, selector, orchestra.WaitOptions{TimeoutMs: readTimeoutMs})
	if err != nil {
		var timeoutErr *orchestra.FindTimeoutError
		if errors.As(err, &timeoutErr) {
			return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
				"Cannot read \"%s\": element not found within %dms", field, readTimeoutMs))}
		}
		panic(err)
	}
	node := matches[0].Node

	if isReadAttributeField(field) {
		value, ok := node.Attributes[field]
		if !ok {
			return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Element has no \"%s\" field", field))}
		}
		ctx.Variables[s.As] = expression.StringValue(value)
		return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Read %s into \"%s\"", field, writtenAs))}
	}

	value, ok := readStateField(node, field)
	if !ok {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf("Element does not report the \"%s\" field", field))}
	}
	ctx.Variables[s.As] = expression.BoolValue(value)
	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Read %s into \"%s\"", field, writtenAs))}
}

func isReadAttributeField(field string) bool {
	for _, f := range script.ReadAttributeFields {
		if f == field {
			return true
		}
	}
	return false
}

// readStateField reads one of ReadStateFields from node. The second
// return value is false when the underlying accessibility runtime did
// not report the field (a nil tri-state pointer) or field names
// neither a known attribute nor state field.
func readStateField(node wire.TreeNodeWire, field string) (bool, bool) {
	var ptr *bool
	switch field {
	case "clickable":
		ptr = node.Clickable
	case "enabled":
		ptr = node.Enabled
	case "focused":
		ptr = node.Focused
	case "selected":
		ptr = node.Selected
	case "checked":
		ptr = node.Checked
	case "visible":
		ptr = node.Visible
	default:
		return false, false
	}
	if ptr == nil {
		return false, false
	}
	return *ptr, true
}
