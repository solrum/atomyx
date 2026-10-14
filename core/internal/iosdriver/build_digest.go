package iosdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// xctestProjectSpecFileName is the project generator spec at the
	// project root. It decides the project file's content, so it is a
	// compiled input.
	xctestProjectSpecFileName = "project.yml"
	// xctestPbxprojFileName is the project file inside the .xcodeproj.
	xctestPbxprojFileName = "project.pbxproj"
	// xcodeProbeTimeout bounds each command that reads the selected
	// Xcode.
	xcodeProbeTimeout  = 15 * time.Second
	finderMetadataName = ".DS_Store"
)

// xcodeIdentityCache holds the selected Xcode identity after its first
// successful read, so one launcher reads it once.
type xcodeIdentityCache struct {
	mu    sync.Mutex
	value string
}

// buildDigest returns the SHA-256 digest of everything that decides the
// content of the compiled runner product for this launch's platform.
//
// The inputs are the relative path and content of every regular file
// under the Tests, Sources and App directories (Finder metadata
// excluded), the project spec and the project file, the destination
// kind, and the selected Xcode developer directory and version. A
// physical-device build also covers the signing team, the signing
// style and the provisioning profile, because a simulator build
// overrides every signing setting with constants. A missing source
// directory contributes nothing, and any other read failure is returned.
func (l *XctestLauncher) buildDigest(ctx context.Context) (string, error) {
	sum := sha256.New()
	writeDigestField(sum, "kind", string(l.cfg.Kind))
	if l.cfg.Kind == KindDevice {
		writeDigestField(sum, "devTeam", l.cfg.DevTeam)
		writeDigestField(sum, "codeSignStyle", l.cfg.CodeSignStyle)
		writeDigestField(sum, "provisioningProfile", l.cfg.ProvisioningProfileSpecifier)
	}
	writeDigestField(sum, "xcode", l.selectedXcode(ctx))

	files, err := l.digestInputFiles()
	if err != nil {
		return "", err
	}
	for _, rel := range files {
		if err := writeDigestFile(sum, filepath.Join(l.cfg.ProjectDir, rel), rel); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// digestInputFiles lists, sorted, the project-relative paths of every
// file that feeds the digest.
func (l *XctestLauncher) digestInputFiles() ([]string, error) {
	files := []string{
		xctestProjectSpecFileName,
		filepath.Join(l.cfg.projectFileName(), xctestPbxprojFileName),
	}
	for _, name := range xctestSourceDirNames {
		err := filepath.WalkDir(filepath.Join(l.cfg.ProjectDir, name), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() || d.Name() == finderMetadataName {
				return nil
			}
			rel, relErr := filepath.Rel(l.cfg.ProjectDir, path)
			if relErr != nil {
				return relErr
			}
			files = append(files, rel)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("iosdriver: scan %s for the build digest: %w", name, err)
		}
	}
	sort.Strings(files)
	return files, nil
}

// writeDigestFile adds one file to sum. An absent file is recorded as
// absent, so deleting an input changes the digest.
func writeDigestFile(sum hash.Hash, path, rel string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeDigestField(sum, "absent", filepath.ToSlash(rel))
			return nil
		}
		return fmt.Errorf("iosdriver: read %s for the build digest: %w", rel, err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("iosdriver: stat %s for the build digest: %w", rel, err)
	}
	writeDigestField(sum, "file", fmt.Sprintf("%s\x00%d", filepath.ToSlash(rel), info.Size()))
	if _, err := io.Copy(sum, file); err != nil {
		return fmt.Errorf("iosdriver: read %s for the build digest: %w", rel, err)
	}
	return nil
}

// writeDigestField adds a length-prefixed name and value to sum, so two
// different field sequences never produce the same byte stream.
func writeDigestField(sum hash.Hash, name, value string) {
	_, _ = fmt.Fprintf(sum, "%d:%s=%d:%s\n", len(name), name, len(value), value)
}

// selectedXcode reads the selected Xcode developer directory and
// version through the launcher's command runner. A command that fails
// contributes an empty line, so a missing toolchain does not hide a
// source change, and the failure itself surfaces when the build runs.
func (l *XctestLauncher) selectedXcode(ctx context.Context) string {
	l.xcode.mu.Lock()
	defer l.xcode.mu.Unlock()
	if l.xcode.value != "" {
		return l.xcode.value
	}
	developerDir, dirOK := l.hostOutput(ctx, "xcode-select", "-p")
	version, versionOK := l.hostOutput(ctx, "xcodebuild", "-version")
	value := developerDir + "\n" + version
	if dirOK && versionOK {
		l.xcode.value = value
	}
	return value
}

func (l *XctestLauncher) hostOutput(ctx context.Context, name string, args ...string) (string, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, xcodeProbeTimeout)
	defer cancel()
	result, err := l.proc.Run(probeCtx, name, args...)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(result.Stdout), true
}
