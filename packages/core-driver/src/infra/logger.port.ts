/**
 * Structured logging abstraction. Core modules log through this
 * interface; feature consumers (MCP server, CLI, Studio, Synapse)
 * inject their own implementation to route logs to the appropriate
 * destination — stderr, file, editor panel, remote log service.
 *
 * `child(ctx)` returns a new logger whose `ctx` is merged into
 * every subsequent log entry. Used to add run ID / session ID /
 * driver platform to a contextual chain without passing metadata
 * through every call site.
 */
export interface Logger {
  debug(msg: string, ctx?: LogContext): void;
  info(msg: string, ctx?: LogContext): void;
  warn(msg: string, ctx?: LogContext): void;
  error(msg: string, err?: Error, ctx?: LogContext): void;
  child(ctx: LogContext): Logger;
}

export type LogContext = Readonly<Record<string, unknown>>;

export type LogLevel = "debug" | "info" | "warn" | "error";

/**
 * Default logger — writes JSON lines to stderr. Usable by any
 * consumer that doesn't want to bring its own destination.
 * Respects `minLevel` for runtime verbosity control.
 */
export class ConsoleLogger implements Logger {
  constructor(
    private readonly minLevel: LogLevel = "info",
    private readonly baseCtx: LogContext = {},
  ) {}

  debug(msg: string, ctx?: LogContext): void {
    this.write("debug", msg, undefined, ctx);
  }
  info(msg: string, ctx?: LogContext): void {
    this.write("info", msg, undefined, ctx);
  }
  warn(msg: string, ctx?: LogContext): void {
    this.write("warn", msg, undefined, ctx);
  }
  error(msg: string, err?: Error, ctx?: LogContext): void {
    this.write("error", msg, err, ctx);
  }

  child(ctx: LogContext): Logger {
    return new ConsoleLogger(this.minLevel, { ...this.baseCtx, ...ctx });
  }

  private write(
    level: LogLevel,
    msg: string,
    err: Error | undefined,
    ctx: LogContext | undefined,
  ): void {
    if (!shouldLog(level, this.minLevel)) return;
    const entry: Record<string, unknown> = {
      t: new Date().toISOString(),
      level,
      msg,
      ...this.baseCtx,
      ...ctx,
    };
    if (err) {
      entry.err = { message: err.message, stack: err.stack };
    }
    process.stderr.write(JSON.stringify(entry) + "\n");
  }
}

/** No-op logger — suitable as a default in tests and embedded use. */
export class NoopLogger implements Logger {
  debug(): void {}
  info(): void {}
  warn(): void {}
  error(): void {}
  child(): Logger {
    return this;
  }
}

const LEVEL_ORDER: Record<LogLevel, number> = {
  debug: 0,
  info: 1,
  warn: 2,
  error: 3,
};

function shouldLog(level: LogLevel, min: LogLevel): boolean {
  return LEVEL_ORDER[level] >= LEVEL_ORDER[min];
}
