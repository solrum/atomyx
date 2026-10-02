package inspector

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"dev.atomyx/internal/diag"
)

const maxRequestBodyBytes = 16 << 10

// decodeBody reads the JSON request body into dst. It rejects an
// unknown field, trailing data and a body over maxRequestBodyBytes, so
// a misspelled key is an error instead of a silently empty value. The
// error it returns is coded INVALID_ARGUMENT.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return invalidArgument("the request body is not valid JSON for this route: "+bodyProblem(err),
			"Send a JSON object with exactly the fields the route documents.")
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return invalidArgument("the request body has data after the JSON object", "Send one JSON object.")
	}
	return nil
}

// bodyProblem names what is wrong with a body without echoing it.
func bodyProblem(err error) string {
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return "syntax error"
	case errors.As(err, &typeErr):
		return "field " + typeErr.Field + " has the wrong type"
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return strings.TrimPrefix(err.Error(), "json: ")
	case errors.Is(err, io.EOF):
		return "empty body"
	}
	return "unreadable body"
}

func invalidArgument(message, remedy string) error {
	return diag.New(diag.CodeInvalidArgument, message, remedy, nil)
}
