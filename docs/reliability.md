# Atomyx: reliability

## Purpose

For a maintainer, an adopter evaluating Atomyx, or a release owner who
needs to judge whether the runtime is dependable on real devices.
Answers one question: how is reliability measured, what thresholds
count as reliable, and how do you reproduce the numbers yourself.

This doc does not hold any measured result. It does not explain how to
write a script (`yml-script-reference.md`), how to set up a device
(`device-setup.md`), or how the scheduler and the soak flow are built
(`../.claude/docs/development.md`).

## TL;DR

- Reliability is measured by three things run on real devices: the
  end-to-end conformance corpus, a nightly device matrix, and a soak of
  one demo flow repeated many times.
- Every failure in a soak is classified as an infrastructure failure
  or a product failure from its error code, and only infrastructure
  failures count toward the infrastructure failure rate.
- The bar is four conditions at once, listed under Thresholds.
- No number is recorded on this page. Run `scripts/e2e-trend.sh` to
  see the current figures from your own nightly results.

## What is measured

### The end-to-end conformance corpus

`make e2e PLATFORM=<iossim|android|ios>` runs one behavioral corpus
against a real device or simulator through `scripts/e2e.sh`. The same
corpus runs against every driver implementer, and the in-memory mock
runs it without a device as part of `make check`. The run writes
pass, fail, and skip counts and the skip gate verdict to a summary
under `e2e-results/`.

### The skip allowlist and the skip gate

A test may skip itself only with a reason key defined in
`core/internal/driver/drivertest/skip.go`. The file
`scripts/e2e-skip-allowlist.txt` names, per platform, the keys that
are allowed to appear in a green run, split into a permanent section
and a temporary section. `scripts/e2e-skip-gate.sh` compares every
skip of a run against that list. A skip with no key, or with a key the
platform does not list, is an unexpected skip and fails the run even
when every test passed.

### The device matrix, run nightly

`scripts/nightly.sh` reads a matrix file with one device per line and
runs the corpus and a short soak on each, one device at a time. It
records one `result.txt` per device per night under
`e2e-results/nightly/`. A failing or skipped device is recorded and
the next one still runs.

### The soak

`scripts/soak.sh` repeats the demo flow `examples/soak/demo-soak.yml`
on one device, resets the demo app before each iteration, and keeps
going after a failed iteration. Its summary reports passed and failed
counts, the infrastructure failure count, and the infrastructure
failure rate.

Failures are classified by error code. The table in
`core/internal/history/classify.go` assigns each error code to one of
two classes:

- Infrastructure: the device, the on-device agent, the host tooling,
  or a resource held by another process caused the failure. Examples
  are an unreachable agent, a failed app install, a tool timeout, and
  a port already in use.
- Product: the test or the app under test caused the failure, such as
  an assertion, a missing element, or an invalid argument.

A run that ends because the device was lost or the run was interrupted
has no verdict and counts only as an infrastructure failure. A failure
whose code the table does not list counts as a product failure.

## Thresholds

Atomyx counts as reliable on a platform when all four hold:

- The nightly matrix is green on every device for two consecutive
  weeks.
- The soak infrastructure failure rate is under 0.5 percent per
  platform.
- The number of unexpected skips is zero.
- Every infrastructure failure in the window has been triaged to a
  cause, a fix, or a recorded decision to accept it.

The soak script enforces the rate limit for a single soak through its
exit status. The two-week window and the triage are judged from the
trend report.

## How to reproduce

Run the corpus on one platform:

```bash
make e2e PLATFORM=android
make e2e PLATFORM=iossim
make e2e PLATFORM=ios
```

The `ios` leg drives a physical iPhone and needs a signed demo build
and its bundle id in the environment. The variables, the build
command, and the soak requirements for that leg are in
`../.claude/docs/development.md`.

Run a soak of 100 iterations on one device:

```bash
scripts/soak.sh android <device-id> 100
```

Run the nightly matrix once, with a matrix file copied from
`scripts/matrix.example` and edited to your device ids. The `--dry-run`
form prints the plan without touching a device:

```bash
scripts/nightly.sh --dry-run <matrix-file>
scripts/nightly.sh <matrix-file>
```

Read the figures from the last two weeks of nightly results, one table
per device with green nights, soak runs, and the pooled infrastructure
failure rate:

```bash
scripts/e2e-trend.sh --nights 14
```

The pooled rate is the sum of infrastructure failures over the sum of
soak runs, so a night with few runs does not weigh as much as a night
with many.

## References

- `../.claude/docs/development.md`: the e2e, soak, nightly, and trend
  report mechanics.
- `device-setup.md`: preparing an Android or iOS device.
- `cli.md`: the `atomyx runs stats` command the soak reads.
