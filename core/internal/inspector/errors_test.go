package inspector

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var upperSnake = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

func TestErrors_EveryRejectionUsesAnUpperSnakeCodeAndCarriesARemedy(t *testing.T) {
	srv, _ := newTestServer(t, testRoutes)
	token := authorize(t, srv)
	origin := map[string]string{"Origin": "http://" + srv.hostPort()}
	cases := map[string]*httptest.ResponseRecorder{
		"foreign host":   srv.do(call{method: http.MethodGet, path: "/api/v1/tree", host: "evil.example:1", header: map[string]string{tokenHeader: token}}),
		"foreign origin": srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":"x"}`, header: map[string]string{"Origin": "http://evil.example"}}),
		"missing token":  srv.do(call{method: http.MethodGet, path: "/api/v1/tree"}),
		"wrong code":     srv.do(call{method: http.MethodPost, path: authPath, body: `{"code":"wrong"}`, header: origin}),
		"bad body":       srv.do(call{method: http.MethodPost, path: authPath, body: `not json`, header: origin}),
	}
	for name, rec := range cases {
		var body struct {
			Error struct {
				Code   string `json:"code"`
				Remedy string `json:"remedy"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: body %q is not an error payload: %v", name, rec.Body.String(), err)
		}
		if !upperSnake.MatchString(body.Error.Code) {
			t.Errorf("%s: code %q is not upper snake case", name, body.Error.Code)
		}
		if body.Error.Remedy == "" {
			t.Errorf("%s: code %s has no remedy", name, body.Error.Code)
		}
	}
}
