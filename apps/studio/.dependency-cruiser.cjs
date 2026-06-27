/**
 * Studio-scoped dependency rules. Scope is `apps/studio/` only —
 * this config does NOT affect the repo-root `.dependency-cruiser.cjs`
 * or any other package's tooling.
 *
 * Enforces the four-layer architecture described in
 * `.claude/rules/studio-architecture.md`.
 */
module.exports = {
  forbidden: [
    {
      name: "ui-not-imported-by-lower-layers",
      severity: "error",
      comment:
        "ui/ is the top layer — state/, domain/, platform/ must not depend on it.",
      from: { path: "^src/(domain|state|platform)" },
      to: { path: "^src/ui" },
    },
    {
      name: "state-does-not-import-ui-or-platform",
      severity: "error",
      comment:
        "state/ holds references to domain services injected at startup. It must not import ui/ or platform/ directly.",
      from: { path: "^src/state" },
      to: { path: "^src/(ui|platform)" },
    },
    {
      name: "domain-does-not-import-any-other-layer",
      severity: "error",
      comment:
        "domain/ is the pure-TypeScript core. It must not depend on ui/, state/, or platform/.",
      from: { path: "^src/domain" },
      to: { path: "^src/(ui|state|platform)" },
    },
    {
      name: "platform-does-not-import-ui-or-state",
      severity: "error",
      comment:
        "platform/ implements domain ports against Tauri/OS APIs. It must not depend on ui/ or state/.",
      from: { path: "^src/platform" },
      to: { path: "^src/(ui|state)" },
    },
    {
      name: "cross-feature-imports-go-through-index",
      severity: "error",
      comment:
        "Within a layer, features/<A> may import features/<B> ONLY via features/<B>/index.ts. Reaching internal files of another feature bypasses its public surface. Tests are exempt — they legitimately reach mocks and ports for white-box testing.",
      from: {
        path: "^src/(domain|state|platform|ui)/features/([^/]+)/",
        pathNot: "\\.test\\.(ts|tsx)$",
      },
      to: {
        path: "^src/(domain|state|platform|ui)/features/([^/]+)/(?!index\\.(ts|js)$).+",
        pathNot: "^src/(domain|state|platform|ui)/features/$2/",
      },
    },
    {
      name: "primitives-do-not-reach-feature-internals",
      severity: "error",
      comment:
        "ui/primitives holds generic building blocks that must stay below features in the dependency graph. Reaching a feature's internals would invert the direction. Shell is exempt because it IS the composition root.",
      from: { path: "^src/ui/primitives/" },
      to: {
        path: "^src/ui/features/",
      },
    },
    {
      name: "feature-impl-is-private-cross-feature",
      severity: "error",
      comment:
        "Concrete feature implementations (*.zustand.ts, *.tauri.ts, *.fs.ts, *.node.ts, *.impl.ts) are private. A file inside features/<A> must not reach another feature's impl files — go through features/<B>/index.ts and take only the contract type + factory.",
      from: {
        path: "^src/(domain|state|platform|ui)/features/([^/]+)/",
        pathNot: "\\.test\\.(ts|tsx)$",
      },
      to: {
        path: "^src/(domain|state|platform|ui)/features/[^/]+/.+\\.(zustand|tauri|fs|node|impl|muxjs|scrcpy|simctl|coremedia)\\.(ts|js)$",
        pathNot: "^src/$1/features/$2/",
      },
    },
    {
      name: "feature-impl-is-private-outside-features",
      severity: "error",
      comment:
        "Only the composition root (main.tsx) may reach into a feature's impl file. Every other consumer outside features/ must go through features/<X>/index.ts. Tests are exempt — they legitimately white-box their own feature.",
      from: {
        path: "^src/",
        pathNot: [
          "^src/main\\.tsx$",
          "^src/platform/index\\.ts$",
          "^src/(domain|state|platform|ui)/features/",
          "\\.test\\.(ts|tsx)$",
        ],
      },
      to: {
        path: "^src/(domain|state|platform|ui)/features/[^/]+/.+\\.(zustand|tauri|fs|node|impl|muxjs|scrcpy|simctl|coremedia)\\.(ts|js)$",
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
