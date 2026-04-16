import { z } from "zod";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { defineTool, orchestraOrFail } from "../tool-definition.js";

const ScreenshotArgs = z.object({}).strict();

/**
 * Detect file extension from the first bytes of a buffer.
 * JPEG starts with FF D8, PNG starts with 89 50 4E 47.
 */
function detectExt(bytes: Uint8Array): string {
  if (bytes[0] === 0xff && bytes[1] === 0xd8) return "jpg";
  return "png";
}

/**
 * `screenshot` — saves a compressed JPEG to
 * `.atomyx/screenshots/` in the working directory and returns
 * the file path. The calling agent (Claude Code) can read the
 * image directly via its Read tool without extra permissions
 * because the file lives inside the project tree.
 */
export const screenshotTool = defineTool({
  name: "screenshot",
  description:
    "LAST RESORT — only use when get_ui_tree cannot answer your question " +
    "(e.g. custom-rendered canvas, Flutter without semantics, visual " +
    "layout validation, or when you suspect the UI tree is missing " +
    "elements). Saves a compressed JPEG to .atomyx/screenshots/ and " +
    "returns the file path. Read the file to view. Always try " +
    "get_ui_tree or find_element FIRST — they are faster, cheaper, " +
    "and give you actionable selectors directly.",
  inputSchema: ScreenshotArgs,
  async execute(_args, ctx) {
    const orchestra = orchestraOrFail(ctx);
    const bytes = await orchestra.screenshot();
    const ext = detectExt(bytes);
    const dir = join(process.cwd(), ".atomyx", "screenshots");
    mkdirSync(dir, { recursive: true });
    const filename = `screenshot-${Date.now()}.${ext}`;
    const filepath = join(dir, filename);
    writeFileSync(filepath, bytes);
    return {
      path: filepath,
      sizeBytes: bytes.length,
      format: ext,
    };
  },
});
