package iosdriver_test

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/version"
)

func connectExternal(t *testing.T, kind iosdriver.Kind, udid string, reported map[string]any) error {
	t.Helper()
	conn := newDefaultFakeConn()
	conn.Respond("ping", func(map[string]any) (map[string]any, error) {
		reply := map[string]any{"protocolVersion": version.AgentProtocolVersion, "capabilities": map[string]any{}}
		for k, v := range reported {
			reply[k] = v
		}
		return reply, nil
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{
		Background:     process.NewFakeBackground(),
		UDID:           udid,
		Kind:           kind,
		ExternalRunner: true,
		ExternalTunnel: true,
	})
	return d.Connect(context.Background())
}

func TestIosDriver_Connect_ExternalRunner_ChecksTheReportedIdentity(t *testing.T) {
	tests := []struct {
		name     string
		kind     iosdriver.Kind
		reported map[string]any
		wantErr  bool
		legacy   bool
	}{
		{"simulator, same udid in another case", iosdriver.KindSimulator, map[string]any{"simulatorUdid": "sim-1"}, false, false},
		{"simulator, swapped for another simulator", iosdriver.KindSimulator, map[string]any{"simulatorUdid": "SIM-2"}, true, false},
		{"simulator, runner reports no udid", iosdriver.KindSimulator, map[string]any{}, true, true},
		{"device, same udid in another case", iosdriver.KindDevice, map[string]any{"targetUdid": "dev-1"}, false, false},
		{"device, swapped for another device", iosdriver.KindDevice, map[string]any{"targetUdid": "DEV-2"}, true, false},
		{"device, runner reports no udid", iosdriver.KindDevice, map[string]any{}, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			udid := "SIM-1"
			if tc.kind == iosdriver.KindDevice {
				udid = "DEV-1"
			}

			err := connectExternal(t, tc.kind, udid, tc.reported)

			var idErr *iosdriver.RunnerIdentityError
			switch {
			case !tc.wantErr && err != nil:
				t.Fatalf("Connect() error = %v, want nil", err)
			case tc.wantErr && !errors.As(err, &idErr):
				t.Fatalf("Connect() error = %v, want *RunnerIdentityError", err)
			case tc.wantErr && idErr.Legacy != tc.legacy:
				t.Fatalf("Legacy = %v, want %v", idErr.Legacy, tc.legacy)
			}
		})
	}
}

func TestIosDriver_Connect_OwnedRunner_SkipsTheIdentityCheck(t *testing.T) {
	d, _ := connectedDriverAndConn(t, "")
	if !d.IsConnected() {
		t.Fatal("a driver without ExternalRunner must connect to a runner that reports no udid")
	}
}
