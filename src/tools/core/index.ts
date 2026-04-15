/**
 * Core strategy classes used by Tool implementations. Each class is an
 * injectable, unit-testable unit of business logic — Tool handlers orchestrate
 * them but do not contain the logic inline.
 *
 * Adding a new strategy: drop in a class, export it here, inject into the
 * relevant Tool via constructor.
 */
export { SelectorResolutionPipeline } from "./selector-resolution-pipeline.js";
export { ImeGeometricGuard } from "./ime-geometric-guard.js";
export {
  FuzzyResourceMatcher,
  type FuzzyMatch,
  type FuzzyMatchResult,
  type FuzzyAmbiguousResult,
  type FuzzyNoMatchResult,
} from "./fuzzy-resource-matcher.js";
export { AmbiguityDetector } from "./ambiguity-detector.js";
export { StructuralInputFinder, type InputMatch, type InputQuery } from "./structural-input-finder.js";
export {
  TransitionClassifier,
  type LoadingSignal,
  type OverlayAnalysis,
  type TargetStateChange,
  type TransitionDiagnostics,
} from "./transition-classifier.js";
export { UiTreeCache, type CachedDump } from "./ui-tree-cache.js";
