import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Orchestra } from "@atomyx/driver/orchestra";
import { SystemClock, NoopLogger } from "@atomyx/core/infra";
import { MockDriver, node } from "@atomyx/driver/testing";
import { defineCommand } from "@atomyx/driver/script";
import type { AnyCommandDefinition } from "@atomyx/driver/script";
import type { ScriptDefinition } from "@atomyx/shared/script";
import type { CapturedRequest } from "@atomyx/shared/script";
import { ScriptRunner } from "../runner/script-runner.js";
import { DEFAULT_COMMANDS } from "./index.js";

// ---------------------------------------------------------------------------
// Seed command — injects a pre-built CapturedRequest into ctx.captures
// so assertApi / extract / branch tests don't need a real proxy.
// ---------------------------------------------------------------------------

const seedCaptureCommand = defineCommand({
  command: "seedCapture",
  async execute(
    args: { as: string; captured: CapturedRequest },
    ctx,
  ) {
    ctx.captures.set(args.as, args.captured);
    return { ok: true, detail: `Seeded capture "${args.as}"` };
  },
});

const COMMANDS_WITH_SEED: readonly AnyCommandDefinition[] = [
  ...DEFAULT_COMMANDS,
  seedCaptureCommand as AnyCommandDefinition,
];

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const LOGIN_CAPTURE: CapturedRequest = {
  method: "POST",
  url: "https://api.test.com/login",
  status: 200,
  headers: { "content-type": "application/json" },
  body: {
    token: "abc123",
    user: { id: 1, name: "Test User", roles: ["admin", "editor"] },
    items: [{ sku: "A" }, { sku: "B" }],
    count: 42,
    empty: "",
    flag: true,
  },
  timestamp: 1700000000000,
};

const ERROR_CAPTURE: CapturedRequest = {
  method: "POST",
  url: "https://api.test.com/login",
  status: 401,
  headers: {},
  body: { error: "invalid_credentials" },
  timestamp: 1700000001000,
};

function buildRunner(treeOverride?: ReturnType<typeof node>) {
  const driver = new MockDriver();
  const tree = treeOverride ?? node({
    id: "root",
    bounds: "0,0,430,932",
    role: "container",
    children: [
      node({ id: "login_btn", text: "Login", role: "button", clickable: true, bounds: "50,100,380,150" }),
      node({ id: "email", text: "", hint: "Email", role: "text-field", clickable: true, bounds: "50,200,380,250" }),
    ],
  });
  driver.stageHierarchyRepeated(tree, 100);
  const clock = new SystemClock();
  const logger = new NoopLogger();
  const orchestra = new Orchestra({ driver, clock, logger });
  const runner = new ScriptRunner({
    orchestra,
    clock,
    logger,
    commands: COMMANDS_WITH_SEED,
  });
  return { runner, driver };
}

function seedStep(as: string, captured: CapturedRequest) {
  return { command: "seedCapture", as, captured } as never;
}

function script(
  steps: readonly unknown[],
  overrides?: Partial<ScriptDefinition>,
): ScriptDefinition {
  return {
    appId: "com.test",
    name: "test",
    env: {},
    ...overrides,
    steps: steps as ScriptDefinition["steps"],
  };
}

// ===================================================================
// assertApi operators
// ===================================================================

describe("assertApi", () => {
  // ---- $not_empty ----

  describe("$not_empty", () => {
    it("passes for a non-empty string", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.token": "$not_empty" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("passes for a non-empty array", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.user.roles": "$not_empty" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("passes for a number", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$not_empty" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("fails for null", async () => {
      const nullCapture: CapturedRequest = {
        ...LOGIN_CAPTURE,
        body: { value: null },
      };
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("c", nullCapture),
        { command: "assertApi", from: "c", body: { "$.value": "$not_empty" } },
      ]));
      assert.equal(result.ok, false);
      assert.match(result.steps[1]!.detail!, /not_empty/);
    });

    it("fails for undefined (missing path)", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.missing": "$not_empty" } },
      ]));
      assert.equal(result.ok, false);
    });

    it("fails for empty string", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.empty": "$not_empty" } },
      ]));
      assert.equal(result.ok, false);
    });

    it("fails for empty array", async () => {
      const emptyArrCapture: CapturedRequest = {
        ...LOGIN_CAPTURE,
        body: { items: [] },
      };
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("c", emptyArrCapture),
        { command: "assertApi", from: "c", body: { "$.items": "$not_empty" } },
      ]));
      assert.equal(result.ok, false);
    });
  });

  // ---- $exists / $not_exists ----

  describe("$exists", () => {
    it("passes for any defined value including null", async () => {
      const nullCapture: CapturedRequest = {
        ...LOGIN_CAPTURE,
        body: { value: null },
      };
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("c", nullCapture),
        { command: "assertApi", from: "c", body: { "$.value": "$exists" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("fails for undefined (missing path)", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.nope": "$exists" } },
      ]));
      assert.equal(result.ok, false);
    });
  });

  describe("$not_exists", () => {
    it("passes for undefined (missing path)", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.nope": "$not_exists" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("fails when value is defined", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.token": "$not_exists" } },
      ]));
      assert.equal(result.ok, false);
    });
  });

  // ---- $contains:text ----

  describe("$contains", () => {
    it("passes when string contains substring", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.token": "$contains:abc" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("fails when string does not contain substring", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.token": "$contains:xyz" } },
      ]));
      assert.equal(result.ok, false);
    });

    it("fails when value is not a string", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$contains:4" } },
      ]));
      assert.equal(result.ok, false);
    });
  });

  // ---- Numeric comparisons ----

  describe("$gt / $gte / $lt / $lte", () => {
    it("$gt passes when value is greater", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$gt:41" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("$gt fails when value is equal", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$gt:42" } },
      ]));
      assert.equal(result.ok, false);
    });

    it("$gte passes when value is equal", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$gte:42" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("$lt passes when value is less", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$lt:43" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("$lt fails when value is equal", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$lt:42" } },
      ]));
      assert.equal(result.ok, false);
    });

    it("$lte passes when value is equal", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$lte:42" } },
      ]));
      assert.equal(result.ok, true);
    });
  });

  // ---- $between ----

  describe("$between", () => {
    it("passes when value is within range (inclusive)", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$between:40,50" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("passes at lower bound", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$between:42,50" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("passes at upper bound", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$between:30,42" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("fails when value is outside range", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": "$between:50,60" } },
      ]));
      assert.equal(result.ok, false);
    });
  });

  // ---- Literal match (no $ prefix) ----

  describe("exact match", () => {
    it("literal string 'not_empty' matches literally, not as operator", async () => {
      const literalCapture: CapturedRequest = {
        ...LOGIN_CAPTURE,
        body: { status: "not_empty" },
      };
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("c", literalCapture),
        { command: "assertApi", from: "c", body: { "$.status": "not_empty" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("literal string without $ does not trigger operator", async () => {
      const { runner } = buildRunner();
      // "not_empty" (no $) should fail because actual is "abc123", not "not_empty"
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.token": "not_empty" } },
      ]));
      assert.equal(result.ok, false);
    });

    it("exact match for numbers", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.count": 42 } },
      ]));
      assert.equal(result.ok, true);
    });

    it("exact match for booleans", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.flag": true } },
      ]));
      assert.equal(result.ok, true);
    });

    it("exact match for strings", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.token": "abc123" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("exact match fails for wrong value", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.token": "wrong" } },
      ]));
      assert.equal(result.ok, false);
    });
  });

  // ---- Status assertion ----

  describe("status assertion", () => {
    it("passes when status matches", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", status: 200 },
      ]));
      assert.equal(result.ok, true);
    });

    it("fails when status does not match", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", status: 401 },
      ]));
      assert.equal(result.ok, false);
      assert.match(result.steps[1]!.detail!, /status/);
    });
  });

  // ---- Missing capture ----

  describe("missing capture", () => {
    it("fails when capture variable does not exist", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        { command: "assertApi", from: "nonexistent", body: { "$.x": "$exists" } },
      ]));
      assert.equal(result.ok, false);
      assert.match(result.steps[0]!.detail!, /No captured request/);
    });
  });

  // ---- Dot-path resolution ----

  describe("dot-path resolution", () => {
    it("resolves nested objects", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.user.name": "Test User" } },
      ]));
      assert.equal(result.ok, true);
    });

    it("resolves array indices", async () => {
      const { runner } = buildRunner();
      const result = await runner.run(script([
        seedStep("login", LOGIN_CAPTURE),
        { command: "assertApi", from: "login", body: { "$.items[1].sku": "B" } },
      ]));
      assert.equal(result.ok, true);
    });
  });
});

// ===================================================================
// extract
// ===================================================================

describe("extract", () => {
  it("extracts dot-path values into variables", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      { command: "extract", from: "login", values: { token: "$.body.token", userId: "$.body.user.id" } },
    ]));
    assert.equal(result.ok, true);
    assert.match(result.steps[1]!.detail!, /token/);
    assert.match(result.steps[1]!.detail!, /userId/);
  });

  it("extracted variables available in subsequent steps via ${name}", async () => {
    const { runner } = buildRunner();
    // Extract the user name, then use assertApi to verify a literal match
    // (We can't directly inspect ctx.variables from outside, but we
    // can verify extract succeeded and reported the value.)
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      { command: "extract", from: "login", values: { userName: "$.body.user.name" } },
    ]));
    assert.equal(result.ok, true);
    assert.match(result.steps[1]!.detail!, /Test User/);
  });

  it("fails when capture variable does not exist", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      { command: "extract", from: "nonexistent", values: { x: "$.body.token" } },
    ]));
    assert.equal(result.ok, false);
    assert.match(result.steps[0]!.detail!, /No captured request/);
  });

  it("fails when dot-path resolves to undefined", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      { command: "extract", from: "login", values: { x: "$.body.nonexistent.deep" } },
    ]));
    assert.equal(result.ok, false);
    assert.match(result.steps[1]!.detail!, /undefined/);
  });

  it("handles ${from} wrapper syntax", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      { command: "extract", from: "${login}", values: { t: "$.body.token" } },
    ]));
    assert.equal(result.ok, true);
  });

  it("extracts status code", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      { command: "extract", from: "login", values: { code: "$.status" } },
    ]));
    assert.equal(result.ok, true);
    assert.match(result.steps[1]!.detail!, /200/);
  });
});

// ===================================================================
// handle (UI branching)
// ===================================================================

describe("handle", () => {
  it("executes first matching branch (visible element)", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { visible: "Login" },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("skips non-matching branches and executes matching one", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { visible: "NonExistent" },
            do: [{ command: "tap", selector: { text: "NonExistent" } }],
          },
          {
            when: { visible: "Login" },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("otherwise: fail when no branch matches", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { visible: "NonExistent" },
            do: [{ command: "tap", selector: { text: "NonExistent" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, false);
    assert.match(result.steps[0]!.detail!, /no branch matched/);
  });

  it("otherwise: skip returns ok", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { visible: "NonExistent" },
            do: [{ command: "tap", selector: { text: "NonExistent" } }],
          },
        ],
        otherwise: "skip",
      },
    ]));
    assert.equal(result.ok, true);
    assert.match(result.steps[0]!.detail!, /skipped/);
  });

  it("matches branch with notVisible condition", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { notVisible: "NonExistent" },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("rejects branch when notVisible element is actually visible", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { notVisible: "Login" },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, false);
    assert.match(result.steps[0]!.detail!, /no branch matched/);
  });

  it("matches with combined visible + notVisible conditions", async () => {
    const { runner } = buildRunner();
    // Both conditions must be true: "Login" visible AND "NonExistent" not visible
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { visible: "Login", notVisible: "NonExistent" },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("rejects combined condition when notVisible part fails", async () => {
    const { runner } = buildRunner();
    // visible: "Login" is true, but notVisible: "Login" fails because it IS visible
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { visible: "Login", notVisible: "Login" },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, false);
  });

  it("stages different tree to simulate screen state change", async () => {
    const otpTree = node({
      id: "root",
      bounds: "0,0,430,932",
      role: "container",
      children: [
        node({ id: "otp_field", text: "Enter OTP", role: "text-field", bounds: "50,100,380,150" }),
        node({ id: "verify_btn", text: "Verify", role: "button", clickable: true, bounds: "50,200,380,250" }),
      ],
    });
    const { runner } = buildRunner(otpTree);
    const result = await runner.run(script([
      {
        command: "handle",
        branches: [
          {
            when: { visible: "Login" },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
          {
            when: { visible: "Enter OTP" },
            do: [{ command: "tap", selector: { text: "Verify" } }],
          },
        ],
        otherwise: "fail",
      },
    ]));
    assert.equal(result.ok, true);
  });
});

// ===================================================================
// branch (API-based branching)
// ===================================================================

describe("branch", () => {
  it("matches by status code", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      {
        command: "branch",
        from: "login",
        on: [
          {
            match: { status: 200 },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
          {
            match: { status: 401 },
            do: [{ command: "screenshot" }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("matches by body dot-path", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      {
        command: "branch",
        from: "login",
        on: [
          {
            match: { body: { "$.token": "abc123" } },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("skips non-matching case and falls to next", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", ERROR_CAPTURE),
      {
        command: "branch",
        from: "login",
        on: [
          {
            match: { status: 200 },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
          {
            match: { status: 401 },
            do: [{ command: "screenshot" }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, true);
    // Second step (branch) executed the screenshot do-block
    assert.equal(result.steps[1]!.ok, true);
  });

  it("runs default when no case matches", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      {
        command: "branch",
        from: "login",
        on: [
          {
            match: { status: 500 },
            do: [{ command: "screenshot" }],
          },
        ],
        default: [{ command: "tap", selector: { text: "Login" } }],
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("fails when no case matches and no default", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      {
        command: "branch",
        from: "login",
        on: [
          {
            match: { status: 500 },
            do: [{ command: "screenshot" }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, false);
    assert.match(result.steps[1]!.detail!, /no case matched/);
  });

  it("fails when capture variable is missing", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      {
        command: "branch",
        from: "nonexistent",
        on: [
          {
            match: { status: 200 },
            do: [{ command: "screenshot" }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, false);
    assert.match(result.steps[0]!.detail!, /No captured request/);
  });

  it("handles ${from} wrapper syntax", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      {
        command: "branch",
        from: "${login}",
        on: [
          {
            match: { status: 200 },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("matches by combined status + body", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      {
        command: "branch",
        from: "login",
        on: [
          {
            match: { status: 200, body: { "$.token": "abc123" } },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, true);
  });

  it("rejects when status matches but body does not", async () => {
    const { runner } = buildRunner();
    const result = await runner.run(script([
      seedStep("login", LOGIN_CAPTURE),
      {
        command: "branch",
        from: "login",
        on: [
          {
            match: { status: 200, body: { "$.token": "wrong" } },
            do: [{ command: "tap", selector: { text: "Login" } }],
          },
        ],
      },
    ]));
    assert.equal(result.ok, false);
  });
});
