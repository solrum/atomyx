package httpclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"dev.atomyx/internal/runner"
)

const (
	dialTimeout           = 10 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	idleConnectionTimeout = 30 * time.Second
	maxIdleConnections    = 4
)

var (
	errAddressRefused   = errors.New("address refused")
	errRedirectRefused  = errors.New("redirect refused")
	errTooManyRedirects = errors.New("too many redirects")
)

var _ runner.HTTPDoer = (*Client)(nil)

// Options is the network policy of a Client.
type Options struct {
	// AllowHosts lists the hosts a request may reach. An empty list
	// refuses every request.
	AllowHosts []string
	// CAFile is a PEM file of certificate authorities trusted in
	// addition to the system roots. Empty adds none.
	CAFile string
}

// Client sends requests under a host allowlist. It never uses a proxy,
// refuses link-local addresses at dial time, re-checks the allowlist on
// every redirect, and caps the response body. TLS always verifies.
type Client struct {
	allow   allowlist
	http    *http.Client
	initErr error
}

// New builds a Client. A CA file that cannot be loaded does not fail
// construction: every request then fails with the CA file category, so
// a host that never sends a request is unaffected.
func New(opts Options) *Client {
	client := &Client{allow: newAllowlist(opts.AllowHosts)}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if opts.CAFile != "" {
		pool, err := rootsWith(opts.CAFile)
		if err != nil {
			client.initErr = err
		} else {
			tlsConfig.RootCAs = pool
		}
	}

	dialer := &net.Dialer{Timeout: dialTimeout, Control: refuseLinkLocal}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		TLSClientConfig:     tlsConfig,
		TLSHandshakeTimeout: tlsHandshakeTimeout,
		IdleConnTimeout:     idleConnectionTimeout,
		MaxIdleConns:        maxIdleConnections,
	}
	client.http = &http.Client{Transport: transport, CheckRedirect: client.checkRedirect}
	return client
}

func rootsWith(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("no certificate found")
	}
	return pool, nil
}

// refuseLinkLocal rejects a connection to a link-local address, which
// covers cloud metadata endpoints. It runs on the address the dialer
// resolved, so a name that resolves to such an address is refused too.
func refuseLinkLocal(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errAddressRefused
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return errAddressRefused
	}
	if addr.Unmap().IsLinkLocalUnicast() {
		return errAddressRefused
	}
	return nil
}

func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > runner.HTTPMaxRedirects {
		return errTooManyRedirects
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return errRedirectRefused
	}
	if !c.allow.allows(req.URL) {
		return errRedirectRefused
	}
	return nil
}

// Do sends req and returns the final response. Every error is a
// *runner.HTTPError, except a canceled context, which returns the
// context's own error.
func (c *Client) Do(ctx context.Context, req runner.HTTPRequest) (runner.HTTPResponse, error) {
	if c.initErr != nil {
		return runner.HTTPResponse{}, &runner.HTTPError{Kind: runner.HTTPFailureCAFile}
	}
	target, err := url.Parse(req.URL)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return runner.HTTPResponse{}, &runner.HTTPError{Kind: runner.HTTPFailureOther}
	}
	if len(c.allow) == 0 {
		return runner.HTTPResponse{}, &runner.HTTPError{Kind: runner.HTTPFailureNoHostsAllowed}
	}
	if !c.allow.allows(target) {
		return runner.HTTPResponse{}, &runner.HTTPError{Kind: runner.HTTPFailureHostNotAllowed}
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return runner.HTTPResponse{}, &runner.HTTPError{Kind: runner.HTTPFailureOther}
	}
	for name, value := range req.Headers {
		httpReq.Header.Set(name, value)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return runner.HTTPResponse{}, classify(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.ContentLength > runner.HTTPMaxResponseBodyBytes {
		return runner.HTTPResponse{}, &runner.HTTPError{Kind: runner.HTTPFailureBodyTooLarge}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, runner.HTTPMaxResponseBodyBytes+1))
	if err != nil {
		return runner.HTTPResponse{}, classify(ctx, err)
	}
	if len(body) > runner.HTTPMaxResponseBodyBytes {
		return runner.HTTPResponse{}, &runner.HTTPError{Kind: runner.HTTPFailureBodyTooLarge}
	}
	return runner.HTTPResponse{Status: resp.StatusCode, Headers: flattenHeaders(resp.Header), Body: body}, nil
}

func flattenHeaders(header http.Header) map[string]string {
	flat := make(map[string]string, len(header))
	for name, values := range header {
		flat[name] = strings.Join(values, ", ")
	}
	return flat
}

// classify maps a transport error to a failure category. The original
// error is dropped because its text carries the URL.
func classify(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil && !errors.Is(ctxErr, context.DeadlineExceeded) {
		return ctxErr
	}
	kind := runner.HTTPFailureOther
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var netErr net.Error
	switch {
	case errors.Is(err, errAddressRefused):
		kind = runner.HTTPFailureAddressRefused
	case errors.Is(err, errRedirectRefused):
		kind = runner.HTTPFailureRedirectNotAllowed
	case errors.Is(err, errTooManyRedirects):
		kind = runner.HTTPFailureTooManyRedirects
	case errors.Is(err, context.DeadlineExceeded):
		kind = runner.HTTPFailureTimeout
	case errors.Is(err, syscall.ECONNREFUSED):
		kind = runner.HTTPFailureConnectionRefused
	case errors.As(err, &dnsErr):
		kind = runner.HTTPFailureDNS
	case errors.As(err, &certErr), isTLSError(err):
		kind = runner.HTTPFailureTLS
	case errors.As(err, &netErr) && netErr.Timeout():
		kind = runner.HTTPFailureTimeout
	}
	return &runner.HTTPError{Kind: kind}
}

func isTLSError(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var recordHeader tls.RecordHeaderError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &recordHeader)
}
