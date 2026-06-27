import type { JsonRpcEvent } from "../transport/types.js";

export type EventListener = (event: JsonRpcEvent) => void;

/**
 * In-process pub/sub for sidecar events. Services emit through the
 * bus; the transport subscribes once and forwards each event over
 * the wire.
 *
 * Exists to keep services ignorant of the transport — a test can
 * attach a collecting listener without touching stdio, and swapping
 * transport (stdio → socket) only touches the subscriber.
 */
export class EventBus {
  private readonly listeners = new Set<EventListener>();

  subscribe(listener: EventListener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  emit(event: JsonRpcEvent): void {
    // Snapshot the list so listeners that unsubscribe during
    // iteration do not mutate the set we're walking.
    for (const listener of Array.from(this.listeners)) {
      try {
        listener(event);
      } catch {
        /* listener failure must not prevent others from receiving */
      }
    }
  }
}
