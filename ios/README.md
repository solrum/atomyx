# adet — iOS control plane

> 🟡 **Status**: TODO. The bridge approach has **not been decided**. See [../docs/ios.md](../docs/ios.md) for the open design questions.

## Why this directory is empty (today)

iOS automation cannot run an embedded HTTP server inside a 3rd-party app (sandboxing + background execution limits). Whatever the iOS control plane looks like, it will be **host-side**, not on-device.

**We have not yet chosen how** the host-side bridge will work. Several candidate approaches exist (Appium + WebDriverAgent, direct WDA, custom XCTest runner, `idb`, Accessibility Inspector, etc.) — each with unresolved tradeoffs. See `../docs/ios.md` for the full comparison.

Until a choice is made and prototyped, this directory is a placeholder.

## What would go here — depends on the choice

If we end up with a Mac-native helper app, an Xcode project might live here. If we use Appium, this directory stays empty and the only iOS code is the TypeScript adapter in `../src/adapters/`. If we use a custom XCTest runner, an Xcode test target project belongs here. The decision shapes the layout.

## Want to help decide iOS support?

Start with [../docs/ios.md](../docs/ios.md). The document lists candidate approaches and the open questions that need answers before coding begins.

**Do not** start implementing a specific approach without a prototype + discussion in an issue first — the cost of picking wrong is a multi-week rewrite.

## What's already ready on the host side

The `DeviceController` interface in `../src/adapters/device-controller.port.ts` is platform-agnostic. Whatever iOS approach wins, the adapter implementation plugs into the existing tool layer without touching `../src/runner/`, `../src/explorer/`, or `../src/tools/`.

There is a stub adapter at `../src/adapters/engine-appium.adapter.ts` — the filename is historical, **not** a commitment to Appium. It should be renamed once the real approach is chosen.
