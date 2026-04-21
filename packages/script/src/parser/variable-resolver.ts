/**
 * Resolve `${varName}` placeholders in a value against an env
 * map. Operates recursively on strings, arrays, and object
 * values. Runs before Zod validation so resolved strings flow
 * through type checking.
 *
 * Unresolved variables are left as-is (not an error) — the
 * runner will catch missing variables at execution time if
 * they matter.
 */
export function resolveVariables<T>(
  value: T,
  env: Readonly<Record<string, string>>,
): T {
  if (typeof value === "string") {
    return value.replace(/\$\{(\w+)\}/g, (_, key: string) => {
      return env[key] ?? `\${${key}}`;
    }) as T;
  }
  if (Array.isArray(value)) {
    return value.map((item) => resolveVariables(item, env)) as T;
  }
  if (typeof value === "object" && value !== null) {
    const resolved: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value)) {
      resolved[k] = resolveVariables(v, env);
    }
    return resolved as T;
  }
  return value;
}
