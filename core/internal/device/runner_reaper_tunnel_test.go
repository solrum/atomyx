package device

import (
	"syscall"
	"testing"
	"time"

	"dev.atomyx/internal/process"
)

const reapTestTunnelPid = 4400

var reapTestTunnelStart = reapTestStart.Add(3 * time.Second)

// newTunnelReapFixture is a physical device runner record that also names
// a live iproxy tunnel leading its own group.
func newTunnelReapFixture(t *testing.T) *reapFixture {
	t.Helper()
	f := newReapFixture(t, IosAgentKindDevice)
	f.inspector.Set(process.ProcessInfo{
		PID: reapTestTunnelPid, PGID: reapTestTunnelPid, UID: reapTestUID, Command: "iproxy", StartTime: reapTestTunnelStart,
	})
	f.state.TunnelPid = reapTestTunnelPid
	f.state.TunnelStartTime = reapTestTunnelStart
	f.write()
	return f
}

func (f *reapFixture) signaledGroups() map[int][]syscall.Signal {
	got := map[int][]syscall.Signal{}
	for _, c := range f.signals.recorded() {
		got[c.pgid] = append(got[c.pgid], c.sig)
	}
	return got
}

func TestReap_AnOrphanedTunnelGetsSIGTERMWithItsRunner(t *testing.T) {
	f := newTunnelReapFixture(t)

	out := f.reap()

	groups := f.signaledGroups()
	if len(groups[reapTestTunnelPid]) != 1 || groups[reapTestTunnelPid][0] != syscall.SIGTERM {
		t.Fatalf("tunnel signals = %v, want one SIGTERM", groups[reapTestTunnelPid])
	}
	if len(groups[reapTestPid]) != 1 || !out.Signaled || !out.TunnelSignaled || out.TunnelKilled {
		t.Fatalf("outcome = %+v signals = %v, want the runner and the tunnel both ended by SIGTERM", out, groups)
	}
	f.assertRecordRemoved()
}

func TestReap_ATunnelStillAliveAfterItsRunnerDiedIsEnded(t *testing.T) {
	f := newTunnelReapFixture(t)
	f.inspector.Remove(reapTestPid)

	out := f.reap()

	groups := f.signaledGroups()
	if out.Signaled || !out.TunnelSignaled || len(groups[reapTestPid]) != 0 || len(groups[reapTestTunnelPid]) != 1 {
		t.Fatalf("outcome = %+v signals = %v, want only the tunnel signaled", out, groups)
	}
	f.assertRecordRemoved()
}

func TestReap_ATunnelThatIgnoresSIGTERMGetsSIGKILLAfterTheGracePeriod(t *testing.T) {
	f := newTunnelReapFixture(t)
	f.signals.ignoresTerm = map[int]bool{reapTestTunnelPid: true}
	f.signals.exitOn[syscall.SIGKILL] = true

	out := f.reap()

	got := f.signaledGroups()[reapTestTunnelPid]
	if len(got) != 2 || got[0] != syscall.SIGTERM || got[1] != syscall.SIGKILL || !out.TunnelKilled {
		t.Fatalf("tunnel signals = %v outcome = %+v, want SIGTERM then SIGKILL", got, out)
	}
}

func TestReap_ATunnelThatHasAlreadyExitedIsNotSignaled(t *testing.T) {
	f := newTunnelReapFixture(t)
	f.inspector.Remove(reapTestTunnelPid)

	out := f.reap()

	if out.TunnelSignaled || len(f.signaledGroups()[reapTestTunnelPid]) != 0 {
		t.Fatalf("outcome = %+v signals = %v, want the gone tunnel left alone", out, f.signaledGroups())
	}
	f.assertRecordRemoved()
}

func TestReap_AnESRCHFromTheTunnelGroupIsTolerated(t *testing.T) {
	f := newTunnelReapFixture(t)
	f.signals.failWith = map[int]error{reapTestTunnelPid: syscall.ESRCH}

	out := f.reap()

	if !out.TunnelSignaled || out.TunnelKilled {
		t.Fatalf("outcome = %+v, want the vanished tunnel group treated as ended", out)
	}
	f.assertRecordRemoved()
}

func TestReap_TunnelGuards(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(f *reapFixture)
	}{
		{"the pid was reused by a later process", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestTunnelPid, PGID: reapTestTunnelPid, UID: reapTestUID, Command: "iproxy", StartTime: reapTestTunnelStart.Add(time.Hour)})
		}},
		{"the process belongs to another user", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestTunnelPid, PGID: reapTestTunnelPid, UID: reapTestUID + 1, Command: "iproxy", StartTime: reapTestTunnelStart})
		}},
		{"the process is not iproxy", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestTunnelPid, PGID: reapTestTunnelPid, UID: reapTestUID, Command: "sshd", StartTime: reapTestTunnelStart})
		}},
		{"the process left its own group", func(f *reapFixture) {
			f.inspector.Set(process.ProcessInfo{PID: reapTestTunnelPid, PGID: reapTestTunnelPid + 1, UID: reapTestUID, Command: "iproxy", StartTime: reapTestTunnelStart})
		}},
		{"the tunnel is the caller's own group", func(f *reapFixture) {
			f.state.TunnelPid = reapTestOwnPG
			f.inspector.Set(process.ProcessInfo{PID: reapTestOwnPG, PGID: reapTestOwnPG, UID: reapTestUID, Command: "iproxy", StartTime: reapTestTunnelStart})
			f.write()
		}},
		{"the pid cannot be a process", func(f *reapFixture) {
			f.state.TunnelPid = 1
			f.write()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTunnelReapFixture(t)
			tc.mutate(f)

			out := f.reap()

			if out.TunnelSignaled {
				t.Fatalf("outcome = %+v, want the tunnel left alone", out)
			}
			for pgid := range f.signaledGroups() {
				if pgid != reapTestPid {
					t.Fatalf("signals = %v, want only the runner group signaled", f.signaledGroups())
				}
			}
			f.assertRecordRemoved()
		})
	}
}

func TestReap_ARecordFromBeforeARebootLeavesTheTunnelAloneToo(t *testing.T) {
	f := newTunnelReapFixture(t)
	f.state.HostBootTime = reapTestBoot.Add(-time.Hour)
	f.write()

	out := f.reap()

	if out.TunnelSignaled || out.Discarded == "" {
		t.Fatalf("outcome = %+v, want the stale record discarded with no tunnel signal", out)
	}
	f.assertNoSignal()
}
