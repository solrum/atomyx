import { isScenarioYaml } from "@atomyx/script";
import type { Dispatcher } from "../../infra/transport/dispatcher.js";
import { DispatcherError } from "../../infra/transport/dispatcher.js";
import type { ScriptRunnerService } from "./script.service.js";

export function registerScriptHandlers(
  dispatcher: Dispatcher,
  service: ScriptRunnerService,
): void {
  dispatcher.register("runScript", async (params) => {
    const yaml = (params as { yaml?: unknown })?.yaml;
    if (typeof yaml !== "string" || yaml.length === 0) {
      throw new DispatcherError(
        "InvalidParams",
        "params.yaml must be a non-empty string",
      );
    }
    if (isScenarioYaml(yaml)) {
      const cwd = (params as { cwd?: unknown })?.cwd;
      if (typeof cwd !== "string" || cwd.length === 0) {
        throw new DispatcherError(
          "InvalidParams",
          "scenario runs require params.cwd (directory of the scenario file)",
        );
      }
      return service.runScenario(yaml, cwd);
    }
    return service.run(yaml);
  });
  dispatcher.register("stopScript", () => {
    service.stop();
    return null;
  });
  dispatcher.register("isRunning", () => ({ running: service.isRunning() }));
}
