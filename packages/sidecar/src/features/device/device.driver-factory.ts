import type { Driver, Clock, Logger } from "@atomyx/driver";
import { Orchestra, SystemClock } from "@atomyx/driver";
import { AndroidDriver } from "@atomyx/android-driver";
import { IosDriver } from "@atomyx/ios-driver";
import {
  createIosSimDriver,
  isSimDirectSupported,
} from "@atomyx/ios-sim-driver";
import type { DeviceDescriptor } from "./device.types.js";
import type { IosAgentService } from "../ios-agent/index.js";

export interface DriverFactoryDeps {
  readonly clock?: Clock;
  readonly logger?: Logger;
  /** Optional ios-agent service. When provided, the factory calls
   *  startSimHid for simulator targets that qualify for the HID path
   *  and falls back to IosDriver when the helper fails to start. */
  readonly iosAgentService?: IosAgentService;
}

/**
 * Maps a DeviceDescriptor to a live Driver + Orchestra pair.
 *
 * Single responsibility: pick the right concrete driver based on
 * platform, configure it from the descriptor, and return an
 * Orchestra ready for Finder / ScrollController. Holds no state
 * itself — a fresh pair is produced each call.
 *
 * The factory is testable: inject a custom clock / logger via ctor.
 * Swap entire families (Android → WebDriver) by editing this file;
 * DeviceService does not know a new family exists.
 */
export class DriverFactory {
  private readonly clock: Clock;
  private readonly logger: Logger | undefined;
  private readonly iosAgentService: IosAgentService | undefined;

  constructor(deps: DriverFactoryDeps = {}) {
    this.clock = deps.clock ?? new SystemClock();
    this.logger = deps.logger;
    this.iosAgentService = deps.iosAgentService;
  }

  async build(device: DeviceDescriptor): Promise<{
    readonly driver: Driver;
    readonly orchestra: Orchestra;
  }> {
    const driver = await this.createDriver(device);
    await driver.connect();
    const orchestra = new Orchestra({
      driver,
      clock: this.clock,
      logger: this.logger,
    });
    return { driver, orchestra };
  }

  private async createDriver(device: DeviceDescriptor): Promise<Driver> {
    if (device.platform === "android") {
      return new AndroidDriver({ serial: device.id });
    }

    const iosOpts = {
      udid: device.id,
      kind: device.kind === "simulator" ? ("simulator" as const) : ("device" as const),
      devTeam: process.env.ATOMYX_DEV_TEAM,
      projectDir: process.env.ATOMYX_IOS_DRIVER_DIR,
    };

    const simHidOptIn = process.env.ATOMYX_SIM_HID === "1";
    const simHidSupported = isSimDirectSupported();
    process.stderr.write(
      `[driver-factory] createDriver platform=${device.platform} kind=${device.kind} optIn=${simHidOptIn} supported=${simHidSupported} hasAgentService=${!!this.iosAgentService}\n`,
    );

    // For iOS Simulator targets, take the direct-HID path when the
    // user opts in with `ATOMYX_SIM_HID=1` AND the host meets the
    // capability floor (arm64 + verified Xcode). Default is the
    // XCUITest path. For physical device targets the XCUITest path
    // is always used — the HID adapter is simulator-only.
    if (
      device.kind === "simulator" &&
      simHidOptIn &&
      simHidSupported
    ) {
      if (this.iosAgentService) {
        try {
          const hidPort = await this.iosAgentService.startSimHid(device.id);
          process.stderr.write(
            `[driver-factory] sim-hid engaged for ${device.id} on port ${hidPort}\n`,
          );
          return createIosSimDriver({ ...iosOpts, hidPort });
        } catch (err) {
          const msg = err instanceof Error ? err.message : String(err);
          process.stderr.write(
            `[driver-factory] sim-hid start failed for ${device.id}, falling back to XCUITest: ${msg}\n`,
          );
        }
      } else {
        process.stderr.write(
          `[driver-factory] sim-hid opt-in set but iosAgentService missing for ${device.id}, falling back to XCUITest\n`,
        );
      }
    }
    return new IosDriver(iosOpts);
  }
}
