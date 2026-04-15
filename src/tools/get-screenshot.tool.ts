import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { AdetContext } from "../runtime/adet-context.js";
import { requireController } from "../runtime/adet-context.js";
import type { JsonSchema } from "../types.js";
import { Tool } from "./core/tool.js";

export class GetScreenshotTool extends Tool<{
  args: Record<string, never>;
  result: { path: string; bytes: number; format: "png" };
}> {
  readonly name = "get_screenshot";
  readonly description =
    "Capture screenshot to .adet/screenshots/ (or $ADET_SCREENSHOT_DIR). Returns path + size.";
  readonly schema: JsonSchema = { type: "object", properties: {} };

  async execute(_args: Record<string, never>, ctx: AdetContext) {
    const { base64 } = await requireController(ctx).screenshot();
    const buf = Buffer.from(base64, "base64");
    const dir =
      process.env.ADET_SCREENSHOT_DIR ?? join(process.cwd(), ".adet", "screenshots");
    mkdirSync(dir, { recursive: true });
    const path = join(dir, `screenshot-${Date.now()}.png`);
    writeFileSync(path, buf);
    return { path, bytes: buf.length, format: "png" as const };
  }
}
