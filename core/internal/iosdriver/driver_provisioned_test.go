package iosdriver_test

import (
	"context"
	"testing"

	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
)

func TestIosDriver_Connect_Device_ExternalTunnelSuppressesIproxy(t *testing.T) {
	bg := process.NewFakeBackground()
	d := iosdriver.New(newDefaultFakeConn(), process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: freeTCPPort(t), Background: bg, ExternalTunnel: true,
	})

	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v, want the driver to dial the provisioner's tunnel", err)
	}
	if got := bg.Invocations(); len(got) != 0 {
		t.Fatalf("background starts = %+v, want no iproxy because the tunnel has an owner", got)
	}
}

func TestIosDriver_Connect_Device_WithoutExternalTunnelStillStartsIproxy(t *testing.T) {
	bg := &tunnelBackground{}
	d := iosdriver.New(newDefaultFakeConn(), process.NewFake(), iosdriver.Options{
		UDID: "DEV-1", Kind: iosdriver.KindDevice, Port: freeTCPPort(t), Background: bg,
	})
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() { _ = d.Disconnect() }()
	if got := bg.Invocations(); len(got) != 1 || got[0].Name != "iproxy" {
		t.Fatalf("background starts = %+v, want the driver's own iproxy", got)
	}
}
