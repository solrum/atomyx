import type { Dispatcher } from "./transport/dispatcher.js";
import type { EventBus } from "./events/event-bus.js";
import type { Session } from "./session/session.js";

/**
 * What every feature receives at registration time. Deliberately
 * narrow — a feature that needs more (a logger, a clock, a stores
 * registry) takes it from its own constructor, not by growing
 * this interface. Adding a field means a cross-cutting capability
 * has to reach every feature; that is a separate decision.
 */
export interface SidecarContext {
  readonly dispatcher: Dispatcher;
  readonly events: EventBus;
  readonly session: Session;
}

export interface FeatureHandle {
  readonly dispose?: () => Promise<void>;
}

export type FeatureRegistrar = (ctx: SidecarContext) => FeatureHandle;
