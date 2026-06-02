/**
 * Studio-scoped ESLint config. Scope is `apps/studio/` only.
 *
 * The `no-restricted-imports` overrides encode the one-way
 * dependency rule from `.claude/rules/studio-architecture.md`
 * so contributors see violations in their editor.
 */
module.exports = {
  root: true,
  env: { browser: true, es2022: true, node: true },
  parser: "@typescript-eslint/parser",
  parserOptions: {
    ecmaVersion: 2022,
    sourceType: "module",
    ecmaFeatures: { jsx: true },
  },
  plugins: ["@typescript-eslint", "react", "react-hooks"],
  extends: [
    "eslint:recommended",
    "plugin:@typescript-eslint/recommended",
    "plugin:react/recommended",
    "plugin:react-hooks/recommended",
  ],
  settings: { react: { version: "18" } },
  rules: {
    "react/react-in-jsx-scope": "off",
    "react/prop-types": "off",
    "@typescript-eslint/no-unused-vars": [
      "warn",
      { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
    ],
    // Bans the zero-arg singleton-accessor call shape at consumer sites
    // (e.g. `editor()`). The structural script
    // scripts/check-feature-api.mjs catches the matching export-side
    // antipattern in feature index.ts files. Both fail builds.
    "no-restricted-syntax": [
      "error",
      {
        selector:
          "CallExpression[callee.type='Identifier'][callee.name=/^(actions|androidAgent|apps|bookmarks|devices|editor|iosAgent|layout|logs|mirror|mirrorWindow|navHistory|notifications|popups|problems|projectConfig|projects|runConfigs|runs|runtimeStatus|settings|terminal|themes|todos|uiInspector|workspace|workspaceSearch|workspaceState)$/][arguments.length=0]",
        message:
          "Use getFeature<T>(KEY) or useXxx() instead of the removed zero-arg singleton accessor.",
      },
    ],
  },
  overrides: [
    {
      files: ["src/domain/**/*.{ts,tsx}"],
      rules: {
        "no-restricted-imports": [
          "error",
          {
            patterns: [
              {
                group: [
                  "react",
                  "react-dom",
                  "react/*",
                  "react-dom/*",
                  "@tauri-apps/*",
                  "monaco-editor",
                  "monaco-editor/*",
                  "monaco-yaml",
                  "zustand",
                  "zustand/*",
                ],
                message:
                  "domain/ must stay pure TypeScript. Wrap this capability in a port and put the concrete implementation under platform/ or state/.",
              },
            ],
          },
        ],
      },
    },
    {
      files: ["src/state/**/*.{ts,tsx}"],
      excludedFiles: ["src/state/features/*/index.ts"],
      rules: {
        "no-restricted-imports": [
          "error",
          {
            patterns: [
              {
                group: [
                  "react",
                  "react-dom",
                  "react/*",
                  "react-dom/*",
                  "@tauri-apps/*",
                  "monaco-editor",
                  "monaco-editor/*",
                  "monaco-yaml",
                ],
                message:
                  "state/ must not import UI or platform code. Use a domain service (accessed via getServices()) or move this file.",
              },
            ],
          },
        ],
      },
    },
    {
      // A feature's index.ts ships its React hook alongside the
      // contract + factory (feature-api rule). React is allowed
      // here ONLY because of that hook — everywhere else in
      // state/ the broader ban still applies.
      files: ["src/state/features/*/index.ts"],
      rules: {
        "no-restricted-imports": [
          "error",
          {
            patterns: [
              {
                group: [
                  "react-dom",
                  "react-dom/*",
                  "@tauri-apps/*",
                  "monaco-editor",
                  "monaco-editor/*",
                  "monaco-yaml",
                ],
                message:
                  "state/ must not import UI or platform code. Use a domain service (accessed via getServices()) or move this file.",
              },
            ],
          },
        ],
      },
    },
    {
      files: ["src/platform/**/*.{ts,tsx}"],
      rules: {
        "no-restricted-imports": [
          "error",
          {
            patterns: [
              {
                group: [
                  "react",
                  "react-dom",
                  "react/*",
                  "react-dom/*",
                  "monaco-editor",
                  "monaco-editor/*",
                  "monaco-yaml",
                ],
                message:
                  "platform/ implements domain ports against Tauri/OS APIs — no UI-framework code here.",
              },
            ],
          },
        ],
      },
    },
    {
      // Only main.tsx (composition root), the platform/index barrel,
      // the feature's own files, and tests may import feature-impl
      // files. Everyone else must go through features/<X>/index.ts.
      files: ["src/**/*.{ts,tsx}"],
      excludedFiles: [
        "src/main.tsx",
        "src/platform/index.ts",
        "src/**/features/*/**",
        "src/**/*.test.{ts,tsx}",
      ],
      rules: {
        "no-restricted-imports": [
          "error",
          {
            patterns: [
              {
                group: [
                  "**/features/*/*.zustand",
                  "**/features/*/*.zustand.*",
                  "**/features/*/*.tauri",
                  "**/features/*/*.tauri.*",
                  "**/features/*/*.fs",
                  "**/features/*/*.fs.*",
                  "**/features/*/*.node",
                  "**/features/*/*.node.*",
                  "**/features/*/*.impl",
                  "**/features/*/*.impl.*",
                  "**/features/*/*.muxjs",
                  "**/features/*/*.muxjs.*",
                  "**/features/*/*.scrcpy",
                  "**/features/*/*.scrcpy.*",
                  "**/features/*/*.simctl",
                  "**/features/*/*.simctl.*",
                  "**/features/*/*.coremedia",
                  "**/features/*/*.coremedia.*",
                ],
                message:
                  "Reach features only through features/<X>/index.ts (contract type + factory). Concrete impls are private — changing them should require one edit, not a sweep of consumers.",
              },
            ],
          },
        ],
      },
    },
  ],
  ignorePatterns: ["dist", "src-tauri/target", "node_modules"],
};
