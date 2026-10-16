package runner

import (
	"strings"
	"testing"

	"dev.atomyx/internal/diag"
	"dev.atomyx/internal/driver"
)

func TestOpenLinkCommand_HandsTheLinkToTheDriver(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- openLink: myapp://orders/42?tab=all&sort=new\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	calls := driverCalls(d, "OpenLink")
	if len(calls) != 1 || calls[0].Args[0] != "myapp://orders/42?tab=all&sort=new" || calls[0].Args[1] != "" {
		t.Fatalf("got OpenLink calls %v, want one for the link with no app", calls)
	}
	if got := detail(lastStep(t, result)); got != "Opened myapp://orders/42?tab=all&sort=new" {
		t.Errorf("detail = %q", got)
	}
}

func TestOpenLinkCommand_NamesTheAppWhenTheStepDoes(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\n---\n- openLink:\n    link: myapp://home\n    appId: com.other\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	calls := driverCalls(d, "OpenLink")
	if len(calls) != 1 || calls[0].Args[1] != "com.other" {
		t.Fatalf("got OpenLink calls %v, want one in com.other", calls)
	}
}

func TestOpenLinkCommand_DetailKeepsThePlaceholder(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	result := runConfigured(t, r, "appId: com.test\nenv:\n  token: s3cret\n---\n- openLink: myapp://auth?token=${token}\n")
	if !result.Ok {
		t.Fatalf("got Ok=false: %s", debugSteps(result.Steps))
	}
	if calls := driverCalls(d, "OpenLink"); len(calls) != 1 || calls[0].Args[0] != "myapp://auth?token=s3cret" {
		t.Fatalf("got OpenLink calls %v, want the expanded link", calls)
	}
	if got := detail(lastStep(t, result)); strings.Contains(got, "s3cret") || got != "Opened myapp://auth?token=${token}" {
		t.Errorf("detail = %q, want the placeholder as written", got)
	}
}

func TestOpenLinkCommand_FailsTheStepWhenTheDeviceCannotOpenLinks(t *testing.T) {
	r, d := newConfiguredRunner(nil, false, nil)
	d.SetCapabilities(driver.Capabilities{CanOpenLink: false})
	result := runConfigured(t, r, "appId: com.test\n---\n- openLink: myapp://home\n")
	if result.Ok {
		t.Fatal("got Ok=true, want the step to fail")
	}
	if got := lastStep(t, result); got.Code != diag.CodeLinkUnsupported {
		t.Errorf("code = %q, want %q", got.Code, diag.CodeLinkUnsupported)
	}
}

func TestSummarizeStep_OpenLink(t *testing.T) {
	def := mustParse(t, "appId: com.test\n---\n- openLink: myapp://home\n")
	if got, want := SummarizeStep(def.Steps[0]).Text, `openLink: "myapp://home"`; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}
