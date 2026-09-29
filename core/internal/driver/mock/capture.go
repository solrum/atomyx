package mock

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// syntheticCaptureImagePNG is the fixed, PNG-decodable payload Driver
// returns for every mid-gesture capture waypoint, so a test asserting
// on the routed artifact gets a real, stable image rather than an
// empty placeholder.
var syntheticCaptureImagePNG = encodeSyntheticCaptureImage()

func encodeSyntheticCaptureImage() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0x1a, G: 0x2b, B: 0x3c, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("mock: encoding the synthetic 1x1 capture image failed: " + err.Error())
	}
	return buf.Bytes()
}
