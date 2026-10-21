package runner

import "context"

// Limits every HTTPDoer adapter enforces, so the runner and the adapter
// agree on them.
const (
	// HTTPMaxResponseBodyBytes is the largest response body an adapter
	// returns. A larger body fails the request with HTTPFailureBodyTooLarge.
	HTTPMaxResponseBodyBytes = 1 << 20
	// HTTPMaxRedirects is the most redirects an adapter follows for one
	// request.
	HTTPMaxRedirects = 3
)

// HTTPRequest is one request an httpRequest step sends from the host
// running the script.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// HTTPResponse is the final response of a request, after redirects.
// Headers holds one joined value per header name.
type HTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// HTTPDoer sends HTTP requests on behalf of httpRequest steps. An
// implementation enforces the network policy: which hosts and addresses
// a request may reach, the redirect limit, and the response size cap.
// Do returns an *HTTPError for a failure it can classify, and the error
// text of every failure must not contain the URL, a header or the body,
// because a request can carry credentials.
type HTTPDoer interface {
	Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error)
}

// HTTPFailure is the category of a request that did not produce a
// response.
type HTTPFailure string

const (
	HTTPFailureNoHostsAllowed     HTTPFailure = "noHostsAllowed"
	HTTPFailureHostNotAllowed     HTTPFailure = "hostNotAllowed"
	HTTPFailureRedirectNotAllowed HTTPFailure = "redirectNotAllowed"
	HTTPFailureTooManyRedirects   HTTPFailure = "tooManyRedirects"
	HTTPFailureAddressRefused     HTTPFailure = "addressRefused"
	HTTPFailureConnectionRefused  HTTPFailure = "connectionRefused"
	HTTPFailureDNS                HTTPFailure = "dns"
	HTTPFailureTLS                HTTPFailure = "tls"
	HTTPFailureTimeout            HTTPFailure = "timeout"
	HTTPFailureBodyTooLarge       HTTPFailure = "bodyTooLarge"
	HTTPFailureCAFile             HTTPFailure = "caFile"
	HTTPFailureOther              HTTPFailure = "other"
)

// HTTPError is a classified request failure. Its text is the category
// description alone.
type HTTPError struct {
	Kind HTTPFailure
}

func (e *HTTPError) Error() string { return httpFailureText(e.Kind) }

// httpFailureText describes a failure category for a step detail.
func httpFailureText(kind HTTPFailure) string {
	switch kind {
	case HTTPFailureNoHostsAllowed:
		return "no host is allowed, list the hosts a script may reach under http.allowHosts in the project config"
	case HTTPFailureHostNotAllowed:
		return "the host is not allowed, add it to http.allowHosts in the project config"
	case HTTPFailureRedirectNotAllowed:
		return "a redirect led to a host that is not allowed by http.allowHosts"
	case HTTPFailureTooManyRedirects:
		return "too many redirects"
	case HTTPFailureAddressRefused:
		return "the address is refused, link-local and metadata addresses are never reachable"
	case HTTPFailureConnectionRefused:
		return "connection refused"
	case HTTPFailureDNS:
		return "the host name could not be resolved"
	case HTTPFailureTLS:
		return "TLS verification failed, trust a self-signed server with http.caFile in the project config"
	case HTTPFailureTimeout:
		return "the request timed out"
	case HTTPFailureBodyTooLarge:
		return "the response body is larger than 1 MiB"
	case HTTPFailureCAFile:
		return "the certificate file named by http.caFile could not be loaded"
	default:
		return "the request failed"
	}
}
