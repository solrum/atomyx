#!/usr/bin/env node
import { composeSidecar } from "./compose.js";

export * from "./compose.js";
export * from "./infra/index.js";
export * from "./features/device/index.js";
export * from "./features/app/index.js";
export * from "./features/script/index.js";
export * from "./features/inspection/index.js";
export * from "./features/meta/index.js";

/**
 * Default entrypoint for ESM imports. The bundled binary uses
 * `cli.ts` so it never has to evaluate `import.meta.url`.
 */
function isDirectRun(): boolean {
  if (!process.argv[1]) return false;
  const url = new URL(import.meta.url);
  return url.pathname === process.argv[1] || url.pathname.endsWith(process.argv[1]);
}

if (isDirectRun()) {
  const handle = composeSidecar({
    input: process.stdin,
    output: process.stdout,
    onError: (err) => {
      process.stderr.write(
        `${JSON.stringify({
          event: "sidecarError",
          payload: {
            message: err instanceof Error ? err.message : String(err),
          },
        })}\n`,
      );
    },
  });
  handle.start();
  const shutdown = async () => {
    await handle.dispose();
    process.exit(0);
  };
  process.on("SIGINT", () => void shutdown());
  process.on("SIGTERM", () => void shutdown());
}
