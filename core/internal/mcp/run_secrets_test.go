package mcp

import (
	"context"
	"testing"

	"dev.atomyx/internal/logging"
	"dev.atomyx/internal/orchestra"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/storage"
)

func secretsTestSession(t *testing.T) *runSession {
	t.Helper()
	root := t.TempDir()
	return newRunSession(runSessionDeps{
		Store:   storage.NewRunStore(),
		Catalog: testRunCatalog(storage.NewInMemoryStorage(), root),
		Starter: testRunStarter(root),
		Clock:   orchestra.NewFakeClock(0),
		Logger:  logging.NoopLogger{},
	})
}

func secretsOf(t *testing.T, s *runSession) *runner.RunSecrets {
	t.Helper()
	var seen *runner.RunSecrets
	_, err := s.RunScript(context.Background(), "probe", nil, func(target scriptTarget) (runner.ScriptResult, error) {
		seen = target.Secrets
		return runner.ScriptResult{Ok: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == nil {
		t.Fatal("the execution got no secret set")
	}
	return seen
}

func TestOpenRunKeepsOneSecretSetAcrossScriptExecutions(t *testing.T) {
	s := secretsTestSession(t)
	if _, err := s.Start(context.Background(), "explore", nil, nil); err != nil {
		t.Fatal(err)
	}

	first := secretsOf(t, s)
	first.Add("value-from-first-call")
	second := secretsOf(t, s)

	if first != second {
		t.Fatal("two executions of one open run got different secret sets")
	}
	if got := second.String("value-from-first-call"); got != orchestra.SecureMask {
		t.Fatalf("the second execution lost the first one's secret: %q", got)
	}
}

func TestEachOwnRunGetsAFreshSecretSet(t *testing.T) {
	s := secretsTestSession(t)

	first := secretsOf(t, s)
	first.Add("value-from-first-run")
	second := secretsOf(t, s)

	if first == second {
		t.Fatal("two separate runs shared a secret set")
	}
	if got := second.String("value-from-first-run"); got != "value-from-first-run" {
		t.Fatalf("a secret crossed into another run folder: %q", got)
	}
}
