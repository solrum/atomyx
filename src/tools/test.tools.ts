import { requireController } from "../runtime/atomyx-context.js";
import { resolveTestCaseStorage } from "../storage/test-case-storage.js";
import type { ToolCategory } from "./tool-factory.js";

export const registerTestTools: ToolCategory = (factory, ctx) => {
  factory.register({
    name: "start_test_session",
    description: "Clear the action recorder and start a fresh exploratory test session.",
    inputSchema: { type: "object", properties: {} },
    handler: async () => {
      ctx.recordedActions.length = 0;
      return { ok: true, recordedAt: Date.now() };
    },
  });

  factory.register({
    name: "get_recorded_actions",
    description: "Return the actions recorded so far in this session.",
    inputSchema: { type: "object", properties: {} },
    handler: async () => ({
      actions: [...ctx.recordedActions],
      count: ctx.recordedActions.length,
    }),
  });

  factory.register({
    name: "save_as_test_case",
    description:
      "Persist the recorded action sequence as a TestCase. Default storage writes a JSON file " +
      "to ~/.atomyx/test-cases/. Set ATOMYX_ENGINE_URL env var to ALSO POST to a synapse engine. " +
      "If both are configured, the case is saved to both targets (composite).",
    inputSchema: {
      type: "object",
      required: ["title"],
      properties: {
        title: { type: "string" },
        description: { type: "string" },
        projectId: { type: "string", description: "Required when ATOMYX_ENGINE_URL is set" },
        suiteId: { type: "string", description: "Required when ATOMYX_ENGINE_URL is set" },
      },
    },
    handler: async (args: { title: string; description?: string; projectId?: string; suiteId?: string }) => {
      const ctl = requireController(ctx);
      const storage = resolveTestCaseStorage();
      const result = await storage.save({
        title: args.title,
        description: args.description,
        projectId: args.projectId,
        suiteId: args.suiteId,
        deviceId: ctl.deviceId,
        platform: ctl.platform,
        actions: [...ctx.recordedActions],
        savedAt: Date.now(),
      });
      return {
        ok: true,
        storage: storage.name,
        targets: result.targets,
        actionCount: ctx.recordedActions.length,
      };
    },
  });
};
