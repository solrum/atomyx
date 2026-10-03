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
| `--report <formats>` | Enables report generation and selects its output formats: `html`, `json`, or both, separated by a comma. Overrides the project config's `report` block. | the project config's `report.formats`, or `html` when reporting is enabled by `--report-dir` alone | no |
| `--report-dir <path>` | Root directory the run folder is written under. See "Run folder" below. | the project config's `report.outDir`, or the storage root | no |
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

The run folder is `<root>/runs/v1/<id>/`, and the root is the first of
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

Tooling that reads a report uses the `report.html` and `report.json`
files in the run folder's `report` directory. The flat
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
| `--report <formats>` | Enables report generation and selects its output formats: `html`, `json`, or both, separated by a comma. Same precedence rule as `atomyx driver run`. | the project config's `report.formats`, or `html` when reporting is enabled by `--report-dir` alone | no |
| `--report-dir <path>` | Root directory the run folder is written under. Same rule as `atomyx driver run`. | the project config's `report.outDir`, or the storage root | no |
| `--help`, `-h` | Prints this command's usage and exits. | off | no |

Selecting by tag alone requires `testRoots` in the project config, or
a `--glob` to narrow the search, because a tag-only selector has no
other way to bound which directory to scan. `atomyx driver suite`
does not accept `--file`, `--platform`, `--device`, `--proxy`, or
`--json`, because those are specific to `atomyx driver run`.

Example:

```
atomyx driver suite --tag smoke --tag regression
```

Exit codes: `0` when every flow passed, `1` when the suite ran and at
least one flow failed, `2` when the selector matched no flows or the
command was misused, `130` when SIGINT interrupted the suite, and `143`
when SIGTERM did. An interrupted suite prints `interrupted` and finalizes its run
folder as cancelled before it exits.

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
| `--report-dir <path>` | Run root for `--migrate-runs`. Defaults to `ATOMYX_STORAGE_DIR`, then `~/.atomyx`. | none | no |
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
shows the live UI tree and screenshot of the device and tests selectors
against the tree.

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

The command takes the device like `run` does, then listens on
`127.0.0.1` with a system-chosen port and nothing else. It prints one
line to stdout, the URL, which names `127.0.0.1` and carries a
single-use code in the fragment:

```
http://127.0.0.1:<port>/#<code>
```

Unless `--no-open` is set, the command also opens the URL in the
default browser. The code then appears in the browser's process
arguments, where another local account can read it, so every code
exchange is logged to stderr. A code that is used twice, or a wrong
one, is logged as refused.

- **A fresh URL.** A reload loses the page's token. Press Enter in the
  terminal to print a fresh URL, which the command also opens unless
  `--no-open` is set. The end of stdin does not stop the server.
- **Code lifetime.** A URL works for 60 seconds after the command prints
  it, and for one visit. After that the page says the code was refused
  because it expired, and the server answers `CODE_REFUSED`. Press
  Enter in the terminal for a fresh URL. The terminal log records the
  refusal as `inspector.auth.expired`.
- **Idle release.** The command keeps the device until
  `session.mcpIdleTimeoutMs` passes without an action taken in the page,
  such as asking for a fresh capture, suggesting a selector, testing a
  selector or copying a step. Reading what the session already holds
  does not count, so an open page that only polls does not keep the
  device. After the timeout the command releases the device, so another
  Atomyx process can take it, and the page shows that it is released.
- **Reconnect.** The page's reconnect button takes the device again.
  The command never does so on its own. When another process holds the
  device by then, reconnect fails with `DEVICE_HELD` and the device
  stays released. The server and the token keep working across a
  release and a reconnect.
  The page shows the released banner with a Reconnect button when the
  feed or any action answers `410` with `DEVICE_RELEASED`. Pressing it
  posts to `/api/v1/reconnect`, and on success the page resumes the
  capture feed and refetches the session, the tree and the screenshot.
  When the device is held elsewhere, the banner shows the refusal and
  its remedy and the button stays available.
- **Lost device.** When the device goes away while the command holds
  it, for example the cable is pulled or the runner process exits, the
  command gives the session up, the header shows the holder state
  `lost`, and the page shows a banner with a Reconnect button. Unlike
  `run`, which stops when the device is lost, the inspector keeps
  running. Reconnect takes the device again through the same steps as
  at startup, and fails with the device's own code, such as
  `DEVICE_HELD`, when another process holds it by then.
- **The page.** The header names the device, its platform and kind,
  and the holder state. The page reads the code from the URL fragment,
  removes it from the address bar, and trades it for a token that it
  keeps in memory only. A page opened without a code, or with a code
  that was already used, says so and tells you to press Enter in the
  terminal for a fresh URL.
- **The tree.** The middle pane shows the tree as an outline. Each row
  shows the role and the most useful of the id, label and text, with
  secret values already masked by the server. Only the first three levels
  show at first, and a twisty, a click or the arrow keys open and
  close nodes. Only the rows near the viewport are drawn, so a tree
  of tens of thousands of nodes stays responsive.
- **Search.** The search box matches the id, label, text, hint and
  role of every node. It opens only the paths to the matches, marks
  the matching text, and Enter steps to the next match while Shift
  and Enter steps back. Selecting a row, by click, arrow key or match
  step, selects its node for the detail panels.
- **The panels.** The right pane holds four panels, from top to bottom.
  - Properties lists every attribute of the selected node as the tree
    response returns it, with the node's path, its bounds and its state
    flags. A secure node shows its masked values here, and its tree row
    is labelled by its hint or label.
  - Selector suggestions asks the server for candidate selectors for
    the selected node. Each candidate shows `unique` or `ambiguous`, how
    many nodes it matches, and `obscured` when another node covers the
    target. A candidate button copies its selector into the tester. A
    selection from an earlier capture is not sent, and a `TREE_STALE`
    answer shows a notice and reloads the tree. A capture is named by
    its session generation and its revision together, so a tree from
    before a reconnect is never mistaken for one after it, although
    both may number their revisions alike.
  - Selector tester takes the selector as one JSON object, the same
    shape a script step uses: `id`, `label`, `text`, `hint`, `value`,
    `role`, `enabled`, `clickable`, `focused` and `nth`, where a text
    field may also be `{"regex": "..."}`. Test selector lists the
    matches in resolution order with their paths and bounds, and a match
    button selects that node in the tree.
  - Copy as YAML renders one step for the tester's selector. The action
    is `tap`, `type`, `waitFor`, `assertVisible`, `assertNotVisible` or
    `assertExists`, and `type` also takes the text. The server renders
    the YAML, and for a secure target it writes a placeholder such as
    `${password}` instead of the text, which the panel explains. Copy
    puts the step on the clipboard through the browser, which may refuse
    it, in which case the step stays selectable.
- **The highlight.** The selected node's bounds are drawn over the
  screenshot only when the server reports that the tree and the
  screenshot were captured together and can be matched, the tree on
  screen names the screenshot on screen as its own, and the selection
  belongs to the tree currently shown. Otherwise no box is drawn and a
  note under the screenshot says why. While the screenshot is
  highlightable, a click on it selects the deepest node whose bounds
  contain the clicked point, and the tree opens the path to that node.
- **Live updates.** The page follows the capture feed, so the
  screenshot and the header change as the holder captures. A feed that
  drops or ends is reopened after a growing pause. When the device has
  been released, the page shows a banner and stops reading until the
  device is taken again. The Refresh button asks for a fresh capture,
  and a tool call or another capture in progress shows as a notice
  that does not block the page.
- **Stopping.** Ctrl-C and SIGTERM stop the server and release the
  device.

Exit codes: `0` when the inspector stopped without a signal, `1` when
the device could not be reached or held or the server failed, `2` when
the command was misused, `130` when Ctrl-C stopped it, and `143` when
SIGTERM did.

## Device ownership

`run`, `suite`, `doctor`, and `inspect` take exclusive ownership of the device for
the length of the command. Only one Atomyx process, whether this CLI or
an `atomyx-mcp` server, can drive a device at a time.

- A second command against a device that is already owned fails with
  code `DEVICE_HELD`. The message names the holder's process id,
  command, and state. Stop that process or wait for it to finish, then
  run the command again.
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
