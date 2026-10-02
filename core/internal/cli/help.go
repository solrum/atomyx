package cli

import "io"

// PrintCommandHelp writes the full flag reference for command to w.
func PrintCommandHelp(command Command, w io.Writer) {
	switch command {
	case CommandRun:
		_, _ = io.WriteString(w, `atomyx driver run
  Run a single YML test script against a device.

USAGE
  atomyx driver run --file <path> [flags]

FLAGS
  --file <path>            Path to the YML test script. Required.
  --platform <ios|android> Target platform. Defaults to android.
  --device <id>            Device identifier, as reported by
                           atomyx driver list-devices.
  --proxy <type:path>      Network capture target, for example
                           file:/tmp/capture.jsonl.
  --var <KEY=VALUE>        Inject a variable, available to the script
                           as ${KEY}. Repeatable. Overrides the same
                           key from --var-file and from the script's
                           own env block, in that order.
  --var-file <path>        JSON file of flat key-value string
                           variables, merged in before --var is
                           applied.
  --config <path>          Path to the project config file.
  --env <name>             Named environment to resolve platform,
                           device, proxy, and variables from.
  --preconditions <skip|force>
                           Override every precondition's own policy
                           for this run.
                           skip does nothing for every precondition.
                           force runs every precondition's setup
                           unconditionally.
  --skip-preconditions     Shorthand for --preconditions skip.
  --json                   Output the result as JSON instead of text.
  --help, -h               Show this message.

EXAMPLES
  atomyx driver run --file test.yml --platform ios --device <UDID>
  atomyx driver run --file flow.yml --proxy file:/tmp/capture.jsonl

EXIT CODES
  0    the script passed
  1    the script ran and failed, or the run could not start (a
       missing script file, a script that fails to parse, or a
       device that could not be reached)
  2    the command was misused
`)
	case CommandSuite:
		_, _ = io.WriteString(w, `atomyx driver suite
  Run a set of YML test scripts and scenarios selected by a named
  config suite, or by ad-hoc --tag / --glob flags.

USAGE
  atomyx driver suite [<name>] [flags]

FLAGS
  <name>                   A suite name declared in the project
                           config's suites block. Mutually exclusive
                           with --tag and --glob.
  --tag <name>             Select files whose top-level tags field
                           includes this tag. Repeatable; a file
                           matches when it carries any of the given
                           tags.
  --glob <pattern>         Select files whose path (relative to a
                           test root) matches this glob. Repeatable;
                           a file matches when it matches any of the
                           given globs. Supports *, **, and ?.
  --config <path>          Path to the project config file.
  --env <name>             Named environment to resolve platform,
                           device, proxy, and variables from.
  --var <KEY=VALUE>        Inject a variable, available to every
                           entry as ${KEY}. Repeatable.
  --var-file <path>        JSON file of flat key-value string
                           variables, merged in before --var is
                           applied.
  --preconditions <skip|force>
                           Override every precondition's own policy
                           for every entry in this suite.
  --skip-preconditions     Shorthand for --preconditions skip.
  --report <formats>       Write a report bundle in the given
                           comma-separated formats: html, json, or
                           both.
  --report-dir <path>      Run root. The run folder is written under
                           <path>/runs/v1/<id>/, and alone this flag
                           implies an html report. Defaults to the
                           config report.outDir, then
                           ATOMYX_STORAGE_DIR, then ~/.atomyx.
  --help, -h               Show this message.

EXAMPLES
  atomyx driver suite smoke
  atomyx driver suite --tag smoke
  atomyx driver suite --glob "flows/**/*.yml" --tag critical

EXIT CODES
  0    every entry in the suite passed
  1    the suite ran and at least one entry failed
  2    the command was misused, or no flow matched the selector
`)
	case CommandListDevices:
		_, _ = io.WriteString(w, `atomyx driver list-devices
  Enumerate devices Atomyx can currently see: Android devices and
  emulators through adb, booted iOS simulators, and iOS physical
  devices connected over USB.

USAGE
  atomyx driver list-devices [flags]

FLAGS
  --json                   Output the device list as JSON instead of
                           a formatted list.
  --help, -h               Show this message.

EXAMPLES
  atomyx driver list-devices
  atomyx driver list-devices --json
`)
	case CommandDoctor:
		_, _ = io.WriteString(w, `atomyx driver doctor
  Connect to a device and report its health: whether the connection
  succeeded and whether it answers a bounded liveness check. Does not
  run a flow or touch app state. A device that another Atomyx process
  holds is reported with its holder and is not probed.

USAGE
  atomyx driver doctor [flags]

FLAGS
  --platform <ios|android> Target platform. Defaults to android.
  --device <id>            Device identifier, as reported by
                           atomyx driver list-devices.
  --json                   Output the health report as JSON instead
                           of text.
  --migrate-runs           Instead of a health check, rewrite the flat run
                           records under the run root as run folders.
                           Needs no device, is safe to repeat, and exits
                           1 when a record is damaged or conflicts with
                           an existing folder. Cannot be combined with
                           --platform or --device.
  --report-dir <path>      Run root for --migrate-runs. Defaults to
                           ATOMYX_STORAGE_DIR, then ~/.atomyx.
  --help, -h               Show this message.

EXAMPLES
  atomyx driver doctor
  atomyx driver doctor --platform ios --device <UDID>
  atomyx driver doctor --json
  atomyx driver doctor --migrate-runs
  atomyx driver doctor --migrate-runs --json --report-dir <path>

EXIT CODES
  0    the device is reachable and healthy, or held by another process
  1    the connection could not be established, or the device is
       reachable but unhealthy, or with
       --migrate-runs a record is damaged or in conflict
  2    the command was misused
`)
	case CommandVersion:
		_, _ = io.WriteString(w, `atomyx driver version
  Print the driver module's version and exit.

USAGE
  atomyx driver version

EXAMPLES
  atomyx driver version
`)
	case CommandHelp:
		_, _ = io.WriteString(w, `atomyx driver help
  Print the driver module usage and exit. Equivalent to running
  atomyx driver with no command.

USAGE
  atomyx driver help

EXAMPLES
  atomyx driver help
`)
	}
}

// PrintModuleHelp writes the driver module's own top-level usage to
// w: every command it registers, and the shortcuts that reach them.
func PrintModuleHelp(w io.Writer) {
	_, _ = io.WriteString(w, `atomyx driver
  Device-interaction subcommands: run scripts, run suites, and list
  connected devices.

USAGE
  atomyx driver <command> [flags]

COMMANDS
  run [flags]              Run a single YML test script.
  suite [<name>] [flags]   Run a set of YML test scripts and
                           scenarios selected by name, tag, or glob.
  list-devices [--json]    Enumerate connected devices.
  doctor [flags]           Connect to a device and report its health.
  version                  Print the driver module's version and exit.
  help                     Print this usage and exit.

Run atomyx driver <command> --help for the full flag reference of
that command.

SHORTCUTS
  atomyx run ...            same as atomyx driver run ...
  atomyx suite ...          same as atomyx driver suite ...
  atomyx devices            same as atomyx driver list-devices
  atomyx doctor ...         same as atomyx driver doctor ...

SEE ALSO
  https://atomyx.dev (documentation)
`)
}
