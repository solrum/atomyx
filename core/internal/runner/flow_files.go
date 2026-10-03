package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"dev.atomyx/internal/script"
)

// resolveStepsOrFile returns sof's inline steps directly, or loads
// them from the file it names through loader, interpolating the
// file's `${var}` placeholders against env. It is the shared
// resolution path for a `handle`/`branch` `do` block.
//
// A bad `do` file (missing, or failing to parse) panics rather than
// returning an error detail string of its own: the runner's per-step
// dispatch recovers any panic reaching a CommandDefinition.Execute
// call and reports it as an "Exception: " prefixed detail, so
// resolveStepsOrFile lets that failure propagate the same way. The
// second return value stays reserved for the one failure mode that
// is not a panic: no FlowLoader configured for this runner at all.
func resolveStepsOrFile(loader FlowLoader, sof, written script.StepsOrFile, env map[string]string) ([]script.ScriptStep, *string) {
	if sof.File == nil {
		return sof.Steps, nil
	}

	// Resolve to an absolute path before loading or reporting it, so
	// the error message and the loader both see the same path
	// regardless of the current working directory.
	filePath := *sof.File
	if abs, err := filepath.Abs(filePath); err == nil {
		filePath = abs
	}
	shownPath := filePath
	if written.File != nil {
		shownPath = absolutePath(*written.File)
	}

	if loader == nil {
		return nil, strPtr(fmt.Sprintf("no flow loader configured to resolve file: %s", shownPath))
	}

	flow, err := loader.LoadFlow(filePath, env)
	if err != nil {
		var parseErr *script.ParseError
		if errors.As(err, &parseErr) {
			panic(err)
		}
		panic(errors.New(restoreAuthored(nodeStyleFileErrorMessage(err, filePath), filePath, shownPath)))
	}
	return flow.Steps, nil
}

// nodeStyleFileErrorMessage renders a Go file-read error into the
// same "<code>: <description>" shape command failures use elsewhere
// in this package, naming an errno mnemonic before the description
// rather than surfacing Go's own wrapped path-error text. Go's os
// package errors carry the underlying condition through
// fs.ErrNotExist / fs.ErrPermission, which is enough to recognize the
// two cases a script author's do-file reference actually hits (the
// file is missing, or its permissions forbid the read). Any other
// failure falls back to err's own message, since no other case
// reachable from this runner needs a fixed mnemonic form.
func nodeStyleFileErrorMessage(err error, path string) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("ENOENT: no such file or directory, open '%s'", path)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("EACCES: permission denied, open '%s'", path)
	default:
		return err.Error()
	}
}

// absolutePath resolves path against the working directory, or returns it
// unchanged when that fails.
func absolutePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
