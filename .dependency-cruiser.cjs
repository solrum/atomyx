/**
 * Atomyx dependency-cruiser config
 *
 * Enforces the cross-package boundary rules from ARCHITECTURE.md
 * §4 — packages may import each other ONLY through public entry
 * points (the package's `index.ts` re-exports). Reaching into
 * another package's internal source files is forbidden.
 *
 * Module ownership is encoded in package NAME PREFIXES:
 *
 *   core-driver-*  → "core-driver" module (Persona: Pure Developer)
 *   test-mgmt-*    → "test-mgmt" module   (Persona: QC Manager)
 *   studio-*       → "studio" module      (Persona: Power User)
 *   cloud-*        → "cloud" module       (Persona: Scale Operator)
 *
 * The cross-module rule (a stricter form of the cross-package
 * rule) says: packages from one module may only import the PUBLIC
 * MAIN package of another module — never sibling sub-packages.
 * E.g. `studio-desktop` may import `@atomyx/test-mgmt` (the main)
 * but not `@atomyx/test-mgmt-storage-file` (an internal of the
 * test-mgmt module).
 *
 * Currently runs in WARN mode for the cross-module rule, ERROR
 * mode for the cross-package deep-import rule. After a baseline
 * scan with no violations, flip the warn rules to error.
 *
 * Usage:
 *
 *   npx depcruise --config .dependency-cruiser.cjs packages/
 */
module.exports = {
  forbidden: [
    {
      name: "no-cross-package-deep-imports",
      severity: "error",
      comment:
        "Packages may only import each other's public entry points " +
        "(@atomyx/<package>) — never reach into another package's " +
        "src/ paths. Use the exported API surface.",
      from: { path: "^packages/([^/]+)/" },
      to: {
        // The negative lookahead on `\\1` confines this rule to
        // CROSS-package imports: same-package imports are allowed
        // to deep-link freely. Without it, an intra-package
        // import of `./foo.ts` would also trip the rule.
        path: "^packages/(?!\\1)([^/]+)/src/",
        pathNot: "/index\\.ts$",
      },
    },
    {
      name: "no-relative-cross-package",
      severity: "error",
      comment:
        "Cross-package imports MUST go through @atomyx/<package> " +
        "package names, not relative paths. Relative ../ paths " +
        "across package directories bypass the boundary entirely.",
      from: { path: "^packages/([^/]+)/" },
      to: {
        path: "^packages/(?!\\1)([^/]+)/",
      },
    },
    {
      name: "no-cross-module-internals",
      severity: "warn",
      comment:
        "Cross-module imports must use the main package only. " +
        "E.g. studio-* may import @atomyx/test-mgmt but NOT " +
        "@atomyx/test-mgmt-storage-file. Internals of one module " +
        "are not part of the public surface of that module.",
      from: { path: "^packages/(studio|cloud|test-mgmt)" },
      to: {
        // matches imports of @atomyx/<other-module>-<sub>
        path: "^packages/(core-driver|test-mgmt|studio|cloud)-",
      },
    },
    {
      name: "not-to-test-from-prod",
      severity: "warn",
      comment:
        "Production code should not import test-only utilities. " +
        "MockDriver belongs in test files only.",
      from: {
        path: "^packages/.+/src/(?!.*\\.test\\.ts$)",
        pathNot: "/testing/",
      },
      to: { path: "/testing/mock-driver" },
    },
    {
      name: "not-to-deprecated-src",
      severity: "warn",
      comment:
        "The legacy `src/` MCP server is being phased out. New " +
        "code in packages/* should not import from it.",
      from: { path: "^packages/" },
      to: { path: "^src/" },
    },
  ],

  options: {
    doNotFollow: {
      path: "node_modules",
    },
    tsPreCompilationDeps: true,
    tsConfig: {
      fileName: "tsconfig.json",
    },
    enhancedResolveOptions: {
      exportsFields: ["exports"],
      conditionNames: ["import", "require", "node", "default"],
    },
    reporterOptions: {
      text: { highlightFocused: true },
    },
  },
};
