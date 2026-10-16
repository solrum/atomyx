package script

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrUnsupportedStep is wrapped by the error RenderStepYAML returns for
// a step kind it does not render.
var ErrUnsupportedStep = errors.New("the step kind cannot be rendered as YAML")

// ErrEmptySelector is wrapped by the error RenderStepYAML returns for a
// selector step whose selector sets none of id, text, label, hint and
// role, because such a step would match the whole screen.
var ErrEmptySelector = errors.New("the selector sets no field")

// RenderStepYAML renders one step as the YAML a script holds: a
// sequence with a single item, ready to paste under a script's steps.
//
// It renders tap, type, waitFor, assertVisible, assertNotVisible and
// assertExists, the steps that act on one selector. Any other step
// returns an error wrapping ErrUnsupportedStep, and a selector step
// with an empty selector returns one wrapping ErrEmptySelector. A step
// that fails the validation ParseScript applies is refused with that
// message.
//
// The output parses back to the same step. A selector that sets text
// and no label renders as text alone, because the parser mirrors the
// text into the label, so a step taken from the parser round-trips
// exactly. A bare text selector renders in the one-line form
// `tap: Sign in`.
func RenderStepYAML(step ScriptStep) (string, error) {
	item, err := stepNode(step)
	if err != nil {
		return "", err
	}
	var violations []string
	validateStep(step, "step", func(format string, args ...any) {
		violations = append(violations, fmt.Sprintf(format, args...))
	})
	if len(violations) > 0 {
		return "", fmt.Errorf("the step is invalid: %s", strings.Join(violations, "; "))
	}
	doc := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{item}}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return "", fmt.Errorf("encode step: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encode step: %w", err)
	}
	return out.String(), nil
}

func stepNode(step ScriptStep) (*yaml.Node, error) {
	switch s := step.(type) {
	case *TapStep:
		return selectorStepNode(CommandTap, s.Selector, nil)
	case *WaitForStep:
		return selectorStepNode(CommandWaitFor, s.Selector, s.TimeoutMs)
	case *AssertVisibleStep:
		return selectorStepNode(CommandAssertVisible, s.Selector, s.TimeoutMs)
	case *AssertNotVisibleStep:
		return selectorStepNode(CommandAssertNotVisible, s.Selector, s.TimeoutMs)
	case *AssertExistsStep:
		return selectorStepNode(CommandAssertExists, s.Selector, s.TimeoutMs)
	case *TypeStep:
		return typeNode(s)
	case nil:
		return nil, fmt.Errorf("%w: no step", ErrUnsupportedStep)
	}
	return nil, fmt.Errorf("%w: %s", ErrUnsupportedStep, step.StepCommand())
}

// selectorStepNode builds the one-key mapping `command: selector`.
func selectorStepNode(command string, sel ScriptSelector, timeoutMs *int) (*yaml.Node, error) {
	value, err := selectorValue(sel, timeoutMs)
	if err != nil {
		return nil, err
	}
	return mapping(command, value), nil
}

func typeNode(s *TypeStep) (*yaml.Node, error) {
	if s.Into == nil {
		return mapping(CommandType, str(s.Text)), nil
	}
	into, err := selectorMapping(*s.Into)
	if err != nil {
		return nil, err
	}
	return mapping(CommandType, mapping("into", into, "text", str(s.Text))), nil
}

// selectorValue renders the value of a selector step: the one-line
// string for a bare text selector with no timeout, and a mapping of
// the selector fields plus the timeout otherwise.
func selectorValue(sel ScriptSelector, timeoutMs *int) (*yaml.Node, error) {
	if text, ok := bareText(sel); ok && timeoutMs == nil {
		return str(text), nil
	}
	m, err := selectorMapping(sel)
	if err != nil {
		return nil, err
	}
	if timeoutMs != nil {
		m.Content = append(m.Content, str("timeout"), integer(*timeoutMs))
	}
	return m, nil
}

func bareText(sel ScriptSelector) (string, bool) {
	if sel.Text == nil || sel.ID != nil || sel.Hint != nil || sel.Role != nil || sel.Nth != nil {
		return "", false
	}
	if sel.TextRegex != nil || sel.IDRegex != nil || sel.LabelRegex != nil || sel.HintRegex != nil {
		return "", false
	}
	if sel.Label != nil && *sel.Label != *sel.Text {
		return "", false
	}
	return *sel.Text, true
}

func selectorMapping(sel ScriptSelector) (*yaml.Node, error) {
	hasContent := sel.ID != nil || sel.Text != nil || sel.Label != nil || sel.Hint != nil ||
		sel.IDRegex != nil || sel.TextRegex != nil || sel.LabelRegex != nil || sel.HintRegex != nil
	if !hasContent && sel.Role == nil {
		return nil, ErrEmptySelector
	}
	m := &yaml.Node{Kind: yaml.MappingNode}
	add := func(key string, value *string, regex *RegexPattern) {
		switch {
		case regex != nil:
			m.Content = append(m.Content, str(key), regexNode(*regex))
		case value != nil:
			m.Content = append(m.Content, str(key), str(*value))
		}
	}
	add("id", sel.ID, sel.IDRegex)
	add("text", sel.Text, sel.TextRegex)
	mirrored := sel.Label != nil && sel.Text != nil && *sel.Label == *sel.Text ||
		sel.LabelRegex != nil && sel.TextRegex != nil && *sel.LabelRegex == *sel.TextRegex
	if !mirrored {
		add("label", sel.Label, sel.LabelRegex)
	}
	add("hint", sel.Hint, sel.HintRegex)
	add("role", sel.Role, nil)
	if sel.Nth != nil {
		m.Content = append(m.Content, str("nth"), integer(*sel.Nth))
	}
	return m, nil
}

// regexNode renders a regex pattern as the {regex, flags} mapping,
// omitting flags when none are set.
func regexNode(p RegexPattern) *yaml.Node {
	if p.Flags == "" {
		return mapping("regex", str(p.Regex))
	}
	return mapping("regex", str(p.Regex), "flags", str(p.Flags))
}

func mapping(pairs ...any) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode}
	for i := 0; i < len(pairs); i += 2 {
		m.Content = append(m.Content, str(pairs[i].(string)), pairs[i+1].(*yaml.Node))
	}
	return m
}

// str builds a string scalar. The explicit tag makes the encoder quote
// a value that would otherwise read back as another type, such as
// "true" or "42".
func str(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

func integer(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(n)}
}
