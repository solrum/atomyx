package inspector

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"dev.atomyx/internal/clock"
)

func newClockedServer(t *testing.T, clk clock.Clock) (*Server, recordingLogger) {
	t.Helper()
	logger := newRecordingLogger()
	srv, err := New(Config{Controller: fakeController{}, Logger: logger, Entropy: &counterReader{}, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, logger
}

func postCode(srv *Server, code string) (status int, errorCode string) {
	rec := srv.do(call{
		method: http.MethodPost, path: authPath, body: `{"code":"` + code + `"}`,
		header: map[string]string{"Origin": "http://" + srv.hostPort()},
	})
	if rec.Code == http.StatusOK {
		return rec.Code, ""
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body.Error.Code
}

func TestBootstrapCode_ExpiresAfterItsLifetime(t *testing.T) {
	clk := clock.NewManualClock(1_000)
	srv, logger := newClockedServer(t, clk)
	url, _ := srv.NewBootstrapURL()
	code := url[strings.Index(url, "#")+1:]

	clk.Advance(codeLifetimeMs + 1)
	status, errCode := postCode(srv, code)
	if status != http.StatusUnauthorized || errCode != codeCodeRefused {
		t.Fatalf("expired code: status %d code %q, want 401 CODE_REFUSED", status, errCode)
	}
	rec := srv.do(call{
		method: http.MethodPost, path: authPath, body: `{"code":"` + code + `"}`,
		header: map[string]string{"Origin": "http://" + srv.hostPort()},
	})
	if !strings.Contains(rec.Body.String(), "Press Enter") {
		t.Errorf("body %s names no remedy to press Enter", rec.Body)
	}
	last := logger.snapshot()[0]
	if last.msg != "inspector.auth.expired" {
		t.Errorf("first log entry = %q, want inspector.auth.expired", last.msg)
	}
}

func TestBootstrapCode_ValidAtExactlyItsLifetime(t *testing.T) {
	clk := clock.NewManualClock(1_000)
	srv, _ := newClockedServer(t, clk)
	url, _ := srv.NewBootstrapURL()
	code := url[strings.Index(url, "#")+1:]

	clk.Advance(codeLifetimeMs)
	if status, errCode := postCode(srv, code); status != http.StatusOK {
		t.Fatalf("code at its lifetime: status %d code %q, want 200", status, errCode)
	}
}

func TestBootstrapCode_AFreshCodeAfterExpiryWorks(t *testing.T) {
	clk := clock.NewManualClock(0)
	srv, _ := newClockedServer(t, clk)
	stale, _ := srv.NewBootstrapURL()
	clk.Advance(codeLifetimeMs + 1)
	fresh, _ := srv.NewBootstrapURL()

	if status, _ := postCode(srv, stale[strings.Index(stale, "#")+1:]); status == http.StatusOK {
		t.Fatal("the replaced code must not work")
	}
	if status, errCode := postCode(srv, fresh[strings.Index(fresh, "#")+1:]); status != http.StatusOK {
		t.Fatalf("fresh code: status %d code %q, want 200", status, errCode)
	}
}
