# Atomyx: known limitations

## Purpose

For a script author or an agent operator who hit an unexpected
result and wants to know whether it is a known issue before digging
further. Answers one question: what does Atomyx currently not do, or
do wrongly, that a user can hit, and how do you work around it.

This doc does not cover expected behavior. For what a tool or a
script command is supposed to do, read `mcp-tools.md` or
`yml-script-reference.md` instead. Each item below states the
symptom, where it applies, and a workaround verified against the
current code or docs.

## Summary

- [Obscurement check can flag a dialog's backdrop as blocking the dialog's own button](#obscurement-check-can-flag-a-dialogs-backdrop-as-blocking-the-dialogs-own-button)
- [YAML selectors cannot express a regex, so a merged accessibility label defeats a short match](#yaml-selectors-cannot-express-a-regex-so-a-merged-accessibility-label-defeats-a-short-match)
- [iOS press_key types an unsupported key as literal text instead of pressing it](#ios-press_key-types-an-unsupported-key-as-literal-text-instead-of-pressing-it)
- [Typing can fail on the iOS Simulator when a field never reports focus or a keyboard](#typing-can-fail-on-the-ios-simulator-when-a-field-never-reports-focus-or-a-keyboard)
- [A misplaced otherwise in a handle step is silently ignored](#a-misplaced-otherwise-in-a-handle-step-is-silently-ignored)
- [A focus-timeout error omits the field name for role-only selectors](#a-focus-timeout-error-omits-the-field-name-for-role-only-selectors)
- [The CLI needs signing settings in the environment to drive a physical iPhone](#the-cli-needs-signing-settings-in-the-environment-to-drive-a-physical-iphone)
- [An iOS run without --device is not provisioned](#an-ios-run-without---device-is-not-provisioned)

## Obscurement check can flag a dialog's backdrop as blocking the dialog's own button

**Symptom**: a tap on a selector that matches a visible, tappable
button inside a modal dialog fails with `ok: false` and a reason such
as `element is visually obscured by [role=... id=... label=...]`,
naming the dialog's own backdrop as the obscurer, even though the
button renders on top of that backdrop on screen.

**Where**: any `tap`, `long_press`, or `type` call that resolves its
target by selector, on Android and iOS. A tap by coordinates is not
affected.

**Why**: the obscurement check walks the accessibility tree in
pre-order and treats the last node whose bounds contain the target's
center point as topmost, on the assumption that a later position in
that walk means a later paint order. When the backdrop node comes
after the button node in that walk, and carries its own role, id, or
label rather than being a plain, unlabeled container, the check
accepts it as a real obscurer, even though the button is what
actually paints on top of it.

**Workaround**: tap by coordinates using the center point that
`find_element` or `wait_for_element` returns, instead of tapping by
selector. The coordinate path dispatches the tap directly to the
device and skips scrolling and the obscurement check entirely.

## YAML selectors cannot express a regex, so a merged accessibility label defeats a short match

**Symptom**: a `text` or `label` selector for an element's visible
short title, for example `"Products"`, never matches, because the
element's actual label attribute is longer. Flutter Semantics
commonly merges a title and a subtitle into one label, for example
`"Products\nBrowse our catalog"`, newline included.

**Where**: any YAML script selector field (`text`, `id`, `label`,
`hint`). The MCP tool surface is not affected, because it accepts a
regex pattern there.

**Workaround**: in a YAML script, give the full merged value,
including the newline, as a double-quoted string, for example
`"Products\nBrowse our catalog"`. A double-quoted YAML string decodes
`\n` to a real newline character before Atomyx compares it against
the label. Over MCP, pass a regex pattern instead, for example
`{"label": {"regex": "^Products"}}`, which matches the prefix
regardless of what follows it. See `yml-script-reference.md`'s
selector matching section for the full explanation.

## iOS press_key types an unsupported key as literal text instead of pressing it

**Symptom**: `press_key` with `tab`, `escape`, or `delete` on iOS
reports `ok: true` and types the literal text `tab`, `escape`, or
`delete` into the focused field, instead of pressing that key or
rejecting the call.

**Where**: `press_key` over MCP, and the equivalent YAML script step,
iOS only. Android rejects these same keys with a tool error instead.

**Workaround**: on iOS, use only `back`, `home`, and `enter`, the
keys `mcp-tools.md`'s press_key platform support table lists as
recognized there. For any other key, use a visible on-screen control
instead of `press_key`.

## Typing can fail on the iOS Simulator when a field never reports focus or a keyboard

**Symptom**: a `type` step targeting a specific field fails with a
message stating that no field held focus and no on-screen keyboard
appeared, even though the field visibly took focus on screen.

**Where**: observed on the iOS Simulator running
`examples/test-login-flow.yml`. Not observed on Android, where the
same script passes.

**As observed, not proven**: the field visibly takes focus, but the
driver reports no focused element and no on-screen keyboard, so the
wait for the field to hold focus times out. This reproduced on every
attempt regardless of whether the Simulator's hardware keyboard
connection was toggled, so that setting is not a confirmed cause.

**Workaround**: run a text-input flow like this one against a
physical iOS device or an Android target instead of the iOS
Simulator. `examples/README.md` marks this script as verified on
Android for this reason.

## A misplaced otherwise in a handle step is silently ignored

**Symptom**: a `handle` step with `otherwise` written as a sibling
key of `handle` itself, outside the branch list, parses with no
validation error, but behaves as if `otherwise` were never set. When
no branch matches, the step fails after the default timeout instead
of applying the intended skip behavior.

**Where**: the `handle` YAML script step.

**Why**: a step normalizes by reading exactly one command key
(`handle`) and passing only that key's value, the branch array, to
the handle logic. A sibling key placed next to `handle` at the step
level is never read, so it has no effect and produces no error.
Without a recognized `otherwise` entry inside the branch list, a step
whose branches never match falls through to the default: fail after
a 5 second poll budget.

**Workaround**: put `otherwise` inside the branch list, as its own
list entry:

```yaml
- handle:
    - when:
        visible: "Success"
      do:
        - screenshot: success
    - otherwise: fail
```

## A focus-timeout error omits the field name for role-only selectors

**Symptom**: a `type` step whose `into` selector uses only `role` and
`nth`, with no `id`, `label`, `text`, or `value`, fails, when the
field never takes focus, with a message reading that no field was
ready for input because nothing held focus and no keyboard was
shown. That is the same wording used when no field was targeted at
all, even though a specific field was targeted. The failure itself is
real. Only the wording is misleading.

**Where**: `type` with an `into` selector, on Android and iOS.

**Why**: the error names the target by which selector field resolved
the match, checked in order against the resolved element's `id`,
`label`, `text`, `value`, and `hint` attributes. `role` and `nth` are
never part of that check, so a selector using only those two fields
always produces an empty name, and the error falls back to its
generic wording.

**Workaround**: add a `label` or `text` value to the selector when
the target element has one, so the error names the field that failed
to take focus.

## The CLI needs signing settings in the environment to drive a physical iPhone

**Symptom**: `atomyx driver run --device <udid>` against a physical
iPhone fails with `AGENT_UNREACHABLE` and a message naming
`ATOMYX_DEV_TEAM`, even though `platforms/ios-agent/device.env` holds
the team.

**Where**: `atomyx` and `atomyx-mcp` on a physical iOS device.

**Why**: only `make` reads `device.env`. The binaries read the signing
settings from the environment, and the project config has no key for
them.

**Workaround**: export `ATOMYX_DEV_TEAM` before the command, and also
`ATOMYX_IOS_DRIVER_DIR` when the binary does not sit inside the
checkout. See `getting-started.md`.

## An iOS run without --device is not provisioned

**Symptom**: `atomyx driver run --platform ios` with no `--device`
does not launch a runner. It dials the default runner port, 22087, and
fails with an unreachable agent when nothing serves there.

**Where**: iOS, `run`, `suite`, and `doctor` without `--device`.

**Why**: a launched runner is keyed on a device udid, and no udid
means nothing to key it on.

**Workaround**: pass `--device <udid>`, or start a runner yourself
with `make serve` before the command.
