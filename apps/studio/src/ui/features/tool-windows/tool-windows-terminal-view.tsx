import { useEffect, useRef } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import { getFeature } from "../../../state/core/registry.js";
import type { TerminalSession, TerminalApi } from "../../../state/features/terminal/index.js";
import { TERMINAL_KEY } from "../../../state/features/terminal/index.js";
import type { WorkspaceApi } from "../../../state/features/workspace/index.js";
import { WORKSPACE_KEY } from "../../../state/features/workspace/index.js";
import { useSettings } from "../../../state/features/settings/index.js";

function resolveCssVar(name: string, fallback: string): string {
  const v = getComputedStyle(document.body).getPropertyValue(name).trim();
  return v.length > 0 ? v : fallback;
}

export function TerminalView() {
  const hostRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const sessionRef = useRef<TerminalSession | null>(null);
  const { settings } = useSettings();

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;

    const term = new Terminal({
      fontSize: 13,
      fontFamily: settings.useBundledFont
        ? '"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace'
        : 'ui-monospace, SFMono-Regular, Menlo, monospace',
      cursorBlink: true,
      theme: {
        background: resolveCssVar("--ui-bg-primary-bg", "#1e1e1e"),
        foreground: resolveCssVar("--ui-text-primary", "#dddddd"),
        cursor: resolveCssVar("--ui-accent", "#589df6"),
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host);
    fit.fit();
    termRef.current = term;

    let killed = false;

    void getFeature<TerminalApi>(TERMINAL_KEY)
      .spawn(
        {
          cols: term.cols,
          rows: term.rows,
          workspacePath: getFeature<WorkspaceApi>(WORKSPACE_KEY).getSnapshot().currentPath ?? null,
        },
        (data) => term.write(data),
      )
      .then((session) => {
        if (killed) {
          session.kill();
          return;
        }
        sessionRef.current = session;
        term.onData((data) => {
          sessionRef.current?.write(data);
        });
        term.onResize(({ cols, rows }) => {
          sessionRef.current?.resize(cols, rows);
        });
      });

    const resizeObserver = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        /* host detached */
      }
    });
    resizeObserver.observe(host);

    return () => {
      killed = true;
      resizeObserver.disconnect();
      sessionRef.current?.kill();
      term.dispose();
      termRef.current = null;
      sessionRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return <div ref={hostRef} className="h-full w-full" />;
}
