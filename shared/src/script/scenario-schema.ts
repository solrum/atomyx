import { z } from "zod";

/**
 * Single source of truth for the shape of every YAML scenario file
 * Atomyx accepts. A scenario is an ordered group of independent
 * scripts run as one unit — each script has its own clean execution
 * context (variables, captures, artifacts) and produces its own
 * result.
 *
 * Distinguishes from `requires` / `runFlow` (intra-script
 * composition that merges contexts and produces one result) by
 * giving each child script a fresh runner instance and reporting N
 * results aggregated into a `ScenarioResult`.
 *
 * Scenario file extension: `.scenario.yml`. Studio's editor uses
 * the extension to dispatch to the scenario validator instead of
 * the script validator.
 */

const ScenarioFormatSchema = z
  .string()
  .regex(
    /^atomyx-scenario\/v\d+$/,
    "format must match 'atomyx-scenario/v<n>' (e.g. 'atomyx-scenario/v1')",
  )
  .describe(
    "Scenario format version. Format: 'atomyx-scenario/v<n>'. " +
      "Current stable: 'atomyx-scenario/v1'. Omit to default to the stable version.",
  );

export const SCENARIO_DEFAULTS = {
  format: "atomyx-scenario/v1",
  onFailure: "stop",
} as const;

export const ScenarioDefinitionSchema = z
  .object({
    format: ScenarioFormatSchema.optional().describe(
      "Scenario format version. Defaults to " +
        `'${SCENARIO_DEFAULTS.format}' when omitted.`,
    ),
    name: z
      .string()
      .min(1)
      .describe(
        "Human-readable scenario name. Shown in run reports and the " +
          "Studio scenario view.",
      ),
    description: z
      .string()
      .optional()
      .describe(
        "Longer description of what this scenario verifies. Surfaced " +
          "in reports and the Studio detail view.",
      ),
    scripts: z
      .array(z.string().min(1))
      .min(1)
      .describe(
        "Ordered list of script paths to run. Paths are resolved " +
          "relative to the scenario file. Each script runs in its " +
          "own clean execution context (no shared variables / " +
          "captures). Example: ['flows/login.yml', 'flows/checkout.yml'].",
      ),
    onFailure: z
      .enum(["stop", "continue"])
      .optional()
      .describe(
        "Behaviour when a script in the scenario fails. " +
          "`stop` (default) aborts the scenario and marks remaining " +
          "scripts as skipped. `continue` runs every script regardless " +
          "and reports an aggregate result — useful when surfacing " +
          "all failures in one regression sweep.",
      ),
    env: z
      .record(z.string(), z.string())
      .optional()
      .describe(
        "Variables exposed to every child script. A child script's " +
          "own `env` overrides scenario `env` per-key, so scripts " +
          "remain runnable standalone with the same variable names.",
      ),
    tags: z
      .array(z.string().min(1))
      .optional()
      .describe(
        "Tags for filtering and organizing scenarios. Examples: " +
          "'smoke', 'regression', 'release-candidate'.",
      ),
  })
  .strict();

export type ScenarioDefinition = z.infer<typeof ScenarioDefinitionSchema>;
