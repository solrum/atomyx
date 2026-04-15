/**
 * Tool registry — class-based throughout.
 *
 * The registry instantiates:
 *   1. Core strategies (SelectorResolutionPipeline, ImeGeometricGuard,
 *      FuzzyResourceMatcher, AmbiguityDetector, StructuralInputFinder,
 *      TransitionClassifier, UiTreeCache) — shared across tools.
 *   2. Concrete Tool classes — constructor-injected with the strategies
 *      they need.
 *   3. Class instances registered via `factory.registerTool(instance)`.
 *
 * Adding a new tool = drop in a new `<name>.tool.ts` that extends
 * `Tool<TShape>`, construct it here, call `registerTool`.
 */

import type { AtomyxContext } from "./runtime/atomyx-context.js";
import {
  AmbiguityDetector,
  FuzzyResourceMatcher,
  ImeGeometricGuard,
  SelectorResolutionPipeline,
  StructuralInputFinder,
  TransitionClassifier,
  UiTreeCache,
} from "./tools/core/index.js";
import { ListDevicesTool, SelectDeviceTool } from "./tools/devices.tool.js";
import { FindElementTool } from "./tools/find-element.tool.js";
import { GetScreenshotTool } from "./tools/get-screenshot.tool.js";
import { GetUiTreeTool } from "./tools/get-ui-tree.tool.js";
import { InputTextTool } from "./tools/input-text.tool.js";
import { LaunchAppTool } from "./tools/launch-app.tool.js";
import {
  AddCaseStudyTool,
  GetCaseStudiesTool,
  GetPlaybookTool,
} from "./tools/playbook-tools.js";
import { ReportBugTool } from "./tools/report-bug.tool.js";
import { TapAndWaitTransitionTool } from "./tools/tap-and-wait-transition.tool.js";
import { TapTool } from "./tools/tap.tool.js";
import { ToolFactory } from "./tools/tool-factory.js";
import {
  FinishRunTool,
  ListAppsTool,
  PressKeyTool,
  StartRunTool,
  SwipeTool,
} from "./tools/trivial.tools.js";
import { WaitForElementTool } from "./tools/wait-for-element.tool.js";

export function buildToolRegistry(ctx: AtomyxContext): ToolFactory {
  const factory = new ToolFactory(ctx);

  // ── Strategy singletons ─────────────────────────────────────────
  const selectorPipeline = new SelectorResolutionPipeline();
  const imeGuard = new ImeGeometricGuard();
  const fuzzyMatcher = new FuzzyResourceMatcher();
  const ambiguity = new AmbiguityDetector();
  const inputFinder = new StructuralInputFinder();
  const transitionClassifier = new TransitionClassifier();
  const uiTreeCache = new UiTreeCache();

  // Mutating tools invalidate the cache so the next read is fresh.
  ctx.invalidateUiCache = () => uiTreeCache.invalidate();

  // ── Devices ─────────────────────────────────────────────────────
  factory.registerTool(new ListDevicesTool());
  factory.registerTool(new SelectDeviceTool());

  // ── App ─────────────────────────────────────────────────────────
  factory.registerTool(new LaunchAppTool(inputFinder));
  factory.registerTool(new ListAppsTool());

  // ── Screen ──────────────────────────────────────────────────────
  factory.registerTool(new GetUiTreeTool(uiTreeCache, ambiguity));
  factory.registerTool(new FindElementTool(uiTreeCache, inputFinder, fuzzyMatcher));
  factory.registerTool(new GetScreenshotTool());

  // ── Actions ─────────────────────────────────────────────────────
  factory.registerTool(new TapTool(selectorPipeline, imeGuard, fuzzyMatcher));
  factory.registerTool(new TapAndWaitTransitionTool(transitionClassifier));
  factory.registerTool(new InputTextTool(inputFinder));
  factory.registerTool(new SwipeTool());
  factory.registerTool(new PressKeyTool());

  // ── Wait ────────────────────────────────────────────────────────
  factory.registerTool(new WaitForElementTool());

  // ── Run / reporting ─────────────────────────────────────────────
  factory.registerTool(new StartRunTool());
  factory.registerTool(new FinishRunTool());
  factory.registerTool(new ReportBugTool());

  // ── Guidance / playbook ─────────────────────────────────────────
  factory.registerTool(new GetPlaybookTool());
  factory.registerTool(new AddCaseStudyTool());
  factory.registerTool(new GetCaseStudiesTool());

  return factory;
}

/**
 * Tools that mutate device state. Used by server.ts to know which tool
 * calls should be appended to the recorded action log.
 */
export const MUTATING_TOOLS = new Set([
  "tap",
  "tap_and_wait_transition",
  "swipe",
  "input_text",
  "press_key",
  "launch_app",
]);
