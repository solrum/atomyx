package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"dev.atomyx/internal/wire"
)

const sidecarSecret = "topsecret-value"

const sidecarSecretScript = "appId: com.test\n---\n- read: { selector: { id: pw }, field: value, as: pwd }\n- pressKey: enter\n"

func runSecretScript(t *testing.T, scenario bool) (events string, result string, logs string) {
	t.Helper()
	ctx := newTestContext()
	drv := selectMockDevice(t, ctx)
	drv.PressKeyErr = errors.New("key rejected for " + sidecarSecret)
	drv.StageHierarchy(wire.TreeNodeWire{
		Attributes: map[string]string{"id": "root", "bounds": "0,0,100,100"},
		Children: []wire.TreeNodeWire{{Attributes: map[string]string{
			"id": "pw", "value": sidecarSecret, "bounds": "0,0,10,10", wire.AttrSecure: wire.AttrSecureTrue,
		}}},
	})
	var mu sync.Mutex
	var seen strings.Builder
	ctx.Events.Subscribe(func(e Event) {
		body, _ := json.Marshal(e.Payload)
		mu.Lock()
		defer mu.Unlock()
		seen.WriteString(string(body) + "\n")
	})
	rec := newRecordingLogger()
	svc := newScriptService(ctx.Session, ctx.Events, rec)

	var res any
	var err error
	if scenario {
		dir := t.TempDir()
		if writeErr := os.WriteFile(filepath.Join(dir, "child.yml"), []byte(sidecarSecretScript), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		res, err = svc.RunScenario(context.Background(), "name: S\nscripts:\n  - child.yml\n", dir)
	} else {
		res, err = svc.Run(context.Background(), sidecarSecretScript)
	}
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	body, _ := json.Marshal(res)
	mu.Lock()
	defer mu.Unlock()
	return seen.String(), string(body), fmt.Sprint(*rec.entries)
}

func TestScriptService_NeverEmitsASecretItHoldsInEventsResultOrLogs(t *testing.T) {
	for _, scenario := range []bool{false, true} {
		t.Run(fmt.Sprintf("scenario=%t", scenario), func(t *testing.T) { requireNoSidecarLeak(t, scenario) })
	}
}

func requireNoSidecarLeak(t *testing.T, scenario bool) {
	t.Helper()
	events, result, logs := runSecretScript(t, scenario)

	if !strings.Contains(logs, "command handler panicked") {
		t.Fatalf("the run never logged the failing key press, so the log check is vacuous:\n%s", logs)
	}
	for name, text := range map[string]string{"events": events, "result": result, "logs": logs} {
		if strings.Contains(text, sidecarSecret) {
			t.Errorf("%s carry the secret value:\n%s", name, text)
		}
	}
}
