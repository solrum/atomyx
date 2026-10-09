package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestPrintCommandHelpRunDocumentsConfigAndEnvFlags guards against the
// run command's flag table (commandFlags[CommandRun]) accepting
// --config and --env while its help text omits them.
func TestPrintCommandHelpRunDocumentsConfigAndEnvFlags(t *testing.T) {
	var buf bytes.Buffer
	PrintCommandHelp(CommandRun, &buf)

	text := buf.String()
	if !strings.Contains(text, "--config") {
		t.Errorf("run help: got %q, want it to document --config", text)
	}
	if !strings.Contains(text, "--env") {
		t.Errorf("run help: got %q, want it to document --env", text)
	}
}

// TestPrintCommandHelpDoctorDocumentsExitCodes guards against the
// doctor command's help text drifting from its actual exit-code
// contract.
func TestPrintCommandHelpDoctorDocumentsExitCodes(t *testing.T) {
	var buf bytes.Buffer
	PrintCommandHelp(CommandDoctor, &buf)

	text := buf.String()
	if !strings.Contains(text, "--platform") || !strings.Contains(text, "--device") {
		t.Errorf("doctor help: got %q, want it to document --platform and --device", text)
	}
	if !strings.Contains(text, "EXIT CODES") {
		t.Errorf("doctor help: got %q, want an EXIT CODES section", text)
	}
}

func TestInspectHelpDocumentsFlagsEnterAndExitCodes(t *testing.T) {
	var out bytes.Buffer
	PrintCommandHelp(CommandInspect, &out)
	for _, want := range []string{"--platform", "--device", "--no-open", "Enter", "130", "143"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("inspect help lacks %q", want)
		}
	}
}

// TestPrintCommandHelpRunAndSuiteDocumentVideo guards against the --video
// flag being accepted by run and suite while their help omits it.
func TestPrintCommandHelpRunAndSuiteDocumentVideo(t *testing.T) {
	for _, command := range []Command{CommandRun, CommandSuite} {
		var buf bytes.Buffer
		PrintCommandHelp(command, &buf)
		if !strings.Contains(buf.String(), "--video") {
			t.Errorf("%s help: want it to document --video, got %q", command, buf.String())
		}
	}
}
