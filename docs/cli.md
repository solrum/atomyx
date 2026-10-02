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
| `atomyx driver list-devices` | Lists Android, iOS simulator, and iOS physical devices. |
| `atomyx driver version` | Prints the driver module's version. |
| `atomyx skills init` | Copies the bundled skill and agent files into a project. |
| `atomyx skills update-skills` | Updates an existing skills install to the bundled version. |

Three shortcuts skip the `driver` module name: `atomyx run`, `atomyx
suite`, and `atomyx devices` map to `atomyx driver run`, `atomyx
driver suite`, and `atomyx driver list-devices`. Two shortcuts skip
the `skills` module name: `atomyx init` and `atomyx update-skills`.

The CLI ships exactly two modules: `driver` and `skills`. The MCP
server is a separate stdio binary, `atomyx-mcp`, that an AI client
launches directly instead of going through the `atomyx` binary.

## Module and command structure

```
atomyx driver run [flags]
atomyx driver suite [name] [flags]
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
| `--report-dir <path>` | Directory to write the report bundle into. Required when reporting is enabled and the config sets no `report.outDir`. | the project config's `report.outDir` | no |
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

Reporting is opt-in: it runs only when the project config declares a
`report` block, or when `--report` or `--report-dir` is given. When
reporting runs, the CLI writes a report bundle under
`<outDir>/<runId>/` and prints the bundle's directory. The CLI flags
take precedence over the config's `report` block field by field, so
`--report-dir` alone can redirect a config-declared report without
repeating `--report`.

Example:

```
atomyx driver run --file test.yml --platform ios --device <UDID>
atomyx driver run --file test.yml --report html,json --report-dir ./reports
```

Exit codes: `0` when the script passed, `1` when the script ran and
failed, `2` when the command was misused or a usage or config error
occurred, `130` when SIGINT interrupted the run, and `143` when SIGTERM
did. An interrupted run prints `interrupted` and still writes the
partial report.

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
| `--report-dir <path>` | Directory to write the report bundle into. Same rule as `atomyx driver run`. | the project config's `report.outDir` | no |
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
when SIGTERM did. An interrupted suite prints `interrupted` and still
writes the partial report.

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
| `atomyx devices [flags]` | `atomyx driver list-devices [flags]` |
| `atomyx init [flags]` | `atomyx skills init [flags]` |
| `atomyx update-skills [flags]` | `atomyx skills update-skills [flags]` |

## References

- [`docs/README.md`](./README.md): index of the end-user documentation directory.
- [`.claude/docs/architecture.md`](../.claude/docs/architecture.md): the framework's high-level contract.
