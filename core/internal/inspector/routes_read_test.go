package inspector

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/session"
	"dev.atomyx/internal/wire"
)

func sampleTree() wire.TreeNodeWire {
	return wire.TreeNodeWire{
		Attributes: map[string]string{"role": "container"},
		Children: []wire.TreeNodeWire{
			{Attributes: map[string]string{"role": "button", "label": "Sign in"}},
		},
	}
}

func sampleSnapshot() session.Snapshot {
	return session.Snapshot{
		Generation: 3,
		Tree:       sampleTree(), HasTree: true, TreeEpoch: 4, TreeRevision: 9,
		Screenshot: []byte("\x89PNG-bytes"), HasScreenshot: true, ScreenshotFormat: session.ImagePNG,
		ScreenshotEpoch: 4, ScreenshotRevision: 7,
		Scale: 2.5, Paired: true, Highlightable: true,
	}
}

func TestRoutes_ReadGroupDeclaresOperatorActionsOnly(t *testing.T) {
	operator := map[string]bool{}
	for _, r := range ReadRoutes(&stubController{}) {
		operator[r.Method+" "+r.Path] = r.Operator
	}
	want := map[string]bool{
		"GET /api/v1/session":     false,
		"GET /api/v1/tree":        false,
		"GET /api/v1/screenshot":  false,
		"GET /api/v1/events":      false,
		"POST /api/v1/tree/fresh": true,
	}
	if len(operator) != len(want) {
		t.Fatalf("routes = %v, want %v", operator, want)
	}
	for k, v := range want {
		if got, ok := operator[k]; !ok || got != v {
			t.Errorf("route %s: operator = %v (present %v), want %v", k, got, ok, v)
		}
	}
}

func TestRoutes_ReadRoutesNeedTheToken(t *testing.T) {
	h := newAPIHarness(t, &stubController{}, ReadRoutes)
	for _, path := range []string{routeSession, routeTree, routeScreenshot, routeEvents} {
		rec := h.srv.do(call{method: http.MethodGet, path: path})
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", path, rec.Code)
		}
	}
	rec := h.srv.do(call{method: http.MethodPost, path: routeTreeFresh, header: map[string]string{"Origin": "http://" + h.srv.hostPort()}})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST fresh without a token = %d, want 401", rec.Code)
	}
}

func TestSession_ReportsHolderAndState(t *testing.T) {
	ctrl := &stubController{}
	ctrl.info.Device.ID = "emulator-5554"
	ctrl.info.Device.Name = "Pixel"
	ctrl.info.Holder.PID = 4242
	ctrl.info.Holder.Command = "atomyx inspect"
	ctrl.info.Holder.Role = "inspector"
	ctrl.info.State = session.StateIdle
	rec := newAPIHarness(t, ctrl, ReadRoutes).get(routeSession)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got struct {
		Device struct{ ID, Name string }
		Holder struct {
			PID     int
			Command string
		}
		State string
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Device.ID != "emulator-5554" || got.Holder.PID != 4242 || got.Holder.Command != "atomyx inspect" || got.State != "idle" {
		t.Fatalf("session = %+v", got)
	}
}

func TestTree_ServesTheStoredTreeWithEpochRevisionAndFlags(t *testing.T) {
	ctrl := &stubController{snap: sampleSnapshot()}
	rec := newAPIHarness(t, ctrl, ReadRoutes).get(routeTree)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	var got treeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Epoch != 4 || got.Revision != 9 || got.ScreenshotRevision != 7 {
		t.Errorf("epoch, revision, screenshot revision = %d, %d, %d", got.Epoch, got.Revision, got.ScreenshotRevision)
	}
	if got.Generation != 3 {
		t.Errorf("generation = %d, want 3", got.Generation)
	}
	if got.Scale != 2.5 || !got.Paired || !got.Highlightable {
		t.Errorf("scale, paired, highlightable = %v, %v, %v", got.Scale, got.Paired, got.Highlightable)
	}
	if len(got.Tree.Children) != 1 || got.Tree.Children[0].Attributes["label"] != "Sign in" {
		t.Errorf("tree = %+v", got.Tree)
	}
}

func TestTree_PassesTheServerDecidedFlagsThroughUnchanged(t *testing.T) {
	snap := sampleSnapshot()
	snap.Paired, snap.Highlightable, snap.Scale = false, false, 0
	rec := newAPIHarness(t, &stubController{snap: snap}, ReadRoutes).get(routeTree)
	var got treeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Paired || got.Highlightable || got.Scale != 0 {
		t.Fatalf("flags = paired %v, highlightable %v, scale %v, want all zero", got.Paired, got.Highlightable, got.Scale)
	}
}

func TestTree_NothingStoredIsACodedNotFound(t *testing.T) {
	rec := newAPIHarness(t, &stubController{}, ReadRoutes).get(routeTree)
	if rec.Code != http.StatusNotFound || codeOf(t, rec) != codeNoTree {
		t.Fatalf("status = %d, code = %q, want 404 %s", rec.Code, codeOf(t, rec), codeNoTree)
	}
}

func TestScreenshot_ServesBytesWithTypeAndHeaders(t *testing.T) {
	cases := map[session.ImageFormat]string{
		session.ImagePNG:     "image/png",
		session.ImageJPEG:    "image/jpeg",
		session.ImageUnknown: "application/octet-stream",
	}
	for format, wantType := range cases {
		snap := sampleSnapshot()
		snap.ScreenshotFormat = format
		rec := newAPIHarness(t, &stubController{snap: snap}, ReadRoutes).get(routeScreenshot)
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status = %d", format, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); got != wantType {
			t.Errorf("%q: content type = %q, want %q", format, got, wantType)
		}
		if rec.Body.String() != "\x89PNG-bytes" {
			t.Errorf("%q: body = %q", format, rec.Body.String())
		}
		for header, want := range map[string]string{
			headerGeneration: "3", headerEpoch: "4", headerRevision: "7", headerScale: "2.5", headerPaired: "true", headerHighlightable: "true",
		} {
			if got := rec.Header().Get(header); got != want {
				t.Errorf("%q: %s = %q, want %q", format, header, got, want)
			}
		}
	}
}

func TestScreenshot_NothingStoredIsACodedNotFound(t *testing.T) {
	snap := sampleSnapshot()
	snap.HasScreenshot, snap.Screenshot = false, nil
	rec := newAPIHarness(t, &stubController{snap: snap}, ReadRoutes).get(routeScreenshot)
	if rec.Code != http.StatusNotFound || codeOf(t, rec) != codeNoScreenshot {
		t.Fatalf("status = %d, code = %q, want 404 %s", rec.Code, codeOf(t, rec), codeNoScreenshot)
	}
}

func TestFresh_ReturnsTheCapturedPair(t *testing.T) {
	ctrl := &stubController{freshSnap: sampleSnapshot()}
	rec := newAPIHarness(t, ctrl, ReadRoutes).post(routeTreeFresh, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	if ctrl.freshCalls != 1 {
		t.Fatalf("FreshTree calls = %d, want 1", ctrl.freshCalls)
	}
	containsAll(t, rec.Body.String(), `"revision":9`, `"highlightable":true`)
}

func TestFresh_HolderBusyIsAConflictWithTheCodedBody(t *testing.T) {
	ctrl := &stubController{freshErr: diag.NewHolderBusy("a tool call is running")}
	rec := newAPIHarness(t, ctrl, ReadRoutes).post(routeTreeFresh, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if got := codeOf(t, rec); got != diag.CodeHolderBusy {
		t.Fatalf("code = %q, want %s", got, diag.CodeHolderBusy)
	}
	containsAll(t, rec.Body.String(), "a tool call is running", `"remedy"`)
}

func TestFresh_OtherFailuresAreMappedWithoutLeakingText(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"closed", session.ErrClosed, http.StatusGone, codeSessionClosed},
		{"unclassified", errors.New("dial /Users/me/secret.sock failed"), http.StatusInternalServerError, diag.CodeUnknown},
		{"device", diag.New(diag.CodeAgentUnreachable, "agent gone", "restart it", nil), http.StatusBadGateway, diag.CodeAgentUnreachable},
		{"cancelled", context.Canceled, statusClientClosedRequest, diag.CodeUnknown},
		{"wrapped cancel", fmt.Errorf("capture: %w", context.Canceled), statusClientClosedRequest, diag.CodeUnknown},
		{"out of time", context.DeadlineExceeded, http.StatusGatewayTimeout, diag.CodeToolTimeout},
	}
	for _, tc := range cases {
		rec := newAPIHarness(t, &stubController{freshErr: tc.err}, ReadRoutes).post(routeTreeFresh, "")
		if rec.Code != tc.status || codeOf(t, rec) != tc.code {
			t.Errorf("%s: status = %d code = %q, want %d %s", tc.name, rec.Code, codeOf(t, rec), tc.status, tc.code)
		}
		if strings.Contains(rec.Body.String(), "secret.sock") {
			t.Errorf("%s: body leaks the error text: %s", tc.name, rec.Body)
		}
	}
}

func TestFresh_RequiresTheServedOrigin(t *testing.T) {
	ctrl := &stubController{freshSnap: sampleSnapshot()}
	h := newAPIHarness(t, ctrl, ReadRoutes)
	rec := h.srv.do(call{method: http.MethodPost, path: routeTreeFresh, header: map[string]string{tokenHeader: h.token}})
	if rec.Code != http.StatusForbidden || ctrl.freshCalls != 0 {
		t.Fatalf("status = %d, fresh calls = %d, want 403 and 0", rec.Code, ctrl.freshCalls)
	}
}

func TestReads_NeverCallTheDevice(t *testing.T) {
	ctrl := &stubController{snap: sampleSnapshot()}
	h := newAPIHarness(t, ctrl, ReadRoutes)
	h.get(routeSession)
	h.get(routeTree)
	h.get(routeScreenshot)
	if ctrl.freshCalls != 0 {
		t.Fatalf("FreshTree calls = %d, want 0", ctrl.freshCalls)
	}
}

// flushRecorder reports every flush on a channel, so a test can wait
// for exactly the writes it expects without sleeping.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed chan struct{}
}

func (f *flushRecorder) Flush() {
	f.ResponseRecorder.Flush()
	f.flushed <- struct{}{}
}

func startEvents(t *testing.T, ctrl *stubController) (cancel context.CancelFunc, rec *flushRecorder, done chan struct{}) {
	t.Helper()
	h := newAPIHarness(t, ctrl, ReadRoutes)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, routeEvents, nil)
	req.Host = h.srv.hostPort()
	req.Header.Set(tokenHeader, h.token)
	rec = &flushRecorder{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan struct{}, 16)}
	done = make(chan struct{})
	go func() {
		defer close(done)
		h.srv.Handler().ServeHTTP(rec, req)
	}()
	return cancel, rec, done
}

func TestEvents_StreamsOneFlushedLinePerObservation(t *testing.T) {
	ctrl := &stubController{feed: make(chan session.Observation)}
	cancel, rec, done := startEvents(t, ctrl)

	<-rec.flushed // the headers
	ctrl.feed <- session.Observation{Kind: session.ObservedTree, Epoch: 3, Revision: 11, Stable: true}
	<-rec.flushed
	ctrl.feed <- session.Observation{Kind: session.ObservedScreenshot, Epoch: 3, Revision: 5}
	<-rec.flushed
	cancel()
	<-done

	if got := rec.Header().Get("Content-Type"); got != "application/x-ndjson" {
		t.Errorf("content type = %q", got)
	}
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	var lines []eventLine
	for sc.Scan() {
		var l eventLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		lines = append(lines, l)
	}
	want := []eventLine{
		{Kind: "tree", Epoch: 3, Revision: 11, Stable: true},
		{Kind: "screenshot", Epoch: 3, Revision: 5, Stable: false},
	}
	if len(lines) != len(want) || lines[0] != want[0] || lines[1] != want[1] {
		t.Fatalf("lines = %+v, want %+v", lines, want)
	}
}

func TestEvents_ATerminalObservationIsSentThenTheStreamEnds(t *testing.T) {
	for kind, name := range map[session.ObservationKind]string{
		session.ObservedDropped: "dropped",
		session.ObservedClosed:  "closed",
	} {
		feed := make(chan session.Observation, 2)
		feed <- session.Observation{Kind: kind, Epoch: 8}
		close(feed)
		_, rec, done := startEvents(t, &stubController{feed: feed})
		<-done
		var last eventLine
		if err := json.Unmarshal([]byte(strings.TrimSpace(rec.Body.String())), &last); err != nil {
			t.Fatalf("%s: body %q: %v", name, rec.Body, err)
		}
		if last.Kind != name || last.Epoch != 8 {
			t.Errorf("%s: terminal line = %+v", name, last)
		}
	}
}

func TestEvents_AClientDisconnectEndsTheStreamWithoutATerminalLine(t *testing.T) {
	cancel, rec, done := startEvents(t, &stubController{feed: make(chan session.Observation)})
	<-rec.flushed
	cancel()
	<-done
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body)
	}
}

func TestEvents_AServerCloseEndsOpenStreams(t *testing.T) {
	ctrl := &stubController{feed: make(chan session.Observation)}
	h := newAPIHarness(t, ctrl, ReadRoutes)
	go func() { _ = h.srv.Serve() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+h.srv.Addr()+routeEvents, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(tokenHeader, h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := h.srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(resp.Body).ReadString('\n'); err == nil {
		t.Fatal("read after close succeeded, want the stream to end")
	}
}

func TestEvents_ASessionThatCannotBeSubscribedIsACodedFailure(t *testing.T) {
	rec := newAPIHarness(t, &stubController{subscribe: session.ErrClosed}, ReadRoutes).get(routeEvents)
	if rec.Code != http.StatusGone || codeOf(t, rec) != codeSessionClosed {
		t.Fatalf("status = %d code = %q, want 410 %s", rec.Code, codeOf(t, rec), codeSessionClosed)
	}
}

func TestEventKind_NamesEveryObservationKind(t *testing.T) {
	want := map[session.ObservationKind]string{
		session.ObservedTree: "tree", session.ObservedScreenshot: "screenshot", session.ObservedScreenSize: "screen_size",
		session.ObservedDropped: "dropped", session.ObservedClosed: "closed", session.ObservationKind(99): "unknown",
	}
	for k, name := range want {
		if got := eventKind(k); got != name {
			t.Errorf("eventKind(%d) = %q, want %q", k, got, name)
		}
	}
}
