# Conformance corpus format

## Purpose

Who reads this: a contributor adding or reviewing a differential
conformance case under `core/internal/conformance/corpus/`. When to
read it: before adding a new `*.json` case file, or before changing
the loader in `../corpus.go`. What this does not cover: how two
responses are compared (see `../diff.go`), or the frozen golden
fixtures under `../goldens/` that most of the differential suites in
this package diff their live Go output against (see `../golden.go`).

## Summary

A corpus case is one JSON file holding a name, a description, and an
ordered list of JSON-RPC steps. Each step names a method, its
optional params, and the exact response value the harness expects,
embedded directly in the case file as its `expect` field.
`corpus_test.go`'s `TestLoadCase_MetaHandshake` and `TestLoadCorpus`
exercise `LoadCase` and `LoadCorpus` against `meta-handshake.json` to
prove the loader itself is correct; none of the 14 differential
suites under this package replay a generic RPC-step case this way
today. Each of them instead loads its own dedicated corpus type (see
the sibling sections in this file and the per-suite `format.md` files
under this directory's subdirectories) and diffs the Go
implementation's live output against a frozen golden fixture at
`../goldens/<suite>/<case-name>.json`, described in `../golden.go`.

## Fields

- `name` (string, required): case identifier. By convention it
  matches the filename without the `.json` extension.
- `description` (string, required): one sentence stating what the
  case exercises.
- `steps` (array, required, at least one entry): JSON-RPC calls
  executed against the sidecar in order.
  - `method` (string, required): the JSON-RPC method name.
  - `params` (object, optional): request params. Omitted or empty
    means the call is sent with an empty params object.
  - `expect` (any JSON value, required): the response's `result`
    value. Comparison is structural, so key order and whitespace do
    not matter, but every key, value, and array element must match.

See `meta-handshake.json` in this directory for a worked example: it
calls `protocolVersion` then `listMethods` and asserts the recorded
response for each.

## Adding a case

1. Save the case as `<name>.json` in this directory, with `expect`
   holding the response value the case should assert.
2. No registration step is needed: `LoadCorpus` in `../corpus.go`
   reads every `*.json` file in this directory, sorted by filename.
3. `corpus_test.go` covers `LoadCase` and `LoadCorpus` directly against
   this format; it does not replay a case against a live implementation.

## Normalizer corpus format

Who reads this: a contributor adding or reviewing a differential
tree-normalizer case under `normalizers/android/` or
`normalizers/ios/`. When to read it: before adding a new `*.json`
case file in either directory, or before changing the loader in
`../normalizer_corpus.go`. What this does not cover: the RPC-step
corpus format above, or how a golden fixture is structured (see
`../golden.go`).

A normalizer case is one JSON file holding a name, a description, and
one raw tree, in the raw shape a device agent reports (Android's
`GET /tree` shape, or the Swift driver's `dumpRawTree` shape). Unlike
an RPC-step case, a normalizer case carries no `expect` field: the
differential test normalizes `rawTree` through the Go normalizer and
diffs the result against a frozen golden fixture at
`../goldens/normalizers/<android|ios>/<name>.json`.

Fields:

- `name` (string, required): case identifier, matching the filename
  without the `.json` extension.
- `description` (string, required): one sentence stating what the
  case exercises.
- `rawTree` (object, required): one raw tree node in the platform's
  own raw shape, decoded on the Go side into
  `transport.AndroidTreeNode` (Android) or `iosdriver.IosRawElement`
  (iOS).

Adding a case:

1. Save the case as `<name>.json` in `normalizers/android/` or
   `normalizers/ios/`.
2. No registration step is needed: `LoadNormalizerCorpus` in
   `../normalizer_corpus.go` reads every `*.json` file in the given
   directory, sorted by filename.
3. Run `go test ./internal/conformance/... -update` to write the
   normalized output as the case's golden fixture, then review the
   generated file before committing it alongside the case.

## Driver (pure-helper) corpus format

Who reads this: a contributor adding or reviewing a differential pure-
helper case under `driver/`. When to read it: before adding a new
`*.json` case file in that directory, or before changing the loader
in `../driver_corpus.go`. What this does not cover: the stateful
Orchestra pipeline (scroll-into-view, retry, and the keyboard-dismiss
gate), which this corpus does not exercise because it lives above the
pure functions this corpus targets. Those are covered by
`core/internal/orchestra`'s MockDriver-based behavior tests instead.

A driver case is one JSON file holding a name, a description, a
function name, and that function's inputs. Like a normalizer case, a
driver case carries no `expect` field: the differential test runs the
same inputs through the Go implementation and diffs the result against
a frozen golden fixture at `../goldens/driver/<name>.json`.

Common fields:

- `name` (string, required): case identifier, matching the filename
  without the `.json` extension.
- `description` (string, required): one sentence stating what the
  case exercises.
- `function` (string, required): one of `"compileSelector"`,
  `"find-in-tree"`, `"detectObscurement"`, `"hashVisibleLeaves"`,
  `"bucketBounds"`, `"swipeDirection"`, `"scrollTowardElement"`,
  `"swipeCenter"`, `"clamp"`, or `"suggestSelectors"`. The remaining fields required depend
  on which.

`"compileSelector"`, `"find-in-tree"`, and `"detectObscurement"` take
a `selector` and a `tree`:

- `selector` (object, required): a `Selector` value. Pattern fields
  (`id`, `text`, `label`, `hint`, `value`) accept either a plain JSON
  string (an exact-match literal) or `{"regex": string, "flags"?:
  string}` (a compiled pattern), matching the wire shape
  `core/internal/mcp/schema_helpers.go` compiles MCP tool input
  through.
- `tree` (object, required): one `TreeNode` in the canonical
  cross-platform shape (`attributes`, `children`, and the optional
  `clickable` / `enabled` / `focused` / `selected` / `checked` /
  `visible` state booleans). Every node, including leaves, must carry
  an explicit `children` array; an omitted `children` field is a
  malformed `TreeNode`.

`"compileSelector"` returns every node the compiled, priority-
broadened, ranked filter matches, in order. `"find-in-tree"` resolves
only the first match to its parsed bounds and center point (or `null`
bounds and point when the matched node carries no `bounds`
attribute), or reports `found: false` when nothing matches.
`"detectObscurement"` resolves the selector's first match within the
tree and reports `found: false` when nothing matches, or
`{found: true, result}` where `result` is the `ObscurementResult`
(`{obscured: false}`, or `{obscured: true, obscurer: {role, id,
label}}`) `detectObscurement` computes for that match against the
same tree.

`"hashVisibleLeaves"` takes only a `tree` (same shape as above) and
returns the djb2 hex hash of its visible leaves, the tree-signature
`ScrollController` uses to detect scroll-boundary saturation.

`"bucketBounds"` takes a `bounds` field holding a plain JSON string
(the raw `"left,top,right,bottom"` attribute, not a parsed object)
and returns the string with each component quantized into a bucket
index. A component that fails to parse as a number passes through
unchanged.

`"swipeDirection"` takes no selector or tree. Instead:

- `screen` (object, required): `{width, height}` in logical points.
- `direction` (string, required): one of `"up"`, `"down"`, `"left"`,
  `"right"`.
- `fraction` (number, required): the swipe distance as a fraction of
  the screen in the swipe axis.

It returns `{from, to}`, the two endpoints `Orchestra.swipeDirection`
(TypeScript) and `Orchestra.SwipeDirection` (Go) dispatch a swipe
gesture between.

`"scrollTowardElement"` takes:

- `bounds` (object, required): `{left, top, right, bottom}`, the
  off-viewport element's bounds.
- `screen` (object, required): `{width, height}` in logical points.
- `maxScrollFraction` (number, required): the scroll-amount cap, as a
  fraction of the screen height.

It returns `{from, to}`, the swipe endpoints `ScrollController`
dispatches to move that element toward the viewport's vertical
center.

`"swipeCenter"` takes:

- `screen` (object, required): `{width, height}` in logical points.
- `direction` (string, required): `"up"` or `"down"`.
- `maxScrollFraction` (number, required): the swipe distance, as a
  fraction of the screen height.

It returns `{from, to}`, the swipe endpoints `ScrollController`'s
scroll-search probe dispatches for a blind center-column swipe.

`"clamp"` takes `value`, `min`, and `max` (numbers, all required) and
returns `value` clamped into `[min, max]`, or `min` when the range is
inverted (`max < min`).

`"suggestSelectors"` takes a `tree` and a `targetPath` (array of
child indexes from the root, `[]` for the root, required) and returns
the ranked candidates `orchestra.SuggestSelectors` produces, each as
`{selector, unique, matchCount}`.

Adding a case:

1. Save the case as `<name>.json` in `driver/`.
2. No registration step is needed: `LoadDriverCorpus` in
   `../driver_corpus.go` reads every `*.json` file in the directory,
   sorted by filename.
3. Run `go test ./internal/conformance/... -update` to write the
   function's output as the case's golden fixture, then review the
   generated file before committing it alongside the case.
