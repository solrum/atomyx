/**
 * Mode C — exploratory testing agent loop.
 *
 * Architecture:
 *   - Anthropic SDK directly (not via MCP)
 *   - Reuses the in-process tool registry (src/registry.ts)
 *   - Uses prompt caching for the system prompt + tool definitions
 *   - Loops until: max steps reached, model returns no tool_use, or critical bug
 *
 * Run with: ANTHROPIC_API_KEY=... node dist/cli/main.js explore --app=... --goal=...
 */

import Anthropic from "@anthropic-ai/sdk";

import type { DeviceController } from "../adapters/device-controller.port.js";
import { buildToolRegistry } from "../registry.js";
import { createAtomyxContext } from "../runtime/atomyx-context.js";
import type { Bug, Finding } from "../state/results.js";
import type { JsonSchema } from "../types.js";
import { EXPLORER_SYSTEM_PROMPT } from "./system-prompt.js";

const MODEL = process.env.ATOMYX_EXPLORE_MODEL ?? "claude-sonnet-4-6";
const MAX_TOKENS = 4096;

// Tools the explorer is NOT allowed to call. These would distract from
// actual testing or duplicate functionality.
const FORBIDDEN_TOOLS = new Set([
  "list_devices",
  "select_device",
  "run_test_spec",
  "save_as_test_case",
  "start_test_session",
  "get_recorded_actions",
  "start_run",
  "finish_run",
]);

export interface ExplorationConfig {
  app: string;
  goal: string;
  maxSteps: number;
  earlyExitOnCritical?: boolean;
}

export interface ExplorationSummary {
  goal: string;
  steps: number;
  bugs: Bug[];
  findings: Finding[];
  durationMs: number;
  finalMessage?: string;
  stopReason: "max_steps" | "model_done" | "critical_bug" | "error";
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
}

function toAnthropicTool(t: { name: string; description: string; inputSchema: JsonSchema }) {
  return {
    name: t.name,
    description: t.description,
    input_schema: {
      type: "object" as const,
      properties: (t.inputSchema.properties ?? {}) as Record<string, unknown>,
      required: t.inputSchema.required ?? [],
    },
  };
}

function buildToolDefinitions(allTools: ReadonlyArray<{ name: string; description: string; inputSchema: JsonSchema }>) {
  return allTools
    .filter((t) => !FORBIDDEN_TOOLS.has(t.name))
    .map(toAnthropicTool);
}

function summarizeText(content: Anthropic.ContentBlock[]): string {
  return content
    .filter((b): b is Anthropic.TextBlock => b.type === "text")
    .map((b) => b.text)
    .join("\n");
}

export async function runExploration(
  ctl: DeviceController,
  config: ExplorationConfig,
): Promise<ExplorationSummary> {
  if (!process.env.ANTHROPIC_API_KEY) {
    throw new Error("ANTHROPIC_API_KEY env var is required for Mode C (exploratory).");
  }

  const client = new Anthropic();
  const startedAt = Date.now();

  // Build a private context + tool factory for this exploration. Pre-populate
  // with the controller so handlers can use it without needing select_device.
  const explorerCtx = createAtomyxContext();
  explorerCtx.controller = ctl;
  const factory = buildToolRegistry(explorerCtx);
  const allTools = factory.build();

  explorerCtx.history.start();
  explorerCtx.results.startRun({
    name: `explore: ${config.goal}`,
    source: "exploratory",
    deviceId: ctl.deviceId,
    platform: ctl.platform,
    meta: { app: config.app, goal: config.goal, maxSteps: config.maxSteps },
  });

  // Launch the app upfront so the model starts in a known state
  try {
    await ctl.launchApp(config.app);
    await new Promise((r) => setTimeout(r, 1500));
  } catch (err) {
    console.error(`[explorer] failed to launch ${config.app}:`, err);
  }

  const tools = buildToolDefinitions(allTools);
  const messages: Anthropic.MessageParam[] = [
    {
      role: "user",
      content: `Test this app exploratorily.

App: ${config.app}
Goal: ${config.goal}
Max tool calls: ${config.maxSteps}

Begin by calling get_ui_tree to see the current state, then proceed.`,
    },
  ];

  let totalInput = 0;
  let totalOutput = 0;
  let totalCacheRead = 0;
  let stopReason: ExplorationSummary["stopReason"] = "max_steps";
  let finalMessage: string | undefined;
  let toolCallCount = 0;

  for (let turn = 0; turn < 80; turn++) {
    if (toolCallCount >= config.maxSteps) {
      stopReason = "max_steps";
      // Inject a notice so the model produces a final summary
      messages.push({
        role: "user",
        content: "You have reached max_steps. Do not call any more tools — write your final SUMMARY now.",
      });
    }

    let response: Anthropic.Message;
    try {
      response = await client.messages.create({
        model: MODEL,
        max_tokens: MAX_TOKENS,
        system: [
          {
            type: "text",
            text: EXPLORER_SYSTEM_PROMPT,
            cache_control: { type: "ephemeral" },
          },
        ],
        tools: tools as any,
        messages,
      });
    } catch (err) {
      console.error("[explorer] API error:", err);
      stopReason = "error";
      break;
    }

    totalInput += response.usage.input_tokens;
    totalOutput += response.usage.output_tokens;
    totalCacheRead += (response.usage as any).cache_read_input_tokens ?? 0;

    // Append assistant message
    messages.push({ role: "assistant", content: response.content });

    const toolUses = response.content.filter(
      (b): b is Anthropic.ToolUseBlock => b.type === "tool_use",
    );

    if (toolUses.length === 0) {
      // Model finished — extract final summary
      finalMessage = summarizeText(response.content);
      stopReason = stopReason === "max_steps" ? "model_done" : stopReason;
      break;
    }

    // Execute each tool call
    const toolResults: Anthropic.ToolResultBlockParam[] = [];
    for (const tu of toolUses) {
      toolCallCount++;
      console.log(`[explorer] step ${toolCallCount}/${config.maxSteps}: ${tu.name}`);
      try {
        const tool = factory.byName(tu.name);
        if (!tool) throw new Error(`Unknown tool: ${tu.name}`);
        const result = await tool.handler((tu.input ?? {}) as Record<string, unknown>);
        toolResults.push({
          type: "tool_result",
          tool_use_id: tu.id,
          content: JSON.stringify(result).slice(0, 30000),  // cap large payloads
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        toolResults.push({
          type: "tool_result",
          tool_use_id: tu.id,
          content: `ERROR: ${message}`,
          is_error: true,
        });
      }

      // Early exit on critical bug
      if (config.earlyExitOnCritical) {
        const bugs = explorerCtx.results.currentRun()?.bugs ?? [];
        if (bugs.some((b) => b.severity === "critical")) {
          stopReason = "critical_bug";
          break;
        }
      }
    }

    messages.push({ role: "user", content: toolResults });

    if (stopReason === "critical_bug") break;
  }

  explorerCtx.results.finishRun(stopReason === "error" ? "error" : "passed");
  const path = explorerCtx.results.persistLocal();
  console.log(`[explorer] result saved to ${path}`);

  const run = explorerCtx.results.currentRun()!;
  return {
    goal: config.goal,
    steps: toolCallCount,
    bugs: run.bugs,
    findings: run.findings,
    durationMs: Date.now() - startedAt,
    finalMessage,
    stopReason,
    inputTokens: totalInput,
    outputTokens: totalOutput,
    cacheReadTokens: totalCacheRead,
  };
}
