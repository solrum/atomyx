package process

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseProcStat(t *testing.T) {
	cases := []struct {
		name       string
		stat       string
		wantCmd    string
		wantPGID   int
		wantTicks  int64
		wantErrMsg string
	}{
		{"plain command", "4242 (atomyx) " + tail(777, 123456), "atomyx", 777, 123456, ""},
		{"command with spaces", "4242 (Web Content) " + tail(888, 55), "Web Content", 888, 55, ""},
		{"command with parentheses", "4242 (a) b (c) " + tail(999, 7), "a) b (c", 999, 7, ""},
		{"command ending in a parenthesis", "4242 (x)) " + tail(5, 9), "x)", 5, 9, ""},
		{"no parentheses", "4242 atomyx S 1", "", 0, 0, "malformed stat record"},
		{"short tail", "4242 (atomyx) S 1 2 3", "", 0, 0, "short stat record"},
		{"bad pgrp", "4242 (atomyx) S 1 x " + "0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 5", "", 0, 0, "parse the process group"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := parseProcStat(tc.stat)
			if tc.wantErrMsg != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrMsg) {
					t.Fatalf("parseProcStat error = %v, want %q", err, tc.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProcStat error = %v", err)
			}
			if rec.command != tc.wantCmd || rec.pgid != tc.wantPGID || rec.startTicks != tc.wantTicks {
				t.Fatalf("parseProcStat = %+v, want command %q pgid %d ticks %d", rec, tc.wantCmd, tc.wantPGID, tc.wantTicks)
			}
		})
	}
}

// tail builds the text after the command: field 3 (state), field 4
// (ppid), field 5 (pgrp), fields 6 to 21 as filler, field 22 (start
// time), then trailing fields.
func tail(pgrp int, start int64) string {
	return fmt.Sprintf("S 1 %d 4321 34816 -1 4194560 100 0 0 0 1 2 0 0 20 0 1 0 %d 1000000 200 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0", pgrp, start)
}

func TestParseProcStatBootTime(t *testing.T) {
	const procStat = "cpu  1 2 3 4\ncpu0 1 2 3 4\nintr 5\nctxt 6\nbtime 1700000000\nprocesses 7\n"
	got, err := parseProcStatBootTime(procStat)
	if err != nil {
		t.Fatalf("parseProcStatBootTime error = %v", err)
	}
	if want := time.Unix(1700000000, 0).UTC(); !got.Equal(want) {
		t.Fatalf("boot time = %v, want %v", got, want)
	}
	if _, err := parseProcStatBootTime("cpu 1 2\n"); err == nil {
		t.Fatal("missing btime line: want an error")
	}
	if _, err := parseProcStatBootTime("btime abc\n"); err == nil {
		t.Fatal("non-numeric btime: want an error")
	}
}

func TestProcStartTime(t *testing.T) {
	boot := time.Unix(1700000000, 0).UTC()
	got := procStartTime(boot, 12345)
	if want := boot.Add(123450 * time.Millisecond); !got.Equal(want) {
		t.Fatalf("procStartTime = %v, want %v", got, want)
	}
}
