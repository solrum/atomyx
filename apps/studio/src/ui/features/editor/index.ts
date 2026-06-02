// Editor feature's public surface. Internal files (editor-pane,
// editor-tabs, script-editor, decorations, marker-filter,
// file-breadcrumb, monaco-init, problems-subscriber) are
// consumed only by the shell's composition root; they stay NOT
// re-exported here so cross-feature code that legitimately needs
// the editor goes through monaco-active.
export * from "./monaco-active.js";
