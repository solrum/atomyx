# Examples

## Purpose

Runnable YAML scripts and a fixture app to try Atomyx against, and to
use as templates for your own scripts. Read
[`../docs/yml-script-reference.md`](../docs/yml-script-reference.md)
for the format these scripts use.

## Fixture app

`atomyx-demo/` is a Flutter app, cross-compiled to both platforms,
bundle id `dev.atomyx.demo` on both Android and iOS. Every script
below needs it installed except `ios-settings.yml`, which drives the
stock Settings app instead and needs nothing extra installed. See
[`atomyx-demo/README.md`](./atomyx-demo/README.md) for a
screen-by-screen breakdown of what it exercises.

### Prerequisites

Flutter 3.38.6, pinned in
[`atomyx-demo/.fvmrc`](./atomyx-demo/.fvmrc). Confirm your installed
version matches before building:

```bash
flutter --version
```

### Android: build and install

```bash
cd examples/atomyx-demo
flutter pub get
flutter build apk --debug
adb install -r build/app/outputs/flutter-apk/app-debug.apk
```

The debug APK lands at
`examples/atomyx-demo/build/app/outputs/flutter-apk/app-debug.apk`.
`adb install -r` installs it on whichever device or emulator `adb`
currently targets, replacing any existing install of the same app.

### iOS simulator: build and install

```bash
cd examples/atomyx-demo
flutter pub get
flutter build ios --debug --simulator
xcrun simctl install booted build/ios/iphonesimulator/Runner.app
```

`--simulator` skips code signing, since a simulator build needs
none. `xcrun simctl install booted` installs onto whichever
simulator is currently booted.

A `dev.atomyx.demo` install from an older checkout can be out of date
with the example scripts below, causing wait timeouts against screens
or labels that changed since that build. Before running the examples,
rebuild and reinstall the demo app from your current checkout using
the build steps above.

## Scripts

The Platform column names the platform or platforms a script has
been verified against. Not verified means this pass did not run it
on either platform, not that it cannot run there.

| File | Shows | Platform | Needs demo app | Needs proxy |
|---|---|---|---|---|
| `test-login-flow.yml` | Login, an OTP branch with `handle`, and `assertVisible` | Android | Yes | No |
| `api-branching.yml` | Branching a flow on a captured API response | Not verified | Yes | Yes |
| `api-capture-assert.yml` | `capture` and `assertApi` against network traffic | Not verified | Yes | Yes |
| `composed-flow.yml` | A flow that composes `requires` and `runFlow` sub-flows | Android | Yes | No |
| `run-timeline-demo.yml` | The report and step-trace surface a run produces | Android | Yes | No |
| `checkout-regression.scenario.yml` | A scenario file that composes several scripts into one suite run | Not verified | Yes | Yes |
| `flows/` | Reusable flow fragments (`flows/sign-in.yml`, `flows/sign-out.yml`) that `requires` and `runFlow` pull in | Not verified | Yes | No |
| `ios-settings.yml` | Navigating the stock iOS Settings app with `launchApp`, `waitFor`, `tap`, and `assertVisible` | iOS | No | No |

`test-login-flow.yml` passes on Android. On the iOS simulator it
currently fails at the login step: the email field visibly takes
focus, but the driver reports no focused element and no on-screen
keyboard, so the wait for a ready input field times out. This
happened on every attempt, with or without the simulator's hardware
keyboard setting changed.

Run any script directly:

```bash
atomyx run --file examples/test-login-flow.yml --platform android --device <serial>
```

Run the scenario file the same way. `atomyx run` recognizes the
scenario format and runs each of its scripts in order:

```bash
atomyx run --file examples/checkout-regression.scenario.yml --platform android --device <serial>
```

## Writing your own

Copy the script closest to what you need and edit it in place. A
script's `appId` must match the app under test, so point it at
`dev.atomyx.demo` while you learn the format, then swap in your own
app.
