// Re-export shared cross-module types so consumers can import
// from either @atomyx/shared/script or @atomyx/driver/script.
export type {
  ScriptDefinition,
  ScriptStep,
  ScriptSelector,
  NetworkCapture,
  CapturedRequest,
  ScriptArtifacts,
} from "@atomyx/shared/script";

// Execution contracts — depend on Orchestra (from driver) +
// Clock/Logger (from core), so they live here, not in shared.
export * from "./command-definition.js";
