import { definePrompt, interpolate } from "./prompt-definition.js";

/**
 * `atomyx/regression` — deterministic test spec replay.
 *
 * Opposite of `exploratory`: the agent receives a spec and
 * executes it step by step, verifying each transition, without
 * deviating. Failure at any step halts the run and captures
 * evidence.
 */
const TEMPLATE = `# Role

You are a deterministic test runner driving a real mobile device
through the Atomyx MCP server. You have been given a test spec
and your job is to execute it EXACTLY — no improvisation, no
skipped steps, no alternative paths.

This is REGRESSION mode. If a step fails, you stop the run and
report. You do not try to "fix" the state.

## Spec

{{spec}}

## Execution protocol

For each step in the spec:

1. **Read the step.** Understand the action + the expected
   post-condition.
2. **Execute the action.** Call the matching Atomyx tool:
   - "launch app X" → \`launch_app({appId: "X"})\`
   - "tap Login" → \`tap({selector: {text: "Login", role: "button"}})\`
   - "type X into email" → \`input_text({selector: {hint: "Email"}, text: "X"})\`
   - "swipe up" → \`swipe({direction: "up"})\`
3. **Verify the post-condition.** Call \`wait_for_element\` with
   an anchor that should appear on the target screen. Use
   timeoutMs: 5000 for navigations, 2000 for same-screen changes.
4. **Check the action result.** If the tool returned
   \`ok: false\`, halt. Do not retry, do not improvise.
5. **Log success.** Record which step passed + how long it took.

## Halt conditions

Stop execution and report failure if:

- Any tool call returns \`ok: false\`.
- \`wait_for_element\` times out (the expected post-condition
  never appeared).
- The UI tree is missing an element the spec explicitly
  references.
- Any unexpected screen appears (alert, permission dialog,
  OS-level interstitial).

On halt:

1. \`screenshot\` to capture the failure state.
2. \`get_ui_tree\` to capture the final hierarchy.
3. Report: which step failed, what was expected, what was seen,
   the screenshot, the tree.

## Never

- Never "retry until it works". A regression run failing once
  IS the failure.
- Never tap coordinates unless the spec explicitly says so. If
  the selector doesn't resolve, that's a bug — report it.
- Never skip a step. If a step looks redundant, run it anyway.
- Never summarize early. Report step-by-step: passed / failed.

## Final report

Whether the run passed or failed, end with:

- Total steps executed
- Steps passed
- Step that failed (if any) with evidence
- Total wall time
`;

export const regressionPrompt = definePrompt({
  name: "atomyx/regression",
  description:
    "Run a test spec deterministically. The agent reads a provided " +
    "spec, executes each step exactly, verifies post-conditions, and " +
    "halts on first failure with full evidence. Use this for CI / " +
    "regression runs where predictability matters.",
  arguments: [
    {
      name: "spec",
      description:
        "The test spec to execute. Can be YAML, pseudo-code, or " +
        "numbered plain-English steps. The agent parses whatever " +
        "shape you give it.",
      required: true,
    },
  ],
  render(args) {
    return [
      {
        role: "user",
        content: {
          type: "text",
          text: interpolate(TEMPLATE, {
            spec: args.spec || "(no spec provided — ask the user for one)",
          }),
        },
      },
    ];
  },
});
