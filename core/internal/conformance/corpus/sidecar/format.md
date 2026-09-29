# Sidecar corpus format

## Purpose

Who reads this: a contributor adding or reviewing a device-free
sidecar differential case under `corpus/sidecar/`. When to read it:
before adding a new `*.json` case file here, or before changing the
loader in `../../sidecar_corpus.go` or the replay logic in
`../../sidecar_differential_test.go`. What this does not cover: the
RPC-step corpus format used by `corpus/*.json` (documented in
`../format.md`), or the frozen golden fixture a case's trace is
diffed against (see `../../golden.go`).

## Summary

A sidecar case seeds a fixed device list and, for cases that select a
device, a scripted `MockDriver` program, then replays a sequence of
JSON-RPC calls against an in-process Go sidecar (`core/internal/sidecar`
wired the same way `cmd/atomyx-runtime` wires it). The differential
test diffs the resulting trace, every step's response or wire error
code followed by every event the sidecar published, against a frozen
golden fixture at `../../goldens/sidecar/<name>.json`.

## Fields

Every case has:

- `name` (string, required): case identifier, matching the filename
  without the `.json` extension.
- `description` (string, required): one sentence stating what the
  case exercises.
- `deviceSnapshots` (array of arrays of device descriptors, optional):
  fed to a `StaticProbe`: each scan call consumes the next queued
  snapshot, in order. A descriptor is `{id, platform, name, kind,
  state}`. Omitted or empty means no device is ever reachable.
- `driver` (object, optional): the `MockDriver` program staged after a
  successful `selectDevice` step: `hierarchies` (an ordered queue of
  raw `TreeNode` trees, same shape as `corpus/runner/`'s driver
  program), `capabilities` (a partial override merged onto the mock's
  default capabilities), and `screen` (`{width, height}`). Omitted
  when no case step ever depends on driver state.
- Exactly one of:
  - `steps` (array): an ordered list of `{method, params}` JSON-RPC
    calls, replayed sequentially, each response awaited before the
    next call is sent.
  - `concurrentStop` (object): the one non-linear shape this corpus
    supports, documented below.

## The linear `steps` shape

Each step is `{method, params}`; `params` is optional (an absent or
empty `params` sends an empty object, matching the wire contract's own
handling of missing params). There is no `expect` field: the
differential test replays the step sequence against the Go sidecar and
diffs the resulting trace (every step's response or wire error code,
followed by every event the sidecar published) against the frozen
golden. Only the wire error **code** is compared for a step that
errors, never the message text: a `runScript` parse failure surfaces
the underlying script parser's own error text, which is not expected
to match the recorded golden word for word (the `corpus/script/`
differential excludes message text for the same reason).

Three wire fields are normalized to a fixed placeholder before
diffing because they are inherently non-deterministic: `runId` (a
fresh random UUID per run), `startedAt`, and `durationMs` (both
wall-clock-derived: the `ScriptRunner`'s own step timing clock is a
real `SystemClock` regardless of the `MockDriver`'s Orchestra clock,
so elapsed real time never matches the golden's recorded value).
Normalization replaces the value only; a field the golden carries that
Go's trace omits, or vice versa, still surfaces as a real mismatch.

## The `concurrentStop` shape

Fields: `yaml` (the script to run, whose header sets `stepDelay` to
the same value as `stepDelayMs` below), `stepDelayMs` (documents the
inter-step delay `yaml` declares, kept as a separate field so the
margin below is checked against a named value rather than a string
parsed out of `yaml`), and `stopAfterMs` (how long the differential
test waits, in real time, after firing `runScript` before calling
`stopScript`).

This shape exists because every in-command failure is caught per step
(an assertion failure, an unknown command, even a command that
observes its context canceled mid-execution) and reported as a
graceful `ok: false` step, never as a thrown error out of the run. The
**only** device-free way to produce a genuine `runErrored` event is a
cancellation observed at the top of the step loop, between two steps,
which only happens during the script's own inter-step delay
(`stepDelay`). The differential test:

1. Selects the device (and stages `driver`, if any).
2. Fires `runScript` in the background.
3. Waits `stopAfterMs` of real time, chosen well inside `stepDelayMs`
   so the run is still inside its first inter-step delay.
4. Calls `isRunning` (expected `true`) and then `stopScript` while the
   run is still in flight.
5. Joins the backgrounded `runScript` call, expecting a wire error.
6. Calls `isRunning` once more (expected `false`).

`runScript`'s inter-step delay goes through `orchestra.SystemClock.Sleep`,
which takes a `context.Context` and returns as soon as it is canceled,
so a stop arriving mid-delay ends the run before the next step starts.
Exactly how many steps complete before that cancellation is observed
still depends on real-time scheduling, not on anything this corpus can
pin down, so `runSidecarConcurrentStop` excludes `stepStarted` and
`stepCompleted` events from the comparison. The run's lifecycle point
is not timing-sensitive: it always ends at `runStarted`,
`runStopRequested`, `runErrored`, in that order, with `isRunning` true
then false around them, so those events are compared in full, and the
`message` field they carry (the run's own cancellation error text) is
normalized like `runId`.

## Adding a case

1. Save the case as `<name>.json` in this directory.
2. No registration step is needed: `LoadSidecarCorpus` in
   `../../sidecar_corpus.go` reads every `*.json` file here, sorted by
   filename.
3. Run `go test ./internal/conformance/... -update` to write the
   resulting trace as the case's golden fixture, then review the
   generated file before committing it alongside the case.

## Out of scope

Mirror input, the iOS/Android agent lifecycle (`ensureIosAgent`,
`ensureAndroidAgent`, agent status), simulator HID, and scenario runs
are not covered by this corpus: this Go build does not register those
feature groups at all (see `cmd/atomyx-runtime/main.go`), so there is
nothing device-free to differential-test against them yet.
