# Maestro command parity

## Purpose

Who reads this: a QA engineer or an AI agent moving a Maestro flow to Atomyx, and a contributor deciding which YAML step to build next.

When to read it: before translating a Maestro flow, and before adding a step that Maestro already has.

What this does not cover: the syntax of each Atomyx step (see [`yml-script-reference.md`](./yml-script-reference.md)) or the MCP tool surface (see [`mcp-tools.md`](./mcp-tools.md)).

## TL;DR

Each row names one Maestro command, the Atomyx way to do it, and a status:

- Present: a YAML step does it today.
- Partial: the engine or a combination of steps does part of it, but no single step matches.
- Missing: nothing in Atomyx does it yet. Work to add it is planned.
- Out of scope: Atomyx does not plan to add it, and the reason is in the row.

A Maestro flow that uses only present commands runs as is after a syntax change. A flow that uses a partial or missing command needs the workaround in the row, or waits for the command to land.

## Core commands

| Maestro command | Atomyx equivalent | Status | Note |
| --- | --- | --- | --- |
| `launchApp` | `launchApp` | Partial | The step takes no parameters. Launch mode and data clearing come from the project launch policy. |
| `launchApp` with arguments | None | Missing | The driver port carries arguments, but neither real adapter delivers them. |
| `launchApp` with permissions | None | Missing | Needs a permission capability on the driver. |
| `tapOn` | `tap` | Present | Selector fields are literal strings, or `{regex, flags}` for a pattern. |
| `inputText` | `type` | Present | Clears the field first by default and interpolates `${var}`. |
| `assertVisible` | `assertVisible` | Present | Accepts `timeout` to poll. |
| `assertNotVisible` | `assertNotVisible` | Present | Accepts `timeout` to wait for disappearance. |
| `assertTrue` | `assertExpr` | Present | |
| `extendedWaitUntil` | `waitFor`, or `assertVisible` and `assertNotVisible` with `timeout` | Present | |
| `takeScreenshot` | `screenshot` | Present | |
| `pressKey` | `pressKey` | Present | Covers back, home, enter, tab, escape, delete, space and the arrows. |
| `back` | `back` | Present | |
| `swipe` | `swipe`, or `pointer` for coordinates | Present | |
| `doubleTapOn` | `pointer` | Partial | Expressed as two taps in one gesture. No dedicated step. |
| `longPressOn` | `longPress` | Present | |
| `runFlow` | `runFlow` | Present | Conditions go through `handle` and `branch`. |

## Control flow

| Maestro command | Atomyx equivalent | Status | Note |
| --- | --- | --- | --- |
| `repeat` with `times` or `while` | `repeat` | Present | `while` takes `visible`, `notVisible` or `expr`. A loop with `while` and no `times` fails after 50 iterations. |
| `retry` | None | Missing | Retries exist only per suite entry. |
| Regex in selectors | `text`, `id`, `label` and `hint` as `{regex, flags}` | Present | A plain string stays an exact match. A regex match is unanchored, so write `^` and `$` to match a whole value. |

## App and device state

| Maestro command | Atomyx equivalent | Status | Note |
| --- | --- | --- | --- |
| `clearState` | None as a step | Partial | The driver clears app data and the launch policy can request it. No step exposes it. |
| `stopApp` | None as a step | Partial | The engine stops an app. No step exposes it. |
| `killApp` | None as a step | Partial | The engine kills an app. No step exposes it. |
| `openLink` | None | Missing | Needs a link capability on the driver. |
| `setLocation` | None | Missing | The capability flag exists with no method behind it. |
| `travel` | None | Missing | Depends on `setLocation`. |
| `setOrientation` | None | Missing | The capability flag exists with no method behind it. |
| `hideKeyboard` | None as a step | Partial | The engine hides the keyboard. No step exposes it. |
| `eraseText` | `type` | Partial | `type` clears the field first. No standalone erase step. |
| `scrollUntilVisible` | `tap` and `type` | Partial | Both scroll the target into view on their own. No standalone step. |
| `waitForAnimationToEnd` | None as a step | Partial | The engine waits for a screen transition. No step exposes it. |

## Text and clipboard

| Maestro command | Atomyx equivalent | Status | Note |
| --- | --- | --- | --- |
| `copyTextFrom` | `read` into a variable | Partial | Binds an element field to a variable. |
| `pasteText` | `type` with `${var}` | Partial | Types the stored value. The device clipboard is not used. |
| Device clipboard read and write | `read` then `type` with `${var}` | Out of scope | Android 10 and later blocks background clipboard reads, and iOS prompts on paste, so a device clipboard path is unreliable on both. |
| `inputRandom` variants | None | Missing | The runner has no value generator. |

## Scripting and media

| Maestro command | Atomyx equivalent | Status | Note |
| --- | --- | --- | --- |
| `evalScript` and `runScript` | `assertExpr` for boolean checks | Out of scope | Arbitrary JavaScript breaks deterministic runs and widens the attack surface. |
| AI assertions | MCP tools | Out of scope | An AI agent drives Atomyx through the MCP server, so the assertion belongs to the agent. |
| `addMedia` | None | Out of scope | Revisit when real flows show demand. |
| Airplane mode | None | Out of scope | No portable control on a non-rooted device. |
| `clearKeychain` | None | Out of scope | No public API to clear the keychain from a test runner. |
| Permission grants on a physical iOS device | None | Out of scope | iOS offers no public API to grant a permission from a test runner. |
| Volume, power and lock keys | None | Missing | `pressKey` does not cover these keys. |

## References

- [`yml-script-reference.md`](./yml-script-reference.md) documents every present step.
- [`mcp-tools.md`](./mcp-tools.md) documents the tool surface an agent uses instead of scripting.
