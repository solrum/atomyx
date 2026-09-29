package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Handler is the minimal unit the Dispatcher knows about. It owns its
// own params validation and may return a *DispatcherError to control
// the error code surfaced on the wire; any other error becomes
// ErrInternalError. Handlers are not coupled to transport or session
// internals. They receive params and return a result.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

// DispatcherError carries the wire error code and optional data a
// handler wants surfaced to the caller. Returning any other error
// type from a Handler still reaches the caller, but as a generic
// ErrInternalError with no data.
type DispatcherError struct {
	Code    ErrorCode
	Message string
	Data    any
}

func (e *DispatcherError) Error() string { return e.Message }

// NewDispatcherError constructs a DispatcherError.
func NewDispatcherError(code ErrorCode, message string, data any) *DispatcherError {
	return &DispatcherError{Code: code, Message: message, Data: data}
}

// Dispatcher is a method to Handler registry. Its one responsibility
// is routing a request to the right handler and reporting the
// outcome. It knows nothing about IO streams or the session, since that
// wiring is the composition root's job.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewDispatcher constructs an empty Dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]Handler)}
}

// Register binds method to handler. It returns an error when method
// is already registered. This is a wiring-time mistake the composition root
// should fail fast on, not a wire-visible outcome.
func (d *Dispatcher) Register(method string, handler Handler) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.handlers[method]; exists {
		return fmt.Errorf("sidecar: handler already registered for method %q", method)
	}
	d.handlers[method] = handler
	return nil
}

// Dispatch invokes the handler registered for method with params. It
// returns a *DispatcherError when no handler is registered, when the
// handler itself returns one, or when the handler panics; any other
// error the handler returns is passed through unchanged for the
// caller to classify.
func (d *Dispatcher) Dispatch(ctx context.Context, method string, params json.RawMessage) (result any, err error) {
	d.mu.RLock()
	handler, ok := d.handlers[method]
	d.mu.RUnlock()
	if !ok {
		return nil, NewDispatcherError(ErrMethodNotFound, fmt.Sprintf("No handler registered for method %q", method), nil)
	}

	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("sidecar: handler for method %q panicked: %v", method, r)
		}
	}()
	return handler(ctx, params)
}

// HandleRequest dispatches req and always returns a complete wire
// Response: a success Response on a nil error, or an error Response
// built from a *DispatcherError's code/message/data, falling back to
// ErrInternalError for any other error.
func (d *Dispatcher) HandleRequest(ctx context.Context, req Request) Response {
	result, err := d.Dispatch(ctx, req.Method, req.Params)
	if err == nil {
		return SuccessResponse(req.ID, result)
	}

	var dispatchErr *DispatcherError
	if errors.As(err, &dispatchErr) {
		return ErrorResponse(req.ID, dispatchErr.Code, dispatchErr.Message, dispatchErr.Data)
	}
	return ErrorResponse(req.ID, ErrInternalError, err.Error(), nil)
}

// Methods returns every registered method name, sorted.
func (d *Dispatcher) Methods() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	methods := make([]string, 0, len(d.handlers))
	for method := range d.handlers {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}
