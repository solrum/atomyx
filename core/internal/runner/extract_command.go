package runner

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"dev.atomyx/internal/redact"
	"dev.atomyx/internal/script"
	"dev.atomyx/internal/script/expression"
)

// ExtractCommand extracts fields from a captured request into runtime
// variables, resolving each target's dot-path against the whole
// captured request object (method, url, status, headers, body,
// timestamp), not just its body.
type ExtractCommand struct{}

func (ExtractCommand) Command() string { return script.CommandExtract }

func (ExtractCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.ExtractStep)
	written := authored[*script.ExtractStep](step)
	fromKey := unwrapFromKey(s.From)
	writtenKey := unwrapFromKey(written.From)
	captured, ok := ctx.Captures[fromKey]
	if !ok {
		return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
			"No captured request named \"%s\". Run a \"capture\" step with \"as: %s\" first.", writtenKey, writtenKey))}
	}
	capturedObj := capturedRequestAsAny(captured)

	names := s.Values.Keys()
	extracted := make([]string, 0, len(names))
	for _, name := range names {
		path, _ := s.Values.Get(name)
		writtenValue, _ := written.Values.Get(name)
		writtenPath := writtenValue.Path
		value := resolveDotPath(capturedObj, path.Path)
		if isJSUndefined(value) {
			return CommandResult{Ok: false, Detail: strPtr(fmt.Sprintf(
				"Path \"%s\" resolved to undefined in captured \"%s\"", writtenPath, writtenKey))}
		}
		stored := jsStringOf(value)
		ctx.Variables[name] = expression.StringValue(stored)
		ctx.markSecret(name, looksLikeToken(stored))
		extracted = append(extracted, fmt.Sprintf("%s (%d characters)", name, utf8.RuneCountInString(stored)))
	}

	return CommandResult{Ok: true, Detail: strPtr(fmt.Sprintf("Extracted: %s", strings.Join(extracted, ", ")))}
}

// capturedRequestAsAny renders a CapturedRequest as the generic
// object shape of a decoded JSON body, so resolveDotPath
// can walk it the same way it walks a decoded JSON body.
func capturedRequestAsAny(c CapturedRequest) map[string]any {
	headers := make(map[string]any, len(c.Headers))
	for k, v := range c.Headers {
		headers[k] = v
	}
	return map[string]any{
		"method":    c.Method,
		"url":       c.URL,
		"status":    float64(c.Status),
		"headers":   headers,
		"body":      c.Body,
		"timestamp": float64(c.TimestampMs),
	}
}

// looksLikeToken reports whether an API value has the shape of a
// credential. A response carries no secure marker, so the shape is the
// only signal available.
func looksLikeToken(value string) bool {
	return redact.New(nil).String(value) != value
}
