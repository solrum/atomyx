# Atomyx Demo

Reference Flutter app that exercises every Atomyx YML script
command. Use it as the target when developing / smoke-testing
pointer gestures, selector resolution, input handling, and the
rest of the tool surface.

## Screens

| Screen | Route | Elements |
|---|---|---|
| Login | `/login` | Email field, Password field, Login button |
| OTP | `/otp` | OTP input, Verify button |
| Dashboard | `/dashboard` | Welcome text, Premium Features, Products, Account, Sign out |
| Settings | `/settings` | Profile, Notifications toggle, Dark Mode toggle, Version |
| Products | `/products` | Product list with name, category, price |
| Gestures | `/gestures` | Tap, long-press, drag, reorder, flick fixtures for `pointer` |
| Focus | `/focus` | One text field, focused on open, prefilled with `Prefilled text` |
| Permission | `/permission` | Location permission request button and status text |
| Webview | `/webview` | Bundled local web page with a heading, a text input, and a button |

## Test flows

- **Login then Dashboard**: email `user@example.com` + any password
- **Login then OTP then Dashboard**: email containing `otp` (e.g. `otp@example.com`)
- **Login fail**: empty email or password
- **Dashboard then Settings then back**
- **Dashboard then Products**
- **Dashboard then Gestures**: drives the five pointer fixtures; each card surfaces a counter / status string the script can assert against
- **Dashboard then Sign out then confirm then Login**

## Gestures fixture identifiers

Stable Semantics identifiers on the `/gestures` screen, paired
with the pointer action each exercises:

| Identifier | Visible state | Pointer pattern |
|---|---|---|
| `gesture-tap-target` / `gesture-tap-count` | counter increments per tap | `[down, up]` at one point |
| `gesture-longpress-target` / `gesture-longpress-count` | counter on long-press, container turns green | `[down, up]` with ≥500 ms hold |
| `gesture-drag-target` / `gesture-drag-status` | status `idle` then `dragging` then `released`, dx/dy update | `[down, move…, up]` |
| `gesture-reorder-target` / `gesture-reorder-state` | comma-joined order updates after a row moves | press-and-drag (`[down, wait, move, up]`) |
| `gesture-flick-target` / `gesture-flick-bottom-hits` | counter increments when the list reaches Item 50 | flick (fast `[down, move, up]`) |

## Deep links

The scheme `atomyxdemo://` opens any route above. The URL form is
`atomyxdemo://open/<route>`, for example `atomyxdemo://open/settings`
opens `/settings` and `atomyxdemo://open/focus` opens `/focus`. A path
that matches no route opens `/login`, and a malformed URL never crashes
the app.

```bash
adb shell am start -a android.intent.action.VIEW -d atomyxdemo://open/focus
xcrun simctl openurl booted atomyxdemo://open/focus
```

## Initial route at launch

A launcher can start the app directly on a route, which avoids the system
confirmation a custom URL scheme can raise. Android reads a `route` string
extra on the launching intent, and iOS reads a `--route <path>` or
`-route <path>` launch argument. A script reaches the same screen with
`launchApp: {arguments: {route: /focus}}`.

```bash
adb shell am start -n dev.atomyx.demo/.MainActivity --es route /focus
xcrun simctl launch --terminate-running-process booted dev.atomyx.demo --route /focus
```

## Fixture identifiers

| Route | Identifier | Element |
|---|---|---|
| `/focus` | `focus-input` | Text field, autofocused, starts as `Prefilled text` |
| `/permission` | `permission-request-button` | Button that requests location |
| `/permission` | `permission-status` | Text `Location status: <status>` |
| `/webview` | `webview-heading`, `webview-input`, `webview-button`, `webview-result` | Element ids inside the bundled page `assets/webview/index.html` |

## Build targets

Run from the repository root. Each target prints the output path.

| Target | Result |
|---|---|
| `make demo-android` | Debug APK at `examples/atomyx-demo/build/app/outputs/flutter-apk/app-debug.apk` |
| `make demo-ios-sim` | Unsigned simulator build at `examples/atomyx-demo/build/ios-sim/Build/Products/Debug-iphonesimulator/Runner.app` |
| `make demo-ios-device` | Signed release build at `examples/atomyx-demo/build/ios-device/Build/Products/Release-iphoneos/Runner.app` |

`demo-ios-device` reads `DEV_TEAM` from the gitignored
`platforms/ios-agent/device.env` (the same file the iOS agent Makefile
uses) and never prints it.

Signing is automatic by default and needs an Xcode account sign-in for
`-allowProvisioningUpdates`. A machine without a sign-in can pass
`DEMO_PROFILE=<profile name or UUID>` to sign the Runner target manually
with a provisioning profile installed under
`~/Library/Developer/Xcode/UserData/Provisioning Profiles`, for example a
wildcard development profile of the same team that contains the device:

```bash
make demo-ios-device DEMO_PROFILE="<profile name>"
```

The target writes the gitignored `ios/Flutter/DemoSigning.xcconfig`, which
only the Runner target includes, and removes it again on every run without
`DEMO_PROFILE`. The CocoaPods targets keep automatic signing. Android needs minSdk 24 and iOS needs a
deployment target of 15.0, both required by the webview plugin and the
current Xcode toolchain.

## Run

```bash
cd examples/atomyx-demo
flutter run
```

## Bundle ID

- Android: `dev.atomyx.demo`
- iOS: `dev.atomyx.demo`

## Matching YML scripts

- `examples/test-login-flow.yml` is login + OTP branching + handle
  (exercised end-to-end against this fixture)
