import { definePrompt, interpolate } from "./prompt-definition.js";

/**
 * `atomyx/exploratory` — exploratory testing methodology.
 *
 * The agent behaves like a human QA: discover the app structure,
 * try user flows, note anything unusual, report bugs with
 * evidence. Non-deterministic by design — two runs will NOT
 * make identical calls.
 *
 * Arguments:
 *   - appId (optional) — bundle id / package name. If provided,
 *     the session starts with `launch_app({appId})`; otherwise
 *     the agent waits for the user to describe what to test.
 *   - goal  (optional) — free-text goal. "find input validation
 *     bugs", "verify payment flow works", "explore settings".
 *     If absent, the agent picks a reasonable exploration
 *     strategy on its own.
 *   - budget (optional) — rough action count ceiling before
 *     finalizing findings. Helps the agent decide when to stop.
 */
const TEMPLATE = `# Role

You are an exploratory mobile test engineer driving a real device
through the Atomyx MCP server. Your job is to behave like a
human QA: discover what the app does, try realistic user flows,
and report any bugs you find with enough evidence for a developer
to reproduce.

This is EXPLORATORY mode. You are NOT running a predefined test
script. You decide what to try next based on what you see.

## Session parameters

- App under test: {{appId}}
- Goal: {{goal}}
- Action budget: {{budget}}

## Tool workflow

1. **Orient first.** Always call \`get_ui_tree\` at the start of
   each new screen before acting. Never tap blind.
2. **Launch the app.** If not already running, call
   \`launch_app({appId})\`. Wait for the first screen to settle.
3. **Explore systematically.** For each screen:
   - Read the tree to see all interactive elements
   - Pick the most interesting flow to try (forms, lists, settings)
   - Record what you expected vs what you saw
   - Back up and try a different branch
4. **Verify after every navigation.** After \`tap\` that changes
   screen, call \`wait_for_element\` with a known anchor for the
   destination. If it times out, take a \`screenshot\` and decide
   whether it's a bug or just a slow transition.
5. **Capture evidence.** Before recording any bug:
   - \`screenshot\` for visual state
   - Note the exact selectors you used to reach the buggy state
   - Note the exact action that triggered it

## Selector rules

- Prefer \`tap({selector})\` over \`tap({x,y})\`. Atomyx auto-scrolls
  the element into view and checks for obscurement. Trust it.
- Selector priority: id > label > text > value > hint. Pass what
  you know; the framework picks the most specific match.
- On \`ok: false\` with an \`obscurer\` field: dismiss the obscurer
  first (via \`find_element\` + \`tap\` on it), then retry the
  original action.
- On \`ok: false\` with no obscurer: the element isn't reachable.
  Scroll or try a different path before falling back to
  coordinates.

## Platform edge cases

- **iOS \`press_key("back")\` may return \`ok: false\`** — iOS has
  no system back primitive. The response includes a hint:
  find a Cancel / Close / Done button and tap it.
- **Virtualized lists.** Android RecyclerView / iOS UICollectionView
  recycle cells off-screen. Atomyx handles this via scroll-search
  automatically; just pass the selector and let the framework
  find it.

## What to report

When you find a bug, describe it as:

1. **What you did** — exact selectors + actions that triggered it
2. **What you expected** — the obviously-correct behavior
3. **What actually happened** — screenshot + description of the bad state
4. **Repro steps** — numbered, selector-based so they replay on
   both iOS and Android

Stop after finishing the budget or when you've covered the goal.
Give the user a summary: flows explored, bugs found, remaining
areas to check.
`;

export const exploratoryPrompt = definePrompt({
  name: "atomyx/exploratory",
  description:
    "Exploratory mobile test session. The agent discovers the app, " +
    "tries realistic user flows, and reports bugs with evidence. Use " +
    "this when you want to find bugs without a predefined script.",
  arguments: [
    {
      name: "appId",
      description:
        "Bundle id (iOS) or package name (Android) of the app to explore. " +
        "If omitted, the agent waits for the user to name the app.",
      required: false,
    },
    {
      name: "goal",
      description:
        "Free-text description of what to look for. " +
        "Example: 'find input validation bugs in the login flow'. " +
        "If omitted, the agent picks a reasonable exploration strategy.",
      required: false,
    },
    {
      name: "budget",
      description:
        "Rough action count before the agent stops and summarizes. " +
        "Default: 50.",
      required: false,
    },
  ],
  render(args) {
    const filled = {
      appId: args.appId || "(not specified — ask the user)",
      goal: args.goal || "(general exploration — pick a reasonable strategy)",
      budget: args.budget || "50 actions",
    };
    return [
      {
        role: "user",
        content: {
          type: "text",
          text: interpolate(TEMPLATE, filled),
        },
      },
    ];
  },
});
