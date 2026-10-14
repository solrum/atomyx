package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeHealth struct {
	ready, connected bool
	err              error
}

func (f fakeHealth) AgentHealth(context.Context, string) (bool, bool, error) {
	return f.ready, f.connected, f.err
}

// fakeStarter starts an agent by flipping a scripted health probe.
type fakeStarter struct {
	err   error
	calls int
}

func (f *fakeStarter) StartAgent(context.Context, string) error {
	f.calls++
	return f.err
}

// startableHealth reports an unreachable agent until its starter ran.
type startableHealth struct {
	starter *fakeStarter
	after   fakeHealth
}

func (h startableHealth) AgentHealth(context.Context, string) (bool, bool, error) {
	if h.starter.calls == 0 {
		return false, false, errors.New("connection refused")
	}
	return h.after.ready, h.after.connected, h.after.err
}

type protocolReply struct {
	agent, host int
	err         error
}

type fakeProtocol struct {
	replies []protocolReply
	calls   int
}

func (f *fakeProtocol) AgentProtocol(context.Context, string) (int, int, error) {
	reply := f.replies[min(f.calls, len(f.replies)-1)]
	f.calls++
	return reply.agent, reply.host, reply.err
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
		{"ready", fakeHealth{ready: true, connected: true}, StatusOK, ""},
		{"not ready", fakeHealth{}, StatusManual, "Unlock the device"},
		{"service not bound", fakeHealth{ready: true}, StatusManual, "off and on"},
		{"unreachable", fakeHealth{err: errors.New("connection refused")}, StatusManual, "open the Atomyx app"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewAndroidAgentHealthStep("s1", tc.probe, &fakeStarter{}).Check(context.Background())
			if got.Status != tc.status || !strings.Contains(got.Remedy, tc.remedy) {
				t.Errorf("got %+v, want status %s with remedy containing %q", got, tc.status, tc.remedy)
			}
		})
	}
}

func TestAndroidAgentProtocolCheck(t *testing.T) {
	ctx := context.Background()
	match := &fakeProtocol{replies: []protocolReply{{agent: 1, host: 1}}}
	if got := NewAndroidAgentProtocolStep("s1", match, &fakeReinstaller{}, &fakeAccessibility{}).Check(ctx); got.Status != StatusOK {
		t.Errorf("match: %+v", got)
	}
	old := &fakeProtocol{replies: []protocolReply{{agent: 0, host: 1}}}
	reinstaller := &fakeReinstaller{}
	got := NewAndroidAgentProtocolStep("s1", old, reinstaller, &fakeAccessibility{}).Check(ctx)
	if got.Status != StatusManual || !strings.Contains(got.Message, "version 0 but this host requires version 1") || !strings.Contains(got.Remedy, "assembleDebug") {
		t.Errorf("mismatch: %+v", got)
	}
	if reinstaller.calls != 0 {
		t.Error("Check must never reinstall")
	}
	failed := &fakeProtocol{replies: []protocolReply{{err: errors.New("no route")}}}
	if got := NewAndroidAgentProtocolStep("s1", failed, reinstaller, &fakeAccessibility{}).Check(ctx); got.Status != StatusFailed {
		t.Errorf("unreachable: %+v", got)
	}
}

func TestAndroidAgentProtocolFixReinstallsOnceAndChecksAgain(t *testing.T) {
	ctx := context.Background()
	probe := &fakeProtocol{replies: []protocolReply{{agent: 1, host: 1}}}
	reinstaller := &fakeReinstaller{}
	got := NewAndroidAgentProtocolStep("s1", probe, reinstaller, &fakeAccessibility{}).Fix(ctx)
	if got.Status != StatusFixed || reinstaller.calls != 1 || probe.calls != 1 {
		t.Errorf("fix: %+v reinstalls=%d probes=%d", got, reinstaller.calls, probe.calls)
	}
}

func TestAndroidAgentProtocolFixFailures(t *testing.T) {
	ctx := context.Background()
	stale := protocolReply{agent: 0, host: 1}

	probe := &fakeProtocol{replies: []protocolReply{stale}}
	reinstaller := &fakeReinstaller{}
	got := NewAndroidAgentProtocolStep("s1", probe, reinstaller, &fakeAccessibility{}).Fix(ctx)
	if got.Status != StatusFailed || reinstaller.calls != 1 || !strings.HasPrefix(got.Message, "after a reinstall") {
		t.Errorf("still stale: %+v reinstalls=%d", got, reinstaller.calls)
	}

	got = NewAndroidAgentProtocolStep("s1", &fakeProtocol{replies: []protocolReply{stale}}, &fakeReinstaller{err: errors.New("no apk")}, &fakeAccessibility{}).Fix(ctx)
	if got.Status != StatusFailed || !strings.Contains(got.Message, "no apk") {
		t.Errorf("reinstall error: %+v", got)
	}

	got = NewAndroidAgentProtocolStep("s1", &fakeProtocol{replies: []protocolReply{{err: errors.New("gone")}}}, &fakeReinstaller{}, &fakeAccessibility{}).Fix(ctx)
	if got.Status != StatusFailed || !strings.Contains(got.Message, "after the reinstall") {
		t.Errorf("probe error after reinstall: %+v", got)
	}
}

func TestAndroidAgentProtocolFixStatesTheServicesTheReinstallDisabled(t *testing.T) {
	ctx := context.Background()
	probe := &fakeProtocol{replies: []protocolReply{{agent: 1, host: 1}}}
	services := &fakeAccessibility{others: []string{"com.a/.One"}}
	got := NewAndroidAgentProtocolStep("s1", probe, &fakeReinstaller{}, services).Fix(ctx)
	if got.Status != StatusFixed || !strings.Contains(got.Message, "now disabled: com.a/.One") {
		t.Errorf("fix must name the disabled services: %+v", got)
	}
	reinstaller := &fakeReinstaller{}
	got = NewAndroidAgentProtocolStep("s1", probe, reinstaller, &fakeAccessibility{readErr: errors.New("no shell")}).Fix(ctx)
	if got.Status != StatusFailed || reinstaller.calls != 0 {
		t.Errorf("an unreadable service list must stop before the reinstall: %+v", got)
	}
}

func TestAndroidAgentHealthFixStartsAnAgentThatDoesNotAnswer(t *testing.T) {
	starter := &fakeStarter{}
	probe := startableHealth{starter: starter, after: fakeHealth{ready: true, connected: true}}
	got := NewAndroidAgentHealthStep("s1", probe, starter).Fix(context.Background())
	if got.Status != StatusFixed || starter.calls != 1 {
		t.Errorf("fix: %+v starts=%d", got, starter.calls)
	}
}

func TestAndroidAgentHealthCheckNeverStarts(t *testing.T) {
	starter := &fakeStarter{}
	NewAndroidAgentHealthStep("s1", startableHealth{starter: starter}, starter).Check(context.Background())
	if starter.calls != 0 {
		t.Error("Check must never start the agent")
	}
}

func TestAndroidAgentHealthFixLeavesAnAnsweringAgentToTheOperator(t *testing.T) {
	starter := &fakeStarter{}
	got := NewAndroidAgentHealthStep("s1", fakeHealth{ready: true}, starter).Fix(context.Background())
	if got.Status != StatusManual || starter.calls != 0 || !strings.Contains(got.Remedy, "off and on") {
		t.Errorf("answering but unbound agent: %+v starts=%d", got, starter.calls)
	}
}

func TestAndroidAgentHealthFixFailures(t *testing.T) {
	failing := &fakeStarter{err: errors.New("service refused")}
	got := NewAndroidAgentHealthStep("s1", startableHealth{starter: failing}, failing).Fix(context.Background())
	if got.Status != StatusFailed || !strings.Contains(got.Message, "service refused") || !strings.Contains(got.Remedy, "Open the Atomyx app") {
		t.Errorf("start error: %+v", got)
	}
	starter := &fakeStarter{}
	probe := startableHealth{starter: starter, after: fakeHealth{ready: true}}
	got = NewAndroidAgentHealthStep("s1", probe, starter).Fix(context.Background())
	if got.Status != StatusManual || !strings.Contains(got.Message, "accessibility service is not connected") {
		t.Errorf("started but unbound keeps the manual result: %+v", got)
	}
}
