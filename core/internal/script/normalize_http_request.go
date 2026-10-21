package script

import "strings"

var httpRequestFields = map[string]bool{
	"method": true, "url": true, "headers": true, "body": true, "as": true,
	"expectStatus": true, "poll": true, "timeout": true,
}

// normalizeHTTPRequest reads the object form of an httpRequest step.
// Messages never repeat a field's value, because a value can carry a
// credential.
func normalizeHTTPRequest(value any) (ScriptStep, error) {
	m, ok := asMap(value)
	if !ok {
		return nil, newParseError("httpRequest: expected an object with at least `url`")
	}
	for key := range m {
		if !httpRequestFields[key] {
			return nil, newParseError("httpRequest: unknown field %q", key)
		}
	}
	step := &HTTPRequestStep{Command: CommandHTTPRequest, Method: HTTPMethodGet}

	if raw, present := m["method"]; present && raw != nil {
		method, ok := asString(raw)
		if !ok {
			return nil, newParseError("httpRequest.method: expected a string, got %s", goType(raw))
		}
		step.Method = strings.ToUpper(strings.TrimSpace(method))
	}
	url, ok := asString(m["url"])
	if !ok {
		return nil, newParseError("httpRequest.url: expected a string")
	}
	step.URL = url

	if raw, present := m["headers"]; present && raw != nil {
		headers, err := normalizeHTTPHeaders(raw)
		if err != nil {
			return nil, err
		}
		step.Headers = headers
	}
	if raw, present := m["body"]; present && raw != nil {
		body, err := normalizeHTTPBody(raw)
		if err != nil {
			return nil, err
		}
		step.Body = body
	}
	if raw, present := m["as"]; present && raw != nil {
		as, ok := asString(raw)
		if !ok {
			return nil, newParseError("httpRequest.as: expected a string, got %s", goType(raw))
		}
		step.As = as
	}
	var err error
	if step.ExpectStatus, err = optionalIntField(m, "expectStatus", "httpRequest.expectStatus"); err != nil {
		return nil, err
	}
	if step.TimeoutMs, err = optionalIntField(m, "timeout", "httpRequest.timeout"); err != nil {
		return nil, err
	}
	if raw, present := m["poll"]; present && raw != nil {
		poll, err := normalizeHTTPPoll(raw)
		if err != nil {
			return nil, err
		}
		step.Poll = poll
	}
	return step, nil
}

func optionalIntField(m map[string]any, key, label string) (*int, error) {
	raw, present := m[key]
	if !present || raw == nil {
		return nil, nil
	}
	n, ok := asInt(raw)
	if !ok {
		return nil, newParseError("%s: expected a whole number", label)
	}
	return &n, nil
}

func normalizeHTTPHeaders(raw any) (map[string]string, error) {
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("httpRequest.headers: expected an object of header names to values")
	}
	headers := make(map[string]string, len(m))
	for name, v := range m {
		if v == nil {
			return nil, newParseError("httpRequest.headers.%s: expected a value", name)
		}
		switch v.(type) {
		case string, int, int64, float64, bool:
			headers[name] = jsString(v)
		default:
			return nil, newParseError("httpRequest.headers.%s: expected a string, got %s", name, goType(v))
		}
	}
	return headers, nil
}

// normalizeHTTPBody accepts a string, sent as written, or an object,
// sent as JSON.
func normalizeHTTPBody(raw any) (any, error) {
	if s, ok := asString(raw); ok {
		return s, nil
	}
	if om, ok := asOrderedMap(raw); ok {
		return om, nil
	}
	return nil, newParseError("httpRequest.body: expected a string or an object, got %s", goType(raw))
}

func normalizeHTTPPoll(raw any) (*HTTPPoll, error) {
	m, ok := asMap(raw)
	if !ok {
		return nil, newParseError("httpRequest.poll: expected an object with `until`")
	}
	for key := range m {
		if key != "until" && key != "interval" {
			return nil, newParseError("httpRequest.poll: unknown field %q", key)
		}
	}
	until, ok := asString(m["until"])
	if !ok {
		return nil, newParseError("httpRequest.poll.until: expected a JSON path string")
	}
	interval, err := optionalIntField(m, "interval", "httpRequest.poll.interval")
	if err != nil {
		return nil, err
	}
	return &HTTPPoll{Until: until, IntervalMs: interval}, nil
}
