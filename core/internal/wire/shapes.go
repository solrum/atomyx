package wire

// Point is a screen coordinate used by gesture and geometry data.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Size is a two-dimensional extent, such as a device screen size.
type Size struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Bounds is an axis-aligned rectangle expressed as its four edges.
type Bounds struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}
