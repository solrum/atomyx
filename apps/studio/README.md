# @atomyx/studio

**Status: parked developer preview.** Feature work on Studio is
paused. The app still builds and runs on demand, so a contributor
can still use it locally or cut a signed release when needed. The
active surfaces of Atomyx are the core runtime, the CLI, and the
MCP server.

Desktop IDE for authoring, validating, and running Atomyx YAML test
scripts on real mobile devices. Currently macOS only.

## Known limitations

- **Run controls**: Pause / Resume / Step Back / Step Forward are
  not supported. A run is one Run, one Stop, one Replay.
- **Run-bar artifacts**: `screenshot` script steps capture bytes
  on the device but the result is not surfaced in the
  run-history view. Use the inspector's manual capture button to
  grab screenshots interactively.
- **Mirror recording**: Every backend (scrcpy, simctl) returns
  `supportsRecording: false`, so the mirror toolbar does not show
  a Record button. The internal recording API stays in the code
  for a future backend that supports it.
- **iOS real-device mirror**: only iOS Simulator and Android
  device mirror are wired. Mirroring a physical iPhone over
  CoreMediaIO is not supported in this build.
- **Android mirror input**: tap, long-press, swipe, and sending
  text to the focused field (via the toolbar insert box) work.
  Two-finger pinch is unavailable because the accessibility service has
  no simultaneous-pointer gesture. Backward delete clears the whole
  focused field rather than one character, so the live
  keystroke-streaming path is offered on iOS Simulator only;
  Android uses the compose-then-commit insert box instead.
- **Bugs / Chat panes**: out of scope for this release. Neither
  panel ships.
- **Settings dialog**: only Inspector auto-refresh and the global
  font / save toggles are user-customisable. Theme tokens still
  ship via `themes/*.json` files; the colour-pickers in the
  preferences pane were removed.
- **Updater**: the plugin is wired but inactive until the project
  owner generates and configures the signing key (see "Wiring the
  in-app updater" below).
- **Distribution**: macOS only. Windows / Linux Tauri builds are
  not configured.

## Quick start

```bash
cd apps/studio
npm install
npm run tauri:dev      # native window, full Tauri backend
# or
npm run dev            # renderer only in the browser (no MCP / no native fs)
```

Prerequisites:

- Node 20 (see `.nvmrc`).
- Rust >= 1.77 and the `tauri-cli` (installed via `npm install`).
- macOS 12+.

## Architecture

Studio is organized in four layers with a strict one-way
dependency rule. **Read
[`.claude/rules/studio-architecture.md`](../../.claude/rules/studio-architecture.md)
before changing anything under `src/`.**

```
src/
├── ui/         React components, styling, Monaco wiring
├── state/      Zustand stores (UI state only)
├── domain/     Pure TS: ports, contracts, validators
└── platform/   Tauri adapters implementing domain ports
```

Import direction: `ui` then `state` then `domain` ← `platform`. Circular
imports and backwards imports are rejected by
`dependency-cruiser` and ESLint.

The only file allowed to import from all four layers is
`src/main.tsx` is the composition root.

## Public contracts

These surfaces carry semver-like discipline, meaning removing a field,
renaming a method, or tightening a schema is a breaking change
that needs an ADR before it lands.

1. Domain ports live at `src/domain/**/*.port.ts`.
2. Artifact-store on-disk folder layout under
   `~/Library/Application Support/dev.atomyx.studio/runs/<id>/`.
3. Zod schemas from `@atomyx/shared/script`, which is the script format
   itself.

## Scripts

| Command | What it does |
|---|---|
| `npm run dev` | Vite dev server (renderer only). |
| `npm run tauri:dev` | Native Tauri window with Rust backend. |
| `npm run build` | TypeScript project build + Vite build. |
| `npm run tauri:build` | Build native app bundle (macOS DMG). |
| `npm run typecheck` | Composite-project type check, no emit. |
| `npm run lint` | ESLint, including layer `no-restricted-imports`. |
| `npm run depcruise` | Enforce one-way layer imports. |
| `npm run test` | All tests (domain + state). |
| `npm run test:domain` | Domain-only tests (must pass under `node:test` with no DOM). |

## Folder picker, artifact store, settings

All three are delegated to the Tauri Rust backend via the
`platform/` adapters. Default locations (macOS):

<!-- atomyx-allow-path: ~/Library/Application Support/dev.atomyx.studio/config.json -->
- Artifact store: `~/Library/Application Support/dev.atomyx.studio/runs/`.
- Settings: `~/Library/Application Support/dev.atomyx.studio/config.json`.

## Building a signed release

The release bundle is a hardened, notarized DMG. Three secrets need
to be in the environment before `npm run tauri:build` runs:

| Variable | Value | Where it comes from |
|---|---|---|
| `APPLE_SIGNING_IDENTITY` | `Developer ID Application: <your name> (<TEAM_ID>)` | `security find-identity -v -p codesigning` |
| `APPLE_ID` | Apple ID email | The account you signed up to the Apple Developer Program with. |
| `APPLE_PASSWORD` | App-specific password | <https://appleid.apple.com>, then Sign-In and Security, then App-Specific Passwords. |
| `APPLE_TEAM_ID` | Team identifier (10 chars) | The string in parentheses inside `APPLE_SIGNING_IDENTITY`. |

Then:

```bash
cd apps/studio
npm run tauri:build
# DMG lands at src-tauri/target/release/bundle/dmg/
```

`tauri-cli` runs `codesign --options runtime` against the
entitlements at `src-tauri/entitlements.plist`, then submits the
DMG for notarization via `xcrun notarytool` and staples the
result. Without the env vars Tauri falls back to ad-hoc signing
and skips notarization, which is fine for local smoke testing, not for
distribution.

## Wiring the in-app updater

The updater plugin loads a signed `latest.json` manifest from GitHub
Releases (`https://github.com/solrum/atomyx/releases/latest/download/latest.json`)
and verifies it against the public key embedded in the bundle. The
plugin is wired in but disabled out of the box until the project owner
generates the key pair once, then flips the switch.

One-time setup:

```bash
# 1. Generate the key pair. This writes <path>.key (private) and
#    <path>.key.pub (public). Use a passphrase you can stash in a
#    password manager.
npx tauri signer generate -w ~/.tauri/atomyx-studio

# 2. Copy the contents of ~/.tauri/atomyx-studio.key.pub into
#    src-tauri/tauri.conf.json -> plugins.updater.pubkey, and flip
#    plugins.updater.active to true. Commit that change.

# 3. Stash the private key + passphrase as the local env vars
#    Tauri reads at sign time, and keep them in a password manager
#    for the next signed build:
export TAURI_SIGNING_PRIVATE_KEY="$(cat ~/.tauri/atomyx-studio.key)"
export TAURI_SIGNING_PRIVATE_KEY_PASSWORD="<passphrase>"
```

After step 2 the bundle build emits a detached `<dmg>.sig` file
alongside the DMG. Collect the DMG, the signature, and a generated
`latest.json` and upload them to a GitHub Release (see "Cutting a
release" below). The running app polls `endpoints[0]` and prompts
the user to install when a newer version is published.

Rotating the key invalidates every previously installed copy, so they
will refuse to verify the new manifest. Treat the public key as a
ship-once, never-rotate value unless leakage is suspected.

## Cutting a release

There is no automated release pipeline. Cutting a release is a
manual sequence run from a developer machine with the Apple and
Tauri signing env vars from "Building a signed release" above set:

```bash
# Bump the version in apps/studio/src-tauri/tauri.conf.json + package.json
# (matching strings) and commit.
cd apps/studio
npm run tauri:build
# DMG and detached .sig land at src-tauri/target/release/bundle/dmg/
git tag studio-v0.1.0
git push origin studio-v0.1.0
```

Create a GitHub Release for the pushed tag and upload the DMG, the
`.sig` file, and a `latest.json` manifest built from them (see
"Wiring the in-app updater" above for the manifest fields the
running app expects at `endpoints[0]`).
