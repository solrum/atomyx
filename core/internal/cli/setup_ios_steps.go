package cli

import (
	"context"
	"errors"
	"os"
	"strings"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/setup"
)

var errHostCommandMissing = errors.New("this build cannot run host commands")

// hostToolOutput answers the iOS setup ports by running host tools
// through the command runner the composition root supplied.
type hostToolOutput struct{ run HostCommand }

func (h hostToolOutput) DeveloperDir(ctx context.Context) (string, error) {
	if h.run == nil {
		return "", errHostCommandMissing
	}
	out, err := h.run(ctx, "xcode-select", "-p")
	return strings.TrimSpace(out), err
}

func (h hostToolOutput) CodeSigningIdentities(ctx context.Context) (string, error) {
	if h.run == nil {
		return "", errHostCommandMissing
	}
	return h.run(ctx, "security", "find-identity", "-v", "-p", "codesigning")
}

func (d Deps) developerDirReader() setup.DeveloperDirReader {
	return hostToolOutput{run: d.HostCommand}
}

func (d Deps) signingIdentityLister() setup.SigningIdentityLister {
	return hostToolOutput{run: d.HostCommand}
}

// iosTarget classifies the resolved device the way the runner
// provisioner does: a simulator kind is a simulator and any other
// resolved kind is a physical device. A target that was not resolved
// stays unknown.
func iosTarget(t setupTarget) setup.IOSTarget {
	switch t.Kind {
	case "":
		return setup.IOSTargetUnknown
	case device.KindSimulator:
		return setup.IOSTargetSimulator
	default:
		return setup.IOSTargetPhysical
	}
}

func iosXcodeEntry(t setupTarget) setup.Entry {
	return setup.Entry{Step: setup.NewIOSXcodeStep(t.Deps.toolLocator(), t.Deps.developerDirReader())}
}

func iosIproxyEntry(t setupTarget) setup.Entry {
	return setup.Entry{Step: setup.NewIOSIproxyStep(iosTarget(t), t.Deps.toolLocator())}
}

func iosSigningTeamEntry(t setupTarget) setup.Entry {
	return setup.Entry{Step: setup.NewIOSSigningTeamStep(iosTarget(t), os.Getenv("ATOMYX_DEV_TEAM"), t.Deps.signingIdentityLister())}
}
