package mcp

import (
	"bytes"
	"encoding/json"
	"strings"

	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/wire"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// secureField is the member a tool result sets to true on an element
// object that holds a secret, such as a password input. Every tool
// that returns an element or a tree node carries it, because the
// masking pass below keys on it.
const secureField = "secure"

// maskedMembers are the members of a secure element object whose
// string content is replaced by orchestra.SecureMask.
var maskedMembers = [...]string{orchestra.AttrText, orchestra.AttrValue}

// maskSecureResult rewrites every text block of result so a secure
// element object, one whose "secure" member is true, reports
// orchestra.SecureMask in place of its text and value. It runs once
// for every tool call from the tool-call middleware, so a handler
// never masks anything itself and a new tool is covered as soon as it
// marks its secure elements. A block that is not JSON, or that holds
// no secure element, is left untouched byte for byte.
func maskSecureResult(result *mcp.CallToolResult) {
	if result == nil {
		return
	}
	for _, content := range result.Content {
		block, ok := content.(*mcp.TextContent)
		if !ok || !strings.Contains(block.Text, `"`+secureField+`"`) {
			continue
		}
		if masked, changed := maskSecureJSON([]byte(block.Text)); changed {
			block.Text = string(masked)
		}
	}
}

// maskSecureJSON masks every secure element object inside doc. It
// keeps the layout of doc: a document spread over several lines comes
// back indented with the indent unit it already used, and a
// single-line document comes back on one line. Strings are written
// without HTML escaping, so a masked document differs from the
// original only in the masked strings. It reports false, with doc
// unchanged, when doc is not valid JSON or nothing needed masking.
func maskSecureJSON(doc []byte) ([]byte, bool) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, doc); err != nil {
		return doc, false
	}
	masked, changed := maskValue(compact.Bytes())
	if !changed {
		return doc, false
	}
	indent, multiline := indentUnit(doc)
	if !multiline {
		return masked, true
	}
	var out bytes.Buffer
	if err := json.Indent(&out, masked, "", indent); err != nil {
		return doc, false
	}
	return out.Bytes(), true
}

// indentUnit reports the leading whitespace of the first indented
// line of doc, and whether doc spans more than one line.
func indentUnit(doc []byte) (string, bool) {
	lines := bytes.Split(doc, []byte("\n"))
	if len(lines) < 2 {
		return "", false
	}
	for _, line := range lines[1:] {
		trimmed := bytes.TrimLeft(line, " \t")
		if len(trimmed) > 0 && len(trimmed) < len(line) {
			return string(line[:len(line)-len(trimmed)]), true
		}
	}
	return "", true
}

// encodeString renders s as a JSON string without HTML escaping.
func encodeString(s string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

type member struct {
	key   string
	value json.RawMessage
}

// maskValue walks one compact JSON value. Objects keep their member
// order, so a masked document differs from the original only in the
// masked strings.
func maskValue(raw json.RawMessage) (json.RawMessage, bool) {
	switch raw[0] {
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return raw, false
		}
		changed := false
		for i, item := range items {
			var itemChanged bool
			items[i], itemChanged = maskValue(item)
			changed = changed || itemChanged
		}
		if !changed {
			return raw, false
		}
		return encodeArray(items), true
	case '{':
		members, ok := decodeMembers(raw)
		if !ok {
			return raw, false
		}
		changed := maskSecureMembers(members)
		for i := range members {
			var memberChanged bool
			members[i].value, memberChanged = maskValue(members[i].value)
			changed = changed || memberChanged
		}
		if !changed {
			return raw, false
		}
		return encodeMembers(members), true
	}
	return raw, false
}

// maskSecureMembers replaces the masked members of an object whose
// secure member is true, and reports whether it changed any. An empty
// string stays empty, because it carries nothing to hide.
func maskSecureMembers(members []member) bool {
	secure := false
	for _, m := range members {
		if m.key == secureField && string(m.value) == "true" {
			secure = true
		}
	}
	if !secure {
		return false
	}
	maskedValue := encodeString(orchestra.SecureMask)
	changed := false
	for i, m := range members {
		for _, name := range maskedMembers {
			if m.key != name {
				continue
			}
			var text string
			if json.Unmarshal(m.value, &text) == nil && text != "" && text != orchestra.SecureMask {
				members[i].value = maskedValue
				changed = true
			}
		}
	}
	return changed
}

func decodeMembers(raw json.RawMessage) ([]member, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil {
		return nil, false
	}
	var members []member
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, false
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, false
		}
		members = append(members, member{key: key, value: value})
	}
	return members, true
}

func encodeMembers(members []member) json.RawMessage {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, m := range members {
		if i > 0 {
			out.WriteByte(',')
		}
		out.Write(encodeString(m.key))
		out.WriteByte(':')
		out.Write(m.value)
	}
	out.WriteByte('}')
	return out.Bytes()
}

// secureFlag is the value a tool result stores in its secure member
// for node: true for a node that holds a secret, and nil otherwise so
// the member is omitted.
func secureFlag(node wire.TreeNodeWire) *bool {
	if !orchestra.IsSecure(node) {
		return nil
	}
	secure := true
	return &secure
}

func encodeArray(items []json.RawMessage) json.RawMessage {
	var out bytes.Buffer
	out.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			out.WriteByte(',')
		}
		out.Write(item)
	}
	out.WriteByte(']')
	return out.Bytes()
}
