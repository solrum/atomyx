package setup

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReportJSONShape(t *testing.T) {
	r := Report{Steps: []StepReport{
		{ID: "adb", Status: StatusOK, Message: "found"},
		{ID: "apk", Status: StatusManual, Message: "missing", Remedy: "build it"},
	}}
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}

	want := `{"ok":false,"steps":[{"id":"adb","status":"ok","message":"found","remedy":""},{"id":"apk","status":"manual","message":"missing","remedy":"build it"}]}` + "\n"
	if buf.String() != want {
		t.Errorf("json = %s, want %s", buf.String(), want)
	}
}

func TestReportJSONEmitsAnEmptyArrayForNoSteps(t *testing.T) {
	var buf bytes.Buffer
	if err := (Report{}).WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		OK    bool            `json:"ok"`
		Steps json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK || string(decoded.Steps) != "[]" {
		t.Errorf("json = %s, want ok true and steps []", buf.String())
	}
}

func TestReportTextShowsStatusMessageAndRemedy(t *testing.T) {
	r := Report{Steps: []StepReport{{ID: "apk", Status: StatusManual, Message: "missing", Remedy: "build it"}}}
	var buf bytes.Buffer
	r.WriteText(&buf)

	for _, want := range []string{"manual", "apk: missing", "remedy: build it", "Setup is not complete"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("text missing %q:\n%s", want, buf.String())
		}
	}
}
