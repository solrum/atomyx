import { definePrompt } from "./prompt-definition.js";

/**
 * `atomyx/playbook` — static tool-selection reference.
 *
 * Not a methodology per se, but a "cheat sheet" that clients
 * can inject when the agent seems uncertain about which tool
 * to call next, or when you're debugging tool selection
 * behavior. Also useful as an always-loaded preamble for
 * clients that want to give the agent a stable mental model
 * of the Atomyx surface without choosing between exploratory
 * and regression modes.
 *
 * Unlike the other prompts, this has no arguments — it's a
 * pure reference page.
 */
const TEMPLATE = `# Atomyx tool playbook

Decision tree for picking the right tool at each step of a
mobile testing session. Keep this loaded when running any
Atomyx flow.

## Orientation — before every action

\`\`\`
get_ui_tree({limit: 30})          → see current screen elements
screenshot()                       → visual state (for complex UIs
                                       or bug evidence)
\`\`\`

Call \`get_ui_tree\` at the start of each new screen. Don't act
blind.

## Find an element

\`\`\`
find_element({ id: "login_btn" })           → by stable id (best)
find_element({ text: "Sign in" })           → by visible text
find_element({ label: "Login button" })     → by accessibility label
find_element({ hint: "Email address" })     → by input placeholder
find_element({ role: "button", text: "OK" }) → compound filter
\`\`\`

Selector priority (when multiple fields given): id > label >
text > value > hint. Atomyx tries them in order and returns the
first non-empty match.

## Perform an action

### Tap

\`\`\`
tap({ selector: { text: "Login" } })        → find + tap
tap({ x: 215, y: 430 })                      → raw coordinates
\`\`\`

Prefer selectors. The framework handles scroll-into-view,
obscurement detection, and retry automatically. Coordinates are
for when you know EXACTLY where to tap (from a previous
\`find_element\` or screenshot inspection).

### Type

\`\`\`
input_text({ selector: { hint: "Email" }, text: "user@test.com" })
input_text({ selector: { id: "pwd" }, text: "secret", clearFirst: false })
input_text({ x: 200, y: 300, text: "..." })   → coord-based
\`\`\`

\`clearFirst\` defaults to true (erase existing content). Pass
\`false\` to append.

### Swipe

\`\`\`
swipe({ direction: "up" })                           → scroll down
swipe({ direction: "down" })                         → scroll up
swipe({ fromX: 10, fromY: 500, toX: 300, toY: 500 }) → horizontal drag
\`\`\`

### Keys

\`\`\`
press_key({ key: "back" })      → Android system back (iOS may fail)
press_key({ key: "enter" })     → submit / confirm
press_key({ key: "home" })      → home screen
\`\`\`

## Waiting / verification

\`\`\`
wait_for_element({ selector: { text: "Welcome" }, timeoutMs: 5000 })
\`\`\`

Call after every navigation tap. Timeout 5000ms for screens,
2000ms for same-screen changes. Returns \`found: true\` on
success, \`found: false\` on timeout.

## Error handling

Every action tool returns an ActionResult:

\`\`\`
{ ok: true, resolvedBy: "id", detail: "..." }       → success
{ ok: false, reason: "element not found ..." }       → no match
{ ok: false, reason: "...obscured...", obscurer: { role, id, label } }
                                                     → element covered
\`\`\`

### When \`ok: false\` with \`obscurer\`

1. Look at the obscurer's role / id / label. It's usually a
   modal, sheet, alert, or floating button.
2. Dismiss it: \`find_element\` on the obscurer, then \`tap\` it.
   (Dialogs typically have Cancel / Close / OK that dismisses.)
3. Retry the original action.

### When \`ok: false\` with "element not found"

The selector didn't resolve even after scroll-search. Options:

1. Call \`get_ui_tree\` to see what's actually on screen.
2. Try a different selector field (if you used \`text\`, try
   \`label\` or \`id\`).
3. Check if you're on the right screen (maybe a previous
   navigation failed silently).

### iOS \`press_key("back")\` returning \`ok: false\`

iOS has no system back. The response includes a hint: find an
on-screen Cancel / Close / Done button via \`find_element\` +
\`tap\` it instead.

## App lifecycle

\`\`\`
launch_app({ appId: "com.example.app" })
\`\`\`

Launches by bundle id (iOS) or package name (Android). This
is the first action in most sessions.

## Screenshot

\`\`\`
screenshot()   → { base64, format: "png", sizeBytes }
\`\`\`

Take before reporting any bug. Also useful when the UI tree is
ambiguous (Flutter / custom-rendered content).

## Golden rules

1. **Orient before acting.** \`get_ui_tree\` every new screen.
2. **Prefer selectors over coordinates.** The framework handles
   layout edge cases for you.
3. **Verify every navigation.** \`wait_for_element\` catches
   broken transitions early.
4. **Read the ActionResult.** Don't assume success.
5. **On \`obscurer\`, dismiss before retrying.** Don't fight the
   modal.
`;

export const playbookPrompt = definePrompt({
  name: "atomyx/playbook",
  description:
    "Static tool-selection reference. Inject this when the agent " +
    "seems uncertain about which tool to call next, or as a general " +
    "always-loaded preamble. No arguments.",
  arguments: [],
  render() {
    return [
      {
        role: "user",
        content: {
          type: "text",
          text: TEMPLATE,
        },
      },
    ];
  },
});
