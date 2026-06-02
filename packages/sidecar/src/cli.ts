/**
 * Unconditional entrypoint for the bundled sidecar. `src/index.ts`
 * also contains a direct-run check so ESM consumers can import its
 * exports without side effects; the bundled CJS artifact uses this
 * file instead to avoid `import.meta.url` at runtime.
 */
import { composeSidecar } from "./compose.js";

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
