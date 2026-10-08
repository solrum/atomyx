package conformance

import (
	"encoding/json"
	"fmt"

	"dev.atomyx/internal/attach"
	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/wire"
)

// Senders of a frame in an attach transcript.
const (
	attachFromClient = "client"
	attachFromHolder = "holder"
)

// AttachTranscriptStep is one frame of an attach transcript and who sent
// it. Frame holds the exact JSON that crosses the socket.
type AttachTranscriptStep struct {
	From  string          `json:"from"`
	Frame json.RawMessage `json:"frame"`
}

// AttachTranscript is a named, ordered exchange of frames for protocol
// major version 1.
type AttachTranscript struct {
	Name  string
	Steps []AttachTranscriptStep
}

type attachTranscriptBuilder struct {
	steps []AttachTranscriptStep
	err   error
}

func (b *attachTranscriptBuilder) add(from string, f attach.Frame, err error) {
	if b.err != nil {
		return
	}
	if err != nil {
		b.err = err
		return
	}
	raw, err := json.Marshal(f)
	if err != nil {
		b.err = err
		return
	}
	b.steps = append(b.steps, AttachTranscriptStep{From: from, Frame: raw})
}

func (b *attachTranscriptBuilder) request(id int64, messageType string, data any) {
	f, err := attach.NewRequest(id, messageType, data)
	b.add(attachFromClient, f, err)
}

func (b *attachTranscriptBuilder) response(id int64, messageType string, data any) {
	f, err := attach.NewResponse(id, messageType, data)
	b.add(attachFromHolder, f, err)
}

func (b *attachTranscriptBuilder) failure(id int64, messageType string, cause error) {
	b.add(attachFromHolder, attach.NewErrorResponse(id, messageType, cause), nil)
}

func (b *attachTranscriptBuilder) notice(messageType string, data any) {
	f, err := attach.NewNotice(messageType, data)
	b.add(attachFromHolder, f, err)
}

func (b *attachTranscriptBuilder) build(name string) (AttachTranscript, error) {
	if b.err != nil {
		return AttachTranscript{}, fmt.Errorf("building attach transcript %s: %w", name, b.err)
	}
	return AttachTranscript{Name: name, Steps: b.steps}, nil
}

func attachCorpusTree() *wire.TreeNodeWire {
	enabled := true
	return &wire.TreeNodeWire{
		Attributes: map[string]string{"class": "Screen", "bounds": "[0,0][1080,1920]"},
		Children: []wire.TreeNodeWire{{
			Attributes: map[string]string{"id": "login", "text": "Log in", "bounds": "[100,200][400,300]"},
			Children:   []wire.TreeNodeWire{},
			Enabled:    &enabled,
		}},
	}
}

func attachCorpusState(stable bool) attach.CaptureState {
	return attach.CaptureState{Stable: stable, Paired: stable, Highlightable: stable, Scale: 2.5}
}

func attachHandshakeTranscript() (AttachTranscript, error) {
	var b attachTranscriptBuilder
	b.request(1, attach.TypeHello, attach.Hello{
		Protocol: attach.VersionRange{Min: 1, Max: 1},
		Client:   "atomyx inspect",
		Features: []string{attach.FeatureObserve},
	})
	b.response(1, attach.TypeHello, attach.HelloResult{Protocol: 1, Features: []string{attach.FeatureObserve, attach.FeatureObserveState}})
	b.request(2, attach.TypeStatus, nil)
	b.response(2, attach.TypeStatus, attach.Status{
		State:      "idle",
		Device:     attach.DeviceInfo{ID: "emulator-5554", Platform: "android", Name: "Pixel 8"},
		Holder:     attach.HolderInfo{PID: 4242, Command: "atomyx-mcp", Role: "mcp", StartedAt: 1790000000000},
		Generation: 1,
	})
	b.request(3, attach.TypeBye, nil)
	b.response(3, attach.TypeBye, nil)
	return b.build("handshake")
}

func attachObserveTranscript() (AttachTranscript, error) {
	detail := "Element exists"
	var b attachTranscriptBuilder
	b.request(1, attach.TypeSubscribe, attach.Subscribe{RunID: "20261003-101500-smoke", SinceSeq: 4})
	b.response(1, attach.TypeSubscribe, nil)
	b.notice(attach.NoticeTree, attach.TreeNotice{Revision: 4, Epoch: 2, CaptureState: attachCorpusState(true)})
	b.notice(attach.NoticeScreenshot, attach.ScreenshotNotice{Revision: 3, Epoch: 2, CaptureState: attachCorpusState(true)})
	b.notice(attach.NoticeStep, attach.StepNotice{Epoch: 2, Line: attach.StepLine{
		SchemaVersion: 1, RunID: "20261003-101500-smoke", Seq: 5, StepIndex: 1, Command: "assertExists", Phase: "completed",
		Ok: true, DurationMs: 12, Detail: &detail, Artifacts: []string{},
	}})
	b.notice(attach.NoticeState, attach.StateNotice{State: "idle", Command: "atomyx-mcp"})
	b.notice(attach.NoticeState, attach.StateNotice{State: "running", Command: "atomyx run", RunID: "20261003-101500-smoke"})
	b.notice(attach.NoticeTree, attach.TreeNotice{Revision: 5, Epoch: 3, CaptureState: attach.CaptureState{Stable: false, Scale: 2.5}})
	b.notice(attach.NoticeStepGap, attach.StepGapNotice{OldestSeq: 40})
	b.request(2, attach.TypeTreeLatest, nil)
	b.response(2, attach.TypeTreeLatest, attach.TreeResult{Revision: 4, Epoch: 2, Tree: attachCorpusTree(), CaptureState: attachCorpusState(true)})
	b.request(3, attach.TypeScreenshotLatest, nil)
	b.response(3, attach.TypeScreenshotLatest, attach.ScreenshotResult{
		Revision: 3, Epoch: 2, Format: "png", Image: []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a},
		CaptureState: attachCorpusState(true),
	})
	b.request(4, attach.TypeTreeFresh, nil)
	b.response(4, attach.TypeTreeFresh, attach.FreshResult{
		Tree:          attach.TreeResult{Revision: 5, Epoch: 3, Tree: attachCorpusTree(), CaptureState: attachCorpusState(true)},
		Screenshot:    attach.ScreenshotResult{Revision: 4, Epoch: 3, Format: "png", Image: []byte{0x89, 'P', 'N', 'G'}, CaptureState: attachCorpusState(true)},
		Paired:        true,
		Highlightable: true,
		Scale:         2.5,
	})
	b.notice(attach.NoticeClosed, nil)
	return b.build("observe")
}

func attachErrorsTranscript() (AttachTranscript, error) {
	var b attachTranscriptBuilder
	b.request(1, attach.TypeHello, attach.Hello{Protocol: attach.VersionRange{Min: 2, Max: 3}, Client: "future viewer", Features: []string{}})
	b.failure(1, attach.TypeHello, diag.NewUnsupportedVersion(2, 3, 1, 1))
	b.request(2, attach.TypeHello, attach.Hello{Protocol: attach.VersionRange{Min: 1, Max: 1}, Client: "atomyx inspect", Features: []string{}})
	b.response(2, attach.TypeHello, attach.HelloResult{Protocol: 1, Features: []string{attach.FeatureObserve, attach.FeatureObserveState}})
	b.request(3, "control.borrow", nil)
	b.failure(3, "control.borrow", diag.NewUnsupported("control.borrow"))
	b.request(4, attach.TypeTreeFresh, nil)
	b.failure(4, attach.TypeTreeFresh, diag.NewHolderBusy("a tool call is running"))
	b.request(5, attach.TypeStatus, nil)
	b.response(5, attach.TypeStatus, attach.Status{State: "running"})
	return b.build("errors")
}

func attachControlTranscript() (AttachTranscript, error) {
	var b attachTranscriptBuilder
	b.request(1, attach.TypeHello, attach.Hello{
		Protocol: attach.VersionRange{Min: 1, Max: 1},
		Client:   "atomyx run",
		Features: []string{attach.FeatureControl},
	})
	b.response(1, attach.TypeHello, attach.HelloResult{Protocol: 1, Features: []string{attach.FeatureObserve, attach.FeatureObserveState, attach.FeatureControl}})
	b.request(2, attach.TypeControlYield, attach.ControlYield{Command: "atomyx run login.yml"})
	b.failure(2, attach.TypeControlYield, diag.NewHolderBusy("a tool call is running"))
	b.request(3, attach.TypeControlYield, attach.ControlYield{Command: "atomyx run login.yml"})
	b.response(3, attach.TypeControlYield, nil)
	return b.build("control")
}

func attachDebugTranscript() (AttachTranscript, error) {
	var b attachTranscriptBuilder
	b.request(1, attach.TypeHello, attach.Hello{
		Protocol: attach.VersionRange{Min: 1, Max: 1},
		Client:   "atomyx inspect",
		Features: []string{attach.FeatureObserve, attach.FeatureDebug},
	})
	b.response(1, attach.TypeHello, attach.HelloResult{Protocol: 1, Features: []string{attach.FeatureObserve, attach.FeatureObserveState, attach.FeatureDebug}})
	b.request(2, attach.TypeSubscribe, attach.Subscribe{})
	b.response(2, attach.TypeSubscribe, nil)
	b.request(3, attach.TypeDebugVariablesGet, nil)
	b.failure(3, attach.TypeDebugVariablesGet, diag.NewHolderBusy("the run is not paused"))
	b.request(4, attach.TypeDebugBreakpointsSet, attach.DebugBreakpoints{Locations: []string{"/work/login.yml:7", "inline:2"}})
	b.response(4, attach.TypeDebugBreakpointsSet, nil)
	b.request(5, attach.TypeDebugPause, nil)
	b.response(5, attach.TypeDebugPause, nil)
	b.notice(attach.NoticeState, attach.StateNotice{
		State: "paused", Command: "atomyx run", RunID: "20261003-101500-smoke",
		Pause: &attach.PauseLocation{Source: "/work/login.yml", Line: 7, Depth: 0, Command: "tap"},
	})
	b.request(6, attach.TypeDebugVariablesGet, nil)
	b.response(6, attach.TypeDebugVariablesGet, attach.DebugVariables{Variables: map[string]string{"user": "ann", "password": "****"}})
	b.request(7, attach.TypeActStep, attach.ActStep{Step: "tap: {id: submit}"})
	b.response(7, attach.TypeActStep, attach.ActResult{Command: "tap", Outcome: "passed", DurationMs: 41, Artifacts: []string{}})
	b.request(8, attach.TypeDebugStep, attach.DebugResume{})
	b.response(8, attach.TypeDebugStep, attach.DebugResumeResult{Resumed: false, MissReason: "no element matches the next step"})
	b.request(9, attach.TypeDebugStep, attach.DebugResume{Force: true})
	b.response(9, attach.TypeDebugStep, attach.DebugResumeResult{Resumed: true})
	b.notice(attach.NoticeState, attach.StateNotice{State: "running", Command: "atomyx run", RunID: "20261003-101500-smoke"})
	b.request(10, attach.TypeDebugResume, attach.DebugResume{})
	b.failure(10, attach.TypeDebugResume, diag.NewHolderBusy("the run is not paused"))
	return b.build("debug")
}

// AttachCorpus returns the attach transcripts of protocol major version
// 1. Each one is frozen as goldens/attach/v1-<name>.json.
func AttachCorpus() ([]AttachTranscript, error) {
	builders := []func() (AttachTranscript, error){
		attachHandshakeTranscript,
		attachObserveTranscript,
		attachErrorsTranscript,
		attachControlTranscript,
		attachDebugTranscript,
	}
	transcripts := make([]AttachTranscript, 0, len(builders))
	for _, build := range builders {
		transcript, err := build()
		if err != nil {
			return nil, err
		}
		transcripts = append(transcripts, transcript)
	}
	return transcripts, nil
}
