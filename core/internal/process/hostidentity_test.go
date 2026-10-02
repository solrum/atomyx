package process

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSystemInspector_ReportsTheRunningProcess(t *testing.T) {
	info, err := SystemInspector{}.Inspect(os.Getpid())
	if err != nil {
		t.Fatalf("Inspect(self) error = %v", err)
	}
	if info.PID != os.Getpid() || info.UID != os.Geteuid() || info.PGID != OwnProcessGroup() {
		t.Fatalf("Inspect(self) = %+v, want pid %d uid %d pgid %d", info, os.Getpid(), os.Geteuid(), OwnProcessGroup())
	}
	if info.Command == "" || !strings.HasPrefix(filepath.Base(os.Args[0]), info.Command) {
		t.Fatalf("Inspect(self).Command = %q, want the test binary name", info.Command)
	}
	if info.StartTime.IsZero() || info.StartTime.After(time.Now().Add(time.Second)) {
		t.Fatalf("Inspect(self).StartTime = %v, want a past time", info.StartTime)
	}
}

func TestSystemInspector_AnExitedProcessIsNotFound(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := (SystemInspector{}).Inspect(cmd.Process.Pid); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("Inspect(exited pid) error = %v, want ErrProcessNotFound", err)
	}
	if _, err := (SystemInspector{}).Inspect(0); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("Inspect(0) error = %v, want ErrProcessNotFound", err)
	}
}

func TestSystemInspector_BootTimePrecedesProcessStart(t *testing.T) {
	boot, err := SystemInspector{}.BootTime()
	if err != nil {
		t.Fatalf("BootTime() error = %v", err)
	}
	self, err := SystemInspector{}.Inspect(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !boot.Before(self.StartTime) {
		t.Fatalf("boot %v is not before this process start %v", boot, self.StartTime)
	}
}

func TestFakeInspector_ReportsWhatTheTestSets(t *testing.T) {
	f := NewFakeInspector(time.Unix(100, 0))
	f.Set(ProcessInfo{PID: 7, Command: "xcodebuild"})
	if got, err := f.Inspect(7); err != nil || got.Command != "xcodebuild" {
		t.Fatalf("Inspect(7) = %+v, %v", got, err)
	}
	f.Remove(7)
	if _, err := f.Inspect(7); !errors.Is(err, ErrProcessNotFound) {
		t.Fatalf("Inspect(removed) error = %v, want ErrProcessNotFound", err)
	}
	if boot, _ := f.BootTime(); !boot.Equal(time.Unix(100, 0)) {
		t.Fatalf("BootTime() = %v", boot)
	}
}
