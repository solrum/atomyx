package conformance

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/runfolder"
)

func attachGoldenName(name string) string { return "attach/v1-" + name + ".json" }

func TestAttachTranscriptGoldens(t *testing.T) {
	transcripts, err := AttachCorpus()
	if err != nil {
		t.Fatal(err)
	}
	for _, transcript := range transcripts {
		t.Run(transcript.Name, func(t *testing.T) {
			got, err := json.Marshal(transcript.Steps)
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, attachGoldenName(transcript.Name), got, DiffJSON)
		})
	}
}

// attachDataShape maps a message type to a fresh value its data must
// decode into without any unknown field, so a renamed or added field
// fails the golden replay.
func attachDataShape(messageType string) any {
	switch messageType {
	case attach.TypeHello:
		return &attach.Hello{}
	case attach.TypeStatus:
		return &attach.Status{}
	case attach.TypeSubscribe:
		return &attach.Subscribe{}
	case attach.TypeTreeLatest:
		return &attach.TreeResult{}
	case attach.TypeScreenshotLatest:
		return &attach.ScreenshotResult{}
	case attach.TypeTreeFresh:
		return &attach.FreshResult{}
	case attach.NoticeTree:
		return &attach.TreeNotice{}
	case attach.NoticeScreenshot:
		return &attach.ScreenshotNotice{}
	case attach.NoticeStep:
		return &attach.StepNotice{}
	case attach.NoticeStepGap:
		return &attach.StepGapNotice{}
	default:
		return nil
	}
}

func TestAttachGoldensReplayThroughTheFrameReader(t *testing.T) {
	transcripts, err := AttachCorpus()
	if err != nil {
		t.Fatal(err)
	}
	for _, transcript := range transcripts {
		t.Run(transcript.Name, func(t *testing.T) {
			body, err := os.ReadFile(goldenPath(attachGoldenName(transcript.Name)))
			if err != nil {
				t.Fatal(err)
			}
			var steps []AttachTranscriptStep
			if err := json.Unmarshal(body, &steps); err != nil {
				t.Fatal(err)
			}
			var stream bytes.Buffer
			for _, step := range steps {
				var compact bytes.Buffer
				if err := json.Compact(&compact, step.Frame); err != nil {
					t.Fatal(err)
				}
				stream.Write(compact.Bytes())
				stream.WriteByte('\n')
			}
			reader := attach.NewFrameReader(&stream)
			for i, step := range steps {
				frame, err := reader.ReadFrame()
				if err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				if frame.V != attach.ProtocolMajor {
					t.Fatalf("step %d: v = %d", i, frame.V)
				}
				isFailure := frame.Ok != nil && !*frame.Ok
				shape := attachDataShape(frame.Type)
				if step.From == attachFromHolder && frame.Kind() == attach.KindResponse && frame.Type == attach.TypeHello {
					shape = &attach.HelloResult{}
				}
				if shape == nil || isFailure || len(frame.Data) == 0 {
					continue
				}
				decoder := json.NewDecoder(bytes.NewReader(frame.Data))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(shape); err != nil {
					t.Fatalf("step %d (%s): %v", i, frame.Type, err)
				}
			}
		})
	}
}

func TestAttachStepLineMatchesRunfolderStepLine(t *testing.T) {
	detail := "Element exists"
	runLine := runfolder.StepLine{
		SchemaVersion: runfolder.StepLineSchemaVersion, RunID: "20261003-101500-smoke", Seq: 9, StepIndex: 3, Command: "tap",
		Phase: runfolder.StepPhaseCompleted, Ok: true, DurationMs: 41, Detail: &detail,
		Artifacts: []string{"step-004-home.png"},
	}
	attachLine := attach.StepLine{
		SchemaVersion: runLine.SchemaVersion, RunID: runLine.RunID, Seq: runLine.Seq, StepIndex: runLine.StepIndex,
		Command: runLine.Command, Phase: string(runLine.Phase), Ok: runLine.Ok,
		DurationMs: runLine.DurationMs, Detail: runLine.Detail, Artifacts: runLine.Artifacts,
	}
	runJSON, err := json.Marshal(runLine)
	if err != nil {
		t.Fatal(err)
	}
	attachJSON, err := json.Marshal(attachLine)
	if err != nil {
		t.Fatal(err)
	}
	if report, equal, err := DiffJSON(runJSON, attachJSON); err != nil || !equal {
		t.Fatalf("step line JSON differs (err %v):\n%s", err, report)
	}
	checkGolden(t, "attach/v1-step-line.json", attachJSON, DiffJSON)

	var empty attach.StepLine
	var emptyRun runfolder.StepLine
	a, _ := json.Marshal(empty)
	r, _ := json.Marshal(emptyRun)
	if string(a) != string(r) {
		t.Fatalf("zero step lines differ: %s versus %s", a, r)
	}
}

func TestAttachCaptureStateFieldsMatchTheFreshResultPairingFields(t *testing.T) {
	state, err := json.Marshal(attach.CaptureState{Stable: true, Paired: true, Highlightable: true, Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := json.Marshal(attach.FreshResult{Paired: true, Highlightable: true, Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	var stateFields, freshFields map[string]any
	if err := json.Unmarshal(state, &stateFields); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fresh, &freshFields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"paired", "highlightable", "scale"} {
		if stateFields[key] != freshFields[key] {
			t.Fatalf("%s = %v in a capture state and %v in a fresh result", key, stateFields[key], freshFields[key])
		}
	}
}
