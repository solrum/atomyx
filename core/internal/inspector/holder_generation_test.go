package inspector

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
)

type generationTree struct {
	Generation uint64 `json:"generation"`
	Revision   uint64 `json:"revision"`
}

func readGenerationTree(t *testing.T, h *hostHarness) generationTree {
	t.Helper()
	rec := h.get(routeTree)
	if rec.Code != http.StatusOK {
		t.Fatalf("tree = %d, body %s", rec.Code, rec.Body)
	}
	var out generationTree
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHost_ARevisionNeverRepeatsAcrossAReconnectBecauseTheGenerationAdvances(t *testing.T) {
	h := newHostHarness(t)
	before := readGenerationTree(t, h)

	h.clk.Advance(testIdleMs)
	<-h.first.closed
	if rec := h.post(routeReconnect, ""); rec.Code != http.StatusOK {
		t.Fatalf("reconnect = %d, body %s", rec.Code, rec.Body)
	}
	after := readGenerationTree(t, h)

	if before.Revision != after.Revision {
		t.Fatalf("test setup: the two sessions must number a tree alike, got %d and %d", before.Revision, after.Revision)
	}
	if after.Generation <= before.Generation {
		t.Fatalf("generation did not advance across the reconnect: %d then %d", before.Generation, after.Generation)
	}
}

func TestHost_TheGenerationStampsInfoTreeScreenshotAndFreshTree(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	if err := h.host.Reconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	const want = 2

	var info struct {
		Generation uint64 `json:"generation"`
	}
	if err := json.Unmarshal(h.get(routeSession).Body.Bytes(), &info); err != nil || info.Generation != want {
		t.Errorf("session generation = %d (%v), want %d", info.Generation, err, want)
	}
	if got := readGenerationTree(t, h).Generation; got != want {
		t.Errorf("tree generation = %d, want %d", got, want)
	}
	if got := h.get(routeScreenshot).Header().Get(headerGeneration); got != strconv.Itoa(want) {
		t.Errorf("screenshot generation header = %q, want %d", got, want)
	}
	var fresh generationTree
	if err := json.Unmarshal(h.post(routeTreeFresh, "").Body.Bytes(), &fresh); err != nil || fresh.Generation != want {
		t.Errorf("fresh tree generation = %d (%v), want %d", fresh.Generation, err, want)
	}
}

func TestHost_AReleasedHostKeepsReportingTheGenerationOfItsLastDevice(t *testing.T) {
	h := newHostHarness(t)
	h.clk.Advance(testIdleMs)
	<-h.first.closed
	if got := h.host.Info().Generation; got != 1 {
		t.Fatalf("released generation = %d, want 1", got)
	}
}
