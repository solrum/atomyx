// Package redact scrubs secret values out of text before it leaves the
// machine, for example inside a diagnostic bundle. It is pure: it reads
// no file and no process state, so every caller decides where the secret
// values come from.
package redact
