package script

// Fields of a leaf step that take a run variable's value when the step
// is about to run. Every other field, including numbers, durations,
// enumerations, expression source, regex patterns and JSON paths, only
// ever takes values known when the script is parsed: splicing text read
// from a screen or an HTTP response into code or a pattern would change
// what that code or pattern means.

// textKind says how a substituted value is written into an eligible
// string.
type textKind int

const (
	// plainText inserts the value as it is.
	plainText textKind = iota
	// urlText query-escapes a value that lands after the first `?`, so a
	// value holding `a&b=c` stays one parameter value.
	urlText
)

const (
	// anyKey is a path segment matching every key of a mapping.
	anyKey = "*"
	// anyDepth is a path segment matching every string under a node, at
	// any depth through mappings and sequences.
	anyDepth = "**"
)

// runtimeField is one eligible position inside a step command's value.
// An empty path is the value itself, which is eligible only when it is a
// string, as in the shorthand `tap: Sign in`.
type runtimeField struct {
	path []string
	kind textKind
}

// selectorTextKeys are the content fields of a selector mapping that
// take a plain string. A `{regex, flags}` object under one of them, and
// the role and nth fields, are never eligible.
var selectorTextKeys = []string{"id", "text", "label", "hint"}

// selectorFields lists the eligible positions of a selector found at
// prefix: the shorthand string and each plain-string content field.
func selectorFields(prefix ...string) []runtimeField {
	fields := []runtimeField{{path: prefix}}
	for _, key := range selectorTextKeys {
		fields = append(fields, runtimeField{path: appendPath(prefix, key)})
	}
	return fields
}

// stringFields lists the eligible positions of a value that is either a
// bare string or an object holding the string under key.
func stringFields(key string) []runtimeField {
	return []runtimeField{{path: nil}, {path: []string{key}}}
}

func appendPath(prefix []string, key string) []string {
	out := make([]string, 0, len(prefix)+1)
	out = append(out, prefix...)
	return append(out, key)
}

func concatFields(lists ...[]runtimeField) []runtimeField {
	var out []runtimeField
	for _, list := range lists {
		out = append(out, list...)
	}
	return out
}

// runtimeFields maps a leaf command to its eligible positions. A command
// absent from the table resolves nothing at run time.
var runtimeFields = map[string][]runtimeField{
	CommandTap:                selectorFields(),
	CommandWaitFor:            selectorFields(),
	CommandAssertVisible:      selectorFields(),
	CommandAssertNotVisible:   selectorFields(),
	CommandAssertExists:       selectorFields(),
	CommandScrollUntilVisible: selectorFields(),
	CommandRead:               selectorFields("selector"),
	CommandType:               concatFields(stringFields("text"), selectorFields("into")),
	CommandOpenLink:           stringFields("link"),
	CommandHTTPRequest: {
		{path: []string{"url"}, kind: urlText},
		{path: []string{"headers", anyKey}},
		{path: []string{"body", anyDepth}},
	},
}

// mergeRuntimeFields returns base, the command value with parse-time
// values already in place, with each eligible position rewritten from
// pre, the same value as written before any interpolation. Reading the
// eligible text from pre is what keeps a value inserted once from being
// scanned for placeholders again. base and pre have the same shape.
func mergeRuntimeFields(command string, base, pre any, lookup func(string) (string, bool)) any {
	for _, field := range runtimeFields[command] {
		base = mergeAt(base, pre, field.path, field.kind, lookup)
	}
	return base
}

func mergeAt(base, pre any, path []string, kind textKind, lookup func(string) (string, bool)) any {
	if len(path) == 0 {
		text, ok := pre.(string)
		if !ok {
			return base
		}
		return expandText(text, kind, lookup)
	}
	segment := path[0]
	rest := path[1:]
	if segment == anyDepth {
		rest = path
	}
	switch b := base.(type) {
	case OrderedMap:
		p, ok := pre.(OrderedMap)
		if !ok {
			return base
		}
		var out OrderedMap
		for _, key := range b.Keys() {
			value, _ := b.Get(key)
			if segment == key || segment == anyKey || segment == anyDepth {
				if preValue, has := p.Get(key); has {
					value = mergeAt(value, preValue, rest, kind, lookup)
				}
			}
			out.Set(key, value)
		}
		return out
	case []any:
		p, ok := pre.([]any)
		if !ok || segment != anyDepth || len(p) != len(b) {
			return base
		}
		out := make([]any, len(b))
		for i := range b {
			out[i] = mergeAt(b[i], p[i], rest, kind, lookup)
		}
		return out
	case string:
		if segment != anyDepth {
			return base
		}
		return mergeAt(base, pre, nil, kind, lookup)
	}
	return base
}

func expandText(text string, kind textKind, lookup func(string) (string, bool)) string {
	switch kind {
	case urlText:
		return expandURLPlaceholders(text, lookup)
	case plainText:
		expanded, _ := expandPlaceholders(text, lookup).(string)
		return expanded
	}
	return text
}
