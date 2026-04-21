/**
 * @atomyx/driver-wire — canonical HTTP+JSON wire contract between
 * Atomyx host and platform drivers.
 *
 * This package is intentionally small: it exports only Zod schemas
 * and the types inferred from them. It does not implement
 * transport, tree normalization, or any business logic — those
 * live in `@atomyx/driver-{android,ios}` packages that consume this schema.
 *
 * Downstream dependency direction:
 *
 *   @atomyx/driver-wire  (this)
 *        ↑       ↑
 *        │       └── @atomyx/ios-driver
 *        └── @atomyx/android-driver
 *
 * `@atomyx/core` does NOT depend on this package — core operates
 * on the `TreeNode` canonical shape directly via typed interfaces.
 * Drivers bridge between wire JSON and core types; wire-schema is
 * the contract they both implement against.
 */

export * from "./tree-node.schema.js";
export * from "./routes.schema.js";
