package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeHealth struct {
	health AgentHealth
	err    error
}

func (f fakeHealth) AgentHealth(context.Context, string) (AgentHealth, error) { return f.health, f.err }

type protocolReply struct {
	protocol AgentProtocol
	err      error
}

type fakeProtocol struct {
	replies []protocolReply
	calls   int
}

func (f *fakeProtocol) AgentProtocol(context.Context, string) (AgentProtocol, error) {
	reply := f.replies[min(f.calls, len(f.replies)-1)]
	f.calls++
	return reply.protocol, reply.err
}

type fakeReinstaller struct {
	err   error
	calls int
}

func (f *fakeReinstaller) ReinstallAgent(context.Context, string) error {
	f.calls++
	return f.err
}

func TestAndroidAgentHealthStep(t *testing.T) {
	cases := []struct {
		name   string
		probe  fakeHealth
		status Status
		remedy string
	}{
		{"ready", fakeHealth{health: AgentHealth{Ready: true, AccessibilityConnected: true}}, StatusOK, ""},
		{"not ready", fakeHealth{}, StatusManual, "Unlock the device"},
		{"service not bound", fakeHealth{health: AgentHealth{Ready: true}}, StatusManual, "off and on"},
		{"unreachable", fakeHealth{err: errors.New("connection refused")}, StatusFailed, "logcat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewAndroidAgentHealthStep("s1", tc.probe).Check(context.Background())
			if got.Status != tc.status || !strings.Contains(got.Remedy, tc.remedy) {
				t.Errorf("got %+v, want status %s with remedy containing %q", got, tc.status, tc.remedy)
			}
		})
	}
}

func TestAndroidAgentProtocolCheck(t *testing.T) {
	ctx := context.Background()
	match := &fakeProtocol{replies: []protocolReply{{protocol: AgentProtocol{Agent: 1, Host: 1}}}}
	if got := NewAndroidAgentProtocolStep("s1", match, &fakeReinstaller{}).Check(ctx); got.Status != StatusOK {
		t.Errorf("match: %+v", got)
	}
	old := &fakeProtocol{replies: []protocolReply{{protocol: AgentProtocol{Agent: 0, Host: 1}}}}
	reinstaller := &fakeReinstaller{}
	got := NewAndroidAgentProtocolStep("s1", old, reinstaller).Check(ctx)
	if got.Status != StatusManual || !strings.Contains(got.Message, "version 0 but this host requires version 1") || !strings.Contains(got.Remedy, "assembleDebug") {
		t.Errorf("mismatch: %+v", got)
	}
	if reinstaller.calls != 0 {
		t.Error("Check must never reinstall")
	}
	failed := &fakeProtocol{replies: []protocolReply{{err: errors.New("no route")}}}
	if got := NewAndroidAgentProtocolStep("s1", failed, reinstaller).Check(ctx); got.Status != StatusFailed {
		t.Errorf("unreachable: %+v", got)
	}
}

func TestAndroidAgentProtocolFixReinstallsOnceAndChecksAgain(t *testing.T) {
	ctx := context.Background()
	probe := &fakeProtocol{replies: []protocolReply{{protocol: AgentProtocol{Agent: 1, Host: 1}}}}
	reinstaller := &fakeReinstaller{}
	got := NewAndroidAgentProtocolStep("s1", probe, reinstaller).Fix(ctx)
	if got.Status != StatusFixed || reinstaller.calls != 1 || probe.calls != 1 {
		t.Errorf("fix: %+v reinstalls=%d probes=%d", got, reinstaller.calls, probe.calls)
	}
}

func TestAndroidAgentProtocolFixFailures(t *testing.T) {
	ctx := context.Background()
	stale := AgentProtocol{Agent: 0, Host: 1}

	probe := &fakeProtocol{replies: []protocolReply{{protocol: stale}}}
	reinstaller := &fakeReinstaller{}
	got := NewAndroidAgentProtocolStep("s1", probe, reinstaller).Fix(ctx)
	if got.Status != StatusFailed || reinstaller.calls != 1 || !strings.HasPrefix(got.Message, "after a reinstall") {
		t.Errorf("still stale: %+v reinstalls=%d", got, reinstaller.calls)
	}

	got = NewAndroidAgentProtocolStep("s1", &fakeProtocol{replies: []protocolReply{{protocol: stale}}}, &fakeReinstaller{err: errors.New("no apk")}).Fix(ctx)
	if got.Status != StatusFailed || !strings.Contains(got.Message, "no apk") {
		t.Errorf("reinstall error: %+v", got)
	}

	got = NewAndroidAgentProtocolStep("s1", &fakeProtocol{replies: []protocolReply{{err: errors.New("gone")}}}, &fakeReinstaller{}).Fix(ctx)
	if got.Status != StatusFailed || !strings.Contains(got.Message, "after the reinstall") {
		t.Errorf("probe error after reinstall: %+v", got)
	}
}
