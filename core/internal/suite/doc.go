// Package suite discovers the ordered set of test script and
// scenario files a suite selector resolves to: a filesystem walk over
// configured test roots (or the working directory), narrowed by glob
// patterns and by each candidate file's own top-level YAML tags
// field.
package suite
