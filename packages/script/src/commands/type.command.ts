import { defineCommand } from "@atomyx/driver/script";
import type { TypeStep } from "@atomyx/shared/script";
import { compileScriptSelector } from "../parser/selector-compiler.js";

export const typeCommand = defineCommand<TypeStep>({
  command: "type",
  async execute(args, ctx) {
    if (args.into) {
      // Type into a specific field: tap + inputText
      const selector = compileScriptSelector(args.into);
      const result = await ctx.orchestra.inputText(selector, args.text);
      if (!result.ok) {
        return { ok: false, detail: result.reason };
      }
      return { ok: true, detail: `Typed "${args.text}"` };
    }
    // Bare type: input into currently focused field
    await ctx.orchestra.typeText(args.text);
    return { ok: true, detail: `Typed "${args.text}" into focused field` };
  },
});
