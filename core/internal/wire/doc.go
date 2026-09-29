// Package wire holds types shared across device platforms: geometry
// primitives (Point, Bounds, Size) and TreeNodeWire, the normalized
// UI tree shape a future driver layer produces from the raw Android
// and iOS agent trees.
//
// Android and iOS speak distinct wire protocols (HTTP+JSON versus
// newline-delimited TCP JSON) with no shared request or response
// schema between them. Protocol-specific request and response shapes
// live with their own transport client, not in this package.
package wire
