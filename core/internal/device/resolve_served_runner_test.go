package device

import (
	"context"
	"errors"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/version"
)

const servedSimulatorUDID = "8DCBCBB0-A213-4AE0-96F8-3061DDAD4610"

type fakeRunnerIdentifier struct {
	id    ServedRunner
	ok    bool
	ports []int
}

func (f *fakeRunnerIdentifier) IdentifyRunner(_ context.Context, port int) (ServedRunner, bool) {
	f.ports = append(f.ports, port)
	return f.id, f.ok
}

func phone(id string) Descriptor {
	return Descriptor{ID: id, Platform: PlatformIOS, Name: "iPhone", Kind: KindDevice, State: StateOnline}
}

func TestIOSWithoutDeviceKeysOnServedRunner(t *testing.T) {
	runners := &fakeRunnerIdentifier{ok: true, id: ServedRunner{SimulatorUDID: servedSimulatorUDID}}

	desc, err := ResolveDescriptor(context.Background(), NewStaticProbe(), runners, PlatformIOS, "")
	if err != nil {
		t.Fatalf("ResolveDescriptor() error = %v", err)
	}
	identity, err := NewIdentityResolver(nil).Resolve(context.Background(), desc)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if identity.HardwareID != servedSimulatorUDID {
		t.Fatalf("HardwareID = %q, want %q reported by the served runner", identity.HardwareID, servedSimulatorUDID)
	}
	if desc.Kind != KindSimulator || desc.Port != iosControlPort || !desc.RunnerVerified {
		t.Fatalf("descriptor = %+v, want a simulator on the control port so nothing is provisioned", desc)
	}
	if len(runners.ports) != 1 || runners.ports[0] != iosControlPort {
		t.Fatalf("probed ports = %v, want the control port once", runners.ports)
	}
}

func TestResolveDescriptor_ServedRunnerThatNamesItsTargetDevice(t *testing.T) {
	runners := &fakeRunnerIdentifier{ok: true, id: ServedRunner{TargetUDID: "00008101-AAAA"}}

	desc, err := ResolveDescriptor(context.Background(), NewStaticProbe(), runners, PlatformIOS, "")

	if err != nil || desc.ID != "00008101-AAAA" || desc.Kind != KindDevice || desc.Port != iosControlPort || !desc.RunnerVerified {
		t.Fatalf("got %+v, %v, want the named physical device on the control port", desc, err)
	}
}

func TestResolveDescriptor_HandServedRunnerUsesTheSingleConnectedIPhone(t *testing.T) {
	runners := &fakeRunnerIdentifier{ok: true}
	probe := NewStaticProbe([]Descriptor{
		{ID: "SIM-UDID", Platform: PlatformIOS, Kind: KindSimulator, State: StateOnline},
		phone("00008101-AAAA"),
	})

	desc, err := ResolveDescriptor(context.Background(), probe, runners, PlatformIOS, "")

	if err != nil || desc.ID != "00008101-AAAA" || desc.Kind != KindDevice || desc.Port != iosControlPort || desc.RunnerVerified {
		t.Fatalf("got %+v, %v, want the connected iPhone on the control port", desc, err)
	}
}

func TestResolveDescriptor_NoDeviceIsDeviceNotFound(t *testing.T) {
	cases := []struct {
		name    string
		runners RunnerIdentifier
		probe   Probe
		reason  string
	}{
		{"no runner answers", &fakeRunnerIdentifier{}, NewStaticProbe(), "no runner answers"},
		{"no identifier wired", nil, NewStaticProbe(), "no runner is being served"},
		{"hand-served with no iPhone", &fakeRunnerIdentifier{ok: true}, NewStaticProbe(), "0 iPhones"},
		{"hand-served with two iPhones", &fakeRunnerIdentifier{ok: true}, NewStaticProbe([]Descriptor{phone("A"), phone("B")}), "2 iPhones"},
		{"hand-served with an offline iPhone", &fakeRunnerIdentifier{ok: true}, NewStaticProbe([]Descriptor{{ID: "A", Platform: PlatformIOS, Kind: KindDevice, State: StateOffline}}), "0 iPhones"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ResolveDescriptor(context.Background(), c.probe, c.runners, PlatformIOS, "")

			var nf *DeviceNotFoundError
			if !errors.As(err, &nf) || nf.ID != "" || !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("err = %v, want DeviceNotFoundError with an empty id mentioning %q", err, c.reason)
			}
		})
	}
}

func TestResolveDescriptor_ANamedIOSDeviceReusesOrProvisionsByServedRunner(t *testing.T) {
	const deviceUDID = "00008101-AAAA"
	hostVersion := version.AgentProtocolVersion
	cases := []struct {
		name     string
		id       string
		runner   *fakeRunnerIdentifier
		wantPort int
		wantCode string
	}{
		{"simulator match", "SIM-UDID", &fakeRunnerIdentifier{ok: true, id: ServedRunner{SimulatorUDID: "SIM-UDID", ProtocolVersion: hostVersion}}, iosControlPort, ""},
		{"simulator match ignores case", "sim-udid", &fakeRunnerIdentifier{ok: true, id: ServedRunner{SimulatorUDID: "SIM-UDID", ProtocolVersion: hostVersion}}, iosControlPort, ""},
		{"simulator protocol mismatch", "SIM-UDID", &fakeRunnerIdentifier{ok: true, id: ServedRunner{SimulatorUDID: "SIM-UDID", ProtocolVersion: hostVersion + 1}}, 0, diag.CodeAgentVersionMismatch},
		{"simulator other target", "SIM-UDID", &fakeRunnerIdentifier{ok: true, id: ServedRunner{SimulatorUDID: "OTHER", ProtocolVersion: hostVersion + 1}}, 0, ""},
		{"simulator legacy runner", "SIM-UDID", &fakeRunnerIdentifier{ok: true, id: ServedRunner{ProtocolVersion: hostVersion}}, 0, ""},
		{"simulator nothing answers", "SIM-UDID", &fakeRunnerIdentifier{}, 0, ""},
		{"simulator ignores a device target", "SIM-UDID", &fakeRunnerIdentifier{ok: true, id: ServedRunner{TargetUDID: "SIM-UDID", ProtocolVersion: hostVersion}}, 0, ""},
		{"device match", deviceUDID, &fakeRunnerIdentifier{ok: true, id: ServedRunner{TargetUDID: deviceUDID, ProtocolVersion: hostVersion}}, iosControlPort, ""},
		{"device match ignores case", "00008101-aaaa", &fakeRunnerIdentifier{ok: true, id: ServedRunner{TargetUDID: deviceUDID, ProtocolVersion: hostVersion}}, iosControlPort, ""},
		{"device protocol mismatch", deviceUDID, &fakeRunnerIdentifier{ok: true, id: ServedRunner{TargetUDID: deviceUDID}}, 0, diag.CodeAgentVersionMismatch},
		{"device other target", deviceUDID, &fakeRunnerIdentifier{ok: true, id: ServedRunner{TargetUDID: "OTHER", ProtocolVersion: hostVersion}}, 0, ""},
		{"device legacy runner", deviceUDID, &fakeRunnerIdentifier{ok: true, id: ServedRunner{ProtocolVersion: hostVersion}}, 0, ""},
		{"device nothing answers", deviceUDID, &fakeRunnerIdentifier{}, 0, ""},
		{"device ignores a simulator target", deviceUDID, &fakeRunnerIdentifier{ok: true, id: ServedRunner{SimulatorUDID: deviceUDID, ProtocolVersion: hostVersion}}, 0, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			desc, err := ResolveDescriptor(context.Background(), iosProbeFixture(), c.runner, PlatformIOS, c.id)

			if c.wantCode != "" {
				code, _, remedy := diag.Describe(err)
				if code != c.wantCode || !strings.Contains(remedy, "Stop the runner served on port 22087 or rebuild it") {
					t.Fatalf("err = %v, code %q, remedy %q, want %s with the stop-or-rebuild remedy", err, code, remedy, c.wantCode)
				}
				return
			}
			if err != nil || desc.Port != c.wantPort || !strings.EqualFold(desc.ID, c.id) || desc.RunnerVerified != (c.wantPort != 0) {
				t.Fatalf("got %+v, %v, want the scanned device with port %d", desc, err, c.wantPort)
			}
			if len(c.runner.ports) != 1 || c.runner.ports[0] != iosControlPort {
				t.Fatalf("probed ports = %v, want the control port once", c.runner.ports)
			}
		})
	}
}

func TestResolveDescriptor_ANamedIOSDeviceWithoutRunnerIdentifierProvisions(t *testing.T) {
	desc, err := ResolveDescriptor(context.Background(), iosProbeFixture(), nil, PlatformIOS, "SIM-UDID")

	if err != nil || desc.ID != "SIM-UDID" || desc.Port != 0 {
		t.Fatalf("got %+v, %v, want the scanned device with no port", desc, err)
	}
}
