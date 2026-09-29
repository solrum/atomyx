---
name: atomyx-test-loop
description: Run mobile app tests reliably with Atomyx, meaning orient before acting, act with selectors first and coordinates as fallback, then verify after every navigation. Use whenever the user asks to test, verify, check, drive, smoke-test, or interact with a mobile app on iOS or Android.
---

# Atomyx test loop

You drive a real mobile device through the Atomyx MCP tool surface. Every interaction follows the same loop.

## The loop

1. **Orient**: call `get_ui_tree` before deciding what to tap. Never tap blind. Each element reports its `bounds` as a "left,top,right,bottom" string, and when no stable id exists, call `find_element` or `wait_for_element` and use the `center` point it returns as the canonical fallback.
2. **Act**: `tap`, `input_text`, `swipe`, `press_key`. Pass a `selector` (id, text, label, hint, or value) OR `{x, y}` coordinates taken from `find_element`, `wait_for_element`, or a bounds midpoint.
3. **Verify**: `wait_for_element` to confirm the navigation happened. `screenshot` to capture visual evidence when reporting bugs.

## Session bootstrap

Once per session:

1. `list_devices`: see what's available.
2. `select_device`: bind to one.
3. `launch_app` with the bundle id (iOS) or package name (Android).

If `select_device` fails, surface the failure to the user and stop. Do not retry, because the connection problem is upstream of the tool surface.

## Selector strategy

The server tries selector fields in priority order regardless of which ones were passed:

`id`, then `label`, then `text` (a `text` query without a `label` also tries `label`), then `value`, then `hint`

`role`, `enabled`, `clickable`, and `focused` are constraints applied on top of whichever content field matches, not part of the priority chain. Prefer the most specific (`id`) when it exists. Fall back to `text` for human-visible labels, and use a `{regex, flags}` object on any field for partial or case-insensitive matching, for example `{"text": {"regex": "Login", "flags": "i"}}`. Use coordinates only when no stable selector exists, and always source coordinates from a fresh `get_ui_tree`, `find_element`, or `wait_for_element` call, never hard-coded values from a previous run.

## Platform abstraction

The selector schema is the same on every platform: no field is iOS-only or Android-only. Do not branch on platform in your reasoning, because the driver adapter handles any platform difference underneath the same selector shape.

## Re-orient after navigation

After any action that changes the screen (modal opens, screen transition, list reload), the previous UI tree is stale. Call `get_ui_tree` again before the next action. Acting on a stale tree is the single most common cause of test flakiness.

## What NOT to do

- Never tap a coordinate taken from a previous `get_ui_tree` after a screen transition.
- Never retry the same tool with the same arguments after a failure. Re-orient or escalate per the `atomyx-debug-failure` skill instead.
- Never report a bug from a single failure. Verify with `screenshot` and at least one re-attempt at the same step.

## Path reuse

When a multi-step flow (login, deep navigation, state setup) completes successfully, those steps are reusable. See `atomyx-script-authoring` for capturing the flow as a YAML script so the next test session starts from that screen instantly without re-discovering the path.
