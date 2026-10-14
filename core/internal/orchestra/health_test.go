package orchestra

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/driver/mock"
)

func TestOrchestra_CheckHealth_HealthyDriverReportsHealthyTrue(t *testing.T) {
	d := mock.New()
	o, _ := newTestOrchestra(d)

	report := o.CheckHealth(context.Background())

	if !report.Selected {
		t.Fatalf("Selected = false, want true")
	}
	if !report.Healthy {
		t.Fatalf("Healthy = false, want true")
	}
	if report.Code != "" || report.Message != "" || report.Remedy != "" {
		t.Fatalf("healthy report carries code/message/remedy: %+v", report)
	}
}

func TestOrchestra_CheckHealth_UnreachableAgentReportsUnhealthyWithCode(t *testing.T) {
	d := mock.New()
	d.FailNextPingForContract(diag.New(
		diag.CodeAgentUnreachable,
		"could not reach the agent on device test-1",
		"Confirm the agent is running on the device, then try again.",
		nil,
	))
	o, _ := newTestOrchestra(d)

	report := o.CheckHealth(context.Background())

	if !report.Selected {
		t.Fatalf("Selected = false, want true")
	}
	if report.Healthy {
		t.Fatalf("Healthy = true, want false")
	}
	if report.Code != diag.CodeAgentUnreachable {
		t.Fatalf("Code = %q, want %q", report.Code, diag.CodeAgentUnreachable)
	}
	if report.Remedy == "" {
		t.Fatalf("Remedy is empty, want a non-empty remedy")
	}
}

func TestOrchestra_CheckHealth_CarriesTheAgentVersionsInBothOutcomes(t *testing.T) {
	info := driver.AgentInfo{ProtocolVersion: 4, AgentVersion: "0.1.0"}
	for _, tc := range []struct {
		name    string
		pingErr error
		healthy bool
	}{
		{"healthy", nil, true},
		{"unreachable", diag.New(diag.CodeAgentUnreachable, "no answer", "Try again.", nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := mock.New()
			d.Agent = info
			d.PingErr = tc.pingErr
			o, _ := newTestOrchestra(d)

			report := o.CheckHealth(context.Background())

			if report.Healthy != tc.healthy {
				t.Fatalf("Healthy = %v, want %v", report.Healthy, tc.healthy)
			}
			if report.AgentVersion != info.AgentVersion || report.ProtocolVersion != info.ProtocolVersion {
				t.Fatalf("versions = %q and %d, want %q and %d", report.AgentVersion, report.ProtocolVersion, info.AgentVersion, info.ProtocolVersion)
			}
		})
	}
}

func TestOrchestra_CheckHealth_OmitsUnreportedVersionsFromJSON(t *testing.T) {
	o, _ := newTestOrchestra(mock.New())

	encoded, err := json.Marshal(o.CheckHealth(context.Background()))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), "Version") {
		t.Fatalf("report %s carries a version field, want both omitted when the agent reports none", encoded)
	}
}
