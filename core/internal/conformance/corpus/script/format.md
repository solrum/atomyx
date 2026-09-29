# Script corpus format

## Purpose

Who reads this: a contributor adding or reviewing a differential
script parse-and-validate case under
`core/internal/conformance/corpus/script/`. When to read it: before
adding a new `*.json` case file, or before changing the loader in
`../../script_corpus.go`. What this does not cover: how two results
are compared (see `../../diff.go`), or the frozen golden fixture a
case's output is diffed against (see `../../golden.go`).

## Summary

A corpus case is one JSON file holding a name, a description, and the
literal YAML text of one script or flow fragment. The differential
test feeds that YAML to the Go parser and diffs the accept-or-reject
verdict, and, when accepted, the resulting parsed structure, against a
frozen golden fixture at `../../goldens/script/<name>.json`.

## Fields

- `name` (string, required): case identifier. By convention it
  matches the filename without the `.json` extension.
- `description` (string, required): one sentence stating what the
  case exercises.
- `yaml` (string, required): the script or flow fragment's YAML
  source, exactly as a user would author it in a `.yml` file.
- `externalEnv` (object of string to string, optional): variables
  passed to the parser as the caller-supplied override a CLI `--env`
  flag, a scenario, or a parent script's `requires` / `runFlow` would
  merge on top of the script's own `env:` block, with `externalEnv`
  winning on key collision. Omit it for the common case of a script
  parsed with no external override.
- `expectErrorText` (boolean, optional, default `false`): when `true`,
  the case must be REJECTED, with Go's error text verified byte-
  identical to the reference implementation's error when the golden
  was recorded. Set this only for a path where this package
  deliberately mirrors the reference wording (for example, a zod
  issue-shaped message); most invalid-script cases omit this field,
  since error message text is otherwise not expected to match.

## Adding a case

1. Save the case as `<name>.json` in this directory.
2. No registration step is needed: `LoadScriptCorpus` in
   `../../script_corpus.go` reads every `*.json` file in this
   directory, sorted by filename.
3. Run `go test ./internal/conformance/... -update` to write the Go
   parser's verdict (and, when accepted, the parsed structure) as the
   case's golden fixture, then review the generated file before
   committing it alongside the case.
4. A case that must be REJECTED (invalid script) is exactly as
   valuable as one that must be accepted: name it so the intent is
   obvious (for example `pointer-both-actions-and-pointers`).
