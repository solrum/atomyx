package conformance

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
)

// DiffJSON parses two JSON documents into generic values and compares
// them structurally, ignoring key order and formatting. It returns an
// empty report when the values are equal, and otherwise a human-readable
// report describing every mismatched path.
func DiffJSON(want, got []byte) (report string, equal bool, err error) {
	wantValue, err := decodeJSON(want)
	if err != nil {
		return "", false, fmt.Errorf("decoding want value: %w", err)
	}
	gotValue, err := decodeJSON(got)
	if err != nil {
		return "", false, fmt.Errorf("decoding got value: %w", err)
	}
	report = cmp.Diff(wantValue, gotValue)
	return report, report == "", nil
}

// DiffRunnerJSON compares two JSON documents the way DiffJSON does,
// except it first normalizes every failed step or step-event detail
// string down to its stable leading token. The Go runtime is the
// strategic implementation and is authoritative over the human-
// readable wording of an error detail, so this comparison asserts
// only the error code and the structural shape around it (ok flags,
// step indices, command names, durations): everything the caller
// contract actually depends on. A successful step's detail is never
// touched, because it reports a verified outcome, such as an
// extracted value or a checked assertion, rather than an error
// contract, so its exact text is still part of what must match.
//
// Use this instead of DiffJSON for any suite that runs a script
// through both the Go runner and its reference oracle and compares
// their step results: the runner, scenario, and suite differentials
// all nest the same step and step-event shape.
func DiffRunnerJSON(want, got []byte) (report string, equal bool, err error) {
	wantValue, err := decodeJSON(want)
	if err != nil {
		return "", false, fmt.Errorf("decoding want value: %w", err)
	}
	gotValue, err := decodeJSON(got)
	if err != nil {
		return "", false, fmt.Errorf("decoding got value: %w", err)
	}
	normalizeErrorDetails(wantValue)
	normalizeErrorDetails(gotValue)
	report = cmp.Diff(wantValue, gotValue)
	return report, report == "", nil
}

// normalizeErrorDetails walks a decoded JSON value and truncates the
// "detail" field of every object that also carries "ok": false to its
// stable leading token (see normalizeErrorDetailToken). Step and
// step-event objects are the only shape in this package's differential
// suites where "ok" and "detail" sit as siblings, so this walk cannot
// reach into an unrelated field: a successful step's "ok": true object
// is left untouched, and every other field on the object (stepIndex,
// command, durationMs, and so on) is untouched regardless of ok.
func normalizeErrorDetails(value any) {
	switch v := value.(type) {
	case map[string]any:
		if ok, isOk := v["ok"].(bool); isOk && !ok {
			if detail, hasDetail := v["detail"].(string); hasDetail {
				v["detail"] = normalizeErrorDetailToken(detail)
			}
		}
		for _, child := range v {
			normalizeErrorDetails(child)
		}
	case []any:
		for _, child := range v {
			normalizeErrorDetails(child)
		}
	}
}

// normalizeErrorDetailToken returns the stable leading token of an
// error or exception detail string: the substring up to and including
// the first colon, for example "POINTER_MULTI_NOT_SUPPORTED:" or
// "Exception:". Every detail string this codebase produces for a
// failure follows that "<CODE>: <free-text guidance>" shape, and the
// guidance after the colon is Go-owned wording the conformance
// differential does not assert. A string with no colon has no
// free-text remainder to drop, so it is returned unchanged.
func normalizeErrorDetailToken(detail string) string {
	if idx := strings.IndexByte(detail, ':'); idx != -1 {
		return detail[:idx+1]
	}
	return detail
}

func decodeJSON(raw []byte) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}
