package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestParseRunsArgvListFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantTest string
		wantJSON bool
		wantHelp bool
		wantErr  string
	}{
		{name: "no flags"},
		{name: "test and json", args: []string{"--test", "flows/a.yml", "--json"}, wantTest: "flows/a.yml", wantJSON: true},
		{name: "equals form", args: []string{"--test=flows/a.yml"}, wantTest: "flows/a.yml"},
		{name: "help", args: []string{"--help"}, wantHelp: true},
		{name: "short help", args: []string{"-h"}, wantHelp: true},
		{name: "unknown flag", args: []string{"--bogus"}, wantErr: `does not accept "--bogus"`},
		{name: "positional", args: []string{"extra"}, wantErr: `does not accept "extra"`},
		{name: "missing value", args: []string{"--test"}, wantErr: "requires a value"},
		{name: "value looks like flag", args: []string{"--test", "--json"}, wantErr: "requires a value"},
		{name: "empty inline value", args: []string{"--since="}, wantErr: "non-empty value"},
		{name: "duplicate", args: []string{"--json", "--json"}, wantErr: "more than once"},
		{name: "flag of another command", args: []string{"--keep", "3"}, wantErr: `does not accept "--keep"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv, err := ParseRunsArgv(tt.args, RunsCommandList)
			if tt.wantErr != "" {
				var argvErr *ArgvError
				if !errors.As(err, &argvErr) || !strings.Contains(argvErr.Message, tt.wantErr) {
					t.Fatalf("error = %v, want message containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRunsArgv: %v", err)
			}
			if got, _ := flagString(argv.Flags, "--test"); got != tt.wantTest {
				t.Errorf("--test = %q, want %q", got, tt.wantTest)
			}
			if flagBool(argv.Flags, "--json") != tt.wantJSON {
				t.Errorf("--json = %v, want %v", flagBool(argv.Flags, "--json"), tt.wantJSON)
			}
			if argv.Help != tt.wantHelp {
				t.Errorf("Help = %v, want %v", argv.Help, tt.wantHelp)
			}
		})
	}
}
