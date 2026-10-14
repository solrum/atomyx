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
  --break <file>:<line>    Pause the run before the step that starts at
                           that line of a script file, then wait for
                           Enter. Repeatable. Needs a terminal on
                           standard input. With --json the prompt goes
                           to standard error.
  --video                  Record the device screen and keep the video
                           of a failed run in the run folder, whatever
                           the config video setting says. A passed or
                           interrupted run keeps no video.
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
  --device <id>            Run the whole suite on this device, as
                           reported by atomyx driver list-devices.
                           The suite runs in this process on that
                           one device.
  --devices <ids>          Spread the suite over these devices,
                           comma-separated, one worker process per
                           device. Cannot be combined with --device
                           or --shard.
  --shard auto             Spread the suite over every connected,
                           ready device of the platform. Zero
                           devices is a usage error.
  --platform <ios|android> Platform of the devices --shard auto looks
                           for. Defaults to the environment's
                           platform, then android.
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
                           comma-separated formats: html, json,
                           junit, or any combination.
  --report-dir <path>      Run root. The run folder is written under
                           <path>/runs/v1/<id>/, and alone this flag
                           implies an html report. Defaults to the
                           config report.outDir, then
                           ATOMYX_STORAGE_DIR, then ~/.atomyx.
  --video                  Record the device screen and keep the video
                           of each failed entry in the run folder,
                           whatever the config video setting says.
  --retries <n>            Run a failed entry again up to n more times,
                           from 0 to 3. Only a test failure is retried,
                           never a canceled run, a lost device or an
                           entry that cannot be loaded. An entry that
                           fails and then passes is reported as flaky
                           and counts as passed. Overrides the config
                           suite's retries.
  --json                   Print one JSON object to standard output
                           with ok, the run folder path, and the
                           entries as index.json lists them. The
                           human-readable summary goes to standard
                           error instead.
  --help, -h               Show this message.

EXAMPLES
  atomyx driver suite smoke
  atomyx driver suite --tag smoke
  atomyx driver suite smoke --device <UDID> --json
  atomyx driver suite smoke --devices <SERIAL>,<UDID> --json
  atomyx driver suite --glob "flows/**/*.yml" --shard auto
  atomyx driver suite --glob "flows/**/*.yml" --tag critical

EXIT CODES
  0    every entry in the suite passed, or failed only while
       quarantined
  1    the suite ran and at least one entry failed, or a sharded
       suite left entries unrun because every worker stopped
  2    the command was misused, or no flow matched the selector
  130  interrupted by SIGINT
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
	case CommandInspect:
		_, _ = io.WriteString(w, `atomyx driver inspect
  Open the browser inspector for a device: its live UI tree and
  screenshot, with selector suggestions and a selector tester. The
  server listens on 127.0.0.1 only and the device is released when
  the command ends or sits idle. A device another Atomyx process
  holds is watched instead: the page shows its captures, its state
  and its step lines, and asks for a fresh capture only while that
  process is idle.

USAGE
  atomyx driver inspect [flags]

FLAGS
  --platform <ios|android> Target platform. Defaults to android.
  --device <id>            Device identifier, as reported by
                           atomyx driver list-devices.
  --no-open                Print the URL without opening a browser.
  --help, -h               Show this message.

The command prints one URL on stdout. Press Enter in the terminal
for a fresh URL, for example after reloading the page. The device is
released after session.mcpIdleTimeoutMs without an action in the
page, and the page can take it again with its reconnect button. When
the process being watched exits, the page offers the same button,
which takes the device or watches its next holder. The command never
takes a device on its own.

EXAMPLES
  atomyx inspect --platform android
  atomyx driver inspect --platform ios --device <UDID> --no-open

EXIT CODES
  0    the inspector stopped
  1    the device could not be reached or held, its holder serves no
       socket, is still starting or cannot be reached, or the server
       failed
  2    the command was misused
  130  stopped by Ctrl-C
  143  stopped by SIGTERM
`)
	case CommandSetup:
		_, _ = io.WriteString(w, `atomyx driver setup
  Prepare a device to run Atomyx. Runs the platform's setup steps in
  order, applies the safe fixes it can make from this machine, and
  prints the exact action for everything else. A step whose
  prerequisite did not pass is skipped. The command takes the device
  lease without connecting a driver, so a broken agent cannot fail
  inside it. A device that another Atomyx process holds is reported
  with its holder and left unchanged.

USAGE
  atomyx driver setup [flags]

FLAGS
  --platform <ios|android> Target platform. Defaults to android.
  --device <id>            Device identifier, as reported by
                           atomyx driver list-devices.
  --check                  Only observe. Nothing on this machine or the
                           device is changed.
  --json                   Print {ok, steps:[{id, status, message,
                           remedy}]} to standard output instead of
                           text. Status is one of ok, fixed, manual,
                           failed, or skipped. ok is true when every
                           step is ok or fixed.
  --help, -h               Show this message.

EXAMPLES
  atomyx setup --platform android
  atomyx driver setup --platform ios --device <UDID> --check
  atomyx setup --platform android --json

EXIT CODES
  0    every step is ok or fixed
  1    a step is manual, failed, or skipped, or the device is held
       by another Atomyx process
  2    the command was misused
  130  interrupted by SIGINT
  143  stopped by SIGTERM
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
  --bundle <path.zip>      Instead of a health check, write a diagnostic
                           zip: versions of atomyx and the host tools,
                           the connected devices with their lease state,
                           and the meta and log files of the five newest
                           runs. Every file is scrubbed of environment
                           and device.env values and of token-shaped
                           strings. No screenshot is included, no device
                           lease is taken, and device health is not
                           checked. Prints the path on success. Cannot be
                           combined with --migrate-runs, --platform,
                           --device, or --json.
  --report-dir <path>      Run root for --migrate-runs and --bundle.
                           Defaults to ATOMYX_STORAGE_DIR, then ~/.atomyx.
  --help, -h               Show this message.

EXAMPLES
  atomyx driver doctor
  atomyx driver doctor --platform ios --device <UDID>
  atomyx driver doctor --json
  atomyx driver doctor --migrate-runs
  atomyx driver doctor --migrate-runs --json --report-dir <path>
  atomyx driver doctor --bundle atomyx-diagnostics.zip

EXIT CODES
  0    the device is reachable and healthy, or held by another process
  1    the connection could not be established, or the device is
       reachable but unhealthy, or with
       --migrate-runs a record is damaged or in conflict, or with
       --bundle the zip could not be written
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
  inspect [flags]          Open the browser inspector for a device.
  setup [flags]            Prepare a device to run Atomyx.
  version                  Print the driver module's version and exit.
  help                     Print this usage and exit.

Run atomyx driver <command> --help for the full flag reference of
that command.

SHORTCUTS
  atomyx run ...            same as atomyx driver run ...
  atomyx suite ...          same as atomyx driver suite ...
  atomyx devices            same as atomyx driver list-devices
  atomyx doctor ...         same as atomyx driver doctor ...
  atomyx inspect ...        same as atomyx driver inspect ...
  atomyx setup ...          same as atomyx driver setup ...

SEE ALSO
  https://atomyx.dev (documentation)
`)
}
