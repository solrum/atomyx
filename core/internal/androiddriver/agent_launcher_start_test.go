package androiddriver_test

import (
	"context"
	"errors"
	"testing"

	"dev.atomyx/internal/androiddriver"
	"dev.atomyx/internal/process"
)

func TestAgentLauncher_Start_StartsTheServiceWithoutInstallingOrRebinding(t *testing.T) {
	runner := process.NewFake()
	prober := newFakeHealthProber(notReady(), healthy())
	l := androiddriver.NewAgentLauncher(runner, newFakeClock(0), prober, androiddriver.AgentLauncherOptions{Serial: "S1"})

	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	wantArgs := [][]string{
		{"-s", "S1", "forward", "tcp:8765", "tcp:8765"},
		{"-s", "S1", "shell", "am", "start-foreground-service", "-n", "dev.atomyx.agent/.control.AtomyxForegroundService"},
	}
	got := runner.Invocations()
	if len(got) != len(wantArgs) {
		t.Fatalf("adb invocations = %+v, want exactly %d", got, len(wantArgs))
	}
	for i, want := range wantArgs {
		if !equalArgs(got[i].Args, want) {
			t.Errorf("invocation[%d] args = %v, want %v", i, got[i].Args, want)
		}
	}
}

func TestAgentLauncher_Start_TimesOutWhenTheAgentNeverAnswers(t *testing.T) {
	l := androiddriver.NewAgentLauncher(process.NewFake(), newFakeClock(0), newFakeHealthProber(notReady()), androiddriver.AgentLauncherOptions{Serial: "S1", LaunchTimeoutMs: 2_000})

	err := l.Start(context.Background())
	var launchErr *androiddriver.AgentLauncherError
	if !errors.As(err, &launchErr) || launchErr.Code != androiddriver.ErrCodeLaunchTimeout {
		t.Fatalf("Start() error = %v, want a launch-timeout AgentLauncherError", err)
	}
}
