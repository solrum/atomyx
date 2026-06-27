/**
 * Sidecar-scoped dependency rules. Enforces the feature-sliced
 * layout described in `.claude/rules/feature-structure.md`.
 *
 *   - infra/ is leaf — it may not depend on features/.
 *   - features/<A> may depend on features/<B> only via
 *     features/<B>/index.ts (public surface). Reaching internal
 *     files like features/<B>/b.service.ts is forbidden.
 *   - testing/ is infra-like — may not depend on features/.
 *
 * These rules are additive to the repo-wide rules (if any) and
 * only apply when running depcruise inside this package.
 */
module.exports = {
  forbidden: [
    {
      name: "infra-does-not-import-features",
      severity: "error",
      comment:
        "infra/ is the pure cross-cutting layer. Depending on features would couple the framework to business code and reverse the intended direction.",
      from: { path: "^src/infra" },
      to: { path: "^src/features" },
    },
    {
      name: "testing-does-not-import-features",
      severity: "error",
      comment:
        "testing/ provides generic mocks (StaticProbe, MockDriverFactory). It may consume types from features, but runtime behaviour from features must not leak into shared mocks.",
      from: { path: "^src/testing" },
      to: {
        path: "^src/features/[^/]+/(?!.*\\.types\\.(ts|js)$)(?!index\\.(ts|js)$).*",
      },
    },
    {
      name: "cross-feature-imports-go-through-index",
      severity: "error",
      comment:
        "features/<A> may import features/<B> ONLY via features/<B>/index.ts. Reaching internal files of another feature bypasses its public surface and produces brittle coupling.",
      from: { path: "^src/features/([^/]+)/" },
      to: {
        path: "^src/features/([^/]+)/(?!index\\.(ts|js)$).+",
        pathNot: "^src/features/$1/",
      },
    },
    {
      name: "no-circular",
      severity: "error",
      comment: "Circular dependencies forbidden at every boundary.",
      from: {},
      to: { circular: true },
    },
  ],
  options: {
    doNotFollow: { path: "node_modules" },
    tsConfig: { fileName: "tsconfig.json" },
    tsPreCompilationDeps: true,
    enhancedResolveOptions: {
      exportsFields: ["exports"],
      conditionNames: ["import", "require", "node", "default"],
    },
  },
};
