package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/shard"
)

func TestParseShardRequestRejectsConflictingOrEmptyFlags(t *testing.T) {
	cases := []struct {
		name  string
		flags map[string]FlagValue
		want  string
	}{
		{"device and devices", map[string]FlagValue{"--device": "a", "--devices": "a,b"}, "cannot be combined"},
		{"device and shard", map[string]FlagValue{"--device": "a", "--shard": "auto"}, "cannot be combined"},
		{"devices and shard", map[string]FlagValue{"--devices": "a,b", "--shard": "auto"}, "cannot be combined"},
		{"unknown shard mode", map[string]FlagValue{"--shard": "balanced"}, `accepts only "auto"`},
		{"only separators", map[string]FlagValue{"--devices": " , ,"}, "at least one device id"},
	}
	for _, tc := range cases {
		if _, err := parseShardRequest(tc.flags); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
}

func TestParseShardRequestSplitsTrimsAndDeduplicatesDeviceIDs(t *testing.T) {
	req, err := parseShardRequest(map[string]FlagValue{"--devices": "emulator-5554, 192.168.50.29:35901,emulator-5554,"})
	if err != nil || req.auto || !reflect.DeepEqual(req.ids, []string{"emulator-5554", "192.168.50.29:35901"}) {
		t.Fatalf("request = %+v, %v", req, err)
	}
	if req, err := parseShardRequest(map[string]FlagValue{"--shard": "auto"}); err != nil || !req.auto {
		t.Fatalf("request = %+v, %v, want auto", req, err)
	}
}

func TestShardRequestedIgnoresAWorker(t *testing.T) {
	if !shardRequested(map[string]FlagValue{"--shard": "auto"}) || !shardRequested(map[string]FlagValue{"--devices": "a"}) {
		t.Error("a sharding flag was not recognized")
	}
	if shardRequested(map[string]FlagValue{"--device": "a"}) || shardRequested(map[string]FlagValue{"--worker": true, "--devices": "a"}) {
		t.Error("a single device or a worker was taken for a sharding parent")
	}
}

func TestResolveShardDevicesAutoTakesEveryReadyDeviceOfThePlatform(t *testing.T) {
	probe := device.NewStaticProbe([]device.Descriptor{
		{ID: "phone", Platform: device.PlatformAndroid, State: device.StateOnline},
		{ID: "emu", Platform: device.PlatformAndroid, State: device.StateOnline},
		{ID: "locked", Platform: device.PlatformAndroid, State: device.StateUnauthorized},
		{ID: "sim", Platform: device.PlatformIOS, State: device.StateOnline},
	})

	got, err := resolveShardDevices(context.Background(), probe, shardRequest{auto: true}, "android")

	want := []shard.Device{{ID: "phone", Platform: "android"}, {ID: "emu", Platform: "android"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("devices = %+v, %v, want %+v", got, err, want)
	}
}

func TestResolveShardDevicesAutoWithNoReadyDeviceIsAnError(t *testing.T) {
	probe := device.NewStaticProbe([]device.Descriptor{{ID: "sim", Platform: device.PlatformIOS, State: device.StateOnline}})

	_, err := resolveShardDevices(context.Background(), probe, shardRequest{auto: true}, "android")

	if err == nil || !strings.Contains(err.Error(), "no connected android device") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveShardDevicesNamedIDsKeepTheirOwnPlatform(t *testing.T) {
	probe := device.NewStaticProbe([]device.Descriptor{{ID: "8DCBCBB0-A213", Platform: device.PlatformIOS, Kind: device.KindSimulator, State: device.StateOnline}})

	got, err := resolveShardDevices(context.Background(), probe, shardRequest{ids: []string{"192.168.50.29:35901", "8dcbcbb0-a213"}}, "android")

	want := []shard.Device{{ID: "192.168.50.29:35901", Platform: "android"}, {ID: "8DCBCBB0-A213", Platform: "ios"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("devices = %+v, %v, want %+v", got, err, want)
	}
}

func TestWorkerArgsParseAsAWorkerCommandAndCarryTheProjectFlags(t *testing.T) {
	name := "nightly"
	flags := map[string]FlagValue{
		"--config": "ci.yaml", "--env": "staging", "--report-dir": "/runs", "--retries": "2",
		"--var": []string{"A=1", "B=2"}, "--video": true, "--glob": []string{"*.yml"}, "--devices": "a,b", "--json": true,
	}

	args := workerArgs(&name, flags, shard.Device{ID: "emu", Platform: "android"}, "parent-1")

	parsed := mustParseArgv(t, args)
	if parsed.Command != CommandSuite || parsed.Positional == nil || *parsed.Positional != "nightly" {
		t.Fatalf("parsed = %+v from %v", parsed, args)
	}
	got := parsed.Flags
	if got["--device"] != "emu" || got["--platform"] != "android" || !flagBool(got, "--worker") || got["--parent-run"] != "parent-1" {
		t.Errorf("worker flags = %v", got)
	}
	if got["--config"] != "ci.yaml" || got["--env"] != "staging" || got["--report-dir"] != "/runs" || got["--retries"] != "2" || !flagBool(got, "--video") {
		t.Errorf("project flags = %v", got)
	}
	if !reflect.DeepEqual(flagStrings(got, "--var"), []string{"A=1", "B=2"}) {
		t.Errorf("--var = %v", flagStrings(got, "--var"))
	}
	for _, unwanted := range []string{"--glob", "--devices", "--json", "--shard"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("worker command line carries %s: %v", unwanted, args)
		}
	}
}
