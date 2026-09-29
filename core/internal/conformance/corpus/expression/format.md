# Expression corpus format

## Purpose

Who reads this: a contributor adding or reviewing a differential
expression evaluation case under
`core/internal/conformance/corpus/expression/`. When to read it: before
adding a new `*.json` case file, or before changing the loader in
`../../expression_corpus.go`. What this does not cover: how a case is
diffed against its golden fixture (see
`../../expression_differential_test.go` and `../../golden.go`).

## Summary

A corpus case is one JSON file holding a name, a description, an
expression source string, and an optional set of typed variables. The
differential test evaluates that expression with the Go expression
engine and diffs the resulting value or classified error, including
the exact error message text and source position, against a frozen
golden fixture at `../../goldens/expression/<name>.json`.

## Fields

- `name` (string, required): case identifier. Must equal the filename
  without the `.json` extension.
- `description` (string, required): one sentence stating what the
  case exercises.
- `expr` (string, required): the expression source, exactly as a user
  would author it in a script's condition, `assertExpr`, or capture
  pattern.
- `variables` (object of name to typed variable, optional): the
  lookup values available to `expr`. Omit it for an expression that
  references no variables.

Each entry in `variables` has a `type` field (`"number"`, `"string"`,
or `"boolean"`) and exactly one matching payload field:

- `type: "number"` pairs with `number` (string): the decimal text of
  the value, parsed with the same grammar as JavaScript's `Number()`
  applied to a well-formed numeric string, the contract the Go
  expression engine's number formatting matches. Special values are
  written as `"Infinity"`, `"-Infinity"`, and `"NaN"`. The literal text
  `"Inf"` is never valid here: `strconv.ParseFloat("Inf", 64)` parses
  it as positive infinity, but JavaScript's `Number("Inf")` is `NaN`,
  so that spelling alone would not match the recorded golden's
  expected value. Write `"Infinity"` (or `"-Infinity"`) whenever the
  value under test is infinite.
- `type: "string"` pairs with `string` (string): the value verbatim.
- `type: "boolean"` pairs with `boolean` (boolean): the value
  verbatim.

## Adding a case

1. Save the case as `<name>.json` in this directory.
2. No registration step is needed: `LoadExpressionCorpus` in
   `../../expression_corpus.go` reads every `*.json` file in this
   directory, sorted by filename, and validates `name`, `description`,
   and every variable's `type` and `number` text before the
   differential test ever runs. A malformed case fails the load with
   an error naming the file, not a skipped test.
3. Run `go test ./internal/conformance/... -update` to write the
   evaluation result as the case's golden fixture, then review the
   generated file before committing it alongside the case.
4. A case that must be REJECTED (a parse or evaluation error) is
   exactly as valuable as one that must succeed: name it so the intent
   is obvious (for example `numeric-division-by-zero-positive` or
   `structerr-unbalanced-paren-open-only`).
