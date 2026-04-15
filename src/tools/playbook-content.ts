/**
 * Static playbook content — imported by the GetPlaybookTool class. Extracted
 * from the class file so tooling / tests can import the raw markdown
 * without instantiating the tool.
 */
export const PLAYBOOK = `# Atomyx tool playbook — ~18 tools, one choice per action

## Core flow (login example, ~5 calls)

\`\`\`
select_device
launch_app({appId})                                 // returns inputs[] + initialTree
input_text({x: inputs[0].center.x, y: inputs[0].center.y, text: "..."})
input_text({x: inputs[1].center.x, y: inputs[1].center.y, text: "..."})
tap_and_wait_transition({selector: {...}, waitForAbsent: {...}})
\`\`\`

## Tool map

| Intent                                   | Tool                                             |
| ---------------------------------------- | ------------------------------------------------ |
| Pick a device                            | \`list_devices\`, \`select_device\`                   |
| Start an app (clean state)               | \`launch_app\` (forceStop=true default)             |
| See the screen                           | \`get_ui_tree\`                                     |
| Find ONE element                         | \`find_element\`                                    |
| Find MANY elements                       | \`find_element({all: true})\`                       |
| Find an input field by label             | \`find_element({inputField: true, keyword|label})\` |
| Type into a field                        | \`input_text\` (selector OR {x,y})                  |
| Tap something                            | \`tap\` (selector OR {x,y})                         |
| Tap + verify screen transition           | \`tap_and_wait_transition\`                         |
| Wait for an element                      | \`wait_for_element\` (supports absent=true)         |
| Swipe                                    | \`swipe\`                                           |
| System key (back/home)                   | \`press_key\`                                       |
| Capture screenshot                       | \`get_screenshot\`                                  |
| Record test run                          | \`start_run\` / \`finish_run\` / \`report_bug\`          |

## Typing (\`input_text\`)

ONE tool for all typing. Pass either:

- \`{selector, text}\` — selector can point at the LABEL / container / wrapper
  of the field; tool runs a structural strategy chain to find the real
  EditText. Works on native + Flutter + Compose.
- \`{x, y, text}\` — direct coordinates (from \`launch_app\` inputs[] or
  \`find_element({inputField: true})\`).

Auto-clears by default. Handles system IME and custom in-app keypads
(Flutter banking apps). One call.

## Tapping (\`tap\` vs \`tap_and_wait_transition\`)

- \`tap\` — local state change (toggle, tab, checkbox, open menu). Accepts
  \`{selector}\` or \`{x, y}\`.
- \`tap_and_wait_transition\` — REQUIRED for navigation / submit / login /
  network calls. Waits for transition, auto-extends on loading, classifies
  failures (dialog / loading / partial / no-change) with actionable hints.

## When elements have NO resourceId

Coordinates from \`get_ui_tree\` (\`@cx,cy\` at end of each line) are FIRST-CLASS
selectors, not a fallback. They are unambiguous and structurally stable per
render. When an element has no resourceId, prefer:

1. **Coords directly**: \`tap({x: 540, y: 2291})\` or \`input_text({x, y, text})\`.
   Always works, no resolver roundtrip, no localization risk.
2. **\`contentDesc\` / \`text\` ONLY if unique** — check the \`(N×)\` ambiguity
   marker after a selector in the tree. \`(2×)\` means it appears twice; use
   coords instead, or pass \`nth: 0/1\` to disambiguate.
3. **\`find_element({role, nthOfRole})\`** for positional queries like
   "the 4th button" / "the 2nd input" when no stable selector exists.

Example tree line:
\`\`\`
contentDesc="注文" @540,2291 (2×)   ← duplicate, use coords or nth
resourceId="login_btn" @540,1500   ← unique, use selector
\`\`\`

## Android: contentDesc is the primary content selector

On Android, most labels / values / titles are exposed via \`contentDesc\`,
not \`text\`. Material/Compose components consistently set contentDesc; the
\`text\` field is often empty for buttons/icons. When picking a content-based
selector for an Android element, prefer contentDesc.

You don't need to remember this manually — \`tap\` and \`find_element\` auto-
broaden across contentDesc / text / label internally. Pass whichever value
you have; the tool tries contentDesc first.

## Cross-language matching

When a test step says "tap Login" but UI is Japanese/Vietnamese/etc., use
\`find_element({keyword: "login"})\`. Keyword searches
\`resourceId > contentDesc > text\` (case-insensitive substring). resourceId
is usually English in code regardless of UI language.

## Clickable flag is UNRELIABLE on Flutter / Compose / RN

The accessibility \`clickable\` flag is set by Android native widgets but NOT
by Flutter / Compose / React Native — those frameworks dispatch gestures
in-engine via GestureDetector. Tapping a Flutter element with \`clickable=false\`
WILL still dispatch the touch and trigger the handler. Do NOT skip an element
just because clickable is false. The tool layer ignores the flag too.

## NEVER do

- Tap an input field via its visible VALUE (e.g. \`{text: "09044085"}\`).
  Password fields hide it; fresh fields are empty; copy changes.
- Call get_ui_tree repeatedly on an unchanged screen — it's blocked.
  Use \`find_element\` (2s cache) instead.
- Guess English labels for non-English UIs. Use \`find_element({keyword})\`
  or read \`launch_app\`'s \`initialTree\`.

## When you hit an error

Every blocked-tool response has \`reason\` + \`hint\` pointing at the right
tool. Read them. Call \`add_case_study\` after non-obvious recoveries so
future runs benefit.
`;
