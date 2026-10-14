package setup

import (
	"context"
	"strings"
	"testing"
)

type fakeStep struct {
	id     string
	check  Result
	fix    *Result
	calls  *[]string
	cancel context.CancelFunc
}

func (f *fakeStep) ID() string { return f.id }

func (f *fakeStep) Check(context.Context) Result {
	*f.calls = append(*f.calls, "check:"+f.id)
	if f.cancel != nil {
		f.cancel()
	}
	return f.check
}

// fixableStep adds Fix to fakeStep so a step without a fix stays a
// plain Step.
type fixableStep struct{ *fakeStep }

func (f fixableStep) Fix(context.Context) Result {
	*f.calls = append(*f.calls, "fix:"+f.id)
	return *f.fix
}

func okResult() Result { return Result{Status: StatusOK, Message: "fine"} }

func manualResult() Result {
	return Result{Status: StatusManual, Message: "missing", Remedy: "do it"}
}

func build(t *testing.T, entries ...Entry) *Registry {
	t.Helper()
	reg := NewRegistry()
	for _, e := range entries {
		if err := reg.Add(e.Step, e.Requires...); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func TestRunExecutesStepsInRegistrationOrder(t *testing.T) {
	var calls []string
	reg := build(t,
		Entry{Step: &fakeStep{id: "c", check: okResult(), calls: &calls}},
		Entry{Step: &fakeStep{id: "a", check: okResult(), calls: &calls}},
		Entry{Step: &fakeStep{id: "b", check: okResult(), calls: &calls}},
	)

	report := Run(context.Background(), reg, ModeFix)

	if got, want := strings.Join(calls, ","), "check:c,check:a,check:b"; got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	if len(report.Steps) != 3 || report.Steps[0].ID != "c" || report.Steps[2].ID != "b" {
		t.Errorf("report order = %+v", report.Steps)
	}
}

func TestRunSkipsDependentsOfAnUnmetPrerequisite(t *testing.T) {
	var calls []string
	reg := build(t,
		Entry{Step: &fakeStep{id: "adb", check: manualResult(), calls: &calls}},
		Entry{Step: &fakeStep{id: "device", check: okResult(), calls: &calls}, Requires: []string{"adb"}},
		Entry{Step: &fakeStep{id: "agent", check: okResult(), calls: &calls}, Requires: []string{"device"}},
		Entry{Step: &fakeStep{id: "unrelated", check: okResult(), calls: &calls}},
	)

	report := Run(context.Background(), reg, ModeCheck)

	if got, want := strings.Join(calls, ","), "check:adb,check:unrelated"; got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	device, agent := report.Steps[1], report.Steps[2]
	if device.Status != StatusSkipped || !strings.Contains(device.Message, "adb is manual") {
		t.Errorf("device = %+v, want skipped naming adb", device)
	}
	if agent.Status != StatusSkipped || !strings.Contains(agent.Message, "device is skipped") {
		t.Errorf("agent = %+v, want skipped naming device", agent)
	}
	if report.Steps[3].Status != StatusOK {
		t.Errorf("unrelated = %+v, want ok", report.Steps[3])
	}
}

func TestRunSkipsDependentsOfAFailedPrerequisite(t *testing.T) {
	var calls []string
	reg := build(t,
		Entry{Step: &fakeStep{id: "a", check: Result{Status: StatusFailed, Message: "boom", Remedy: "retry"}, calls: &calls}},
		Entry{Step: &fakeStep{id: "b", check: okResult(), calls: &calls}, Requires: []string{"a"}},
	)

	report := Run(context.Background(), reg, ModeCheck)

	if report.Steps[1].Status != StatusSkipped {
		t.Errorf("b = %+v, want skipped", report.Steps[1])
	}
}

func TestRunDoesNotBlockOnAStepThatSkippedItselfAsNotApplicable(t *testing.T) {
	var calls []string
	reg := build(t,
		Entry{Step: &fakeStep{id: "iproxy", check: Result{Status: StatusSkipped, Message: "simulator needs none"}, calls: &calls}},
		Entry{Step: &fakeStep{id: "next", check: okResult(), calls: &calls}, Requires: []string{"iproxy"}},
	)

	report := Run(context.Background(), reg, ModeCheck)

	if report.Steps[1].Status != StatusOK {
		t.Errorf("next = %+v, want ok", report.Steps[1])
	}
}

func TestRunInCheckModeNeverCallsFix(t *testing.T) {
	var calls []string
	fixed := Result{Status: StatusFixed, Message: "repaired"}
	reg := build(t, Entry{Step: fixableStep{&fakeStep{id: "a", check: manualResult(), fix: &fixed, calls: &calls}}})

	report := Run(context.Background(), reg, ModeCheck)

	if got, want := strings.Join(calls, ","), "check:a"; got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	if report.Steps[0].Status != StatusManual || report.Steps[0].Remedy != "do it" {
		t.Errorf("a = %+v, want the check result", report.Steps[0])
	}
}

func TestRunInFixModeFixesAnUnmetStepAndReportsTheFixResult(t *testing.T) {
	var calls []string
	fixed := Result{Status: StatusFixed, Message: "repaired"}
	reg := build(t, Entry{Step: fixableStep{&fakeStep{id: "a", check: manualResult(), fix: &fixed, calls: &calls}}})

	report := Run(context.Background(), reg, ModeFix)

	if got, want := strings.Join(calls, ","), "check:a,fix:a"; got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	if report.Steps[0].Status != StatusFixed {
		t.Errorf("a = %+v, want fixed", report.Steps[0])
	}
}

func TestRunInFixModeLeavesASatisfiedStepAlone(t *testing.T) {
	var calls []string
	fixed := Result{Status: StatusFixed}
	reg := build(t, Entry{Step: fixableStep{&fakeStep{id: "a", check: okResult(), fix: &fixed, calls: &calls}}})

	Run(context.Background(), reg, ModeFix)

	if got, want := strings.Join(calls, ","), "check:a"; got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
}

func TestRunInFixModeKeepsAManualResultForAStepWithoutFix(t *testing.T) {
	var calls []string
	reg := build(t, Entry{Step: &fakeStep{id: "a", check: manualResult(), calls: &calls}})

	report := Run(context.Background(), reg, ModeFix)

	if report.Steps[0].Status != StatusManual {
		t.Errorf("a = %+v, want manual", report.Steps[0])
	}
}

func TestRunSkipsTheRemainingStepsOnceTheContextIsCancelled(t *testing.T) {
	var calls []string
	ctx, cancel := context.WithCancel(context.Background())
	reg := build(t,
		Entry{Step: &fakeStep{id: "a", check: okResult(), calls: &calls, cancel: cancel}},
		Entry{Step: &fakeStep{id: "b", check: okResult(), calls: &calls}},
	)

	report := Run(ctx, reg, ModeFix)

	if got, want := strings.Join(calls, ","), "check:a"; got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	if report.Steps[1].Status != StatusSkipped || report.OK() {
		t.Errorf("report = %+v, want b skipped and the run not ok", report)
	}
}
