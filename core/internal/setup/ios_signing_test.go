package setup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fakeIdentities struct {
	output string
	err    error
}

func (f fakeIdentities) CodeSigningIdentities(context.Context) (string, error) {
	return f.output, f.err
}

const (
	devIdentity     = `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Apple Development: Test Person (AAAAA11111)"` + "\n"
	distIdentity    = `  2) 89ABCDEF0123456789ABCDEF0123456789ABCDEF "Apple Distribution: Test Person (AAAAA11111)"` + "\n"
	otherIdentity   = `  3) 456789ABCDEF0123456789ABCDEF0123456789AB "Apple Development: Other Person (BBBBB22222)"` + "\n"
	noTeamIdentity  = `  4) 6789ABCDEF0123456789ABCDEF0123456789ABCD "Mac Developer: no team id here"` + "\n"
	identitySummary = "     4 valid identities found\n"
)

func TestParseSigningTeams(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", nil},
		{"none found", "     0 valid identities found\n", nil},
		{"one team on two identities", devIdentity + distIdentity + identitySummary, []string{"AAAAA11111"}},
		{"two teams in first-seen order", otherIdentity + devIdentity, []string{"BBBBB22222", "AAAAA11111"}},
		{"identity without a team id", noTeamIdentity + identitySummary, nil},
		{"lowercase id is not a team id", `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Apple Development: X (aaaaa11111)"` + "\n", nil},
		{"id of the wrong length", `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Apple Development: X (AAAA1111)"` + "\n", nil},
		{"id one character too short", `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Apple Development: X (AAAAA1111)"` + "\n", nil},
		{"id one character too long", `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Apple Development: X (AAAAA111111)"` + "\n", nil},
		{"hash of the wrong length", `  1) 0123 "Apple Development: X (AAAAA11111)"` + "\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseSigningTeams(tc.output); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseSigningTeams = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIOSSigningTeamStep(t *testing.T) {
	one := fakeIdentities{output: devIdentity + distIdentity + identitySummary}
	two := fakeIdentities{output: devIdentity + otherIdentity + identitySummary}
	cases := []struct {
		name       string
		target     IOSTarget
		current    string
		identities fakeIdentities
		status     Status
		contains   []string
	}{
		{"simulator needs no signing", IOSTargetSimulator, "", fakeIdentities{err: errors.New("unused")}, StatusOK, []string{"needs no code signing"}},
		{"one team already exported", IOSTargetPhysical, "AAAAA11111", one, StatusOK, []string{"AAAAA11111"}},
		{"export value with spaces around it", IOSTargetPhysical, " AAAAA11111\n", one, StatusOK, nil},
		{"one team not exported", IOSTargetPhysical, "", one, StatusManual, []string{"export ATOMYX_DEV_TEAM=AAAAA11111", "is not set"}},
		{"one team and a different value", IOSTargetPhysical, "ZZZZZ99999", one, StatusManual, []string{"export ATOMYX_DEV_TEAM=AAAAA11111", "ZZZZZ99999, which has no signing identity"}},
		{"several teams none chosen", IOSTargetPhysical, "", two, StatusManual, []string{"AAAAA11111, BBBBB22222", "export ATOMYX_DEV_TEAM=<id>"}},
		{"several teams and one chosen", IOSTargetPhysical, "BBBBB22222", two, StatusOK, []string{"BBBBB22222"}},
		{"no identity", IOSTargetPhysical, "", fakeIdentities{output: "     0 valid identities found\n"}, StatusManual, []string{"Settings, then Accounts", "Manage Certificates"}},
		{"unknown target checks like a physical one", IOSTargetUnknown, "", one, StatusManual, []string{"export ATOMYX_DEV_TEAM=AAAAA11111"}},
		{"tool failure", IOSTargetPhysical, "", fakeIdentities{err: errors.New("security: not found")}, StatusFailed, []string{"security: not found"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewIOSSigningTeamStep(tc.target, tc.current, tc.identities).Check(context.Background())
			if got.Status != tc.status {
				t.Fatalf("status = %s, want %s: %+v", got.Status, tc.status, got)
			}
			text := got.Message + " " + got.Remedy
			for _, want := range tc.contains {
				if !strings.Contains(text, want) {
					t.Errorf("result %+v does not contain %q", got, want)
				}
			}
			if tc.status != StatusOK && got.Remedy == "" {
				t.Error("a result that is not ok needs a remedy")
			}
		})
	}
}
