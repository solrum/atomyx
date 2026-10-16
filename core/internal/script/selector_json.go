package script

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// selectorWire is the JSON form of ScriptSelector: each content field
// is either a JSON string or a {regex, flags} object, the same two
// shapes the YAML authoring form and the JSON schema accept.
type selectorWire struct {
	Text  json.RawMessage `json:"text,omitempty"`
	ID    json.RawMessage `json:"id,omitempty"`
	Label json.RawMessage `json:"label,omitempty"`
	Hint  json.RawMessage `json:"hint,omitempty"`
	Role  *string         `json:"role,omitempty"`
	Nth   *int            `json:"nth,omitempty"`
}

// MarshalJSON encodes a content field as a string when it is an
// exact literal and as a {regex, flags} object when it is a regex.
func (s ScriptSelector) MarshalJSON() ([]byte, error) {
	var w selectorWire
	var err error
	fields := []struct {
		dst     *json.RawMessage
		literal *string
		regex   *RegexPattern
	}{
		{&w.Text, s.Text, s.TextRegex},
		{&w.ID, s.ID, s.IDRegex},
		{&w.Label, s.Label, s.LabelRegex},
		{&w.Hint, s.Hint, s.HintRegex},
	}
	for _, f := range fields {
		switch {
		case f.regex != nil:
			*f.dst, err = json.Marshal(f.regex)
		case f.literal != nil:
			*f.dst, err = json.Marshal(*f.literal)
		}
		if err != nil {
			return nil, err
		}
	}
	w.Role, w.Nth = s.Role, s.Nth
	return json.Marshal(w)
}

// UnmarshalJSON decodes the form MarshalJSON produces.
func (s *ScriptSelector) UnmarshalJSON(data []byte) error {
	var w selectorWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	out := ScriptSelector{Role: w.Role, Nth: w.Nth}
	fields := []struct {
		name    string
		raw     json.RawMessage
		literal **string
		regex   **RegexPattern
	}{
		{"text", w.Text, &out.Text, &out.TextRegex},
		{"id", w.ID, &out.ID, &out.IDRegex},
		{"label", w.Label, &out.Label, &out.LabelRegex},
		{"hint", w.Hint, &out.Hint, &out.HintRegex},
	}
	for _, f := range fields {
		raw := bytes.TrimSpace(f.raw)
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		if raw[0] == '"' {
			var lit string
			if err := json.Unmarshal(raw, &lit); err != nil {
				return fmt.Errorf("selector.%s: %w", f.name, err)
			}
			*f.literal = &lit
			continue
		}
		var p RegexPattern
		if err := json.Unmarshal(raw, &p); err != nil {
			return fmt.Errorf("selector.%s: expected a string or a {regex, flags} object: %w", f.name, err)
		}
		*f.regex = &p
	}
	*s = out
	return nil
}
