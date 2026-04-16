// Subprocess half of `mcp-stdio-smoke.mjs`. Boots the real MCP
// server with a MockDriver via DeviceSession + in-memory storage
// and connects it to StdioServerTransport. The parent script
// spawns this file and drives it via JSON-RPC over stdio.
//
// Lives in scripts/ (inside the repo) so ESM resolution finds
// workspace packages + @modelcontextprotocol/sdk in the repo's
// node_modules. A copy at /tmp would fail to resolve the SDK.
import {
  SystemClock,
  NoopLogger,
  InMemoryStorage,
  RunStore,
  Roles,
} from "@atomyx/core-driver";
import { MockDriver, node } from "@atomyx/core-driver/testing";
import {
  createMcpServer,
  DeviceSession,
} from "@atomyx/core-driver-mcp";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

const driver = new MockDriver();
driver.stageHierarchyRepeated(
  node({
    role: Roles.Container,
    bounds: "0,0,430,932",
    children: [
      node({
        role: Roles.Button,
        id: "login_btn",
        text: "Sign in",
        label: "Login button",
        bounds: "100,400,330,460",
        enabled: true,
        clickable: true,
      }),
      node({
        role: Roles.TextField,
        id: "email",
        hint: "Email address",
        bounds: "40,300,390,340",
        enabled: true,
      }),
    ],
  }),
  500,
);

// Build a DeviceSession whose factories always return the same
// MockDriver — the smoke test doesn't care about real platform
// dispatch, it just exercises the stdio protocol flow.
const session = new DeviceSession({
  factories: {
    ios: () => driver,
    android: () => driver,
  },
  clock: new SystemClock(),
  logger: new NoopLogger(),
});
// Pre-bind so the smoke test's device-touching requests work
// without sending a `select_device` tool call first. Runtime
// device selection is covered by unit tests in
// packages/core-driver-mcp/src/tools/new-tools.test.ts.
await session.select({ platform: "android", id: "mock-device" });

const server = createMcpServer({
  session,
  storage: new InMemoryStorage(),
  runStore: new RunStore(),
});

const transport = new StdioServerTransport();
await server.connect(transport);
