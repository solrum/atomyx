package process

import (
	"context"
	"reflect"
	"testing"
)

func TestWithEnv_AppendsAndLeavesTheParentContextUntouched(t *testing.T) {
	base := context.Background()
	first := WithEnv(base, "A=1")
	second := WithEnv(first, "B=2")

	if got := EnvFrom(base); got != nil {
		t.Errorf("EnvFrom(base) = %v, want nil", got)
	}
	if got := EnvFrom(first); !reflect.DeepEqual(got, []string{"A=1"}) {
		t.Errorf("EnvFrom(first) = %v, want [A=1]", got)
	}
	if got := EnvFrom(second); !reflect.DeepEqual(got, []string{"A=1", "B=2"}) {
		t.Errorf("EnvFrom(second) = %v, want [A=1 B=2]", got)
	}
}

func TestFakes_RecordTheEnvironmentTheContextCarries(t *testing.T) {
	ctx := WithEnv(context.Background(), "X=y")

	run := NewFake()
	_, _ = run.Run(ctx, "cmd")
	if got := run.Invocations()[0].Env; !reflect.DeepEqual(got, []string{"X=y"}) {
		t.Errorf("Fake env = %v, want [X=y]", got)
	}

	bg := NewFakeBackground()
	_, _ = bg.Start(ctx, "cmd")
	if got := bg.Invocations()[0].Env; !reflect.DeepEqual(got, []string{"X=y"}) {
		t.Errorf("FakeBackground env = %v, want [X=y]", got)
	}
}

func TestExecBackground_PassesTheContextEnvironmentToTheChild(t *testing.T) {
	ctx := WithEnv(context.Background(), "ATOMYX_ENV_PROBE=from-context")
	proc, err := NewExecBackground().Start(ctx, "sh", "-c", `printf %s "$ATOMYX_ENV_PROBE"`)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	out := make([]byte, 64)
	n, _ := proc.Stdout().Read(out)
	if _, err := proc.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got := string(out[:n]); got != "from-context" {
		t.Fatalf("child saw %q, want from-context", got)
	}
	if proc.Pid() <= 0 {
		t.Fatalf("Pid() = %d, want a positive pid", proc.Pid())
	}
}

func TestExec_PassesTheContextEnvironmentToTheChild(t *testing.T) {
	ctx := WithEnv(context.Background(), "ATOMYX_ENV_PROBE=from-context")
	res, err := NewExec().Run(ctx, "sh", "-c", `printf %s "$ATOMYX_ENV_PROBE"`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res.Stdout != "from-context" {
		t.Fatalf("child saw %q, want from-context", res.Stdout)
	}
}
