import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

/**
 * Monaco + monaco-yaml spawn web workers that Vite bundles as
 * separate entry points. `worker.format: "es"` keeps them as ES
 * modules so Vite can resolve their imports during dev.
 */
export default defineConfig({
  plugins: [react()],
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
  },
  worker: {
    format: "es",
  },
  build: {
    target: "es2022",
  },
  // mp4box ships CommonJS even under its `module` field, so force
  // Vite's dep-optimizer to pre-bundle it into an ES wrapper;
  // without this the browser hits `Can't find variable: module`
  // the first time the mirror panel mounts.
  optimizeDeps: {
    include: ["mp4box"],
  },
});
