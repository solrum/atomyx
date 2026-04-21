import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { Orchestra, FakeClock, NoopLogger } from "@atomyx/driver";
import { MockDriver } from "@atomyx/driver/testing";
import { createMcpServer } from "../server.js";
import {
  DEFAULT_PROMPTS,
  definePrompt,
  interpolate,
  playbookPrompt,
  exploratoryPrompt,
  regressionPrompt,
  bugReproPrompt,
} from "./index.js";
import {
  ListPromptsRequestSchema,
  GetPromptRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

interface InternalServer {
  _requestHandlers: Map<string, (req: unknown, extra: unknown) => Promise<unknown>>;
}

function buildServer(promptsOverride?: typeof DEFAULT_PROMPTS) {
  const driver = new MockDriver();
  const orchestra = new Orchestra({
    driver,
    clock: new FakeClock(),
    logger: new NoopLogger(),
  });
  const server = createMcpServer({
    orchestra,
    ...(promptsOverride !== undefined ? { prompts: promptsOverride } : {}),
  });
  return server;
}

async function listPrompts(server: ReturnType<typeof createMcpServer>) {
  const handler = (server as unknown as InternalServer)._requestHandlers.get(
    ListPromptsRequestSchema.shape.method.value,
  );
  if (!handler) throw new Error("prompts/list handler not registered");
  return (await handler({ method: "prompts/list", params: {} }, {})) as {
    prompts: Array<{
      name: string;
      description: string;
      arguments: Array<{ name: string; description: string; required: boolean }>;
    }>;
  };
}

async function getPrompt(
  server: ReturnType<typeof createMcpServer>,
  name: string,
  args: Record<string, string> = {},
) {
  const handler = (server as unknown as InternalServer)._requestHandlers.get(
    GetPromptRequestSchema.shape.method.value,
  );
  if (!handler) throw new Error("prompts/get handler not registered");
  return (await handler(
    {
      method: "prompts/get",
      params: { name, arguments: args },
    },
    {},
  )) as {
    description: string;
    messages: Array<{
      role: "user" | "assistant";
      content: { type: "text"; text: string };
    }>;
  };
}

describe("DEFAULT_PROMPTS registry", () => {
  it("ships 4 methodology prompts out of the box", () => {
    assert.equal(DEFAULT_PROMPTS.length, 4);
    const names = DEFAULT_PROMPTS.map((p) => p.name);
    assert.ok(names.includes("atomyx/playbook"));
    assert.ok(names.includes("atomyx/exploratory"));
    assert.ok(names.includes("atomyx/regression"));
    assert.ok(names.includes("atomyx/bug-repro"));
  });

  it("every prompt has a non-empty description", () => {
    for (const p of DEFAULT_PROMPTS) {
      assert.ok(p.description.length > 10, `${p.name} description too short`);
    }
  });

  it("every prompt renders at least one message", () => {
    for (const p of DEFAULT_PROMPTS) {
      const messages = p.render({});
      assert.ok(messages.length >= 1, `${p.name} rendered zero messages`);
      assert.equal(messages[0]!.role, "user");
      assert.equal(messages[0]!.content.type, "text");
      assert.ok(messages[0]!.content.text.length > 100);
    }
  });

  it("every prompt name is unique", () => {
    const seen = new Set<string>();
    for (const p of DEFAULT_PROMPTS) {
      assert.equal(seen.has(p.name), false, `duplicate prompt name: ${p.name}`);
      seen.add(p.name);
    }
  });
});

describe("interpolate", () => {
  it("substitutes {{name}} with values", () => {
    assert.equal(
      interpolate("Hello {{name}}!", { name: "world" }),
      "Hello world!",
    );
  });

  it("leaves missing keys as-is", () => {
    assert.equal(
      interpolate("Hello {{name}}!", {}),
      "Hello {{name}}!",
    );
  });

  it("substitutes multiple occurrences", () => {
    assert.equal(
      interpolate("{{x}} + {{x}} = {{y}}", { x: "2", y: "4" }),
      "2 + 2 = 4",
    );
  });
});

describe("prompts/list via createMcpServer", () => {
  it("returns all 4 default prompts with correct shape", async () => {
    const server = buildServer();
    const result = await listPrompts(server);
    assert.equal(result.prompts.length, 4);
    for (const p of result.prompts) {
      assert.ok(typeof p.name === "string");
      assert.ok(typeof p.description === "string");
      assert.ok(Array.isArray(p.arguments));
      for (const a of p.arguments) {
        assert.ok(typeof a.name === "string");
        assert.ok(typeof a.description === "string");
        assert.ok(typeof a.required === "boolean");
      }
    }
  });

  it("respects custom prompts override", async () => {
    const custom = definePrompt({
      name: "test/custom",
      description: "Custom test prompt",
      arguments: [],
      render: () => [
        { role: "user", content: { type: "text", text: "Custom body" } },
      ],
    });
    const server = buildServer([custom]);
    const result = await listPrompts(server);
    assert.equal(result.prompts.length, 1);
    assert.equal(result.prompts[0]!.name, "test/custom");
  });

  it("prompts:[] disables the prompts capability entirely", async () => {
    const server = buildServer([]);
    const handler = (server as unknown as InternalServer)._requestHandlers.get(
      ListPromptsRequestSchema.shape.method.value,
    );
    // Handler should NOT be registered when prompts list is empty.
    assert.equal(handler, undefined);
  });
});

describe("prompts/get via createMcpServer", () => {
  it("returns rendered messages for atomyx/playbook", async () => {
    const server = buildServer();
    const result = await getPrompt(server, "atomyx/playbook");
    assert.equal(result.messages.length, 1);
    assert.match(result.messages[0]!.content.text, /Atomyx tool playbook/);
  });

  it("substitutes arguments in atomyx/exploratory", async () => {
    const server = buildServer();
    const result = await getPrompt(server, "atomyx/exploratory", {
      appId: "com.example.target",
      goal: "find login bugs",
      budget: "30 actions",
    });
    const body = result.messages[0]!.content.text;
    assert.match(body, /com\.example\.target/);
    assert.match(body, /find login bugs/);
    assert.match(body, /30 actions/);
  });

  it("falls back to defaults when exploratory args are omitted", async () => {
    const server = buildServer();
    const result = await getPrompt(server, "atomyx/exploratory", {});
    const body = result.messages[0]!.content.text;
    assert.match(body, /not specified/);
    assert.match(body, /50 actions/);
  });

  it("substitutes spec into atomyx/regression", async () => {
    const server = buildServer();
    const result = await getPrompt(server, "atomyx/regression", {
      spec: "1. Launch com.x\n2. Tap Login",
    });
    assert.match(result.messages[0]!.content.text, /1\. Launch com\.x/);
  });

  it("substitutes bug description into atomyx/bug-repro", async () => {
    const server = buildServer();
    const result = await getPrompt(server, "atomyx/bug-repro", {
      bug: "Login button does nothing after first tap",
    });
    assert.match(
      result.messages[0]!.content.text,
      /Login button does nothing/,
    );
  });

  it("throws on unknown prompt name", async () => {
    const server = buildServer();
    const err = await getPrompt(server, "nope").catch((e) => e);
    assert.ok(err instanceof Error);
    assert.match(err.message, /Unknown prompt/);
  });
});

describe("individual prompt metadata", () => {
  it("playbookPrompt has no arguments", () => {
    assert.equal((playbookPrompt.arguments ?? []).length, 0);
  });

  it("exploratoryPrompt declares appId + goal + budget as optional", () => {
    const args = exploratoryPrompt.arguments ?? [];
    assert.equal(args.length, 3);
    for (const a of args) {
      assert.equal(a.required, false);
    }
    assert.ok(args.some((a) => a.name === "appId"));
    assert.ok(args.some((a) => a.name === "goal"));
    assert.ok(args.some((a) => a.name === "budget"));
  });

  it("regressionPrompt requires spec", () => {
    const args = regressionPrompt.arguments ?? [];
    assert.equal(args.length, 1);
    assert.equal(args[0]!.name, "spec");
    assert.equal(args[0]!.required, true);
  });

  it("bugReproPrompt requires bug", () => {
    const args = bugReproPrompt.arguments ?? [];
    assert.equal(args.length, 1);
    assert.equal(args[0]!.name, "bug");
    assert.equal(args[0]!.required, true);
  });
});

describe("createMcpServer duplicate prompt detection", () => {
  it("throws on duplicate prompt names", () => {
    const dup1 = definePrompt({
      name: "dup",
      description: "first",
      arguments: [],
      render: () => [{ role: "user", content: { type: "text", text: "1" } }],
    });
    const dup2 = definePrompt({
      name: "dup",
      description: "second",
      arguments: [],
      render: () => [{ role: "user", content: { type: "text", text: "2" } }],
    });
    assert.throws(() => buildServer([dup1, dup2]), /duplicate prompt name/);
  });
});
