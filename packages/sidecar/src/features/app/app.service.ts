import type { Session } from "../../infra/session/session.js";
import type { AppDescriptor } from "./app.types.js";

export interface AppServiceDeps {
  readonly session: Session;
}

/**
 * App-lifecycle operations scoped to the currently selected device.
 *
 * Refuses every call without a selected device — keeps AppService
 * free of "what device?" branching. The caller (JSON-RPC handler)
 * is responsible for surfacing the error code.
 */
export class AppService {
  private readonly session: Session;

  constructor(deps: AppServiceDeps) {
    this.session = deps.session;
  }

  async list(): Promise<readonly AppDescriptor[]> {
    const driver = this.driver();
    const apps = await driver.listApps();
    return apps.map((a) => ({
      bundleId: a.bundleId,
      displayName: a.displayName,
    }));
  }

  async launch(
    bundleId: string,
    opts?: {
      readonly args?: readonly string[];
      readonly env?: Readonly<Record<string, string>>;
      readonly noReset?: boolean;
    },
  ): Promise<void> {
    const driver = this.driver();
    await driver.launchApp(bundleId, {
      args: opts?.args,
      environment: opts?.env,
      noReset: opts?.noReset,
    });
  }

  async currentForeground(): Promise<{
    readonly bundleId: string | null;
    readonly activity?: string;
  }> {
    const driver = this.driver();
    return driver.currentForeground();
  }

  private driver() {
    return this.session.requireDevice().driver;
  }
}
