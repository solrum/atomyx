package process

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestStartTime_ParsesPsOutputInLocalTimeAndAsksForTheCLocale(t *testing.T) {
	f := NewFake()
	f.Default = Result{Stdout: " Thu Oct  2 10:15:02 2026\n"}

	got, err := StartTime(context.Background(), f, 4242)
	if err != nil {
		t.Fatalf("StartTime() error = %v", err)
	}
	want := time.Date(2026, time.October, 2, 10, 15, 2, 0, time.Local).UTC()
	if !got.Equal(want) {
		t.Fatalf("StartTime() = %v, want %v", got, want)
	}
	calls := f.Invocations()
	if len(calls) != 1 || calls[0].Name != "ps" {
		t.Fatalf("invocations = %+v, want one ps call", calls)
	}
	if len(calls[0].Env) != 1 || calls[0].Env[0] != "LC_ALL=C" {
		t.Fatalf("env = %v, want LC_ALL=C so the month and weekday names parse", calls[0].Env)
	}
}

func TestStartTime_PsFailureAndGarbageAreErrors(t *testing.T) {
	failing := NewFake()
	failing.DefaultErr = errors.New("no such process")
	if _, err := StartTime(context.Background(), failing, 1); err == nil {
		t.Fatal("StartTime() error = nil, want the ps failure")
	}
	garbage := NewFake()
	garbage.Default = Result{Stdout: "not a time"}
	if _, err := StartTime(context.Background(), garbage, 1); err == nil {
		t.Fatal("StartTime() error = nil, want a parse failure")
	}
}

func TestParseSysctlBootTime(t *testing.T) {
	got, err := parseSysctlBootTime("{ sec = 1759372800, usec = 41230 } Thu Oct  2 07:20:00 2026\n")
	if err != nil {
		t.Fatalf("parseSysctlBootTime() error = %v", err)
	}
	if !got.Equal(time.Unix(1759372800, 0)) {
		t.Fatalf("parseSysctlBootTime() = %v, want unix 1759372800", got)
	}
	for _, bad := range []string{"", "usec = 5", "{ sec = x, usec = 1 }"} {
		if _, err := parseSysctlBootTime(bad); err == nil {
			t.Errorf("parseSysctlBootTime(%q) error = nil, want a parse failure", bad)
		}
	}
}
