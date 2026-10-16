package device

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/version"
)

const tunneledDeviceUDID = "00008101-AAAA"

type fakeTunnelIdentifier struct {
	fakeRunnerIdentifier
	tunneled   ServedRunner
	tunneledOK bool
	udids      []string
}

func (f *fakeTunnelIdentifier) IdentifyDeviceRunner(_ context.Context, udid string) (ServedRunner, bool) {
	f.udids = append(f.udids, udid)
	return f.tunneled, f.tunneledOK
}

func TestReuseServedRunner_DeviceWithoutHostListenerReusesTheTunneledRunner(t *testing.T) {
	runners := &fakeTunnelIdentifier{
		tunneled:   ServedRunner{TargetUDID: tunneledDeviceUDID, ProtocolVersion: version.AgentProtocolVersion},
		tunneledOK: true,
	}

	desc, err := ResolveDescriptor(context.Background(), iosProbeFixture(), runners, PlatformIOS, tunneledDeviceUDID)

	if err != nil || desc.Port != iosControlPort || !desc.RunnerVerified {
		t.Fatalf("got %+v, %v, want the control port with the runner verified so no runner is launched", desc, err)
	}
	if len(runners.udids) != 1 || runners.udids[0] != desc.ID {
		t.Fatalf("tunneled lookups = %v, want one for %s", runners.udids, desc.ID)
	}
}

func TestReuseServedRunner_TunneledRunnerKeepsTheProtections(t *testing.T) {
	hostVersion := version.AgentProtocolVersion
	cases := []struct {
		name     string
		runner   ServedRunner
		ok       bool
		wantPort int
		wantCode string
	}{
		{"other device", ServedRunner{TargetUDID: "OTHER", ProtocolVersion: hostVersion}, true, 0, ""},
		{"runner that names no device", ServedRunner{ProtocolVersion: hostVersion}, true, 0, ""},
		{"protocol mismatch", ServedRunner{TargetUDID: tunneledDeviceUDID, ProtocolVersion: hostVersion + 1}, true, 0, diag.CodeAgentVersionMismatch},
		{"nothing answers through the tunnel", ServedRunner{}, false, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runners := &fakeTunnelIdentifier{tunneled: c.runner, tunneledOK: c.ok}

			desc, err := ResolveDescriptor(context.Background(), iosProbeFixture(), runners, PlatformIOS, tunneledDeviceUDID)

			if c.wantCode != "" {
				if code, _, _ := diag.Describe(err); code != c.wantCode {
					t.Fatalf("err = %v, want code %s", err, c.wantCode)
				}
				return
			}
			if err != nil || desc.Port != c.wantPort || desc.RunnerVerified {
				t.Fatalf("got %+v, %v, want the device left for a launch", desc, err)
			}
		})
	}
}

func TestReuseServedRunner_HostListenerWinsOverTheTunnel(t *testing.T) {
	runners := &fakeTunnelIdentifier{
		fakeRunnerIdentifier: fakeRunnerIdentifier{ok: true, id: ServedRunner{TargetUDID: tunneledDeviceUDID, ProtocolVersion: version.AgentProtocolVersion}},
	}

	desc, err := ResolveDescriptor(context.Background(), iosProbeFixture(), runners, PlatformIOS, tunneledDeviceUDID)

	if err != nil || desc.Port != iosControlPort || !desc.RunnerVerified {
		t.Fatalf("got %+v, %v, want the host listener reused", desc, err)
	}
	if len(runners.udids) != 0 {
		t.Fatalf("tunneled lookups = %v, want none when the host port answers", runners.udids)
	}
}

func TestReuseServedRunner_SimulatorNeverOpensATunnel(t *testing.T) {
	runners := &fakeTunnelIdentifier{tunneledOK: true, tunneled: ServedRunner{SimulatorUDID: "SIM-UDID", ProtocolVersion: version.AgentProtocolVersion}}

	desc, err := ResolveDescriptor(context.Background(), iosProbeFixture(), runners, PlatformIOS, "SIM-UDID")

	if err != nil || desc.Port != 0 || len(runners.udids) != 0 {
		t.Fatalf("got %+v, %v, lookups %v, want a simulator left for a launch with no tunnel", desc, err, runners.udids)
	}
}

func TestTunnelRunnerIdentifier_ReportsNothingWhenItCannotOpenATunnel(t *testing.T) {
	cases := map[string]TunnelRunnerIdentifier{
		"no background runner": {},
		"no free port":         {Background: process.NewFakeBackground(), freePort: func() (int, error) { return 0, errors.New("exhausted") }},
		"tunnel start fails":   {Background: failingTunnelBackground(), freePort: func() (int, error) { return 54321, nil }},
	}
	for name, identifier := range cases {
		t.Run(name, func(t *testing.T) {
			if got, ok := identifier.IdentifyDeviceRunner(context.Background(), tunneledDeviceUDID); ok {
				t.Fatalf("got %+v, want no runner", got)
			}
		})
	}
}

func failingTunnelBackground() *process.FakeBackground {
	bg := process.NewFakeBackground()
	bg.DefaultErr = errors.New("iproxy is not installed")
	return bg
}
