package iosdriver_test

import (
	"context"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/iosdriver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/transport"
)

func hierarchyFailureCode(t *testing.T, message string) string {
	t.Helper()
	conn := newFakeConn()
	conn.Respond("dumpRawTree", func(map[string]any) (map[string]any, error) {
		return nil, &transport.AgentError{Message: message}
	})
	d := iosdriver.New(conn, process.NewFake(), iosdriver.Options{Background: process.NewFakeBackground(), UDID: "SIM-1", Kind: iosdriver.KindSimulator})
	_, err := d.Hierarchy(context.Background())
	if err == nil {
		t.Fatal("Hierarchy() error = nil, want a failure")
	}
	code, _, _ := diag.Describe(err)
	return code
}

func TestIosDriver_Hierarchy_SnapshotFailureIsTransient(t *testing.T) {
	if got := hierarchyFailureCode(t, "snapshot failed: kAXErrorServerNotFound"); got != diag.CodeAgentNotReady {
		t.Fatalf("code = %q, want %q", got, diag.CodeAgentNotReady)
	}
}

func TestIosDriver_Hierarchy_OtherRejectionStaysPermanent(t *testing.T) {
	if got := hierarchyFailureCode(t, "no app bound, pass bundleIdCandidates or call launchApp first"); got != diag.CodeAgentRejected {
		t.Fatalf("code = %q, want %q", got, diag.CodeAgentRejected)
	}
}
