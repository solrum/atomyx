package iosdriver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// xctestDigestSuffix is appended to a runner product's path to name
	// the file that records the digest the product was built from.
	xctestDigestSuffix = ".digest"
	// xctestDigestFileMode keeps the digest file private to the current
	// user.
	xctestDigestFileMode = 0o600
)

// digestPath names the file recording the digest a product was built
// from.
func digestPath(productPath string) string { return productPath + xctestDigestSuffix }

// readDigest returns the digest recorded for the product at
// productPath, or an empty string when none is recorded.
func readDigest(productPath string) string {
	data, err := os.ReadFile(digestPath(productPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// removeDigest deletes the digest recorded for the product at
// productPath, so a product whose build did not finish never carries
// the digest of the build before it.
func removeDigest(productPath string) {
	_ = os.Remove(digestPath(productPath))
}

// writeDigest records digest for the product at productPath atomically,
// so a reader sees either no digest or the complete one.
func writeDigest(productPath, digest string) error {
	path := digestPath(productPath)
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("iosdriver: create the digest file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := os.Chmod(tmpPath, xctestDigestFileMode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("iosdriver: restrict the digest file: %w", err)
	}
	if _, err := tmp.WriteString(digest + "\n"); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("iosdriver: write the digest file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("iosdriver: write the digest file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("iosdriver: publish the digest file: %w", err)
	}
	return nil
}
