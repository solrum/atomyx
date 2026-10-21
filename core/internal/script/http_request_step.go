package script

// Limits and defaults of the httpRequest step.
const (
	// HTTPRequestDefaultTimeoutMs bounds the whole step, one request or
	// every poll attempt, when the step sets no timeout.
	HTTPRequestDefaultTimeoutMs = 10_000
	// HTTPRequestMaxTimeoutMs is the largest timeout a step may set.
	HTTPRequestMaxTimeoutMs = 120_000
	// HTTPRequestDefaultPollIntervalMs is the pause between poll attempts
	// when the step sets no interval.
	HTTPRequestDefaultPollIntervalMs = 1_000
	// HTTPRequestMinPollIntervalMs is the shortest pause between poll
	// attempts, so a poll cannot hammer the server it waits on.
	HTTPRequestMinPollIntervalMs = 200
)

// HTTP methods an httpRequest step may use.
const (
	HTTPMethodGet    = "GET"
	HTTPMethodPost   = "POST"
	HTTPMethodPut    = "PUT"
	HTTPMethodPatch  = "PATCH"
	HTTPMethodDelete = "DELETE"
)

// HTTPMethods lists the methods an httpRequest step accepts.
var HTTPMethods = []string{HTTPMethodGet, HTTPMethodPost, HTTPMethodPut, HTTPMethodPatch, HTTPMethodDelete}

// HTTPRequestStep sends an HTTP request from the host running the script,
// not from the device. Method is always set and defaults to GET. Headers
// maps header names to values. Body is nil, a string sent as is, or an
// OrderedMap sent as JSON. As, when set, binds the response to a capture
// variable that extract and assertApi read. ExpectStatus is the status
// the response must have, and nil accepts any 2xx. Poll repeats the
// request until its condition holds. TimeoutMs bounds the whole step.
type HTTPRequestStep struct {
	stepSource
	Command      string            `json:"command"`
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         any               `json:"body,omitempty"`
	As           string            `json:"as,omitempty"`
	ExpectStatus *int              `json:"expectStatus,omitempty"`
	Poll         *HTTPPoll         `json:"poll,omitempty"`
	TimeoutMs    *int              `json:"timeoutMs,omitempty"`
}

func (s *HTTPRequestStep) StepCommand() string { return s.Command }
func (s *HTTPRequestStep) isScriptStep()       {}

// HTTPPoll is the polling condition of an httpRequest step. Until is a
// JSON path into the captured response that must resolve to a non-empty
// value. IntervalMs is the pause between attempts, and nil means
// HTTPRequestDefaultPollIntervalMs.
type HTTPPoll struct {
	Until      string `json:"until"`
	IntervalMs *int   `json:"intervalMs,omitempty"`
}
