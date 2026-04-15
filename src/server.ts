import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";
import { buildToolRegistry, MUTATING_TOOLS as RECORDABLE } from "./registry.js";
import { createAdetContext } from "./runtime/adet-context.js";

export function createServer() {
  const ctx = createAdetContext();
  const factory = buildToolRegistry(ctx);
  const tools = factory.build();

  const server = new Server(
    { name: "synapse-adet", version: "0.1.0" },
    { capabilities: { tools: {} } },
  );

  server.setRequestHandler(ListToolsRequestSchema, async () => ({
    tools: tools.map((t) => ({
      name: t.name,
      description: t.description,
      inputSchema: t.inputSchema,
    })),
  }));

  server.setRequestHandler(CallToolRequestSchema, async (req) => {
    const tool = factory.byName(req.params.name);
    if (!tool) throw new Error(`Unknown tool: ${req.params.name}`);

    const args = (req.params.arguments ?? {}) as Record<string, unknown>;
    const startedAt = Date.now();
    try {
      const result = await tool.handler(args);
      const durationMs = Date.now() - startedAt;
      ctx.history.push({
        action: tool.name,
        args,
        status: "ok",
        durationMs,
      });
      if (RECORDABLE.has(tool.name)) {
        ctx.recordedActions.push({ type: tool.name, args, timestamp: Date.now() });
        // Any mutating action may have changed the screen → invalidate the
        // cached UI tree so next find_element / get_ui_tree is fresh.
        ctx.invalidateUiCache();
      }
      ctx.lastToolName = tool.name;
      return { content: [{ type: "text", text: JSON.stringify(result, null, 2) }] };
    } catch (err) {
      const durationMs = Date.now() - startedAt;
      const message = err instanceof Error ? err.message : String(err);
      ctx.history.push({
        action: tool.name,
        args,
        status: "error",
        error: message,
        durationMs,
      });
      return {
        isError: true,
        content: [{ type: "text", text: message }],
      };
    }
  });

  return server;
}
