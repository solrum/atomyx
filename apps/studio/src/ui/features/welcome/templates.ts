export interface ScriptTemplate {
  readonly id: string;
  readonly label: string;
  readonly description: string;
  readonly render: (ctx: TemplateContext) => string;
}

export interface TemplateContext {
  readonly appId: string;
  readonly name: string;
  readonly tags: readonly string[];
}

function header(ctx: TemplateContext): string {
  const tagsLine =
    ctx.tags.length > 0 ? `tags: [${ctx.tags.map((t) => `"${t}"`).join(", ")}]\n` : "";
  return `appId: ${ctx.appId}
name: ${ctx.name}
${tagsLine}env: {}
---
`;
}

/**
 * Starter templates offered by the "New test" wizard. Expand this
 * list when a new shape proves itself useful across several
 * manually-authored scripts — not before.
 */
export const SCRIPT_TEMPLATES: readonly ScriptTemplate[] = [
  {
    id: "blank",
    label: "Blank",
    description: "Minimal skeleton with a launch step.",
    render: (ctx) => `${header(ctx)}- launchApp
`,
  },
  {
    id: "login-flow",
    label: "Login flow",
    description:
      "Type an email and password, tap a submit button, wait for a landing screen.",
    render: (ctx) => `${header(ctx)}- launchApp
- tap: "Sign in"
- type:
    into: "Email"
    text: user@test.com
- type:
    into: "Password"
    text: secret123
- tap: "Continue"
- waitFor:
    text: "Welcome"
    timeout: 10000
- screenshot: home
`,
  },
  {
    id: "search-flow",
    label: "Search flow",
    description: "Launch, tap search, type a query, assert a result row.",
    render: (ctx) => `${header(ctx)}- launchApp
- tap: "Search"
- type: "coffee"
- waitFor:
    text: "Results"
- assertVisible: "coffee"
- screenshot: search_results
`,
  },
];
