import { test } from "node:test";
import { strict as assert } from "node:assert";
import {
  agentStateColor,
  agentStateLabel,
  agentStateTooltip,
  describeAgentState,
} from "./ios-agent-agent-state-display.js";
import type { IosAgentState, IosAgentStatus } from "./ios-agent.port.js";

const ALL_STATES: readonly IosAgentState[] = [
  "idle",
  "building",
  "ready",
  "failed",
];

test("agentStateLabel", async (t) => {
  await t.test("returns a non-empty distinct label for every state", () => {
    const labels = ALL_STATES.map(agentStateLabel);
    for (const l of labels) assert.ok(l.length > 0);
    assert.equal(new Set(labels).size, labels.length);
  });

  await t.test('flags "ready" with a positive label', () => {
    assert.match(agentStateLabel("ready"), /ready/i);
  });
});

test("agentStateColor", async (t) => {
  await t.test("returns a non-empty token for every state", () => {
    for (const s of ALL_STATES) assert.ok(agentStateColor(s).length > 0);
  });
});

test("describeAgentState", async (t) => {
  await t.test("bundles label and color", () => {
    const d = describeAgentState("ready");
    assert.equal(d.label, agentStateLabel("ready"));
    assert.equal(d.color, agentStateColor("ready"));
  });
});

test("agentStateTooltip", async (t) => {
  await t.test("falls back to idle label and default port for null status", () => {
    const out = agentStateTooltip(null);
    assert.match(out, /idle/i);
    assert.match(out, /22087/);
  });

  await t.test("includes the status message verbatim when provided", () => {
    const status: IosAgentStatus = {
      udid: "ABC",
      state: "failed",
      port: 22087,
      message: "code signing rejected",
    } as IosAgentStatus;
    const out = agentStateTooltip(status);
    assert.match(out, /failed/i);
    assert.ok(out.includes("code signing rejected"));
  });

  await t.test("uses the actual port when no message is set", () => {
    const status: IosAgentStatus = {
      udid: "ABC",
      state: "ready",
      port: 30000,
    } as IosAgentStatus;
    assert.match(agentStateTooltip(status), /30000/);
  });
});
