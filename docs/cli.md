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
installs and updates the bundled Claude skill files. A third module,
`runs`, reads the recorded runs: it lists them, reports outcome
statistics per test, and prunes old ones.

```
atomyx <module> <command> [flags]
```

| Command | What it does |
|---|---|
| `atomyx driver run` | Runs a single YML test script against a device. |
| `atomyx driver suite` | Runs a named suite or an ad-hoc selection of flows. |
| `atomyx driver doctor` | Checks that a device is reachable and healthy, or migrates flat run records into run folders. |
| `atomyx driver inspect` | Serves a browser inspector for a device: its live UI tree and screenshot, with selector suggestions. |
| `atomyx driver setup` | Prepares a device to run Atomyx: runs the platform setup steps, fixes what is safe, and prints the exact action for the rest. |
| `atomyx driver list-devices` | Lists Android, iOS simulator, and iOS physical devices. |
| `atomyx driver version` | Prints the driver module's version. |
| `atomyx skills init` | Copies the bundled skill and agent files into a project. |
| `atomyx skills update-skills` | Updates an existing skills install to the bundled version. |
| `atomyx runs list` | Lists recorded runs, newest first. |
| `atomyx runs stats` | Reports pass, fail, and flaky statistics per test key. |
| `atomyx runs prune` | Deletes old runs together with their worker runs. |

Six shortcuts skip the `driver` module name: `atomyx run`, `atomyx
suite`, `atomyx doctor`, `atomyx inspect`, `atomyx setup`, and `atomyx devices` map to
`atomyx driver run`, `atomyx driver suite`, `atomyx driver doctor`,
`atomyx driver inspect`, `atomyx driver setup`, and `atomyx driver list-devices`. Two shortcuts skip
the `skills` module name: `atomyx init` and `atomyx update-skills`.

The CLI ships exactly three modules: `driver`, `skills`, and `runs`. The MCP
server is a separate stdio binary, `atomyx-mcp`, that an AI client
launches directly instead of going through the `atomyx` binary.

## Module and command structure

```
atomyx driver run [flags]
atomyx driver suite [name] [flags]
atomyx driver doctor [flags]
atomyx driver inspect [flags]
atomyx driver setup [flags]
atomyx driver list-devices [flags]
atomyx driver version
atomyx driver help

atomyx skills init [flags]
atomyx skills update-skills [flags]

atomyx runs list [flags]
atomyx runs stats [flags]
atomyx runs prune [flags]
atomyx runs help
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

When the run drove a device whose agent reports its versions, the
`host` object of `meta.json` carries `agentVersion` and
`protocolVersion`. Each is omitted when the agent does not report it.

### Run retention

The project config can bound how many run folders accumulate:

```yaml
retention:
  keepLast: 50
  olderThanDays: 30
```

| Key | Meaning |
|---|---|
| `keepLast` | Keeps the newest this many top-level runs. A positive integer. |
| `olderThanDays` | Selects runs that started more than this many days ago. A positive integer. |

At least one key is required when the block is present, and when both are
set a run is deleted only if both select it. The block does not depend on
`report` and does not turn reports on.

With a `retention` block, `run` and `suite` prune the run folders under the
directory they write, once, before the run starts. `atomyx-mcp` prunes its
storage directory once in the background after it starts. Nothing else
prunes: `inspect`, `doctor`, `setup`, `runs list` and `runs stats` never
delete a run, and a `suite --worker` process never prunes, so a sharded suite
prunes once in its parent.

Rules for the automatic prune:

- `keepLast` counts units, where a sharded suite run and its workers are one unit. The run that is starting is not yet written, so after a `run` or `suite` command the root holds up to `keepLast` earlier units plus the new run.
- A run that is still being written is kept, and a run migration in progress defers the prune. Each such case prints one `warning:` line on stderr, or one warning in the `atomyx-mcp` log, and never changes the exit code.
- Without a `retention` block nothing is deleted.

### Script HTTP requests

The `httpRequest` script step sends requests from the machine running the
script. The project config controls where it may connect:

```yaml
http:
  allowHosts:
    - localhost:8025
    - "*.example.test"
  caFile: certs/test-ca.pem
```

| Key | Meaning |
|---|---|
| `allowHosts` | Hosts a request may reach, each a host or `host:port`. A leading `*.` matches any subdomain but not the bare domain. An entry without a port matches every port. An empty or missing list refuses every request. |
| `caFile` | A PEM file of certificate authorities trusted in addition to the system roots, for a test server with a self-signed certificate. A relative path is resolved against the folder of the config file. |

The `run` and `suite` commands read the block from the config they load, and `atomyx-mcp` reads it from the config in its working directory when it starts. There is no setting that disables certificate verification, and link-local addresses are refused whatever the list holds. See `httpRequest` in the [script reference](./yml-script-reference.md#httprequest).

### Masked values

A run masks secret values with `********` in everything it writes or
prints for the run: the text and JSON result on standard output, error
messages on standard error, step text, the report, failure artifacts,
screenshot file names, the run log, and `index.json` for a suite. A
value is secret when it comes from outside the committed files:

- A project config value written as `${ENV:NAME}`, which is read from
  the environment of the process. When the value mixes text with the
  reference, such as `Bearer ${ENV:NAME}`, the environment value alone
  is masked too.
- A value given with `--var` or `--var-file`.
- A value given in the `env` argument of the MCP `run_script` tool.
- A value the run captured and recognized as secret. `extract` marks a
  stored value secret when it looks like a token, and `read` marks the
  value of a secure field, such as a password input.
- A value captured into one of the variables above, which stays secret
  whatever the new value looks like.

A literal written in a script's `env` block or in a project config
stays readable, because it is already in a committed file. A value
captured from the screen into a new variable that is not recognized as
secret stays readable too.

Masking starts when a value becomes secret, so text written before a
captured value was recognized is not rewritten. A short secret is
masked only when it stands as a whole word, see
[known limitations](./known-limitations.md).

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

A run that ended in failure also records `errorCode` in `meta.json`, the
stable error code of the failure that ended it, such as `AGENT_UNREACHABLE`
or `TOOL_TIMEOUT`. It is absent on a pass, on a failure that carries no
code such as a failed assertion, and on a run recorded without the field.

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
| `--device <id>` | Runs the whole suite on this device, as reported by `atomyx driver list-devices`. The suite runs in this process on that one device. Resolved like `atomyx driver run --device`. Cannot be combined with `--devices` or `--shard`. | the project config environment's device, then the platform default | no |
| `--devices <ids>` | Spreads the suite over these devices, given as comma-separated ids as `atomyx driver list-devices` reports them. The ids may name devices of both platforms. See "Sharding a suite" below. | none | no |
| `--shard auto` | Spreads the suite over every connected, ready device of the resolved platform. `auto` is the only accepted value. See "Sharding a suite" below. | none | no |
| `--platform <ios\|android>` | Platform of the devices `--shard auto` looks for, and of a `--devices` id that the device scan does not list. | the project config environment's platform, then `android` | no |
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
folder as cancelled before it exits. A sharded suite has the exit codes
listed under "Sharding a suite".

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

### Sharding a suite

`--devices a,b` and `--shard auto` run one suite on several devices at
the same time. They cannot be combined with each other or with
`--device`, and either combination is a usage error with exit code `2`.

Choosing devices:

- `--devices` uses the listed devices, in the given order, each once.
  An id the device scan lists keeps the platform it was found on, so one
  list can hold an Android serial and an iOS simulator udid. An id the
  scan does not list, such as an Android device reached over the
  network, gets the platform of `--platform`.
- `--shard auto` uses every connected and ready device of the resolved
  platform, whether or not another process currently holds it. Zero
  devices is a usage error with exit code `2`.

How the suite runs:

- The command takes no lease of its own. It starts one worker process
  per device, each in its own process group, and each worker acquires
  its device the normal way, so a worker can borrow a device from an
  idle `atomyx inspect` or MCP session.
- All flows wait in one queue. A worker gets its next flow when it has
  finished the previous one, so a fast device runs more flows than a
  slow one, and every flow runs exactly once.
- A worker runs its flows with the suite's own retries and quarantine
  list, and keeps its own run folder.
- A worker that fails to start, or cannot acquire its device because
  another process holds it, is dropped with a warning on stderr, and
  the other workers carry on.
- A worker that reports its device lost stops. The flow it was running
  goes back to the queue once for another worker. If that flow also
  loses its device, it ends as failed.
- A flow that no worker ran because every worker stopped is listed as
  failed with no attempts.

Exit codes:

| Code | Meaning |
|---|---|
| `0` | Every flow ran, and every flow that is not quarantined passed. |
| `1` | A flow failed, or flows were left unrun because every worker stopped. |
| `2` | The command was misused, including `--shard auto` with no ready device. |
| `130` | SIGINT interrupted the suite. |

On SIGINT the command asks every worker to stop, ends the ones that do
not, and finalizes its run folder as cancelled. Its exit budget is
longer than a single command's, so it can wait for its workers.

Run folders:

```
<root>/runs/v1/
  <suite-run-id>/            the sharded suite
    meta.json
    index.json
    report/                  merged report and junit.xml
  <worker-run-id-1>/         one folder per worker, a suite run
    meta.json                parentRunId names <suite-run-id>
    index.json
    artifacts/               screenshots, failure evidence and videos
  <worker-run-id-2>/
```

The sharded suite's `meta.json` carries the same counts as any suite,
and records no single device. Each worker's `meta.json` carries
`parentRunId`, the id of the sharded suite. The sharded suite's
`index.json` has the shape described in "Suite index", and each attempt
names the device it ran on in `deviceId` and the worker run folder that
holds its artifacts in `workerRunId`. A flow that was requeued lists its
attempts in two worker run folders, numbered in order. The command never
copies artifacts, so screenshots, failure evidence and videos stay in
the worker run folder, and the merged report does not link videos.

`--json` prints the same object as an unsharded suite. The merged
`junit.xml` and report are built from the same data as `index.json`.

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
the `artifacts` directory of the run folder that holds them, when it
kept any. A sharded suite adds `deviceId` and `workerRunId` to each
attempt, see "Sharding a suite". An unsharded suite omits both.

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

When the connected agent reports its versions, the text output prints
an `agent:` line with the build version and a `protocol:` line with the
wire protocol version, and `--json` carries `agentVersion` and
`protocolVersion`. Each is omitted when the agent does not report it.

When another UI automation session is active on the device, the text
output prints a `warning:` line naming it and advising to stop it if
steps behave unexpectedly, and `--json` carries the same text in a
`warnings` array. A warning never changes the exit code. Only iOS
devices report such sessions.

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
atomyx driver setup [flags]
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

## driver setup

Prepares a device to run Atomyx. It runs the platform's setup steps in
order, applies the safe fixes it can make from this machine, and prints
the exact action for everything else. Each step reports a status, a
message, and, when the operator must act, a remedy.

Usage:

```
atomyx driver setup [flags]
```

Flags:

| Flag | Meaning | Default | Repeatable |
|---|---|---|---|
| `--platform <ios\|android>` | Target platform. | `android` | no |
| `--device <id>` | Device identifier, as reported by `atomyx driver list-devices`. | none | no |
| `--check` | Only observes. Nothing on this machine or the device is changed. | off | no |
| `--json` | Prints the report as JSON instead of text. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

Step statuses:

| Status | Meaning |
|---|---|
| `ok` | The requirement already holds, or does not apply to this device. |
| `fixed` | The step changed the machine or device and the requirement now holds. |
| `manual` | The requirement is unmet and only the operator can satisfy it. The step carries a remedy. |
| `failed` | The step could not establish or verify the requirement. The step carries a remedy. |
| `skipped` | The step did not run because a step it depends on did not pass, or the run was interrupted. |

Steps run in a fixed order. A step whose prerequisite is not `ok` or
`fixed` is skipped, and its message names the prerequisite. With
`--check` no step is ever asked to change anything, so a requirement a
fix would repair is reported `manual` with its remedy.

With `--json`, standard output is one object:

```
{"ok": true, "steps": [{"id": "...", "status": "ok", "message": "...", "remedy": ""}]}
```

`ok` is `true` when every step is `ok` or `fixed`. `steps` is always an
array, and `remedy` is an empty string when a step needs no action.

Setup takes the device's lease for the length of the command without
connecting a driver, so a broken agent cannot fail inside it. When
another Atomyx process holds the device, setup prints the holder and
that nothing was changed, runs no step, and exits `1`. The `--json`
output then carries `ok: false`, an empty `steps` array, `held: true`,
and a `holder` object with `pid`, `command`, `role`, `state`, and
`runId` when known. When no device can be resolved, no lease is taken,
a note goes to stderr, and the steps report what is missing. The lease
is released when the command ends, including on an interrupt.

Exit codes: `0` when every step is `ok` or `fixed`, `1` when any step is
`manual`, `failed`, or `skipped` or the device is held, `2` when the
command was misused, `130` when Ctrl-C stopped it, and `143` when
SIGTERM did.

### Android steps

Run in this order. `Requires` names the step that must be `ok` or `fixed`
first.

| Step | Requires | Checks | Fix |
|---|---|---|---|
| `android-adb` | none | `adb` is on `PATH`. | none, the remedy names the platform tools install. |
| `android-device` | `android-adb` | The resolved serial is listed by `adb devices` in the `device` state. An unauthorized, offline, bootloader, or missing device each gets its own message and remedy. | none |
| `android-apk` | `android-adb` | The agent APK exists at `ATOMYX_ANDROID_APK` or at the built output in the source tree, the same resolution the agent launcher uses. | none, the remedy names the build command. |
| `android-agent-installed` | `android-device` | The agent package is installed. | Installs the agent and starts it, then names the accessibility services that were enabled before and are now disabled, because the install rebinds accessibility. When no APK exists it reports a manual result that points at `android-apk` instead of attempting the install. |
| `android-accessibility` | `android-agent-installed` | The Atomyx accessibility service is enabled. | Enables it. Android keeps one list of enabled services, so enabling Atomyx disables every other enabled accessibility service. The message names the services that were enabled before. |
| `android-agent-health` | `android-accessibility` | The agent's health route answers ready with its accessibility service connected. No driver connects. | Starts an agent that does not answer, for example after a reboot or a force stop, without reinstalling it or changing accessibility, then checks again. An agent that answers but is not ready, or whose accessibility service is not connected, needs the operator. If the start fails, open the Atomyx app on the device. |
| `android-agent-protocol` | `android-agent-health` | The protocol version the agent reports on its ping route equals the one this build requires. | Reinstalls the agent once, checks again, and names the accessibility services that were enabled before and are now disabled. |

A missing APK does not skip the steps after it, so an agent that is
already installed and healthy is still verified. The health and
protocol steps reach the agent through an `adb` port forward for the
device, which setup creates when none exists and removes again when the
step ends. A forward that existed before the step is left in place. The
forward is host plumbing and changes nothing on the device, so it also
happens with `--check`.

### iOS steps

| Step | Requires | Checks | Fix |
|---|---|---|---|
| `ios-xcode` | none | `xcrun` is on `PATH` and the selected developer directory is inside a full Xcode installation. A directory that holds only the command line tools is not enough. | none, the remedy names `xcode-select --install`, or installing Xcode and running `xcode-select -s` for the command line tools case. |
| `ios-iproxy` | none | `iproxy` is on `PATH`. A simulator needs no tunnel, so the step passes without checking. | none, the remedy names `brew install libusbmuxd`. |
| `ios-signing-team` | none | A physical device build has a signing team: `ATOMYX_DEV_TEAM` names the team of a development identity in the keychain. A simulator build needs no signing, so the step passes. | none |

The target is a simulator when the resolved device's kind is
`simulator`, and a physical device for any other resolved kind. When no
iOS device is resolved, `ios-iproxy` and `ios-signing-team` check what a
physical device needs.

`ios-signing-team` lists the development identities with `security
find-identity -v -p codesigning` and reads the team id from the
organizational unit of each identity's certificate subject. The id in
parentheses at the end of an identity name is the certificate holder's
member id, not the team id, so the step does not use it. With one team
found, the step passes when `ATOMYX_DEV_TEAM` equals it, and otherwise the
remedy is the exact line to run, `export ATOMYX_DEV_TEAM=<id>`. With
several teams the step passes when `ATOMYX_DEV_TEAM` equals one of them,
and otherwise the remedy lists them. When no certificate can be read, the
step passes with a note if `ATOMYX_DEV_TEAM` is set, and asks for it when
it is unset. With no development identity, the remedy walks through adding
an Apple ID in Xcode. The step never writes a file, a shell profile, or
the Xcode project.

## Device ownership

`run`, `suite`, `doctor`, `inspect`, and `setup` take exclusive ownership of the device for
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
DEVICE_NOT_FOUND. Serve the runner or pass `--device`.

With `--device <udid>`, a runner already served on port 22087 is used
when it belongs to that device and speaks the host protocol, and Atomyx
never stops it. Otherwise the CLI launches its own runner. A served runner
of that device with another protocol version fails with
AGENT_VERSION_MISMATCH, and the remedy is to stop it or rebuild it.

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

## runs

The `runs` module reads the run folders under the storage directory,
which is `ATOMYX_STORAGE_DIR` when set and `~/.atomyx` otherwise. It
needs no device. A run folder whose record cannot be read is never
listed, counted, or touched, and every command ends its text output
with a footer that gives the number of such records when it is not
zero. The JSON output carries the same number in a top level `skipped`
field.

`--since` takes an RFC 3339 timestamp or a `YYYY-MM-DD` date, which is
read as midnight UTC. Every command accepts `--help` (or `-h`).

### Test key

A test key is the stable identity of the script or scenario a run
executes. `atomyx run` and `atomyx suite` store it as `testKey` in
`meta.json`, and a suite stores one per entry in `index.json`, so the
same test keeps one key whatever directory a run starts from.

The key of a file is its path relative to a project root, written with
forward slashes. The project root is the nearest ancestor directory of
the file, starting at the file's own directory, that holds an Atomyx
config file (`atomyx.config.yaml` or `atomyx.config.yml`). When no
ancestor holds one, it is the nearest ancestor that holds a `.git`
entry. When there is none, it is the file's own directory.

A script outside any config root and any git root is keyed by its file
name alone, so two such files with the same name share one key and their
runs merge in `runs list` and `runs stats`. Keep scripts under a project
config or a git checkout to keep their keys distinct.

When the file exists, symbolic links in its path are resolved before the
key is derived. A link to a script therefore gets the key of the script
itself, whichever path the run was started with.

A run driven through the MCP server has no script file, so its key is
`mcp:` followed by the run name. A run recorded without a key is listed
and filtered under its script path.

### runs list

```
atomyx runs list [--test <key>] [--since <date>] [--json]
```

Lists recorded runs, newest first.

| Flag | Meaning | Default | Required |
|---|---|---|---|
| `--test <key>` | Only runs whose own key matches. A suite run is matched by its own key and its entries are not searched. Use `runs stats` to filter by the key of a suite entry. | all runs | no |
| `--since <date>` | Only runs started at or after this time. | no limit | no |
| `--json` | Prints one JSON document instead of a table. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

The table has these columns:

- `RUN ID`
- `STARTED`: the start time as an RFC 3339 timestamp in UTC
- `KIND`: `script`, `suite`, or `mcp`
- `TEST`: the test key, or `-` when the run has neither a key nor a script path
- `RESULT`: `passed`, `failed`, `cancelled`, or `running` for a run that is still being written
- `DURATION`: `-` while the run is still being written

With no match the command prints `No runs found.`

The JSON document has `runs` and `skipped`. `runs` is always present
and is an empty array when nothing matches. Each run has `runId`,
`startedAt`, `kind`, `testKey`, `result`, and, when known, `outcome`
and `durationMs`. `testKey` holds the script path for a run recorded
without a key.

### runs stats

```
atomyx runs stats [--test <key>] [--last <n>] [--since <date>] [--json]
```

Reports outcome statistics per test key.

| Flag | Meaning | Default | Required |
|---|---|---|---|
| `--test <key>` | Only this test key, including the key of an entry inside a suite run. | all keys | no |
| `--last <n>` | Counts only the most recent `n` runs of each test. A whole number of at least 1. | all runs | no |
| `--since <date>` | Counts only runs started at or after this time. | no limit | no |
| `--json` | Prints one JSON document instead of a table. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

When both `--since` and `--last` are given, the time cut applies first
and `--last` trims what remains.

A suite run counts through its index entries, one outcome per entry
under the entry's test key, and the suite run itself is not counted. An
entry that passed after a retry counts as flaky, and a flaky entry also
counts as passed. A run that is unfinished or cancelled is
left out, and so are the entries of such a suite. An interrupted or
device-lost run counts only as an aborted run, described below. An entry that was never
run, such as one a shard worker did not reach, is left out too.

The table has one row per test key, sorted by flaky rate, then by
failures, both highest first, then by key:

- `TEST`
- `RUNS`
- `PASSED`: includes flaky runs
- `FAILED`
- `INFRA FAILS`: infrastructure failures, which are the failed runs blamed on the infrastructure plus the aborted runs
- `TEST FAILS`: failed runs blamed on the test
- `FLAKY`
- `FLAKY RATE`: flaky runs divided by runs, as a percentage
- `INFRA RATE`: infrastructure failures divided by attempts, as a percentage
- `P50` and `P95`: duration percentiles, `-` when no run has a duration
- `LAST`: up to 10 outcomes, most recent first, one letter each. `P` is passed, `F` is failed, and `K` is flaky.

With no match the command prints `No finished runs found.`

The JSON document has `tests` and `skipped`. `tests` is always present.
Each test has `testKey`, `runs`, `passed`, `failed`, `flaky`,
`flakyRate` (a fraction from 0 to 1), `infraFailures`, `testFailures`,
`abortedRuns`, `attempts`, `infraFailureRate` (a fraction from 0 to 1), `lastOutcomes` (the words
`passed`, `failed`, or `flaky`), `durationP50Ms`, and `durationP95Ms`.
The top level also carries `infraFailures`, `testFailures`, and
`infraFailureRate` summed over all tests, with `abortedRuns` and `attempts`.

A failed run is an infrastructure failure when its outcome is `error`, or
when it ended `failed` with an error code that names the device, the agent,
a timeout, or the host tooling, such as `AGENT_UNREACHABLE`,
`AGENT_NOT_READY`, `TOOL_TIMEOUT`, or `DEVICE_HELD`. Every other failed run
is a test failure, including a run with no error code. A run that lost its
device or was interrupted is an aborted run. It is not in `runs`, `passed`,
`failed`, or `lastOutcomes`, but it counts in `infraFailures`, and `attempts`
is `runs` plus `abortedRuns`, the denominator of `infraFailureRate`. A
cancelled run is an operator decision and is in no count.

### runs prune

```
atomyx runs prune [--keep <n>] [--older-than <days>] [--dry-run] [--json]
```

Deletes old runs.

| Flag | Meaning | Default | Required |
|---|---|---|---|
| `--keep <n>` | Keeps the newest `n` runs and selects the rest. A whole number of at least 1. | off | one of `--keep`, `--older-than`, or a config `retention` block |
| `--older-than <days>` | Selects runs that started more than this many days ago. A whole number of at least 1. | off | one of `--keep`, `--older-than`, or a config `retention` block |
| `--dry-run` | Reports what would be deleted and deletes nothing. | off | no |
| `--json` | Prints one JSON document instead of a table. | off | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

Without `--keep` or `--older-than` the command applies the `retention`
block of the project config (see [Run retention](#run-retention)). Giving
either flag replaces the configured policy entirely, so a lone `--keep`
never inherits `olderThanDays` from the config. With no flags and no
`retention` block the command refuses to run and exits 2, so it never
deletes every run by default.

Prune rules:

- When both `--keep` and `--older-than` are given, a run is deleted only if both select it.
- A sharded suite run and its worker runs form one unit. The unit is kept or deleted as a whole, counts once toward `--keep`, and takes its age from the suite run.
- A run that is still being written is never deleted, and neither is any unit that contains one. The unit is reported as skipped.
- A run record that cannot be read is never touched.
- With `--dry-run` the command classifies every unit and deletes nothing.

The table has the columns `ACTION`, `RUN ID`, `STARTED`, `RUNS` (the
number of runs in the unit), and `WHY`. `ACTION` is `deleted`, `would
delete`, `skipped`, or `failed`. `WHY` gives the selection reason, or
the error for a skipped or failed unit. With no selected unit the
command prints `No runs match the prune criteria.`

The JSON document has `dryRun`, `keep` and `olderThanDays` (each
omitted when not given), `units`, `runsDeleted`, and `skipped`. `units`
is always present. Each unit has `runId`, `startedAt`, `runs` (the
suite run id first, then its workers), `action` (`deleted`,
`wouldDelete`, `skippedLive`, or `failed`), `reason`, and, for a unit
that was not deleted, `error`.

### runs exit codes

| Code | Meaning |
|---|---|
| `0` | The command succeeded. For `prune`, every selected unit was deleted or `--dry-run` was given. |
| `1` | The storage could not be read. For `prune`, a deletion failed or was refused. |
| `2` | The command was misused: an unknown command or flag, a bad flag value, or `prune` with neither flags nor a config `retention` block. |

## Shortcuts

| Shortcut | Same as |
|---|---|
| `atomyx run [flags]` | `atomyx driver run [flags]` |
| `atomyx suite [name] [flags]` | `atomyx driver suite [name] [flags]` |
| `atomyx doctor [flags]` | `atomyx driver doctor [flags]` |
| `atomyx inspect [flags]` | `atomyx driver inspect [flags]` |
| `atomyx setup [flags]` | `atomyx driver setup [flags]` |
| `atomyx devices [flags]` | `atomyx driver list-devices [flags]` |
| `atomyx init [flags]` | `atomyx skills init [flags]` |
| `atomyx update-skills [flags]` | `atomyx skills update-skills [flags]` |

## References

- [`docs/README.md`](./README.md): index of the end-user documentation directory.
- [`.claude/docs/architecture.md`](../.claude/docs/architecture.md): the framework's high-level contract.
