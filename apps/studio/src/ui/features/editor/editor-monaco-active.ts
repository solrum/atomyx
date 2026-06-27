import type * as monacoNs from "monaco-editor";

/**
 * Module-level reference to the currently mounted Monaco editor.
 * Non-React consumers (the keymap dispatcher, for example) call
 * into the editor via this handle instead of lifting state into a
 * store — Monaco is not a React-friendly API.
 *
 * Only one editor mounts at a time in the current layout; the
 * reference is nulled when the sole instance unmounts.
 */
let active: monacoNs.editor.IStandaloneCodeEditor | null = null;

export function setActiveMonacoEditor(
  editor: monacoNs.editor.IStandaloneCodeEditor | null,
): void {
  active = editor;
}

export function clearActiveMonacoEditorIf(
  editor: monacoNs.editor.IStandaloneCodeEditor,
): void {
  if (active === editor) active = null;
}

export function getActiveMonacoEditor():
  | monacoNs.editor.IStandaloneCodeEditor
  | null {
  return active;
}

export function triggerFindInFile(): void {
  const editor = active;
  if (!editor) return;
  editor.focus();
  editor.getAction("actions.find")?.run();
}

export function jumpActiveEditorTo(line: number, column = 1): void {
  const editor = active;
  if (!editor) return;
  editor.focus();
  editor.revealLineInCenter(line);
  editor.setPosition({ lineNumber: line, column });
}

export function triggerGoToLine(): void {
  const editor = active;
  if (!editor) return;
  editor.focus();
  editor.getAction("editor.action.gotoLine")?.run();
}

export function triggerFormatDocument(): void {
  const editor = active;
  if (!editor) return;
  editor.focus();
  editor.getAction("editor.action.formatDocument")?.run();
}

export function getActiveCursor(): { line: number; column: number } | null {
  const editor = active;
  if (!editor) return null;
  const pos = editor.getPosition();
  if (!pos) return null;
  return { line: pos.lineNumber, column: pos.column };
}

export interface InsertRange {
  readonly offset: number;
  readonly length: number;
}

/**
 * Insert a block of YAML into the active editor. Two modes:
 *
 *   - Editor has keyboard focus → insert above the caret line,
 *     matching that line's leading indent so the new step slots
 *     into the surrounding `steps:` block.
 *   - Editor does NOT have focus (user was clicking elsewhere,
 *     e.g. right-clicked in the inspector) → append at end-of-
 *     file. Inheriting indent from the tail line so a trailing
 *     nested step keeps its depth.
 *
 * After the edit, the caret lands at the inserted range and the
 * viewport scrolls the insertion point into view — so an append
 * to a 400-line script doesn't leave the user hunting for where
 * the step went.
 *
 * When `select` is provided the selection is moved onto that
 * substring post-insert so the user can immediately type over a
 * placeholder like `"TODO"`. `select.offset` is measured inside
 * the unindented `block`; the helper translates to line / column.
 *
 * Returns `true` when the insert was dispatched, `false` when no
 * editor is mounted — callers fall back to clipboard in that case.
 */
export function insertAtActiveCursor(
  block: string,
  select?: InsertRange,
): boolean {
  const editor = active;
  if (!editor) return false;
  const model = editor.getModel();
  if (!model) return false;

  const focused = editor.hasTextFocus();
  const caret = editor.getPosition();

  // Pick the anchor line: caret line when the editor has focus,
  // otherwise the last line of the document (append semantics).
  const anchorLine = focused && caret ? caret.lineNumber : model.getLineCount();
  const anchorText = model.getLineContent(anchorLine);
  const anchorIsEmpty = anchorText.trim().length === 0;
  const indentMatch = anchorText.match(/^[ \t]*/);
  const indent = indentMatch ? indentMatch[0] : "";
  const indented = block
    .split("\n")
    .map((l) => (l.length > 0 ? indent + l : l))
    .join("\n");

  // Insert position: start of anchor line when focused, end of
  // document when appending. A leading newline is prepended when
  // appending so the block doesn't share a line with existing
  // content. Trailing newline is appended in focused mode so the
  // line the caret sat on is preserved below the new block.
  let insertText: string;
  let insertRange: monacoNs.IRange;
  let firstInsertedLine: number;
  if (focused && caret) {
    insertText = indented + "\n";
    insertRange = {
      startLineNumber: anchorLine,
      startColumn: 1,
      endLineNumber: anchorLine,
      endColumn: 1,
    };
    firstInsertedLine = anchorLine;
  } else {
    const anchorEnd = model.getLineMaxColumn(anchorLine);
    insertText = anchorIsEmpty ? indented : "\n" + indented;
    insertRange = {
      startLineNumber: anchorLine,
      startColumn: anchorEnd,
      endLineNumber: anchorLine,
      endColumn: anchorEnd,
    };
    firstInsertedLine = anchorIsEmpty ? anchorLine : anchorLine + 1;
  }

  editor.focus();
  editor.executeEdits("inspector-action", [
    {
      range: insertRange,
      text: insertText,
      forceMoveMarkers: true,
    },
  ]);

  if (select) {
    const before = block.slice(0, select.offset);
    const linesBefore = before.split("\n");
    const lineDelta = linesBefore.length - 1;
    const columnInBlock = linesBefore[linesBefore.length - 1]!.length;
    const startLine = firstInsertedLine + lineDelta;
    const startColumn = indent.length + columnInBlock + 1;
    editor.setSelection({
      startLineNumber: startLine,
      startColumn,
      endLineNumber: startLine,
      endColumn: startColumn + select.length,
    });
    editor.revealRangeInCenterIfOutsideViewport({
      startLineNumber: startLine,
      startColumn,
      endLineNumber: startLine,
      endColumn: startColumn + select.length,
    });
  } else {
    const lastInsertedLine =
      firstInsertedLine + indented.split("\n").length - 1;
    editor.setPosition({ lineNumber: lastInsertedLine, column: 1 });
    editor.revealLineInCenterIfOutsideViewport(lastInsertedLine);
  }
  return true;
}
