package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"dev.atomyx/internal/script"
)

const noHTTPClientDetail = "HTTP requests are not available in this session."

// HTTPRequestCommand sends an HTTP request from the host running the
// script, optionally polling until the response satisfies a condition,
// and binds the response to a capture variable.
//
// Every detail is built from the step as the author wrote it and from
// failure categories, never from the executed URL, headers or body, nor
// from the text of a transport error, because those carry credentials.
type HTTPRequestCommand struct{}

func (HTTPRequestCommand) Command() string { return script.CommandHTTPRequest }

func (HTTPRequestCommand) Execute(step script.ScriptStep, ctx *CommandContext) CommandResult {
	s := step.(*script.HTTPRequestStep)
	written := authored[*script.HTTPRequestStep](step)
	label := written.Method + " " + written.URL

	if ctx.HTTP == nil {
		return httpFailed(noHTTPClientDetail)
	}
	req, err := buildHTTPRequest(s)
	if err != nil {
		return httpFailed(fmt.Sprintf("%s: the body cannot be encoded as JSON", label))
	}

	timeoutMs := script.HTTPRequestDefaultTimeoutMs
	if s.TimeoutMs != nil {
		timeoutMs = *s.TimeoutMs
	}
	intervalMs := script.HTTPRequestDefaultPollIntervalMs
	if s.Poll != nil && s.Poll.IntervalMs != nil {
		intervalMs = *s.Poll.IntervalMs
	}
	deadline := ctx.Clock.NowMs() + int64(timeoutMs)

	attempts := 0
	lastStatus := 0
	for {
		remaining := deadline - ctx.Clock.NowMs()
		if remaining <= 0 {
			return httpTimeout(label, written, timeoutMs, attempts, lastStatus)
		}
		attempts++
		resp, failure := sendHTTPRequest(ctx, req, remaining, timeoutMs)
		if failure != "" {
			return httpFailed(fmt.Sprintf("%s failed: %s", label, failure))
		}
		lastStatus = resp.Status
		record := capturedFromHTTPResponse(req, resp, ctx.Clock.NowMs())
		if s.As != "" {
			ctx.Captures[s.As] = record
		}

		if httpStatusPasses(s, resp.Status) && (s.Poll == nil || pollSatisfied(record, s.Poll.Until)) {
			return CommandResult{Ok: true, Detail: strPtr(httpSuccessDetail(label, written, resp.Status, attempts))}
		}
		if s.Poll == nil {
			return httpFailed(fmt.Sprintf("%s returned status %d, expected %s", label, resp.Status, expectedStatusText(s)))
		}

		if ctx.Clock.NowMs()+int64(intervalMs) > deadline {
			return httpTimeout(label, written, timeoutMs, attempts, lastStatus)
		}
		if err := ctx.Clock.Sleep(ctx.Context, int64(intervalMs)); err != nil {
			return httpFailed(fmt.Sprintf("Interrupted: %v", err))
		}
	}
}

func httpFailed(detail string) CommandResult {
	return CommandResult{Ok: false, Detail: strPtr(detail)}
}

func httpSuccessDetail(label string, written *script.HTTPRequestStep, status, attempts int) string {
	detail := fmt.Sprintf("%s returned status %d", label, status)
	if written.As != "" {
		detail += fmt.Sprintf(" as \"%s\"", written.As)
	}
	if written.Poll != nil {
		detail += fmt.Sprintf(" after %d %s", attempts, pluralWord(attempts, "attempt"))
	}
	return detail
}

func httpTimeout(label string, written *script.HTTPRequestStep, timeoutMs, attempts, lastStatus int) CommandResult {
	if written.Poll == nil {
		return httpFailed(fmt.Sprintf("%s failed: the request timed out after %dms", label, timeoutMs))
	}
	return httpFailed(fmt.Sprintf("%s did not satisfy poll.until \"%s\" within %dms after %d %s (last status %d)",
		label, written.Poll.Until, timeoutMs, attempts, pluralWord(attempts, "attempt"), lastStatus))
}

func expectedStatusText(s *script.HTTPRequestStep) string {
	if s.ExpectStatus == nil {
		return "a 2xx status"
	}
	return fmt.Sprintf("%d", *s.ExpectStatus)
}

func httpStatusPasses(s *script.HTTPRequestStep, status int) bool {
	if s.ExpectStatus != nil {
		return status == *s.ExpectStatus
	}
	return status >= 200 && status <= 299
}

// buildHTTPRequest assembles the executed request. An object body is sent
// as JSON, with a JSON content type unless a header already sets one.
func buildHTTPRequest(s *script.HTTPRequestStep) (HTTPRequest, error) {
	req := HTTPRequest{Method: s.Method, URL: s.URL, Headers: make(map[string]string, len(s.Headers)+1)}
	for name, value := range s.Headers {
		req.Headers[name] = value
	}
	switch body := s.Body.(type) {
	case nil:
	case string:
		req.Body = []byte(body)
	default:
		encoded, err := json.Marshal(body)
		if err != nil {
			return HTTPRequest{}, err
		}
		req.Body = encoded
		if !hasHeader(req.Headers, "Content-Type") {
			req.Headers["Content-Type"] = "application/json"
		}
	}
	return req, nil
}

func hasHeader(headers map[string]string, name string) bool {
	for existing := range headers {
		if strings.EqualFold(existing, name) {
			return true
		}
	}
	return false
}

// sendHTTPRequest sends req within the remaining step budget. A non-empty
// failure text describes why no response arrived.
func sendHTTPRequest(ctx *CommandContext, req HTTPRequest, remainingMs int64, timeoutMs int) (HTTPResponse, string) {
	attemptCtx, cancel := context.WithTimeout(ctx.Context, time.Duration(remainingMs)*time.Millisecond)
	defer cancel()
	resp, err := ctx.HTTP.Do(attemptCtx, req)
	if err == nil {
		return resp, ""
	}
	if ctxErr := ctx.Context.Err(); ctxErr != nil {
		return HTTPResponse{}, fmt.Sprintf("Interrupted: %v", ctxErr)
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.Kind == HTTPFailureTimeout {
			return HTTPResponse{}, fmt.Sprintf("the request timed out after %dms", timeoutMs)
		}
		return HTTPResponse{}, httpErr.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return HTTPResponse{}, fmt.Sprintf("the request timed out after %dms", timeoutMs)
	}
	return HTTPResponse{}, httpFailureText(HTTPFailureOther)
}

// capturedFromHTTPResponse renders a response as the record a capture
// step stores, so extract and assertApi read it unchanged.
func capturedFromHTTPResponse(req HTTPRequest, resp HTTPResponse, nowMs int64) CapturedRequest {
	return CapturedRequest{
		Method:      req.Method,
		URL:         req.URL,
		Status:      resp.Status,
		Headers:     resp.Headers,
		Body:        parseJSONBody(resp),
		TimestampMs: nowMs,
	}
}

// parseJSONBody decodes a response body that announces or looks like a
// JSON object or array, and returns nil for anything else.
func parseJSONBody(resp HTTPResponse) any {
	trimmed := strings.TrimSpace(string(resp.Body))
	if trimmed == "" {
		return nil
	}
	looksJSON := strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
	if !looksJSON && !strings.Contains(strings.ToLower(headerValue(resp.Headers, "Content-Type")), "json") {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil
	}
	return decoded
}

func headerValue(headers map[string]string, name string) string {
	for existing, value := range headers {
		if strings.EqualFold(existing, name) {
			return value
		}
	}
	return ""
}

// pollSatisfied reports whether path resolves in record to a value that
// counts as present: not missing, not null, not an empty string and not
// an empty array. Zero and false count as values.
func pollSatisfied(record CapturedRequest, path string) bool {
	value := resolveDotPath(capturedRequestAsAny(record), path)
	if value == nil || isJSUndefined(value) {
		return false
	}
	if s, ok := value.(string); ok && s == "" {
		return false
	}
	return !isEmptyArray(value)
}
