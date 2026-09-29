package conformance

import (
	"flag"
	"os"
	"testing"
)

// updateGoldens is the -update flag: pass it to
// `go test ./internal/conformance/... -update` to regenerate every
// golden fixture under goldens/ from the Go implementation's current
// output, instead of diffing against it.
var updateGoldens = flag.Bool("update", false, "regenerate conformance goldens from the Go implementation")

func TestMain(m *testing.M) {
	flag.Parse()
	goldenUpdateMode = *updateGoldens
	os.Exit(m.Run())
}
