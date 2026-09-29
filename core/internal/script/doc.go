// Package script owns the test-script schema, parser, expression engine,
// and runner that execute a script against an Orchestra instance.
//
// This package currently ports the schema, the validator, and the YAML
// parser. Expression-bearing fields (assertExpr.expr, branch and handle
// conditions, extract and capture patterns) are carried as opaque
// strings; the expression engine and the command runner are ported in
// a later stage.
package script
