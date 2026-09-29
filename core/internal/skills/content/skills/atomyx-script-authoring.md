---
name: atomyx-script-authoring
description: Author and reuse Atomyx YAML test scripts, meaning capture a successful exploration flow as a replayable script, so the next test session resumes from a known screen instead of re-discovering the path. Auto-trigger when the user asks to write, save, replay, or run a YAML script, or when an exploration flow successfully reaches a target screen worth memorizing for future tests.
---

# Atomyx script authoring

Atomyx YAML scripts are deterministic, agent-free recipes for driving an app. Two purposes:

1. **Path memoization**: once a flow reaches a target screen (logged in, navigated to settings, opened a specific record), save the flow. The next test starts from that screen without re-discovering it.
2. **Regression suites**: codify known-good flows so they run identically every time.

## Where scripts live

Convention for a consumer project:

```
.atomyx/scripts/
├── login.yml              reaches logged-in home from cold launch
├── open-settings.yml      reaches settings screen from home
└── add-record.yml         creates a test record from home
```

Compose by running them in sequence: `login.yml` then `open-settings.yml` to reach settings as a logged-in user.

## When to save a script

After a successful flow, ask: would another test need this same starting state? If yes:

1. List the minimal steps that reproduce the state. Drop `get_ui_tree` polling and keep only the tools that drove navigation.
2. For each step, prefer the most stable selector field observed (`id` over `label` over `text` over `hint`), and use a regex pattern such as `{regex: "Login", flags: "i"}` on a field when only a partial or case-insensitive match is stable.
3. Save the YAML under `.atomyx/scripts/<intent>.yml` where `<intent>` names what the script reaches (the target state), not how (the action sequence).

## Running a script

Use `run_script` with the YAML content. The script runs without an LLM, using pure deterministic dispatch. On step failure, the runner reports which step failed and why. The calling agent then decides whether to heal (re-discover the selector) or surface the bug.

## Self-healing on replay

When a saved script fails on replay because a selector no longer matches (the app changed):

1. Run the script with `run_script`. Note which step failed.
2. From the previous successful step's screen, follow the orient/act loop in `atomyx-test-loop` to re-discover the correct selector.
3. Update the YAML with the new selector. Save the updated script.

Re-record only the broken step. Re-recording the whole script throws away the stability work that the rest of the script earned.

## What NOT to put in a script

- No `get_ui_tree` calls. The runner does not need them, and they bloat the script with stale snapshots.
- No `screenshot` calls unless the script is meant as evidence; replay-for-state-setup scripts skip screenshots.
- No hard-coded coordinates. They break across device sizes. If a stable selector does not exist, the script is fragile, so ask the app team to add a stable `id` (a resource id on Android, an accessibility identifier on iOS) instead.
