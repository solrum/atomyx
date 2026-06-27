import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { parseScript } from "./yaml-parser.js";
import { ScriptParseError } from "./selector-compiler.js";

describe("parseScript", () => {
  it("parses a two-document script (config --- steps)", () => {
    const yaml = `
appId: com.example.app
name: Login flow
env:
  email: user@test.com
---
- launchApp
- tap: "Sign in"
- type: \${email}
`;
    const script = parseScript(yaml);
    assert.equal(script.appId, "com.example.app");
    assert.equal(script.name, "Login flow");
    assert.equal(script.env.email, "user@test.com");
    assert.equal(script.steps.length, 3);
    assert.equal(script.steps[0]!.command, "launchApp");
    assert.equal(script.steps[1]!.command, "tap");
    assert.equal(script.steps[2]!.command, "type");
    // Variable resolved
    const typeStep = script.steps[2] as { command: "type"; text: string };
    assert.equal(typeStep.text, "user@test.com");
  });

  it("parses single-document with steps key", () => {
    const yaml = `
appId: com.test
name: Simple
steps:
  - launchApp
  - tap: "OK"
`;
    const script = parseScript(yaml);
    assert.equal(script.appId, "com.test");
    assert.equal(script.steps.length, 2);
  });

  it("resolves external env over script env", () => {
    const yaml = `
appId: com.test
env:
  user: default
---
- type: \${user}
`;
    const script = parseScript(yaml, { user: "override" });
    const step = script.steps[0] as { command: "type"; text: string };
    assert.equal(step.text, "override");
  });

  it("handles tap with extended selector", () => {
    const yaml = `
appId: com.test
---
- tap:
    text: "Login"
    role: button
`;
    const script = parseScript(yaml);
    const step = script.steps[0] as {
      command: "tap";
      selector: { text?: string; role?: string };
    };
    assert.equal(step.selector.text, "Login");
    assert.equal(step.selector.role, "button");
  });

  it("handles type with into selector", () => {
    const yaml = `
appId: com.test
---
- type:
    into: "Email"
    text: hello@test.com
`;
    const script = parseScript(yaml);
    const step = script.steps[0] as {
      command: "type";
      text: string;
      into?: { text?: string };
    };
    assert.equal(step.text, "hello@test.com");
    assert.equal(step.into?.text, "Email");
  });

  it("handles waitFor with timeout", () => {
    const yaml = `
appId: com.test
---
- waitFor:
    text: "Welcome"
    timeout: 10000
`;
    const script = parseScript(yaml);
    const step = script.steps[0] as {
      command: "waitFor";
      timeoutMs?: number;
    };
    assert.equal(step.timeoutMs, 10000);
  });

  it("preserves id / role / nth in waitFor object selector", () => {
    const yaml = `
appId: com.test
---
- waitFor: { id: "com.app:id/login", timeout: 5000 }
- waitFor: { role: "text-field", nth: 0 }
- assertVisible: { id: "com.app:id/done" }
- assertNotVisible: { hint: "Email" }
`;
    const script = parseScript(yaml);
    const [wait, role, visible, notVisible] = script.steps as Array<{
      selector: { id?: string; role?: string; nth?: number; hint?: string };
      timeoutMs?: number;
    }>;
    assert.equal(wait!.selector.id, "com.app:id/login");
    assert.equal(wait!.timeoutMs, 5000);
    assert.equal(role!.selector.role, "text-field");
    assert.equal(role!.selector.nth, 0);
    assert.equal(visible!.selector.id, "com.app:id/done");
    assert.equal(notVisible!.selector.hint, "Email");
  });

  it("mirrors label from text in explicit object selector", () => {
    const yaml = `
appId: com.test
---
- tap: { text: "Continue" }
`;
    const script = parseScript(yaml);
    const step = script.steps[0] as {
      selector: { text?: string; label?: string };
    };
    assert.equal(step.selector.text, "Continue");
    assert.equal(step.selector.label, "Continue");
  });

  it("handles capture command", () => {
    const yaml = `
appId: com.test
---
- capture: "POST /api/transfer as: resp"
`;
    const script = parseScript(yaml);
    const step = script.steps[0] as {
      command: "capture";
      pattern: string;
      as: string;
    };
    assert.equal(step.pattern, "POST /api/transfer");
    assert.equal(step.as, "resp");
  });

  it("throws on empty script", () => {
    assert.throws(() => parseScript(""), ScriptParseError);
  });

  it("throws on unknown command", () => {
    const yaml = `
appId: com.test
---
- unknownCommand: "value"
`;
    assert.throws(() => parseScript(yaml), ScriptParseError);
  });

  it("handles all simple commands", () => {
    const yaml = `
appId: com.test
---
- back
- screenshot
- screenshot: evidence_name
- swipe: up
- pressKey: enter
- sleep: 2000
- assertVisible: "Done"
- assertNotVisible: "Error"
`;
    const script = parseScript(yaml);
    assert.equal(script.steps.length, 8);
    assert.equal(script.steps[0]!.command, "back");
    assert.equal(script.steps[1]!.command, "screenshot");
    assert.equal(script.steps[3]!.command, "swipe");
    assert.equal(script.steps[4]!.command, "pressKey");
    assert.equal(script.steps[5]!.command, "sleep");
    assert.equal(script.steps[6]!.command, "assertVisible");
    assert.equal(script.steps[7]!.command, "assertNotVisible");
  });
});
