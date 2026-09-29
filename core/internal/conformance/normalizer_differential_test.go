package conformance

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/transport"
)

func TestNormalizerDifferential_Android(t *testing.T) {
	cases, err := LoadNormalizerCorpus("corpus/normalizers/android")
	if err != nil {
		t.Fatalf("loading android normalizer corpus: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var raw transport.AndroidTreeNode
			if err := json.Unmarshal(c.RawTree, &raw); err != nil {
				t.Fatalf("decoding rawTree: %v", err)
			}
			goOutput, err := json.Marshal(androiddriver.NormalizeAndroidTree(raw))
			if err != nil {
				t.Fatalf("marshaling Go normalizer output: %v", err)
			}

			checkGolden(t, filepath.Join("normalizers", "android", c.Name+".json"), goOutput, DiffJSON)
		})
	}
}

func TestNormalizerDifferential_Ios(t *testing.T) {
	cases, err := LoadNormalizerCorpus("corpus/normalizers/ios")
	if err != nil {
		t.Fatalf("loading ios normalizer corpus: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			var raw iosdriver.IosRawElement
			if err := json.Unmarshal(c.RawTree, &raw); err != nil {
				t.Fatalf("decoding rawTree: %v", err)
			}
			goOutput, err := json.Marshal(iosdriver.NormalizeIosTree(raw))
			if err != nil {
				t.Fatalf("marshaling Go normalizer output: %v", err)
			}

			checkGolden(t, filepath.Join("normalizers", "ios", c.Name+".json"), goOutput, DiffJSON)
		})
	}
}
