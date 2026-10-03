package inspector

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/session"
)

const (
	// listenAddr is the only address the server binds: loopback with a
	// system-chosen port.
	listenAddr = "127.0.0.1:0"
	// bootstrapHost is the host the bootstrap URL names. It is the IP
	// and never the name localhost, so a resolver cannot redirect it.
	bootstrapHost = "127.0.0.1"

	apiPrefix = "/api/"
	authPath  = "/api/v1/auth"
	pagePath  = "/"

	contentSecurityPolicy = "default-src 'self'; img-src 'self' blob:"
	maxAuthBodyBytes      = 1 << 10
	readHeaderTimeout     = 10 * time.Second
)

// Route is one endpoint of the inspector. Path is exactly "/" for the
// page, exactly "/assets/" for the static files beneath it, or begins
// with /api/ for an API endpoint. The server wraps
// every route in the same Host, Origin and token checks, so a handler
// never repeats them.
type Route struct {
	Method  string
	Path    string
	Handler http.Handler
	// Operator marks a route that acts on behalf of the person at the
	// page, such as asking the holder for a fresh capture. A route that
	// only reads what the session already holds leaves it false, so
	// polling the page never counts as operator activity.
	Operator bool
}

// RouteGroup builds the routes of one feature over the controller the
// server was given. A group is the unit of extension: a feature adds a
// group to Config.Groups and the server never changes.
type RouteGroup func(c session.Controller) []Route

// Config assembles a Server.
type Config struct {
	// Controller is the held session's read and capture port.
	Controller session.Controller
	// Logger receives one entry per bootstrap exchange. It is required,
	// because a hijacked code is visible only through that log.
	Logger logging.Logger
	// Entropy supplies the randomness of codes and tokens. It defaults
	// to crypto/rand and exists so a test can be deterministic.
	Entropy io.Reader
	// Groups contribute the routes of the server.
	Groups []RouteGroup
}

// Server is the loopback inspector server. Create it with New, which
// binds the listener, then call Serve.
type Server struct {
	auth     *authority
	logger   logging.Logger
	handler  http.Handler
	listener net.Listener
	http     *http.Server
	port     string
}

// New binds 127.0.0.1 on a system-chosen port and returns a server that
// is not yet serving. It fails when the controller or the logger is
// missing, when a group yields an invalid or duplicate route, or when
// the listener cannot bind.
func New(cfg Config) (*Server, error) {
	if cfg.Controller == nil {
		return nil, errors.New("inspector: a session controller is required")
	}
	if cfg.Logger == nil {
		return nil, errors.New("inspector: a logger is required")
	}
	entropy := cfg.Entropy
	if entropy == nil {
		entropy = rand.Reader
	}
	s := &Server{auth: newAuthority(entropy), logger: cfg.Logger}

	mux := http.NewServeMux()
	registered := map[string]bool{}
	add := func(r Route) error {
		pattern, err := routePattern(r)
		if err != nil {
			return err
		}
		if registered[pattern] {
			return fmt.Errorf("inspector: route %s is registered twice", pattern)
		}
		registered[pattern] = true
		mux.Handle(pattern, r.Handler)
		return nil
	}
	if err := add(Route{Method: http.MethodPost, Path: authPath, Handler: http.HandlerFunc(s.handleAuth)}); err != nil {
		return nil, err
	}
	for _, group := range cfg.Groups {
		for _, r := range group(cfg.Controller) {
			if err := add(r); err != nil {
				return nil, err
			}
		}
	}

	listener, err := new(net.ListenConfig).Listen(context.Background(), "tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("inspector: listen: %w", err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("inspector: listener address: %w", err)
	}
	s.listener, s.port = listener, port
	s.handler = s.secure(mux)
	s.http = &http.Server{Handler: s.handler, ReadHeaderTimeout: readHeaderTimeout}
	return s, nil
}

func routePattern(r Route) (string, error) {
	switch {
	case r.Method == "" || r.Handler == nil:
		return "", errors.New("inspector: a route needs a method and a handler")
	case r.Path == pagePath:
		return r.Method + " /{$}", nil
	case r.Path == assetsPrefix:
		return r.Method + " " + assetsPrefix, nil
	case strings.HasPrefix(r.Path, apiPrefix) && !strings.ContainsAny(r.Path, "{}"):
		return r.Method + " " + r.Path, nil
	}
	return "", fmt.Errorf("inspector: route path %q must be %q, %q or begin with %q", r.Path, pagePath, assetsPrefix, apiPrefix)
}

// Addr is the address the server listens on, always 127.0.0.1 with the
// chosen port.
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Handler is the full request pipeline, checks included, for tests that
// drive it without a connection.
func (s *Server) Handler() http.Handler { return s.handler }

// NewBootstrapURL mints a single-use code and returns the URL that
// carries it in the fragment. It invalidates any earlier code that was
// never exchanged and leaves issued tokens valid.
func (s *Server) NewBootstrapURL() (string, error) {
	code, err := s.auth.mintCode()
	if err != nil {
		return "", fmt.Errorf("inspector: mint bootstrap code: %w", err)
	}
	return "http://" + net.JoinHostPort(bootstrapHost, s.port) + "/#" + code, nil
}

// Serve answers requests until Close. It returns nil after a Close.
func (s *Server) Serve() error {
	if err := s.http.Serve(s.listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close stops the server and drops open connections, event streams
// included.
func (s *Server) Close() error { return s.http.Close() }

// secure wraps next in the checks every request passes, in this order:
// the Host check for every path, the response headers, then for /api/
// the Origin check of non-GET requests and the token check.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			writeCoded(w, http.StatusForbidden, codeHostNotAllowed, "the request does not name this server",
				"Open the URL the atomyx inspect command printed, which names 127.0.0.1.")
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(r.URL.Path, apiPrefix) {
			if r.Method != http.MethodGet && r.Header.Get("Origin") != "http://"+r.Host {
				writeCoded(w, http.StatusForbidden, codeOriginNotAllowed, "the request does not come from the inspector page",
					"Send the request from the inspector page, not from another site.")
				return
			}
			if r.URL.Path != authPath && !s.auth.valid(r.Header.Get(tokenHeader)) {
				writeCoded(w, http.StatusUnauthorized, codeTokenRequired, "send a valid "+tokenHeader+" header",
					"Open the URL the atomyx inspect command printed, or press Enter in its terminal for a fresh one.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostAllowed(host string) bool {
	return strings.EqualFold(host, bootstrapHost+":"+s.port) || strings.EqualFold(host, "localhost:"+s.port)
}

func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAuthBodyBytes))
	if err := dec.Decode(&body); err != nil {
		writeCoded(w, http.StatusBadRequest, diag.CodeInvalidArgument, "send a JSON body with a code",
			"Open the URL the atomyx inspect command printed, which carries the code.")
		return
	}
	token, ok, err := s.auth.exchange(body.Code)
	fields := logging.LogContext{"remote": r.RemoteAddr}
	switch {
	case err != nil:
		s.logger.Error("inspector.auth.failed", err, fields)
		writeCoded(w, http.StatusInternalServerError, codeInternal, "the token could not be issued",
			"Press Enter in the atomyx inspect terminal for a fresh URL, then try again.")
	case !ok:
		s.logger.Warn("inspector.auth.refused", fields)
		writeCoded(w, http.StatusUnauthorized, codeCodeRefused, "the bootstrap code is wrong or already used",
			"Press Enter in the atomyx inspect terminal for a fresh URL.")
	default:
		s.logger.Info("inspector.auth.issued", fields)
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
