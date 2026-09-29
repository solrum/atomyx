package cli

import "testing"

func TestModulesHasDriverRegistered(t *testing.T) {
	if _, ok := Modules["driver"]; !ok {
		t.Fatalf("Modules[\"driver\"] not registered")
	}
}

func TestModulesHasSkillsRegistered(t *testing.T) {
	if _, ok := Modules["skills"]; !ok {
		t.Fatalf("Modules[\"skills\"] not registered")
	}
}

func TestShortcutRunResolvesToDriverRun(t *testing.T) {
	module, args := Shortcuts["run"]([]string{"--file", "test.yml"})
	if module != "driver" {
		t.Errorf("module: got %q, want driver", module)
	}
	assertStrings(t, args, []string{"run", "--file", "test.yml"})
}

func TestShortcutDevicesResolvesToDriverListDevices(t *testing.T) {
	module, args := Shortcuts["devices"]([]string{"--json"})
	if module != "driver" {
		t.Errorf("module: got %q, want driver", module)
	}
	assertStrings(t, args, []string{"list-devices", "--json"})
}

func TestShortcutDevicesWithoutFlags(t *testing.T) {
	module, args := Shortcuts["devices"](nil)
	if module != "driver" {
		t.Errorf("module: got %q, want driver", module)
	}
	assertStrings(t, args, []string{"list-devices"})
}

func TestShortcutInitResolvesToSkillsInit(t *testing.T) {
	module, args := Shortcuts["init"]([]string{"--force"})
	if module != "skills" {
		t.Errorf("module: got %q, want skills", module)
	}
	assertStrings(t, args, []string{"init", "--force"})
}

func TestShortcutUpdateSkillsResolvesToSkillsUpdateSkills(t *testing.T) {
	module, args := Shortcuts["update-skills"](nil)
	if module != "skills" {
		t.Errorf("module: got %q, want skills", module)
	}
	assertStrings(t, args, []string{"update-skills"})
}

func TestShortcutDoctorResolvesToDriverDoctor(t *testing.T) {
	module, args := Shortcuts["doctor"]([]string{"--platform", "ios"})
	if module != "driver" {
		t.Errorf("module: got %q, want driver", module)
	}
	assertStrings(t, args, []string{"doctor", "--platform", "ios"})
}

func TestShortcutSuiteResolvesToDriverSuite(t *testing.T) {
	module, args := Shortcuts["suite"]([]string{"smoke", "--tag", "critical"})
	if module != "driver" {
		t.Errorf("module: got %q, want driver", module)
	}
	assertStrings(t, args, []string{"suite", "smoke", "--tag", "critical"})
}

func assertStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
