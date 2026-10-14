package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDeveloperDir struct {
	dir string
	err error
}

func (f fakeDeveloperDir) DeveloperDir(context.Context) (string, error) { return f.dir, f.err }

func TestIOSXcodeStep(t *testing.T) {
	present := fakeLocator{"xcrun": "/usr/bin/xcrun"}
	cases := []struct {
		name   string
		tools  ToolLocator
		dir    fakeDeveloperDir
		status Status
		remedy string
	}{
		{"ready", present, fakeDeveloperDir{dir: "/Applications/Xcode.app/Contents/Developer\n"}, StatusOK, ""},
		{"no xcrun", fakeLocator{}, fakeDeveloperDir{dir: "/x"}, StatusManual, "xcode-select --install"},
		{"nothing selected", present, fakeDeveloperDir{err: errors.New("no developer tools")}, StatusManual, "xcode-select -s"},
		{"blank selection", present, fakeDeveloperDir{dir: "  \n"}, StatusManual, "xcode-select -s"},
		{"command line tools only", present, fakeDeveloperDir{dir: "/Library/Developer/CommandLineTools\n"}, StatusManual, "Install Xcode from the App Store"},
		{"renamed Xcode bundle", present, fakeDeveloperDir{dir: "/Applications/Xcode-15.app/Contents/Developer"}, StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewIOSXcodeStep(tc.tools, tc.dir).Check(context.Background())
			if got.Status != tc.status || !strings.Contains(got.Remedy, tc.remedy) {
				t.Errorf("got %+v, want status %s with remedy containing %q", got, tc.status, tc.remedy)
			}
		})
	}
	ok := NewIOSXcodeStep(present, fakeDeveloperDir{dir: "/Applications/Xcode.app/Contents/Developer\n"}).Check(context.Background())
	if !strings.HasSuffix(ok.Message, "/Developer") {
		t.Errorf("the message keeps the directory without its newline: %q", ok.Message)
	}
}

func TestIOSIproxyStep(t *testing.T) {
	have := fakeLocator{"iproxy": "/opt/bin/iproxy"}
	cases := []struct {
		name   string
		target IOSTarget
		tools  ToolLocator
		status Status
	}{
		{"simulator without iproxy", IOSTargetSimulator, fakeLocator{}, StatusOK},
		{"physical with iproxy", IOSTargetPhysical, have, StatusOK},
		{"physical without iproxy", IOSTargetPhysical, fakeLocator{}, StatusManual},
		{"unknown target without iproxy", IOSTargetUnknown, fakeLocator{}, StatusManual},
		{"unknown target with iproxy", IOSTargetUnknown, have, StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewIOSIproxyStep(tc.target, tc.tools).Check(context.Background())
			if got.Status != tc.status {
				t.Fatalf("got %+v, want status %s", got, tc.status)
			}
			if tc.status == StatusManual && !strings.Contains(got.Remedy, "brew install libusbmuxd") {
				t.Errorf("remedy = %q", got.Remedy)
			}
		})
	}
	sim := NewIOSIproxyStep(IOSTargetSimulator, fakeLocator{}).Check(context.Background())
	if !strings.Contains(sim.Message, "not needed for a simulator") {
		t.Errorf("simulator message = %q", sim.Message)
	}
}
