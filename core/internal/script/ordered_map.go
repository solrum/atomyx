package script

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// OrderedMap is a YAML mapping decoded generically while preserving
// the declaration order of its top-level keys, the same way a
// JavaScript object parsed from the same YAML source preserves
// insertion order. It is the decode target for every mapping node
// reachable from a script's step list (see decodeGenericValue), so a
// dot-path-keyed assertion map (AssertApiStep.Body,
// BranchMatchCondition.Body) reports a multi-key failure in the order
// the script author wrote it, rather than in a resorted order that
// would no longer match the script's own layout.
//
// OrderedMap has no UnmarshalJSON or MarshalJSON round-trip use:
// every script this package parses comes from YAML text, never JSON,
// and nothing in this codebase decodes a step field from JSON. Its
// MarshalJSON exists only to render the already-parsed value for a
// caller that serializes a ScriptDefinition to JSON.
type OrderedMap struct {
	keys   []string
	values map[string]any
}

// Set appends key to the declaration order the first time it is seen
// and overwrites its value in place on a repeated key, mirroring how
// a duplicate key in a JavaScript object literal keeps its first
// position but takes its last assigned value.
func (m *OrderedMap) Set(key string, value any) {
	if m.values == nil {
		m.values = map[string]any{}
	}
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// Get reports value and presence for key, matching the Go map
// comma-ok idiom. A nil receiver reports absence, so a step with no
// body assertions needs no nil check before calling Get.
func (m *OrderedMap) Get(key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m.values[key]
	return v, ok
}

// Keys returns the map's keys in declaration order. A nil receiver
// returns no keys.
func (m *OrderedMap) Keys() []string {
	if m == nil {
		return nil
	}
	return m.keys
}

// Len reports the number of entries. A nil receiver reports zero.
func (m *OrderedMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.keys)
}

// ToMap renders m as a plain Go map for a caller that does not care
// about key order. Every general-purpose YAML field reader in this
// package (asMap and its callers) reads a decoded mapping this way;
// only the fields that need declaration order (AssertApiStep.Body,
// BranchMatchCondition.Body, ExtractStep.Values) read an OrderedMap
// directly through Keys and Get instead.
func (m *OrderedMap) ToMap() map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m.keys))
	for k, v := range m.values {
		out[k] = v
	}
	return out
}

// UnmarshalYAML decodes node's mapping pairwise, in source order,
// recursing into decodeGenericValue for each value so a nested
// mapping anywhere under node also preserves its own declaration
// order.
func (m *OrderedMap) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected a YAML mapping, got %s", yamlNodeKindLabel(node))
	}
	om := OrderedMap{values: make(map[string]any, len(node.Content)/2)}
	for i := 0; i+1 < len(node.Content); i += 2 {
		var key string
		if err := node.Content[i].Decode(&key); err != nil {
			return err
		}
		value, err := decodeGenericValue(node.Content[i+1])
		if err != nil {
			return err
		}
		om.Set(key, value)
	}
	*m = om
	return nil
}

// MarshalJSON renders m as a JSON object with its keys in declaration
// order. encoding/json always sorts a Go map's keys alphabetically
// when marshaling one directly, so this builds the object text itself
// instead of marshaling m.values as a map.
//
// The receiver is a value, not a pointer, on purpose: decodeGenericValue
// stores every nested mapping it decodes as a plain OrderedMap value
// inside an `any` (never a pointer), and a value obtained that way is
// never addressable, so a pointer-receiver MarshalJSON would silently
// never be invoked on it. encoding/json would fall back to reflecting
// over OrderedMap's unexported fields and render an empty object
// regardless of content. A *OrderedMap field (nil or not) still
// marshals correctly through this same method: encoding/json promotes
// a value receiver's method to the pointer's method set, and renders a
// nil pointer as the JSON literal `null` before ever calling it.
func (m OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		valJSON, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// OrderedStringMap is OrderedMap's string-valued counterpart: a
// dot-path-keyed map whose values are themselves dot-paths rather
// than arbitrary JSON, used for ExtractStep.Values. Unlike OrderedMap
// it is never a YAML decode target, since extract's normalizer builds one
// field by field from an already-decoded OrderedMap, validating that
// each value is a string as it goes.
type OrderedStringMap struct {
	keys   []string
	values map[string]string
}

// Set appends key to the declaration order the first time it is seen
// and overwrites its value in place on a repeated key, matching
// OrderedMap.Set.
func (m *OrderedStringMap) Set(key, value string) {
	if m.values == nil {
		m.values = map[string]string{}
	}
	if _, exists := m.values[key]; !exists {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// Get reports value and presence for key. A nil receiver reports
// absence.
func (m *OrderedStringMap) Get(key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m.values[key]
	return v, ok
}

// Keys returns the map's keys in declaration order. A nil receiver
// returns no keys.
func (m *OrderedStringMap) Keys() []string {
	if m == nil {
		return nil
	}
	return m.keys
}

// Len reports the number of entries. A nil receiver reports zero.
func (m *OrderedStringMap) Len() int {
	if m == nil {
		return 0
	}
	return len(m.keys)
}

// MarshalJSON renders m as a JSON object with its keys in declaration
// order, matching OrderedMap.MarshalJSON. The receiver is a value for
// the same reason: see OrderedMap.MarshalJSON.
func (m OrderedStringMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')
		valJSON, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// yamlNodeKindLabel renders node.Kind for an error message naming the
// mismatched shape a caller tried to decode as a mapping.
func yamlNodeKindLabel(node *yaml.Node) string {
	switch node.Kind {
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.SequenceNode:
		return "a sequence"
	case yaml.AliasNode:
		return "an alias"
	case yaml.DocumentNode:
		return "a document"
	default:
		return "an unknown YAML node"
	}
}

// decodeGenericValue decodes one YAML node into the same dynamic
// shapes a plain yaml.Node.Decode into an interface{} would produce
// for a scalar (nil, bool, int, float64, or string) or a sequence
// (as []any), except a mapping decodes into an OrderedMap instead of
// a plain Go map, so the mapping's declaration order survives every
// later step of the generic pipeline (variable resolution, step
// normalization) down to whichever field ultimately needs it.
func decodeGenericValue(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.MappingNode:
		var om OrderedMap
		if err := node.Decode(&om); err != nil {
			return nil, err
		}
		return om, nil
	case yaml.SequenceNode:
		return decodeGenericSequence(node)
	default:
		var v any
		if err := node.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil
	}
}

// decodeGenericSequence decodes every element of a YAML sequence node
// via decodeGenericValue, matching the shape a plain
// yaml.Node.Decode into a []any would produce.
func decodeGenericSequence(node *yaml.Node) ([]any, error) {
	out := make([]any, len(node.Content))
	for i, item := range node.Content {
		v, err := decodeGenericValue(item)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}
