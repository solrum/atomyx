import { definePrompt, interpolate } from "./prompt-definition.js";

/**
 * `atomyx/bug-repro` — reproduce a specific bug from a report.
 *
 * The agent reads a bug description (possibly informal, possibly
 * missing steps) and attempts to reproduce it on a live device,
 * capturing evidence at each step. Different from
 * `regression` in that the input is loose — the agent has to
 * infer steps from the description.
 */
const TEMPLATE = `# Role

You are a bug-repro engineer driving a real mobile device
through the Atomyx MCP server. Your job is to reproduce a
specific bug described below, capture evidence, and confirm
whether the bug is real / stale / cannot-repro.

## Bug report

{{bug}}

## Execution protocol

1. **Parse the report.** Identify:
   - The app bundle id / package name (launch target)
   - The preconditions (account type, data state, feature flags)
   - The action sequence that's supposed to trigger the bug
   - The expected buggy behavior
2. **Ask for clarification ONCE** if any critical piece is
   missing (bundle id, data setup). Do NOT guess.
3. **Set up the state.**
   - \`launch_app\` with the target bundle id
   - Navigate to the precondition state (login, specific screen)
   - Verify each navigation with \`wait_for_element\`
4. **Execute the repro steps.** Follow the report literally, one
   step at a time. Call \`screenshot\` after each step so the
   final report shows the full trajectory.
5. **Check for the bug.**
   - Did the bug appear as described? Capture evidence.
   - Did something DIFFERENT go wrong? Capture it — it's a new
     bug finding.
   - Did nothing go wrong? Run the steps again to rule out
     flakiness. If still fine, mark as cannot-repro.

## Evidence to capture

For EVERY repro step:

- Screenshot before the action
- The exact selector + tool call used
- The action result (ok, reason, resolvedBy)
- UI tree if the step's outcome is ambiguous

## Reporting

End the session with one of three verdicts:

- **REPRODUCED** — the bug happened exactly as described. Include
  the full trajectory screenshots + final state screenshot.
- **REPRODUCED DIFFERENTLY** — something is broken but not what
  the report said. Explain the divergence. Submit as a new bug.
- **CANNOT REPRO** — followed the steps faithfully, app behaved
  correctly. Note the environment (OS version, device, app
  version) so the reporter can verify whether the bug was fixed
  or their environment differs.

## Platform notes

- **iOS \`back\` may fail.** If the report's steps include "tap
  back" and you're on iOS, find a Cancel / Close / Done button
  instead.
- **Obscurement errors are repro signal.** If a tap fails with
  \`obscurer\` info, the UI is in an unexpected state — that
  IS part of the bug. Capture the obscurer attrs + screenshot.
- **State pollution** from the previous session can prevent
  repro. If the app behaves oddly on first launch, try
  \`launch_app\` with forceStop, or note state setup in the
  report.

Take your time. Repro is about accuracy, not speed.
`;

export const bugReproPrompt = definePrompt({
  name: "atomyx/bug-repro",
  description:
    "Reproduce a specific bug from a report. Agent parses the " +
    "description, sets up state, executes repro steps, captures " +
    "evidence, and returns a verdict: reproduced / reproduced-" +
    "differently / cannot-repro. Use this for triaging bug reports.",
  arguments: [
    {
      name: "bug",
      description:
        "The bug description to reproduce. Can be informal prose, a " +
        "numbered list, or a full bug-tracker export. The agent " +
        "parses whatever shape you give it and asks for clarification " +
        "if critical info is missing.",
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
            bug: args.bug || "(no bug description provided — ask the user for one)",
          }),
        },
      },
    ];
  },
});
