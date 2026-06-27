export class DriverNotReadyError extends Error {
  readonly code = "driver-not-ready" as const;

  constructor(message: string) {
    super(message);
    this.name = "DriverNotReadyError";
  }
}

export class StreamingTouchNotSupportedError extends Error {
  readonly code = "streaming-touch-not-supported" as const;

  constructor(message: string) {
    super(message);
    this.name = "StreamingTouchNotSupportedError";
  }
}
