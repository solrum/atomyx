package iosdriver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDigestFile_RoundTripAndRemoval(t *testing.T) {
	product := filepath.Join(t.TempDir(), "Runner.app")
	if got := readDigest(product); got != "" {
		t.Fatalf("readDigest() = %q, want empty before any write", got)
	}
	if err := writeDigest(product, "abc"); err != nil {
		t.Fatal(err)
	}
	if got := readDigest(product); got != "abc" {
		t.Fatalf("readDigest() = %q, want abc", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(product))
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries after the write, want only the digest file", len(entries))
	}
	removeDigest(product)
	if got := readDigest(product); got != "" {
		t.Fatalf("readDigest() = %q, want empty after removal", got)
	}
}
