# Atomyx CLI reference

## Purpose

This is a reference document. It answers exactly one question: what
does each `atomyx` CLI command do, and what are its flags.

Read this before running an `atomyx` command whose flags you are
unsure of, or before scripting `atomyx` into a CI pipeline. It does
not cover MCP tool usage (the agent-facing surface), installation, or
project config file authoring. See
[`core/internal/config/atomyx-config.schema.json`](../core/internal/config/atomyx-config.schema.json)
for the config schema and [`docs/README.md`](./README.md) for where
those other guides land.

Every command below runs `--help` (or `-h`) after the command name to
print this same reference from the terminal, for example
`atomyx driver run --help`.

## Summary

The `atomyx` binary routes to modules. Today one module ships
commands: `driver`, for device interaction. A second module, `skills`,
installs and updates the bundled Claude skill files.

```
atomyx <module> <command> [flags]
```

| Command | What it does |
|---|---|
| `atomyx driver run` | Runs a single YML test script against a device. |
| `atomyx driver suite` | Runs a named suite or an ad-hoc selection of flows. |
| `atomyx driver doctor` | Checks that a device is reachable and healthy, or migrates flat run records into run folders. |
| `atomyx driver inspect` | Serves a browser inspector for a device: its live UI tree and screenshot, with selector suggestions. |
| `atomyx driver list-devices` | Lists Android, iOS simulator, and iOS physical devices. |
| `atomyx driver version` | Prints the driver module's version. |
| `atomyx skills init` | Copies the bundled skill and agent files into a project. |
| `atomyx skills update-skills` | Updates an existing skills install to the bundled version. |

Five shortcuts skip the `driver` module name: `atomyx run`, `atomyx
suite`, `atomyx doctor`, `atomyx inspect`, and `atomyx devices` map to
`atomyx driver run`, `atomyx driver suite`, `atomyx driver doctor`,
`atomyx driver inspect`, and `atomyx driver list-devices`. Two shortcuts skip
the `skills` module name: `atomyx init` and `atomyx update-skills`.

The CLI ships exactly two modules: `driver` and `skills`. The MCP
server is a separate stdio binary, `atomyx-mcp`, that an AI client
launches directly instead of going through the `atomyx` binary.

## Module and command structure

```
atomyx driver run [flags]
atomyx driver suite [name] [flags]
atomyx driver doctor [flags]
atomyx driver inspect [flags]
atomyx driver list-devices [flags]
atomyx driver version
atomyx driver help

atomyx skills init [flags]
atomyx skills update-skills [flags]
```

`atomyx <module>` with no command prints that module's usage. `atomyx
help`, `atomyx --help`, and `atomyx -h` print the root usage listing
every module.

## driver run

Runs a single YML test script against a device: connects to the
driver, executes each step or scenario script, and reports the
result.

Usage:

```
atomyx driver run --file <path> [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--file <path>` | Path to the YML test script. Required. | none | no |
| `--platform <ios\|android>` | Target platform. | the selected config environment's platform, or `android` | no |
| `--device <id>` | Device identifier, as reported by `atomyx driver list-devices`. | none | no |
| `--proxy <type:path>` | Network capture target, for example `file:/tmp/capture.jsonl`. | the selected config environment's proxy, when configured | no |
| `--config <path>` | Path to an Atomyx project config file. | discovered by walking up from the current directory for `atomyx.config.yaml` or `atomyx.config.yml` | no |
| `--env <name>` | Selects a named environment from the project config. | applied automatically when the config declares exactly one environment | no |
| `--var <KEY=VALUE>` | Injects a variable, available to the script as `${KEY}`. Overrides `--var-file`, the selected config environment's variables, and the script's own `env` block, in that order. | none | yes |
| `--var-file <path>` | JSON file of flat key-value string variables, merged in before `--var`. | none | no |
| `--preconditions <skip\|force>` | Overrides every precondition's own policy for this run. `skip` does nothing for every precondition. `force` runs every precondition's setup unconditionally. Takes precedence over the scenario's and the project config's `preconditions` field. | the precondition's own policy | no |
| `--skip-preconditions` | Shorthand for `--preconditions skip`. | off | no |
| `--report <formats>` | Enables report generation and selects its output formats: `html`, `json`, `junit`, or any combination separated by commas. Overrides the project config's `report` block. | the project config's `report.formats`, or `html` when reporting is enabled by `--report-dir` alone | no |
| `--report-dir <path>` | Root directory the run folder is written under. See "Run folder" below. | the project config's `report.outDir`, or the storage root | no |
| `--video` | Records the device screen and keeps the video of a failed run. See "Screen recording" below. | the project config's `video` setting, which defaults to `off` | no |
| `--break <file>:<line>` | Holds the run before the authored top-level step at this location. See "Debugging a run" below. | none | yes |
| `--json` | Outputs the result as JSON instead of text. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

A script declaring `proxy: required` needs a network capture adapter
configured before it runs. When `--proxy` is not given and the
selected config environment declares no proxy either, the run is
refused before any step executes: exit code `1`, zero steps reported,
and a top level abort naming the code `PROXY_REQUIRED`, an
operator-facing message, and a remedy pointing at `--proxy`. The
`--json` output carries the same information as a top level `abort`
object (`reason`, `code`, `message`, `remedy`). See the `proxy` field
and the Proxy Setup section in
[`yml-script-reference.md`](./yml-script-reference.md) for how to
configure capture per platform.

Every run writes a run folder under a storage root, with or without
`--report`. See "Run folder" below for the layout and the root
resolution.

Example:

```
atomyx driver run --file test.yml --platform ios --device <UDID>
atomyx driver run --file test.yml --report html,json --report-dir ./reports
```

Exit codes: `0` when the script passed, `1` when the script ran and
failed, `2` when the command was misused or a usage or config error
occurred, `130` when SIGINT interrupted the run, and `143` when SIGTERM
did. An interrupted run prints `interrupted` and finalizes its run folder as
cancelled before it exits.

### Debugging a run

`--break <file>:<line>` pauses the run before the step written at that
line of that script file, and `--break` can be repeated. Example:

```
atomyx run --file test.yml --break test.yml:12 --break login.yml:3
```

- A location is a script file and a 1-based line. The file may be the
  script given to `--file` or a script a scenario runs. The inline
  `inline:<line>` form applies only to MCP `run_script` and is refused
  here.
- A location that matches no authored top-level step of the run's
  scripts is a usage error. The check runs before the device is taken,
  so a typo never holds a device.
- When the run stops, the terminal prints where it stopped and says to
  press Enter. Enter resumes the run.
- Before a resume, the run checks that the next tap, type, long press
  or read can find its target. When it cannot, the terminal prints why
  and a second Enter resumes anyway.
- Under `--json` the pause prompt goes to stderr, so the JSON result on
  stdout stays parseable.
- Standard input must be a terminal, because a paused run waits for a
  key. Otherwise the command exits with `2` before the device is taken,
  so a job without a terminal can never hang.
- A breakpoint the run never reached is reported as a warning on stderr
  when the run ends.
- Ctrl-C while paused interrupts the run and exits with `130`.
- `atomyx suite` refuses `--break`.

Terminal debugging only resumes. To step, run a step or read variables
while the run is paused, attach `atomyx inspect` to the paused run. See
[known limitations](./known-limitations.md).

### Run folder

Every `run` and `suite` command writes a run folder, with or without
`--report`. The folder holds the run's `meta.json`, its screenshots in
`artifacts/`, and, when a report is requested, the rendered report in
`report/`. The CLI prints `Run folder: <path>` on stderr when the
command ends. With `--json`, the printed result carries the same path
in a top level `runFolder` field and the line is not printed.

### Failure artifacts

When a step of a script run fails, the run folder's `artifacts/`
directory receives three files for that step, where `NNN` is the step's
1-based position in the script, the same number the report prints as
`Failed at step N`:

- `step-NNN-failure.png`, the screenshot. The extension follows the
  image the device returns, so a JPEG screenshot is `step-NNN-failure.jpg`.
- `step-NNN-tree.json`, the UI hierarchy at the moment of failure.
- `step-NNN-log.jsonl`, one JSON object per event of the failed step,
  followed by a note object for each artifact that could not be captured.

A scenario or suite run writes the same three files for each script that
fails, with the script's place in the run as a name prefix, for example
`1-c1-yml-step-002-failure.png` for the second script of a scenario.
The prefix is the script's scope, made of its position and its path,
lowercased and reduced to letters, digits, and hyphens, so two scripts
never share a name. A scenario or suite also writes each screenshot into
`artifacts/` the moment it is taken, under the same name the report
links.

The capture is bounded at 5 seconds. When the bound is reached, the
files that finished are kept and the log ends with a `truncated` note
naming what is missing. A run that stops because the device became
unreachable writes none of these files, and a capture problem never
changes the exit code or the step result.

### Screen recording

A run records the device screen when `--video` is given or when the
project config sets the top-level key `video` to `onFailure`. The
values are `off`, the default, and `onFailure`. The flag turns recording
on whatever the config says, and without the flag the config decides.
With recording off, the command makes no recorder call.

The recording covers the end of the run. It rotates through segments
and keeps the last two, so a long run keeps its final minutes and never
grows a file without bound.

On Android the device writes a frame only when the screen changes, so a
run shorter than about six seconds can keep a video of a single frame.
The failure screenshot of the run still covers that case.

The video is kept only when the run fails. A passed run, a run stopped
by SIGINT or SIGTERM, and a run that lost its device keep no video, and
the recording process on the device is stopped in every case. Kept
files are written to `artifacts/` as `video-run-1.mp4`,
`video-run-2.mp4`, and so on, oldest first.

The kept files are reported in these places:

- `meta.json` has a `recording` object with `state`, which is `kept`,
  `unsupported`, or `error`, the `files` when the state is `kept`, and
  the error `code` otherwise. The object is absent when recording was
  off or the video was discarded.
- The `--json` result of `run` carries the same object in a top level
  `recording` field. Its `files` are names relative to the run folder's
  `artifacts` directory.
- `report.json` carries it under `metadata.recording`, and
  `report.html` links each file in its header.
- The CLI prints one `Video: <path>` line per kept file on stderr.

A device that cannot record, such as a physical iPhone, reports state
`unsupported` and the run continues normally.

`atomyx driver suite` records once per flow. Each flow's files are named
from its one-based position, for example `video-entry-2-1.mp4`, so flows
never collide. The suite's `meta.json` has no `recording` object. The
suite report lists the kept files under each flow instead.


these that is set:

1. `--report-dir <path>`.
2. The project config's `report.outDir`.
3. The `ATOMYX_STORAGE_DIR` environment variable.
4. `~/.atomyx`, the same default root the MCP server uses.

A relative path in any of these is made absolute when the root is
resolved, so every path the tools print is absolute.

The id has the form `<yyyymmdd-hhmmss>-<label>-<4 hex>`, where the
label comes from the script name for `run` and the suite name for
`suite`. The report is at `<root>/runs/v1/<id>/report/report.html`, and
its images link to `../artifacts/<file>`. A report is rendered only
when the project config declares a `report` block or when `--report`
or `--report-dir` is given, and `--report-dir` alone implies `html`.
The CLI flags take precedence over the config's `report` block field
by field, so `--report-dir` alone can redirect a config-declared
report without repeating `--report`.

Tooling that reads a report uses the `report.html`, `report.json` and
`junit.xml` files in the run folder's `report` directory. The flat
`<outDir>/<runId>/report.html` location is not written.

`meta.json` ends with a result and an outcome. A passing run is
`passed`, a run that ran and failed is `failed`, a run that ended in an
error before it produced a result is `failed` with outcome `error`, a
run that lost its device is `cancelled` with outcome `deviceLost` and
exits with code `1`, and
a run stopped by SIGINT or SIGTERM is `cancelled`. The meta is
finalized before the process exits.

## driver suite

Runs a named suite declared in the project config, or an ad-hoc suite
selected by `--tag` or `--glob`. A suite name and `--tag` or `--glob`
cannot be combined.

Usage:

```
atomyx driver suite [name] [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--tag <name>` | Selects flows carrying this tag. A flow matches when it carries at least one of the given tags. | none | yes |
| `--glob <pattern>` | Selects flows whose path matches this glob, resolved against each `testRoots` directory in the project config, or against the current directory when `testRoots` is not set. A flow matches when it matches at least one glob. | none | yes |
| `--config <path>` | Path to an Atomyx project config file. Same discovery rule as `atomyx driver run`. | discovered, same rule as `run` | no |
| `--env <name>` | Selects a named environment from the project config. Same selection rule as `atomyx driver run`. | same rule as `run` | no |
| `--var <KEY=VALUE>` | Injects a variable, available to every flow in the suite. Same precedence rule as `atomyx driver run`. | none | yes |
| `--var-file <path>` | JSON file of flat key-value string variables, merged in before `--var`. | none | no |
| `--preconditions <skip\|force>` | Overrides every precondition's own policy for every flow in the suite. | the precondition's own policy | no |
| `--skip-preconditions` | Shorthand for `--preconditions skip`. | off | no |
| `--report <formats>` | Enables report generation and selects its output formats: `html`, `json`, `junit`, or any combination separated by commas. Same precedence rule as `atomyx driver run`. | the project config's `report.formats`, or `html` when reporting is enabled by `--report-dir` alone | no |
| `--report-dir <path>` | Root directory the run folder is written under. Same rule as `atomyx driver run`. | the project config's `report.outDir`, or the storage root | no |
| `--device <id>` | Runs the whole suite on this device, as reported by `atomyx driver list-devices`. The suite runs in this process on that one device and is not split across devices. Resolved like `atomyx driver run --device`. | the project config environment's device, then the platform default | no |
| `--video` | Records the device screen once per flow and keeps the video of each failed flow. See "Screen recording" below. | the project config's `video` setting, which defaults to `off` | no |
| `--retries <n>` | Runs a failed flow again up to `n` more times, from 0 to 3. Overrides the config suite's `retries`. See "Retries and quarantine" below. | the config suite's `retries`, or `0` | no |
| `--json` | Prints one JSON object to stdout with `ok`, `runFolder` and `entries`, and moves the text summary to stderr. See "Suite JSON output" below. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

Selecting by tag alone requires `testRoots` in the project config, or
a `--glob` to narrow the search, because a tag-only selector has no
other way to bound which directory to scan. `atomyx driver suite`
does not accept `--file`, `--platform`, `--proxy`, or `--break`, because
those are specific to `atomyx driver run`.

Example:

```
atomyx driver suite --tag smoke --tag regression
```

Exit codes: `0` when every flow passed, `1` when the suite ran and at
least one flow failed, `2` when the selector matched no flows or the
command was misused, `130` when SIGINT interrupted the suite, and `143`
when SIGTERM did. An interrupted suite prints `interrupted` and finalizes its run
folder as cancelled before it exits.

### Retries and quarantine

A named suite in the project config may declare `retries` and
`quarantine`:

```yaml
suites:
  nightly:
    globs: ["flows/**/*.yml"]
    retries: 2
    quarantine: ["flows/flaky/**"]
```

- `retries` is how many more times a failed flow runs after its first
  attempt, from 0 to 3. The default is 0.
- `quarantine` is a list of globs matched against flow paths the same
  way the suite's own `globs` are, relative to each `testRoots`
  directory or to the current directory when `testRoots` is not set.
- `--retries <n>` overrides `retries` for one invocation. An ad-hoc
  suite selected by `--tag` or `--glob` has no config entry, so it
  retries only when `--retries` is given and quarantines nothing.
- `atomyx driver run` never retries.

What is retried:

- A flow that ran and failed, because a step failed or a scenario
  script failed.

What is never retried, because it would fail the same way again or
hide a real fault:

- A suite that was interrupted by SIGINT or SIGTERM.
- A flow that cannot be loaded or parsed.
- A run that stopped early, such as a device that became unreachable.

Before each retry the flow's own `launchApp` runs again. A retry does
not clear app data, so a flow that needs a clean start declares it with
its own `launchApp` options or preconditions. Each attempt keeps its own
artifacts and video, and its step lines in `steps.jsonl` carry an
`attempt` field from the second attempt on.

How the outcomes are reported:

- A flow that fails and then passes is flaky. It counts as passed, and
  the text summary, `index.json` and both reports mark it.
- A quarantined flow still runs and is still retried, but if it fails,
  the failure does not fail the suite. The exit code stays `0` when
  every other flow passed, and `meta.json` carries result `passed`.
- `index.json` lists every attempt of every flow with `flaky` and
  `quarantined`. See "Suite index" below.
- `report.html` shows a flaky badge with the attempt count and lists
  quarantined failures in their own section. `report.json` carries the
  same facts.
- `junit.xml` writes a flaky flow as a passing test case with one
  `flakyFailure` element per failed attempt, and a quarantined failure
  as a skipped test case with the message `quarantined`. See "JUnit
  report" below.

### Suite index

Every suite run folder holds `index.json`, the per-entry record of the
run. It is written when the suite ends, before the report, and also
when the suite is interrupted, in which case it lists the entries that
ran. The file is replaced atomically, so a reader never sees a partial
index. Its JSON Schema is `core/internal/runfolder/atomyx-run-index.schema.json`.

Top-level fields:

- `schemaVersion`: the schema version, currently 1.
- `runId`: the id of the suite run folder.
- `entries`: one object per entry that ran, in suite order.

Fields of an entry:

- `path`: the entry's file path.
- `status`: `passed` or `failed`, the result of its last attempt.
- `flaky`: `true` when the entry failed and then passed. A flaky entry
  counts as passed. Absent otherwise.
- `quarantined`: `true` when the entry matched the quarantine list. A
  quarantined entry that failed does not fail the suite. Absent otherwise.
- `attempts`: every attempt in order. Each has `number` (from 1) and
  `result` (`passed` or `failed`).

An attempt also carries `videos`, the kept video file names relative to
the run folder's `artifacts` directory, when it kept any. The fields
`deviceId` and `workerRunId` are reserved for a suite that runs on
several devices and are absent otherwise.

The suite's `meta.json` carries `flakyEntries` and `quarantinedFailures`
counts, each absent when zero. The suite report lists the same attempts,
flaky entries and videos as `index.json`.

A schema version only ever gains fields, so a reader written for it keeps
working.

### Suite JSON output

With `--json`, `atomyx driver suite` prints one JSON object to stdout
and writes the text summary to stderr, so stdout can be piped to a
parser. The exit codes do not change.

```json
{
  "ok": false,
  "runFolder": "/home/me/.atomyx/runs/v1/20261009-101500-nightly-ab12",
  "entries": [
    {
      "path": "/work/flows/login.yml",
      "status": "failed",
      "attempts": [{ "number": 1, "result": "failed" }]
    }
  ]
}
```

- `ok` is `true` when the suite's exit code is `0`.
- `runFolder` is the absolute path of the suite's run folder. The CLI
  does not print the `Run folder:` line in this mode.
- `entries` has exactly the shape of the entries in `index.json`.

### JUnit report

`--report junit`, or `junit` in the config's `report.formats`, writes
`report/junit.xml` in the run folder. It is a single `testsuite`
element that Jenkins, GitLab and most CI systems read.

For `atomyx driver run`:

- The testsuite holds one `testcase` named after the script or
  scenario.
- A failed run carries a `failure` element that names the failing step
  and its message. A scenario names the first script that did not pass.

For `atomyx driver suite`:

- The testsuite is named after the suite, or `suite` when none was
  given.
- Each flow is one `testcase`, with `classname` set to the suite name
  and `name` set to the flow path. `time` is in seconds.
- A failed flow carries a `failure` element. A flow that could not run
  at all, such as one that fails to load, carries an `error` element.
- A flaky flow passes and carries one `flakyFailure` element per failed
  attempt. This is the Surefire extension. Jenkins shows it, and
  GitLab ignores it and shows a pass.
- A quarantined failure carries a `skipped` element with the message
  `quarantined`.
- The `tests`, `failures`, `errors` and `skipped` counts agree with the
  exit code: `failures` plus `errors` is the number of flows that fail
  the suite, and `skipped` counts quarantined failures.

The suite's JUnit report and `index.json` are built from the same
result, so they list the same flows with the same outcomes.

## driver doctor

Connects to a device and reports whether it answers a bounded liveness
check. It never runs a flow and never touches app state.

Usage:

```
atomyx driver doctor [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--platform <ios\|android>` | Target platform. | `android` | no |
| `--device <id>` | Device identifier, as reported by `atomyx driver list-devices`. | none | no |
| `--json` | Outputs the report as JSON instead of text. | off | no |
| `--migrate-runs` | Rewrites flat run records as run folders instead of checking a device. See below. | off | no |
| `--bundle <path.zip>` | Writes a diagnostic zip instead of checking a device. See below. | none | no |
| `--report-dir <path>` | Run root for `--migrate-runs` and `--bundle`. Defaults to `ATOMYX_STORAGE_DIR`, then `~/.atomyx`. | none | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

When another Atomyx process holds the device, doctor does not probe it.
It prints the holder and that health was not checked, and exits `0`,
because the holder proves something is driving the device. The `--json`
output then carries `held: true`, `healthChecked: false`, and a
`holder` object with `pid`, `command`, `role`, `state`, and `runId`
when known. For a free device, `--json` carries `held: false` and
`healthChecked: true` next to the health fields.

Exit codes: `0` when the device is healthy or held by another process,
`1` when it could not be reached or is unhealthy, `2` when the command
was misused.

### Diagnostic bundle

`atomyx doctor --bundle <path.zip>` writes one zip a contributor can
attach to a bug report, and prints the path of the zip on success.

```
atomyx doctor --bundle atomyx-diagnostics.zip
atomyx doctor --bundle atomyx-diagnostics.zip --report-dir <path>
```

The zip holds:

- `environment.json`: the atomyx version, the host operating system and
  architecture, and the version output of `adb`, `xcodebuild`, `iproxy`,
  and `go`. Each query is bounded to five seconds, and a tool that is
  absent or fails is recorded with its error and never fails the bundle.
- `devices.json`: every connected device with its reachability state and
  its lease state, which is `free`, `held` with the holder's `pid`,
  `command`, `role`, `state`, and `runId` when known, or `unknown` with
  the reason. Each entry carries `healthChecked: false`, because a health
  check needs the device lease and a bundle never takes one.
- `runs/<id>/meta.json`, `runs/<id>/log.jsonl`, and every
  `step-NNN-log.jsonl` log file, including prefixed names such as
  `script-NNN-step-NNN-log.jsonl`, of the five newest runs under the run
  root. No screenshot, accessibility tree, or other artifact is included.

Every file is scrubbed before it is written. The scrubber masks the
value of each environment variable and each value in a `device.env` file
with `[REDACTED]`, longest value first. It finds `device.env` files in
the iOS agent folder of the checkout containing the working directory
or any parent, and directly in the working directory or any parent.
Values shorter than six characters, the words `true` and `false`,
plain numbers, absolute paths, and host-description variables such as
`USER` and `HOME` are not masked, so the bundle stays readable. It also
masks `Bearer` tokens, the value of an `X-Atomyx-Token` header, hex
strings of 32 characters or more, and base64url strings of 32 or more
characters that mix letters and digits. A UUID is kept.

The zip is written to a temporary file next to the target and renamed
into place, so a failed write never leaves a partial zip. `--bundle`
covers every device, so it cannot be combined with `--platform`,
`--device`, `--json`, or `--migrate-runs`.

Exit codes: `0` when the zip is written, `1` when it could not be
written, `2` when the command was misused.

### Migrating run records

`atomyx doctor --migrate-runs` moves run records stored as flat files,
`<root>/runs/<id>.json`, into run folders,
`<root>/runs/v1/<id>/`. Every run keeps its id, and no field is lost:
name, source, status, findings, summary, action count, and duration all
land in the folder's `meta.json`. It never needs a device and cannot be
combined with `--platform` or `--device`.

```
atomyx doctor --migrate-runs
atomyx doctor --migrate-runs --json --report-dir <path>
```

The run root is `--report-dir`, then `ATOMYX_STORAGE_DIR`, then
`~/.atomyx`, the same root the MCP server uses.

Safety:

- A flat record is deleted only after its folder is written, read back,
  and compared with the record.
- A record that cannot be read, a record whose folder fails the
  read-back, and a record whose id already names a folder with
  different content are reported and left where they are. The command
  then continues with the next record, so one bad record never blocks
  the others.
- Each migrated folder records the id of the flat record it came from
  in the `importedFrom` field of its `meta.json`. If a pass is
  interrupted after a folder is written and before the flat record is
  deleted, the next pass sees that the folder names the record as its
  source, treats the flat record as stale, and deletes it, even when
  the folder was edited in between, for example by `update_run_summary`.
  A folder with no such field that differs from the record is a
  conflict.
- The command holds a lock on the run root while it works. A running
  `atomyx-mcp` keeps listing and reading runs. Its `delete_run` and
  `update_run_summary` calls return `RUN_BUSY` until the command ends,
  so no edit is lost and no deleted run comes back. When another run
  operation already holds the lock, the command prints one line to
  stderr, `waiting for another run operation to finish`, and waits.
- Rebuild and restart every `atomyx-mcp` server before you migrate. A
  binary that does not take the run migration lock can delete or edit
  a flat record during the pass.
- Finding `screenshotPath` values are kept as they are. The screenshot
  files are not moved into the run's `artifacts` folder, and
  `delete_run` does not remove them.

Idempotence: the command can be repeated after any interruption. It
finishes the records that are left, never makes a second copy of a run,
and changes nothing when no flat record remains.

Output is a summary of four counts: `migrated`, `already migrated`,
`conflicts`, and `damaged`, followed by one line per conflict or damaged
record with its reason. `already migrated` counts records whose folder
was written by an interrupted earlier pass and only needed the original
removed. With `--json` the output carries `root`, `migrated`,
`alreadyMigrated`, `conflicts`, `damaged`, and `scratchRemoved`, where
each list is always present and each conflict or damaged entry has `id`
and `reason`.

If storage fails, the pass stops early. The command still prints the
report of what it did up to that point, as JSON with `--json`, then the
error, and exits `1`.

`--report-dir` is accepted only together with `--migrate-runs`. A plain
`doctor` call that passes it exits `2`.

Exit codes: `0` when no record is conflicting or damaged, `1` when one
is or when storage failed, and `2` when the command was misused.

## driver inspect

Serves a browser inspector for one device until it is stopped. The page
shows the live UI tree and screenshot of the device, lets you click a
node to see its properties and a selector that finds it, and tests
selectors against the tree.

Usage:

```
atomyx driver inspect [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--platform <ios\|android>` | Target platform. | `android` | no |
| `--device <id>` | Device identifier, as reported by `atomyx driver list-devices`. | none | no |
| `--no-open` | Prints the URL without opening a browser. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

### Starting and opening the page

The command takes the device like `run` does when no other Atomyx
process holds it. When another process holds it, the command watches
that process instead, as described under "Watching a held device". It
prints one line to stdout, the URL, and opens it in your default browser
unless `--no-open` is set:

```
http://127.0.0.1:<port>/#<code>
```

The page works only on this machine, because the command listens on
`127.0.0.1` and nothing else.

- **One visit, 60 seconds.** The code in the URL works once and only
  for 60 seconds after the command printed it. A page opened later, or
  a second time, says the code was refused and what to do about it.
- **A fresh URL.** Press Enter in the terminal to print a fresh URL,
  which the command also opens unless `--no-open` is set. Do this after
  a reload too, because a reload forgets the page's login. Pressing
  Enter never stops the inspector, and neither does the end of the
  terminal's input.
- **Who used the code.** When the browser opens the URL, the code
  appears in its process arguments, where another local account could
  read it. The terminal therefore logs every attempt to use a code, and
  logs a refused or expired one as a warning, so a hijack is visible.

### What you see and do

- **The header.** It names the device, its platform and kind, and the
  holder state, which is `idle` while you only look and `running` while
  a capture is in progress. Refresh asks the device for a new tree and
  screenshot taken together. When a tool call or another capture is
  already running, the page shows a short notice and carries on.
- **The tree.** The middle pane shows the tree as an outline. Each row
  shows the role and the most useful of the id, label and text, with
  secret values already masked. The first three levels are open at
  first. A twisty, a click or the arrow keys open and close nodes. Only
  the rows near the window are drawn, so a tree of tens of thousands of
  nodes stays responsive.
- **Search.** The search box matches the id, label, text, hint and role
  of every node, opens only the paths to the matches and marks the
  matching text. Enter steps to the next match and Shift with Enter
  steps back. Stepping to a match selects it.
- **The screenshot and the highlight.** The selected node is outlined on
  the screenshot, and a click on the screenshot selects the topmost node
  under the pointer. When several nodes contain the point, the one drawn
  last wins, so a dialog drawn over a button wins over the button and
  over everything inside it. The page outlines and picks only when all
  of these hold, and otherwise says under the screenshot why not:
  - The tree and the screenshot were captured together, so they show
    the same screen.
  - The device reported its screen size, so positions can be scaled.
  - The tree on the page names the screenshot on the page as its own.
    The two are fetched separately, so one may be newer than the other
    for a moment.
  - The selected node comes from the tree on the page.
- **The panels.** The right pane holds four panels, from top to bottom.
  - Properties lists every attribute of the selected node with its
    position in the tree, its bounds and its state flags. A secure
    field shows its masked values, and its row is labelled by its hint
    or label.
  - Selector suggestions lists candidate selectors for the selected
    node. Each shows `unique` or `ambiguous`, how many nodes it
    matches, and `obscured` when another node covers the target. A
    candidate button copies its selector into the tester. A node from
    an earlier capture gets no suggestions, and a tree that changed
    while you asked shows a notice and reloads.
  - Selector tester takes a selector written as one JSON object, the
    same shape a script step uses: `id`, `label`, `text`, `hint`,
    `value`, `role`, `enabled`, `clickable`, `focused` and `nth`. A text
    field may also be `{"regex": "..."}`. Test selector lists the
    matches in the order a run resolves them, with their bounds, and a
    match button selects that node in the tree.
  - Copy as YAML renders one script step for the tester's selector. The
    action is `tap`, `type`, `waitFor`, `assertVisible`,
    `assertNotVisible` or `assertExists`, and `type` also takes the
    text. For a secure field the step holds a placeholder such as
    `${password}` instead of the text, and the panel says to supply it
    as an environment variable when the script runs. Copy puts the step
    on the clipboard. When the browser refuses, the step stays selectable
    so you can copy it by hand.
- **Debugging controls.** While the watched holder runs a script with
  debugging enabled, the page shows a Debugger panel with Pause, Resume,
  Step and a breakpoint list written as `<file>:<line>`, and a Run step
  panel that runs one step on the device. Resume, Step and Run step work
  only while the run is paused. When the next step may not find its
  target, the panel says why and offers Resume anyway. A person who runs
  steps here is recorded as a human action in the run folder. See
  [Debugging a run](#debugging-a-run).
- **Live updates.** The page follows the device as the holder captures,
  so the screenshot and the header change on their own. A page and tree
  from before a reconnect are never mixed with ones after it.

### When the device is released, lost or the server is gone

- **Idle release.** The command keeps the device until the number of
  milliseconds in `session.mcpIdleTimeoutMs` passes with no action of
  yours. Refresh, suggestions, tests, rendering a step and the reconnect
  button each count. A page that only polls does not, and neither does
  a capture or a test that is still running, which holds the device
  until it ends. After the timeout the command releases the device so
  another Atomyx process can take it, and the page shows a banner with a
  Reconnect button.
- **Lost device.** When the device goes away while the command holds it,
  for example the cable is pulled or the runner process exits, the
  command gives it up, the holder state reads `lost`, and the page shows
  the same banner. Unlike `run`, which stops when the device is lost,
  the inspector keeps running.
- **Hand-over.** An idle inspector gives the device to a `run`, a
  `suite` or an MCP `select_device` that asks for it. The page then
  shows that process as the holder and its steps, and shows the
  released banner when that process ends. A capture or action in
  progress makes the inspector refuse, and the other process fails with
  `DEVICE_HELD`.
- **Reconnect.** The Reconnect button takes the device again, with the
  same steps as at startup, and the page resumes and reloads the tree
  and screenshot. The command never reconnects on its own, so it does
  not take the device from the next process that wants it. When another
  process holds the device by then, the button takes it over if that
  process is idle and can give it up, and that process is told who took
  it. Otherwise the page watches that process when it serves a socket, and
  the banner shows `DEVICE_HELD` and what to do when it does not. The
  button stays available. The page keeps its login across a release and a
  reconnect.
- **The server is gone.** When the command has stopped or crashed, the
  page cannot reach it. After several failed attempts in a row it shows
  a banner saying it lost contact and to check the terminal. When the
  server answers with an error that trying again cannot fix, such as a
  closed session or a refused login, the page shows that banner at once
  with the server's own message and advice.
- **Time limits.** A request that runs out of time is reported as
  `TOOL_TIMEOUT` with advice to try again or to reconnect.
- **Stopping.** Ctrl-C and SIGTERM stop the server and release the
  device.

### Watching a held device

When another Atomyx process holds the device, the inspector watches it
and does not drive it. The terminal says which process it watches, and
the page shows:

- **Holder.** The header names the holder's command and its state, for
  example `atomyx run (running)`.
- **Captures.** The tree and screenshot that the holder last captured,
  updated as the holder captures new ones.
- **Steps.** A Steps panel lists the step lines of the run the holder
  drives, newest last, with each step's number, command, outcome and
  duration.
- **Refresh.** A fresh capture works only while the holder is idle. While
  the holder is running, the Refresh button is disabled and the page
  says the holder is busy. A request that reaches a busy holder is
  refused with `HOLDER_BUSY`.
- **Holder exits.** When the watched process exits, the page shows the
  released banner with code `HOLDER_EXITED`. The Reconnect button then
  takes the device when it is free, or watches the next holder when
  another process has taken it in the meantime. The command never does
  either on its own. When that process serves no socket, the button
  reports `DEVICE_HELD` and the banner stays.

The command fails with exit code `1`, prints no URL, and names the
holder, when:

- the holder serves no socket, so nothing can watch it.
- the holder has not published its socket within 130 seconds, so it is
  still starting. Run the command again in a moment.
- the holder published a socket that does not answer. Stop that process
  or wait for it to finish.

All three carry the code `DEVICE_HELD`.

Exit codes: `0` when the inspector stopped without a signal, `1` when
the device could not be reached or held, when its holder cannot be
watched, or when the server failed, `2` when the command was misused,
`130` when Ctrl-C stopped it, and `143` when SIGTERM did.

## Device ownership

`run`, `suite`, `doctor`, and `inspect` take exclusive ownership of the device for
the length of the command. Only one Atomyx process, whether this CLI or
an `atomyx-mcp` server, can drive a device at a time.

- `atomyx-mcp --platform <ios|android> --device <id>` binds an MCP
  server to one device. It selects that device on the first call that
  needs one, and `select_device` naming another device fails with code
  `DEVICE_PINNED`.
- `run` and `suite` take a device from an idle holder that can give it
  up, which is an idle `atomyx-mcp` session or an idle `atomyx inspect`.
  The command asks the holder, the holder releases the device, and the
  command takes it. The holder is told which command took it, and it
  watches the command until the command ends. The device then stays
  free until the holder's operator or agent takes it again. A holder
  that is working, such as another `run`, or that serves no socket
  keeps the device, and the command fails as described next.
- A `run`, `suite` or `doctor` command against a device that a holder
  keeps fails with code `DEVICE_HELD`. `doctor` never asks a holder to
  give the device up. The message names the
  holder's process id, command, and state. Stop that process or wait
  for it to finish, then run the command again, or use
  `atomyx inspect` to watch it. `inspect` does not fail on a held
  device, because it watches the holder.
- When another process takes the device in the short gap between the
  holder's release and the command's acquisition, the command fails
  with `DEVICE_HELD` naming that process.
- On iOS, a runner that is missing or stale is built before ownership is
  taken, so a long build never holds the device. The command prints
  `building runner` to stderr while it builds. The build has a timeout
  of 300 seconds, fixed and not configurable, which also covers waiting
  for another Atomyx process that is building the same runner. When it
  elapses the command fails
  with code `RUNNER_BUILD_TIMEOUT`, which is distinct from the codes of
  a lease conflict.
- After the build, taking the device and launching its driver has its
  own budget of 120 seconds by default, which excludes the build. A
  runner that is already fresh launches without waiting for another
  process's build. When the budget elapses the command fails with code
  `TOOL_TIMEOUT`. Configure it with `session.deviceAcquireTimeoutMs` in
  the project config or the `ATOMYX_DEVICE_ACQUIRE_TIMEOUT_MS`
  environment variable. The environment variable wins.
- The two bounds are the build timeout of 300 seconds and the acquisition
  budget of 120 seconds, so a command waits at most 300 seconds plus 120
  seconds for them. That total does not include the device identity
  lookup that runs before them.
- The CLI reads the project config for these settings from the working
  directory, looking in each parent directory when it finds none there.
  It does not read the file named by `--config` for them, so run the
  command from inside the project or set the environment variable.
- Ownership is released when the command ends, including on an
  interrupt.
- If a command crashes or is killed with `kill -9`, the next command
  for an iOS device ends the runner process the crashed command left
  behind, and on a physical iPhone the `iproxy` tunnel it left behind,
  then continues.
- If the device is lost while a run is in progress, the run is stopped
  and the command reports it. The device is lost when its lease is
  taken away, or when the runner process of an iOS device, or the USB
  tunnel to a physical iPhone, exits on its own. The command then ends
  the remaining runner and tunnel processes itself, finalizes the run
  as `cancelled` with outcome `deviceLost`, writes no failure bundle,
  and exits with code `1`. A script that stops because its liveness
  probe found the device or agent unreachable ends the same way.

A run on iOS without `--device` is not provisioned by the CLI. It asks
the runner served on the default port, 22087, which device it belongs
to, and takes ownership of that device like any other run. A runner
that names no device is taken to serve the single connected iPhone. When
no runner answers, or a runner that names no device is served while the
number of connected iPhones is not exactly one, the command fails with
DEVICE_NOT_FOUND. Serve the runner or pass `--device` for an
automatically launched runner.

## driver list-devices

Enumerates devices Atomyx can currently see: Android devices and
emulators through `adb`, booted iOS simulators, and iOS physical
devices connected over USB.

Each device also reports its lease, read from the lease file without
creating or changing it, so the listing shows which devices another
Atomyx process drives.

- A free device shows nothing extra in the text listing.
- A held device ends its line with the holder's command and state, for
  example `  emulator-5554  [ready]  held by atomyx run, running`. When
  the holder's details cannot be read, the line ends with `held by an
  unknown process`.
- A device whose lease could not be probed ends its line with
  `lease unknown`.
- With `--json`, every device carries a `lease` object. Its `state` is
  `free`, `held` or `unknown`. A held lease adds a `holder` object with
  `pid`, `command`, `role`, `state` and, while a run is open, `runId`. An
  unknown lease adds an `error` message.

Usage:

```
atomyx driver list-devices [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--json` | Outputs the device list as JSON instead of a formatted list. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

Example:

```
atomyx driver list-devices --json
```

## driver version

Prints the driver module's version and exits.

Usage:

```
atomyx driver version
```

Example:

```
atomyx driver version
```

## skills init

Copies the bundled Atomyx skill and agent files into
`<target>/.claude/`. Defaults to the current working directory.

Usage:

```
atomyx skills init [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--target <path>` | Destination directory. | the current working directory | no |
| `--force` | Overwrites existing files without prompting. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

Example:

```
atomyx skills init --target ./my-project --force
```

## skills update-skills

Updates an existing Atomyx skills install to the bundled version.

Usage:

```
atomyx skills update-skills [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--target <path>` | Destination directory. | the current working directory | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

Example:

```
atomyx skills update-skills
```

## Shortcuts

| Shortcut | Same as |
|---|---|
| `atomyx run [flags]` | `atomyx driver run [flags]` |
| `atomyx suite [name] [flags]` | `atomyx driver suite [name] [flags]` |
| `atomyx doctor [flags]` | `atomyx driver doctor [flags]` |
| `atomyx inspect [flags]` | `atomyx driver inspect [flags]` |
| `atomyx devices [flags]` | `atomyx driver list-devices [flags]` |
| `atomyx init [flags]` | `atomyx skills init [flags]` |
| `atomyx update-skills [flags]` | `atomyx skills update-skills [flags]` |

## References

- [`docs/README.md`](./README.md): index of the end-user documentation directory.
- [`.claude/docs/architecture.md`](../.claude/docs/architecture.md): the framework's high-level contract.
