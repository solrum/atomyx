import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { AtomyxContext } from "../runtime/atomyx-context.js";
import { requireController } from "../runtime/atomyx-context.js";
import type { JsonSchema } from "../types.js";
import { Tool } from "./core/tool.js";

export class GetScreenshotTool extends Tool<{
  args: Record<string, never>;
  result: { path: string; bytes: number; format: "png" };
}> {
  readonly name = "get_screenshot";
  readonly description =
    "Capture screenshot to .atomyx/screenshots/ (or $ATOMYX_SCREENSHOT_DIR). Returns path + size.";
  readonly schema: JsonSchema = { type: "object", properties: {} };

  async execute(_args: Record<string, never>, ctx: AtomyxContext) {
    const { base64 } = await requireController(ctx).screenshot();
    const buf = Buffer.from(base64, "base64");
    const dir =
      process.env.ATOMYX_SCREENSHOT_DIR ?? join(process.cwd(), ".atomyx", "screenshots");
    mkdirSync(dir, { recursive: true });
    const path = join(dir, `screenshot-${Date.now()}.png`);
    writeFileSync(path, buf);
    return { path, bytes: buf.length, format: "png" as const };
  }
}
