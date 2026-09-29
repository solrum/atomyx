package orchestra

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
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
