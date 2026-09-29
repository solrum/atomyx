# Runner corpus case format

## Purpose

Reference for the JSON shape of one file under `corpus/runner/`. Read
this before adding or editing a runner differential case. It does not
cover how the differential test executes a case (see
`runner_differential_test.go`) or the frozen golden fixture a case's
output is diffed against (see `../../golden.go`).

## Shape

Each `*.json` file is one case:

```json
{
  "name": "assert-api-multi-key-order",
  "description": "assertApi fails two body keys at once, in declaration order",
  "env": { "userId": "42" },
  "yaml": "appId: com.test\nenv:\n  x: \"1\"\n---\n- launchApp\n",
  "flowFiles": {
    "login.yml": "- tap: Login\n- type: \"${email}\"\n"
  },
  "driver": {
    "hierarchies": [ { "attributes": {}, "children": [] } ],
    "capabilities": { "canMultiPointer": true },
    "screen": { "width": 430, "height": 932 }
  },
  "network": {
    "requests": [
      { "method": "POST", "url": "/api/login", "status": 200,
        "headers": {}, "body": { "ok": true }, "timestampMs": 0 }
    ]
  }
}
```

Field reference:

- `name` (required): must equal the filename stem. Used as the Go
  subtest name.
- `description` (required): one sentence, shown in a mismatch report.
- `env` (optional): the external environment merged over the script's
  own `env:` block before `${var}` interpolation, matching
  `ScriptRunnerDeps`'s externalEnv argument to `ParseScript` /
  `parseScript`.
- `yaml` (required): the main script source, YAML text exactly as a
  user would author it. May contain the placeholder token `{{DIR}}`
  (see "Flow files and the `{{DIR}}` placeholder" below).
- `flowFiles` (optional): a map of filename to YAML source. Every flow
  file a `requires` entry, a `runFlow.file`, a `handle`/`branch`
  string `do`/`default`, or a string `setup` needs is declared here.
  Flow file content may itself contain `{{DIR}}` (a flow file that
  references a second flow file, or that reads a parent variable
  through `${var}` interpolation, needs no placeholder for the
  variable case, only for a further file reference).
- `driver` (required): the `MockDriver` program (see below).
- `network` (optional): the `NetworkCapture` program. Omitting this
  field means the run has NO network capture adapter configured
  (`ScriptRunnerDeps.NetworkCapture` / `networkCapture` is absent),
  exercising `capture`'s "no adapter configured" failure path. A case
  that exercises `capture`/`assertApi`/`extract`/`branch` success
  needs this field present, even with a `proxy: optional` script.
- `expectRunError` (optional, default `false`): marks a case whose
  main script's `requires` names a flow file that cannot be loaded
  (missing, or failing to parse). There is no comparable `ScriptResult`
  for this case: a bad required flow makes the Go runner's `Run` return
  a non-nil error instead of a graceful result, so this case has no
  golden fixture and the differential test asserts only that Go failed
  to complete, rather than diffing a result. A missing or invalid file
  reached through `runFlow`, `handle`, or `branch` instead is a normal
  step failure with its own comparable `ScriptResult` (see "Flow
  files" below) and does not set this field.

### `driver`

- `hierarchies` (required, at least one element): an ordered array of
  `TreeNode` JSON objects (the same cross-platform shape: `attributes`,
  `children`, and the optional booleans `clickable`, `enabled`,
  `focused`, `selected`, `checked`, `visible`), staged onto the mock
  driver in order. Once exhausted, the last staged tree is returned
  from every further `hierarchy()` call (sticky final state), the same
  semantics `MockDriver.stageHierarchy` documents.
  Most cases need only one tree: the runner's own retry/poll loops
  resolve against a single hierarchy when the target element is
  already present in it.
- `capabilities` (optional): a partial override merged over the mock
  driver's default capability set (`canScreenshot`, `canEraseText`,
  `canHideKeyboard` true; everything else false; `supportedKeyCodes`
  `[back, home, enter]`). Only set the fields a case's `pointer`
  gesture needs (`canMultiPointer`, `canPressure`).
- `screen` (optional): overrides the mock driver's default screen size
  (`{width: 430, height: 932}`).

### `network`

- `requests`: an ordered array of `CapturedRequest` objects
  (`method`, `url`, `status`, `headers`, `body`, `timestampMs`). The
  test harness's fake `NetworkCapture` serves them to `capture` steps
  in declaration order: each `capture` step's `waitForRequest(pattern,
  timeoutMs)` scans the remaining (not yet consumed) requests for the
  first one whose `"METHOD /path"` matches `pattern` (`*` wildcard
  supported, case-sensitive method), consumes it, and returns it
  immediately with no simulated delay. A `pattern` that matches
  nothing fails the step with `No request matching "<pattern>" was
  recorded.`, an exact fixed string that is test-harness scaffolding,
  not part of the runner contract under test, so it carries no
  differential signal on its own.

## Flow files and the `{{DIR}}` placeholder

The Go runner resolves a flow-file reference (`requires`,
`runFlow.file`, a string `do`/`default`/`setup`) as an absolute path
(`filepath.Abs`), a no-op when the reference is already absolute. The
differential test exploits this to make a case's flow-file references
immune to the test binary's own working directory:

1. The differential test creates one fresh temporary directory per
   case.
2. It writes every entry in `flowFiles` to `<tempdir>/<filename>`,
   after replacing every occurrence of the literal token `{{DIR}}` in
   that entry's own source with the temporary directory's absolute
   path.
3. It performs the same `{{DIR}}` replacement on the case's own `yaml`
   field before parsing it.
4. The Go runner then reads flow files directly off that temporary
   directory, resolving each reference from the absolute path already
   baked into the YAML text it parses.

A case that references a flow file therefore always writes the
reference as `{{DIR}}/name.yml`, never a bare relative name.

A case that exercises a *missing* flow file references a
`{{DIR}}/name.yml` path with no matching `flowFiles` entry, so nothing
is ever written at that path. A case that exercises an *invalid* flow
file gives its `flowFiles` entry content that fails to parse: the
shortest reliable way is a single YAML document with no `steps:` key
and no `---` separator (e.g. `"appId: com.bad\n"`), which fails with
the "Single-document script must have a 'steps' array" error
regardless of what else is wrong with it.

## Determinism

Every case runs against a `FakeClock` that starts at 0 and advances
only when a command explicitly sleeps (a `sleep` step, `handle`'s poll
loop, `assertNotVisible`'s poll loop): advancing is immediate on both
sides, so no case ever waits on real time. A case that wants to
exercise a timeout path sets `timeoutMs: 0` (or omits it where the
command's own default already resolves in one instant check) so the
poll loop's very first check is also its last, needing only one staged
hierarchy.
