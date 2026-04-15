# iOS support — open design questions

> **Status**: 🟡 TODO — **the iOS bridge approach has not been decided**. This document captures the constraints, candidate approaches, and open questions. It is **not** an implementation plan. A contributor evaluating iOS support should first agree on the approach, then update this doc with the chosen architecture before writing code.

---

## Why iOS needs a different approach from Android

The Android control plane runs an **embedded HTTP server inside the device APK** (`android/app/src/main/java/.../control/HttpControlServer.kt`). This is possible because Android lets 3rd-party apps run foreground services that host sockets on `localhost`.

iOS sandboxing makes the same approach impossible:

1. A 3rd-party iOS app cannot host a persistent HTTP server in the background — the OS suspends the process within seconds of backgrounding.
2. Custom accessibility services on iOS are gated behind system-level entitlements that 3rd-party developers cannot obtain.
3. `XCTest` / `XCUITest` APIs (the only reliable way to drive arbitrary apps) run inside a test runner process that must be launched via `xcodebuild`, which requires a Mac host.

**Consequence**: iOS needs a **host-side control plane** of some kind. Where exactly that lives, and what it talks to, is still an open question.

---

## Candidate bridge approaches

None of the following are chosen. They are starting points for evaluation. Each has tradeoffs we need to measure before committing.

### Option A — Appium 2.x + xcuitest driver + WebDriverAgent

The mainstream choice. Appium wraps XCUITest via its `xcuitest` driver; WebDriverAgent (WDA) is the XCTest runner that Appium bundles.

**Pros**: mature, actively maintained, handles WDA signing for us, W3C WebDriver protocol is HTTP-friendly.
**Cons**: heavyweight (Appium server + Node runtime + WDA rebuild on every session), signing/provisioning complexity on physical devices, latency spikes during reconnect.
**Unknowns**: how much adet's selector-first mental model maps cleanly to XCUITest predicate strings; how reliable WDA is across iOS 17/18.

### Option B — Direct WebDriverAgent (skip Appium)

Run WDA standalone and speak its HTTP protocol directly from adet.

**Pros**: fewer moving pieces, lower latency, no Appium server process.
**Cons**: we have to handle WDA signing, device provisioning, and Xcode version drift ourselves — all things Appium currently absorbs. Breaking changes in WDA land without warning.
**Unknowns**: whether adet maintainers want to take on that ongoing maintenance burden.

### Option C — `idb` (Facebook / Meta) or similar private toolchain

A lower-level tool that talks to iOS simulators and devices via private channels.

**Pros**: simpler protocol than WebDriver, no XCTest overhead.
**Cons**: largely unmaintained, limited selector support, breaks on every iOS release, relies on private Apple APIs, cannot do some things adet needs (e.g. real accessibility tree dumps).
**Unknowns**: whether a modern fork exists that's worth depending on.

### Option D — Custom XCTest runner, no Appium/WDA

Write our own XCTest-based runner, ship an Xcode project that users build and sign.

**Pros**: full control, smallest surface area, most correct.
**Cons**: we're essentially rebuilding WebDriverAgent. Months of work before anything moves. Users must install Xcode and sign our test target.
**Unknowns**: whether this pays off for an open-source project or if the maintenance cost dwarfs any gains.

### Option E — Wrap an existing OSS iOS driver from another framework

E.g. Maestro's iOS driver, Detox's XCUITest layer, KIF, Wire.

**Pros**: someone else already solved parts of the problem.
**Cons**: inherits their architectural assumptions. Many are tied to a specific test harness and can't be extracted cleanly.
**Unknowns**: licensing, whether any are genuinely extractable as libraries vs tightly coupled frameworks.

### Option F — Use an Accessibility Inspector approach

Drive iOS via the Accessibility Inspector protocol (the same one Xcode's A11y Inspector uses).

**Pros**: first-class accessibility tree access, native Apple-blessed channel.
**Cons**: undocumented for 3rd-party use, requires Mac app entitlement, unclear if it supports gestures or only inspection.
**Unknowns**: essentially everything — this is research-level.

---

## Open questions the choice depends on

Before picking an option, we need to answer:

1. **Target user**: do we optimize for developers with Xcode installed (Option A/B/D), or for CI-style usage where the user has Xcode anyway?
2. **Physical devices vs simulators**: is simulator-first acceptable for v1, or do we need physical device support immediately? Signing requirements are very different.
3. **Latency budget**: Android adet does tap round-trip in ~30ms. What's our tolerated latency for iOS? Appium typically adds 100-300ms per command.
4. **Session model**: iOS automation is session-oriented (create a WebDriver session, reuse it). Android adet is stateless per call. Are we OK with the abstraction leak?
5. **Selector mapping**: Android uses `AccessibilityNodeInfo.viewIdResourceName`, `text`, `contentDescription`. iOS uses `name`, `label`, `value`, `identifier`. Can we normalize these losslessly at the adapter layer, or will specs need per-platform hints?
6. **Maintenance cost**: who maintains the iOS bridge when Apple ships an iOS that breaks it? adet is open source; if no maintainer exists, the bridge rots.
7. **Install UX**: Android adet is "install APK, toggle a setting, done". What's the equivalent minimum UX for iOS? A `brew install` + `xcode-select` dance is already a lot.

---

## What's already in place

The TypeScript side is **platform-agnostic** and ready to accept an iOS adapter:

- `src/adapters/device-controller.port.ts` defines `DeviceController` (Inspector + Actor + AppManager + Lifecycle). Every iOS adapter candidate implements this interface.
- `src/adapters/device-router.ts` routes `select_device` by platform — iOS is an empty branch today.
- `src/adapters/engine-appium.adapter.ts` is a **naming-only placeholder**. The file name mentions Appium because it was the first guess; it should be **renamed** once an approach is chosen. All methods throw `not implemented`. Treat it as a stub interface, not as a commitment to Appium.

Nothing in `src/runner/`, `src/explorer/`, `src/tools/`, or `src/storage/` knows about platforms. They all talk to `DeviceController`.

## What's missing

Everything else:

- Chosen approach (see options above)
- The adapter implementation
- Device enumeration for iOS (Android uses `adb devices`; iOS candidates: `xcrun xctrace list devices`, `idevice_id`, Appium's device list endpoint — depends on approach)
- Installation and first-run UX for developers
- Smoke tests
- Updated selector mapping docs (platform-neutral selector → platform-specific query)
- CI configuration if we want iOS support in automated testing

---

## How to propose an approach

1. Read this doc fully
2. Prototype your preferred option for 2-3 days against a single spec: launch Settings, open Wi-Fi, tap the first network, assert the password prompt appears
3. Measure latency per tool call, session setup time, and failure recovery
4. Open a discussion issue with: the option, prototype results, and a short write-up of tradeoffs
5. If accepted, replace this document with a real implementation plan

**Do not** start implementing without a prototype + discussion. The cost of picking wrong is a multi-week rewrite, and we don't want adet to grow a half-broken iOS path that nobody trusts.

---

## Prior art to read

- [Appium architecture docs](https://appium.io/docs/en/2.0/intro/)
- [WebDriverAgent repo](https://github.com/appium/WebDriverAgent)
- [Maestro iOS driver source](https://github.com/mobile-dev-inc/Maestro)
- Apple's Accessibility Inspector documentation (sparse, check WWDC sessions)
- Any commercial tool's public architecture docs that cover iOS automation

Read for design insight, not to copy code. adet's license is Apache 2.0; respect others' licenses when researching.

---

## TODO checklist for the iOS implementer

When the iOS bridge approach is chosen and you start implementing, work through these in order. Each item is cross-referenced to a `TODO(ios)` marker in the source.

### 1. Connect the adapter (blocking)

- [ ] Implement `IosXctestController` in `src/adapters/ios-xctest.adapter.ts`. Replace every `nope(...)` with real calls through the chosen bridge.
- [ ] `currentForeground()` returns `{ appId: bundleId, screen: viewControllerName? }`. The port is already platform-neutral — do not add iOS-specific field names.
- [ ] `launchApp(appId)` / `forceStopApp(appId)` — `appId` is the bundle id. Translate to whatever the bridge expects.
- [ ] `resolveSelector(selector)` — implement the field mapping per the table in the `IosXctestController` class docstring:
  - `resourceId` → `accessibilityIdentifier`
  - `contentDesc` → `accessibilityLabel`
  - `text` → `value` (StaticText) or `label` (Button)
  - `textContains` → NSPredicate `label CONTAINS[cd] "X"`
  - `hint` → fuzzy any-field
  - `predicate` → raw NSPredicate string, passed through
  - `classChain` → raw XCUITest class chain, passed through

### 2. Platform-neutral rebind hint (non-blocking)

- [ ] `src/tools/preflight.ts` — the `REBIND_HINTS.ios` entry currently points at this doc. Replace with real recovery commands (e.g. "restart WDA via `xcrun simctl spawn ...`" or the equivalent for your bridge).

### 3. `pressKey("back")` semantics (non-blocking)

- [ ] See `TODO(ios)` in `src/adapters/device-controller.port.ts` next to `pressKey`. iOS has no hardware back. Decide: throw a clear "use swipe instead" error, OR implement as a swipe-from-left-edge gesture internally. Option B is friendlier to existing test specs.

### 4. `Selector<P extends Platform>` discriminated union (optional)

- [ ] See `TODO(ios)` above the `Selector` interface in `src/adapters/device-controller.port.ts`. Today `predicate` / `classChain` are additive fields that Android silently ignores. A discriminated union would gate them to the iOS code path at the type level. Nice-to-have, not required.

### 5. Explicit `SelectorAdapter<P>` class (optional, recommended)

- [ ] See `TODO(ios)` in `src/adapters/ios-xctest.adapter.ts`. When iOS has non-trivial per-field mappings (e.g. `text` → `value` vs `label` depending on element kind), an explicit `IosSelectorAdapter` class with unit tests is cleaner than inlining the logic in each `resolveSelector` / `inputText` / `tap` call. Add the class under `src/adapters/` or `src/tools/core/`, depending on where you want the boundary.

### 6. Rename `ios-xctest.adapter.ts` if the bridge isn't XCTest-based (optional)

- [ ] The filename assumes XCTest. If you chose `idb` / `simctl-only` / `devicectl`, rename to reflect the reality. Keep the `IosXctestController` class name in sync.
