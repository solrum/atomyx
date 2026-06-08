import { useEffect, useRef } from "react";
import { Check, X as XIcon } from "lucide-react";
import type { LiveRun } from "../../../state/features/runs/index.js";
import type { StepToken } from "../../../domain/features/runtime/index.js";
import type { RunState } from "./run-views.js";

interface TimelineProps {
  readonly live: LiveRun | null;
  readonly state: RunState;
}

const STICKY_BOTTOM_TOLERANCE_PX = 48;

export function RunTimeline({ live, state }: TimelineProps) {
  const endRef = useRef<HTMLLIElement | null>(null);
  const stickToBottomRef = useRef(true);
  const eventCount = live?.events.length ?? 0;

  useEffect(() => {
    const sentinel = endRef.current;
    if (!sentinel) return;
    const scroller = findScrollParent(sentinel);
    if (!scroller) return;
    const onScroll = (): void => {
      const distance =
        scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
      stickToBottomRef.current = distance <= STICKY_BOTTOM_TOLERANCE_PX;
    };
    onScroll();
    scroller.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      scroller.removeEventListener("scroll", onScroll);
    };
  }, [live]);

  useEffect(() => {
    if (!stickToBottomRef.current) return;
    endRef.current?.scrollIntoView({ block: "end", behavior: "smooth" });
  }, [eventCount, state]);

  if (!live) {
    return (
      <div
        className="timeline"
        style={{
          padding: "var(--gap-5)",
          fontSize: "var(--fs-12)",
          color: "var(--fg-3)",
        }}
      >
        Hit Play to run the current script.
      </div>
    );
  }
  const started = live.events.filter((e) => e.type === "stepStarted");
  const completed = new Map<
    number,
    Extract<typeof live.events[number], { type: "stepCompleted" }>
  >();
  for (const e of live.events) {
    if (e.type === "stepCompleted") completed.set(e.stepIndex, e);
  }
  return (
    <ol className="timeline">
      {started.map((s) => {
        if (s.type !== "stepStarted") return null;
        const done = completed.get(s.stepIndex);
        const stepState: "pending" | "run" | "pass" | "fail" = done
          ? done.ok
            ? "pass"
            : "fail"
          : state === "running"
            ? "run"
            : "pending";
        return (
          <TimelineStep
            key={s.stepIndex}
            index={s.stepIndex}
            command={s.command}
            summary={s.summary || s.command}
            tokens={s.tokens}
            state={stepState}
            durationMs={done?.durationMs ?? null}
            detail={done?.detail ?? null}
            depth={s.depth ?? 0}
            line={s.line}
          />
        );
      })}
      <li ref={endRef} aria-hidden style={{ height: 1, listStyle: "none" }} />
    </ol>
  );
}

function TokenLine({ tokens }: { readonly tokens: readonly StepToken[] }) {
  return (
    <span style={{ fontFamily: "var(--font-mono)" }}>
      {tokens.map((tok, i) => (
        <span key={i} className={tokenClassName(tok.kind)} style={tokenStyle(tok.kind)}>
          {tok.text}
        </span>
      ))}
    </span>
  );
}

function tokenClassName(kind: StepToken["kind"]): string {
  switch (kind) {
    case "keyword":
      return "kw";
    case "identifier":
      return "id";
    case "string":
      return "str";
    case "punct":
      return "punct";
    case "mask":
      return "str";
  }
}

function tokenStyle(kind: StepToken["kind"]): React.CSSProperties | undefined {
  switch (kind) {
    case "keyword":
      return { color: "var(--atomyx-keyword, var(--accent))" };
    case "identifier":
      return { color: "var(--atomyx-selector, var(--info, #82aaff))" };
    case "string":
      return { color: "var(--syntax-string, var(--ok))" };
    case "punct":
      return { color: "var(--fg-3)" };
    case "mask":
      return { color: "var(--fg-2)", letterSpacing: "1px" };
  }
}

function findScrollParent(node: HTMLElement): HTMLElement | null {
  let current: HTMLElement | null = node.parentElement;
  while (current) {
    const overflowY = window.getComputedStyle(current).overflowY;
    if (
      (overflowY === "auto" || overflowY === "scroll" || overflowY === "overlay") &&
      current.scrollHeight > current.clientHeight
    ) {
      return current;
    }
    current = current.parentElement;
  }
  return null;
}

interface TimelineStepProps {
  readonly index: number;
  readonly command: string;
  readonly summary: string;
  readonly tokens: readonly StepToken[] | undefined;
  readonly state: "pending" | "run" | "pass" | "fail";
  readonly durationMs: number | null;
  readonly detail: string | null;
  readonly depth: number;
  readonly line: number | undefined;
}

function TimelineStep({
  command,
  summary,
  tokens,
  state,
  durationMs,
  detail,
  depth,
  line,
}: TimelineStepProps) {
  const metaParts: string[] = [];
  if (line !== undefined) metaParts.push(`line ${line}`);
  if (state === "pass" || state === "fail") {
    if (durationMs !== null) metaParts.push(`${durationMs}ms`);
  } else if (state === "run") {
    metaParts.push("running…");
  } else {
    metaParts.push("waiting");
  }
  return (
    <li
      className={`step ${state}`}
      style={depth > 0 ? { paddingLeft: 8 + depth * 16 } : undefined}
    >
      <div className="gutter">
        <div className="ico">
          {state === "pass" ? (
            <Check style={{ width: "8px", height: "8px" }} />
          ) : state === "fail" ? (
            <XIcon style={{ width: "8px", height: "8px" }} />
          ) : null}
        </div>
        <div className="line" />
      </div>
      <div className="body">
        <div className="cmd">
          {tokens && tokens.length > 0 ? (
            <TokenLine tokens={tokens} />
          ) : (
            <span className="kw">{summary || command}</span>
          )}
        </div>
        <div className="sub">{metaParts.join(" · ")}</div>
        {state === "fail" && detail ? (
          <pre
            className="details"
            style={{
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
              color: "var(--err)",
            }}
          >
            {detail}
          </pre>
        ) : null}
      </div>
    </li>
  );
}
