---
name: atomyx-test-loop
description: Run mobile app tests reliably with Atomyx — orient before acting, act with selectors first and coordinates as fallback, verify after every navigation. Use whenever the user asks to test, verify, check, drive, smoke-test, or interact with a mobile app on iOS or Android.
---

# Atomyx test loop

You drive a real mobile device through the Atomyx MCP tool surface. Every interaction follows the same loop.

## The loop

1. **Orient** — call `get_ui_tree` before deciding what to tap. Never tap blind. Tree elements include inline coordinates as `@cx,cy`; those are the canonical fallback when no stable id exists.
2. **Act** — `tap`, `input_text`, `swipe`, `press_key`. Pass a `Selector` (resourceId / contentDesc / text / textContains / hint) OR `{x, y}` coordinates taken from the tree.
3. **Verify** — `wait_for_element` to confirm the navigation happened. `screenshot` to capture visual evidence when reporting bugs.

## Session bootstrap

Once per session:

1. `list_devices` — see what's available.
2. `select_device` — bind to one.
3. `launch_app` with the bundle id (iOS) or package name (Android).

If `select_device` fails, surface the failure to the user and stop. Do not retry — the connection problem is upstream of the tool surface.

## Selector strategy

The driver tries selectors in priority order regardless of which type was passed:

`resourceId` → `contentDesc` → `text` → `textContains` → `hint`

Prefer the most specific (`resourceId`) when it exists. Fall back to `text` / `textContains` for human-visible labels. Use coordinates only when no stable selector exists, and always source coordinates from a fresh `get_ui_tree`, never hard-coded values from a previous run.

## Platform abstraction

Selector fields specific to iOS (`predicate`, `classChain`) are additive; Android ignores them silently. Do not branch on platform in your reasoning — the driver adapter handles the difference.

## Re-orient after navigation

After any action that changes the screen (modal opens, screen transition, list reload), the previous UI tree is stale. Call `get_ui_tree` again before the next action. Acting on a stale tree is the single most common cause of test flakiness.

## What NOT to do

- Never tap a coordinate taken from a previous `get_ui_tree` after a screen transition.
- Never retry the same tool with the same arguments after a failure — re-orient or escalate per the `atomyx-debug-failure` skill.
- Never report a bug from a single failure — verify with `screenshot` and at least one re-attempt at the same step.

## Path reuse

When a multi-step flow (login, deep navigation, state setup) completes successfully, those steps are reusable. See `atomyx-script-authoring` for capturing the flow as a YAML script so the next test session starts from that screen instantly without re-discovering the path.
