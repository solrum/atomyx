/**
 * @atomyx/driver-wire — shared wire types for Atomyx platform drivers.
 *
 * Exports the canonical `TreeNodeWire` shape and primitive types
 * (`PointWire`, `SizeWire`, `BoundsWire`, etc.) that driver adapters
 * produce and core logic consumes.
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
 * Drivers bridge between platform-native JSON and core types.
 */

export * from "./tree-node.schema.js";
