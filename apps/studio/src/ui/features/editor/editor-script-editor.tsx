import { useEffect, useRef } from "react";
import { ensureMonacoReady, monaco } from "./editor-monaco-init.js";
import { computeScriptDecorations } from "./editor-script-decorations.js";
import { useSettings } from "../../../state/features/settings/index.js";
import {
  clearActiveMonacoEditorIf,
  setActiveMonacoEditor,
} from "./editor-monaco-active.js";
import { getFeature } from "../../../state/core/registry.js";
import type { NavHistoryApi } from "../../../state/features/nav-history/index.js";
import { NAV_HISTORY_KEY } from "../../../state/features/nav-history/index.js";
import type { EditorApi } from "../../../state/features/editor/index.js";
import { EDITOR_KEY } from "../../../state/features/editor/index.js";
import type { BookmarksApi } from "../../../state/features/bookmarks/index.js";
import { BOOKMARKS_KEY } from "../../../state/features/bookmarks/index.js";

export interface ScriptEditorProps {
  readonly value: string;
  readonly onChange: (value: string) => void;
  readonly groupId?: string;
}

/**
 * Monaco + monaco-yaml editor bound to the Atomyx script schema.
 * A single model per mount — tab switching is handled by remounting
 * with a new `value`. Visual styling (colors, fonts) flows in via
 * the CSS-variable layer written by `ui/theme/apply-tokens.ts`;
 * Monaco's own theme is set by `ui/theme/monaco-theme.ts` when
 * the active theme changes.
 */
export function ScriptEditor({ value, onChange, groupId }: ScriptEditorProps) {
  const hostRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const onChangeRef = useRef(onChange);
  const groupIdRef = useRef(groupId);
  const { settings } = useSettings();

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    groupIdRef.current = groupId;
  }, [groupId]);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    ensureMonacoReady();
    const editorApi = getFeature<EditorApi>(EDITOR_KEY);

    const uriSuffix = groupIdRef.current ?? "primary";
    const model = monaco.editor.createModel(
      value,
      "yaml",
      monaco.Uri.parse(`inmemory://atomyx-script-${uriSuffix}.yml`),
    );

    const fontFamily = settings.useBundledFont
      ? '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace'
      : 'ui-monospace, SFMono-Regular, Menlo, monospace';

    const editor = monaco.editor.create(host, {
      model,
      automaticLayout: true,
      glyphMargin: true,
      minimap: { enabled: false },
      fontSize: 13,
      fontFamily,
      lineHeight: 18,
      tabSize: 2,
      insertSpaces: true,
      renderWhitespace: "selection",
      scrollBeyondLastLine: false,
      // IntelliJ-style: gutter auto-sizes with the line count
      // (3 chars for files <=999 lines, auto-grows for larger).
      // Default 5 wastes a wide gutter on short files.
      lineNumbers: "on",
      lineNumbersMinChars: 3,
      lineDecorationsWidth: 4,
      // Block-style selection: drop the rounded edges so multi-
      // line selections read as a contiguous slab instead of a
      // pill stack — matches IntelliJ's full-line selection feel.
      roundedSelection: false,
      selectionHighlight: false,
      occurrencesHighlight: "off",
      // IntelliJ-style overview ruler: a narrow lane on the
      // right edge that paints error / warning / modified-line
      // markers as small ticks aligned to their scroll position.
      // The cursor marker is suppressed so it does not clutter
      // the lane on every keypress.
      overviewRulerLanes: 3,
      overviewRulerBorder: false,
      hideCursorInOverviewRuler: true,
      renderLineHighlight: "gutter",
      smoothScrolling: true,
      scrollbar: {
        verticalScrollbarSize: 10,
        horizontalScrollbarSize: 10,
        verticalSliderSize: 6,
        horizontalSliderSize: 6,
        useShadows: false,
        alwaysConsumeMouseWheel: false,
      },
      // Suggest-list hygiene. monaco-yaml emits each step-level
      // anyOf variant TWICE — once as a Class (object-template
      // insertion) and once as a Property (bare key insertion).
      // Visually they're indistinguishable (both insert the same
      // shorthand shape), so we suppress Class/Snippet/Module
      // kinds and keep only Property/Field. Word-based
      // suggestions are off for the same reason.
      wordBasedSuggestions: "off",
      quickSuggestions: {
        other: true,
        comments: false,
        strings: false,
      },
      suggest: {
        showWords: false,
        showSnippets: false,
        showClasses: false,
        showModules: false,
        showFiles: false,
        showKeywords: false,
        showIssues: false,
        showFields: true,
        showProperties: true,
        showEnums: true,
        showEnumMembers: true,
        showConstants: true,
        showValues: true,
      },
    });
    editorRef.current = editor;
    setActiveMonacoEditor(editor);
    const focusSub = editor.onDidFocusEditorWidget(() => {
      setActiveMonacoEditor(editor);
      const gid = groupIdRef.current;
      if (gid) editorApi.focusGroup(gid);
    });

    const decorationCollection = editor.createDecorationsCollection();
    const refreshDecorations = () => {
      decorationCollection.set(computeScriptDecorations(model, monaco));
    };
    refreshDecorations();

    // IntelliJ-style multi-line selection: paint the full-line
    // background gutter→edge on every line touched by the
    // selection AND suppress Monaco's native per-text-run paint
    // for those lines (toggled via a host-level class so the CSS
    // override is scoped to the active multi-line range only).
    // Single-line selections fall through to Monaco's native
    // paint so partial-text highlights still hug the actual
    // characters instead of grabbing the whole line.
    const selectionFillCollection = editor.createDecorationsCollection();
    const refreshSelectionFill = () => {
      const sel = editor.getSelection();
      const host = editor.getDomNode();
      if (!sel || sel.isEmpty()) {
        selectionFillCollection.clear();
        host?.classList.remove("atomyx-multi-selection");
        return;
      }
      if (sel.startLineNumber === sel.endLineNumber) {
        selectionFillCollection.clear();
        host?.classList.remove("atomyx-multi-selection");
        return;
      }
      host?.classList.add("atomyx-multi-selection");
      const range = new monaco.Range(
        sel.startLineNumber,
        1,
        sel.endLineNumber,
        1,
      );
      selectionFillCollection.set([
        {
          range,
          options: {
            isWholeLine: true,
            className: "atomyx-selection-fill",
          },
        },
      ]);
    };
    const selectionSub = editor.onDidChangeCursorSelection(
      refreshSelectionFill,
    );

    const bookmarkApi = getFeature<BookmarksApi>(BOOKMARKS_KEY);
    const bookmarkCollection = editor.createDecorationsCollection();
    const refreshBookmarks = () => {
      const activePath = editorApi.getSnapshot().activePath;
      if (!activePath) {
        bookmarkCollection.clear();
        return;
      }
      const items = bookmarkApi
        .getSnapshot()
        .items.filter((b) => b.path === activePath);
      bookmarkCollection.set(
        items.map((b) => ({
          range: new monaco.Range(b.line, 1, b.line, 1),
          options: {
            isWholeLine: false,
            glyphMarginClassName: "atomyx-bookmark-glyph",
            glyphMarginHoverMessage: {
              value: b.note ?? `Bookmark · line ${b.line}`,
            },
          },
        })),
      );
    };
    refreshBookmarks();
    const bookmarkUnsub = bookmarkApi.subscribe(() => refreshBookmarks());

    const subscription = model.onDidChangeContent(() => {
      onChangeRef.current(model.getValue());
      refreshDecorations();
    });

    const cursorSub = editor.onDidChangeCursorPosition((e) => {
      if (e.reason !== monaco.editor.CursorChangeReason.Explicit) return;
      const path = editorApi.getSnapshot().activePath;
      if (!path) return;
      getFeature<NavHistoryApi>(NAV_HISTORY_KEY).record({
        path,
        line: e.position.lineNumber,
        column: e.position.column,
      });
    });

    // Faster word-select activation + scroll-quiet click swallowing.
    //
    //   1. Monaco's internal multi-click window is 400 ms
    //      (mouseHandler.js MouseDownState.CLEAR_…) and also relies
    //      on the OS double-click setting via MouseEvent.detail.
    //      Both feel sluggish for keyboard-heavy editing — two
    //      quick clicks at the same spot should immediately select
    //      the word. Detect that ourselves at a tighter 250 ms
    //      window and synthesize the word selection.
    //
    //   2. On a trackpad, the "click to stop inertial scroll"
    //      gesture lands as a real mousedown. If the user then
    //      slides to keep scrolling, Monaco interprets the move as
    //      a drag-selection. Two layers handle this:
    //        (a) any mousedown that arrives within SCROLL_QUIET_MS
    //            of the most recent wheel is preventDefault'd and
    //            stopImmediatePropagation'd so Monaco never sees it;
    //        (b) a wheel that arrives WHILE the mouse is held down
    //            cancels the in-flight drag selection by snapping
    //            the selection back to a caret at the original
    //            mousedown position — covers the case where inertia
    //            had already settled before the user clicked but
    //            then immediately resumed scrolling under the held
    //            finger.
    const FAST_DOUBLE_CLICK_MS = 250;
    const FAST_DOUBLE_CLICK_PX = 4;
    const SCROLL_QUIET_MS = 700;
    // Trackpad clicks naturally drift 1-3 px between fingerdown and
    // fingerup, and Wry/WebKit on macOS occasionally drops mouseup
    // events on a trackpad tap — leaving the editor's drag-select
    // state machine armed across what the user perceives as
    // separate clicks. Two layers handle this:
    //
    //   * DRAG_THRESHOLD_PX gates Monaco from seeing ANY mousemove
    //     until the cursor has travelled at least this far from the
    //     mousedown point. Drift inside the threshold never starts
    //     a selection.
    //   * Every mousemove asserts that a button is actually held
    //     (e.buttons & 1). If the OS told us mousedown but the
    //     subsequent move arrives with no button pressed, the
    //     mouseup was dropped — clear our held flag so Monaco's
    //     drag handler doesn't latch onto the stale state.
    //
    // The window+pointerup / blur listeners below cover the case
    // where focus leaves the editor mid-drag and mouseup never
    // arrives at the host.
    const DRAG_THRESHOLD_PX = 6;
    let lastDownAt = 0;
    let lastDownX = 0;
    let lastDownY = 0;
    let lastWheelAt = 0;
    let mouseIsDown = false;
    let downClientX = 0;
    let downClientY = 0;
    let dragArmed = false;
    let downCaretLine = 0;
    let downCaretColumn = 0;
    const onWheelCapture = () => {
      lastWheelAt = performance.now();
      if (mouseIsDown && downCaretLine > 0) {
        editor.setSelection(
          new monaco.Range(
            downCaretLine,
            downCaretColumn,
            downCaretLine,
            downCaretColumn,
          ),
        );
      }
    };
    const onMouseDownCapture = (e: MouseEvent) => {
      if (e.button !== 0) return;
      const now = performance.now();
      if (now - lastWheelAt < SCROLL_QUIET_MS) {
        e.preventDefault();
        e.stopImmediatePropagation();
        lastDownAt = 0;
        return;
      }
      mouseIsDown = true;
      dragArmed = false;
      downClientX = e.clientX;
      downClientY = e.clientY;
      const downTarget = editor.getTargetAtClientPoint(e.clientX, e.clientY);
      if (downTarget?.position) {
        downCaretLine = downTarget.position.lineNumber;
        downCaretColumn = downTarget.position.column;
      } else {
        downCaretLine = 0;
        downCaretColumn = 0;
      }
      const dt = now - lastDownAt;
      const dx = Math.abs(e.clientX - lastDownX);
      const dy = Math.abs(e.clientY - lastDownY);
      lastDownAt = now;
      lastDownX = e.clientX;
      lastDownY = e.clientY;
      if (
        dt >= FAST_DOUBLE_CLICK_MS ||
        dx > FAST_DOUBLE_CLICK_PX ||
        dy > FAST_DOUBLE_CLICK_PX
      ) {
        return;
      }
      const target = downTarget;
      const position = target?.position;
      if (!position) return;
      const word = editor.getModel()?.getWordAtPosition(position);
      if (!word) return;
      // Defer past Monaco's own mousedown handler — it runs after
      // our capture-phase listener and would otherwise reset the
      // selection to a caret at the click point.
      setTimeout(() => {
        editor.setSelection(
          new monaco.Range(
            position.lineNumber,
            word.startColumn,
            position.lineNumber,
            word.endColumn,
          ),
        );
      }, 0);
      lastDownAt = 0;
    };
    const onMoveCapture = (e: MouseEvent | PointerEvent) => {
      if (!mouseIsDown) return;
      // Wry/WebKit sometimes drops mouseup on trackpad — recover
      // by inspecting the buttons bitfield on every move.
      if ((e.buttons & 1) === 0) {
        mouseIsDown = false;
        dragArmed = false;
        return;
      }
      if (dragArmed) return;
      const dx = Math.abs(e.clientX - downClientX);
      const dy = Math.abs(e.clientY - downClientY);
      if (dx >= DRAG_THRESHOLD_PX || dy >= DRAG_THRESHOLD_PX) {
        dragArmed = true;
        return;
      }
      // Monaco's drag-select pipeline lives on POINTERMOVE (via
      // GlobalPointerMoveMonitor), not mousemove. Blocking only
      // mousemove silently does nothing — block both.
      e.stopImmediatePropagation();
      e.preventDefault();
    };
    const onUpCapture = () => {
      mouseIsDown = false;
      dragArmed = false;
    };
    const onBlurReset = () => {
      mouseIsDown = false;
      dragArmed = false;
    };
    host.addEventListener("wheel", onWheelCapture, {
      capture: true,
      passive: true,
    });
    host.addEventListener("mousedown", onMouseDownCapture, true);
    host.addEventListener("mousemove", onMoveCapture, true);
    window.addEventListener("mousemove", onMoveCapture, true);
    host.addEventListener("pointermove", onMoveCapture, true);
    window.addEventListener("pointermove", onMoveCapture, true);
    host.addEventListener("mouseup", onUpCapture, true);
    window.addEventListener("mouseup", onUpCapture, true);
    window.addEventListener("pointerup", onUpCapture, true);
    window.addEventListener("blur", onBlurReset);

    return () => {
      subscription.dispose();
      cursorSub.dispose();
      focusSub.dispose();
      selectionSub.dispose();
      bookmarkUnsub();
      host.removeEventListener("mousedown", onMouseDownCapture, true);
      host.removeEventListener("mousemove", onMoveCapture, true);
      window.removeEventListener("mousemove", onMoveCapture, true);
      host.removeEventListener("pointermove", onMoveCapture, true);
      window.removeEventListener("pointermove", onMoveCapture, true);
      host.removeEventListener("mouseup", onUpCapture, true);
      window.removeEventListener("mouseup", onUpCapture, true);
      window.removeEventListener("pointerup", onUpCapture, true);
      window.removeEventListener("blur", onBlurReset);
      host.removeEventListener("wheel", onWheelCapture, true);
      decorationCollection.clear();
      bookmarkCollection.clear();
      selectionFillCollection.clear();
      editor.dispose();
      model.dispose();
      editorRef.current = null;
      clearActiveMonacoEditorIf(editor);
    };
    // Mount once per `value` identity; parent remounts with a new key
    // when the active tab changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    const model = editor.getModel();
    if (model && model.getValue() !== value) {
      model.setValue(value);
    }
  }, [value]);

  return <div ref={hostRef} className="h-full w-full" />;
}
