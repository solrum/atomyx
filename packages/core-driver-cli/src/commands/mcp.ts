import { Orchestra, SystemClock, ConsoleLogger, type Driver } from "@atomyx/core-driver";
import { IosDriver } from "@atomyx/core-driver-ios";
import { AndroidDriver } from "@atomyx/core-driver-android";
import { createMcpServer } from "@atomyx/core-driver-mcp";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import type { ParsedArgv } from "../argv.js";

/**
 * `atomyx-driver mcp` — start the MCP stdio server.
 *
 * Wiring sequence (the canonical example for any feature
 * consumer that wants to embed Atomyx as an MCP server):
 *
 *   1. Construct the platform-specific Driver from the parsed
 *      argv. iOS gets `IosDriver({kind, udid, port?})`,
 *      Android gets `AndroidDriver({serial})`. The Driver
 *      interface from `@atomyx/core-driver` hides the difference
 *      from the rest of the pipeline.
 *
 *   2. Connect the driver. For iOS device this spawns iproxy +
 *      handshakes the Swift runner; for iOS simulator it just
 *      opens the loopback TCP; for Android it runs adb forward
 *      + pings the APK's HTTP server.
 *
 *   3. Construct an `Orchestra` injecting the driver, a real
 *      `SystemClock`, and a stderr-bound `ConsoleLogger` at the
 *      requested level. Orchestra is the cross-platform brain
 *      that runs selector resolution, scroll-into-view,
 *      obscurement detection, etc.
 *
 *   4. Hand the orchestra to `createMcpServer`. The factory
 *      returns a configured `@modelcontextprotocol/sdk` Server
 *      with all the default tools registered (launch_app,
 *      tap, find_element, ...).
 *
 *   5. Connect the server to a `StdioServerTransport`. The
 *      function then awaits indefinitely — stdio close
 *      (parent process exit) is the shutdown signal. Any
 *      thrown error during boot propagates to main.ts which
 *      prints + exits non-zero.
 *
 * Graceful shutdown: we install SIGINT / SIGTERM handlers
 * that disconnect the driver before exiting so iproxy /
 * adb forward / open sockets get cleaned up. Without these,
 * Ctrl+C in a serving terminal leaves zombies.
 */
export async function runMcp(argv: ParsedArgv): Promise<void> {
  const driver = createDriver(argv);
  const logger = new ConsoleLogger(argv.logLevel ?? "info");

  logger.info("connecting driver", {
    platform: driver.platform,
    kind: argv.kind,
    device: argv.device,
  });
  await driver.connect();
  logger.info("driver connected");

  // Install shutdown handlers BEFORE wiring the MCP server so
  // a Ctrl+C during boot still cleans up the iproxy/adb tunnel.
  const shutdown = async (signal: string) => {
    logger.info("shutting down", { signal });
    try {
      await driver.disconnect();
    } catch (err) {
      logger.warn("driver disconnect failed", {
        error: (err as Error).message,
      });
    }
    process.exit(0);
  };
  process.on("SIGINT", () => void shutdown("SIGINT"));
  process.on("SIGTERM", () => void shutdown("SIGTERM"));

  const orchestra = new Orchestra({
    driver,
    clock: new SystemClock(),
    logger,
  });

  const server = createMcpServer({ orchestra, logger });
  const transport = new StdioServerTransport();

  logger.info("MCP server starting on stdio");
  await server.connect(transport);
  // Server runs until stdin closes. server.connect resolves
  // when the connection is established; the transport keeps
  // the event loop alive until peer disconnect. No further
  // code path needed — just let the process idle.
}

function createDriver(argv: ParsedArgv): Driver {
  if (argv.platform === "ios") {
    return new IosDriver({
      kind: argv.kind ?? "simulator",
      udid: argv.device ?? "",
      port: argv.port,
    });
  }
  if (argv.platform === "android") {
    return new AndroidDriver({
      serial: argv.device!,
    });
  }
  // argv parser rejects this case — defensive throw for type narrowing.
  throw new Error("internal: createDriver called without a platform");
}
