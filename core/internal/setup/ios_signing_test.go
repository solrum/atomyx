package setup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeIdentities lists the given identity output and answers each
// identity name with the team in teams. A name missing from teams has
// an unreadable certificate.
type fakeIdentities struct {
	output string
	err    error
	teams  map[string]string
}

func (f fakeIdentities) CodeSigningIdentities(context.Context) (string, error) {
	return f.output, f.err
}

func (f fakeIdentities) IdentityTeamID(_ context.Context, identity string) (string, error) {
	team, ok := f.teams[identity]
	if !ok {
		return "", errors.New("no certificate")
	}
	return team, nil
}

// The id in parentheses is the member id of the certificate holder.
// The team id differs and exists only in the certificate subject.
const (
	devName         = "Apple Development: Test Person (MMMMM11111)"
	otherName       = "Apple Development: Other Person (NNNNN22222)"
	legacyName      = "iPhone Developer: Old Person (OOOOO33333)"
	devIdentity     = `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "` + devName + `"` + "\n"
	distIdentity    = `  2) 89ABCDEF0123456789ABCDEF0123456789ABCDEF "Apple Distribution: Test Person (MMMMM11111)"` + "\n"
	otherIdentity   = `  3) 456789ABCDEF0123456789ABCDEF0123456789AB "` + otherName + `"` + "\n"
	legacyIdentity  = `  4) 6789ABCDEF0123456789ABCDEF0123456789ABCD "` + legacyName + `"` + "\n"
	macIdentity     = `  5) 789ABCDEF0123456789ABCDEF0123456789ABCDE "Mac Developer: Test Person (MMMMM11111)"` + "\n"
	identitySummary = "     5 valid identities found\n"
)

func TestParseDevelopmentIdentities(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", nil},
		{"none found", "     0 valid identities found\n", nil},
		{"distribution and mac identities are left out", devIdentity + distIdentity + macIdentity + identitySummary, []string{devName}},
		{"first-seen order", otherIdentity + devIdentity, []string{otherName, devName}},
		{"legacy development name counts", legacyIdentity, []string{legacyName}},
		{"the same name twice is listed once", devIdentity + devIdentity, []string{devName}},
		{"name without a parenthesised id still counts", `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "Apple Development: no id here"` + "\n", []string{"Apple Development: no id here"}},
		{"hash of the wrong length", `  1) 0123 "` + devName + `"` + "\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseDevelopmentIdentities(tc.output); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseDevelopmentIdentities = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIOSSigningTeamStep(t *testing.T) {
	oneTeam := fakeIdentities{output: devIdentity + distIdentity + identitySummary, teams: map[string]string{devName: "TTTTT11111"}}
	twoTeams := fakeIdentities{output: devIdentity + otherIdentity + identitySummary, teams: map[string]string{devName: "TTTTT11111", otherName: "UUUUU22222"}}
	sameTeam := fakeIdentities{output: devIdentity + otherIdentity, teams: map[string]string{devName: "TTTTT11111", otherName: "TTTTT11111"}}
	unreadable := fakeIdentities{output: devIdentity}
	cases := []struct {
		name       string
		target     IOSTarget
		current    string
		identities fakeIdentities
		status     Status
		contains   []string
	}{
		{"simulator needs no signing", IOSTargetSimulator, "", fakeIdentities{err: errors.New("unused")}, StatusOK, []string{"needs no code signing"}},
		{"team already exported", IOSTargetPhysical, "TTTTT11111", oneTeam, StatusOK, []string{"TTTTT11111"}},
		{"export value with spaces around it", IOSTargetPhysical, " TTTTT11111\n", oneTeam, StatusOK, nil},
		{"member id exported instead of the team", IOSTargetPhysical, "MMMMM11111", oneTeam, StatusManual, []string{"export ATOMYX_DEV_TEAM=TTTTT11111", "MMMMM11111, which has no signing identity"}},
		{"team not exported", IOSTargetPhysical, "", oneTeam, StatusManual, []string{"export ATOMYX_DEV_TEAM=TTTTT11111", "is not set"}},
		{"a different value", IOSTargetPhysical, "ZZZZZ99999", oneTeam, StatusManual, []string{"export ATOMYX_DEV_TEAM=TTTTT11111", "ZZZZZ99999, which has no signing identity"}},
		{"two identities of one team", IOSTargetPhysical, "", sameTeam, StatusManual, []string{"export ATOMYX_DEV_TEAM=TTTTT11111"}},
		{"several teams none chosen", IOSTargetPhysical, "", twoTeams, StatusManual, []string{"TTTTT11111, UUUUU22222", "export ATOMYX_DEV_TEAM=<id>"}},
		{"several teams and one chosen", IOSTargetPhysical, "UUUUU22222", twoTeams, StatusOK, []string{"UUUUU22222"}},
		{"several teams and a member id chosen", IOSTargetPhysical, "NNNNN22222", twoTeams, StatusManual, []string{"TTTTT11111, UUUUU22222"}},
		{"no identity", IOSTargetPhysical, "", fakeIdentities{output: "     0 valid identities found\n"}, StatusManual, []string{"Settings, then Accounts", "Manage Certificates"}},
		{"only a distribution identity", IOSTargetPhysical, "TTTTT11111", fakeIdentities{output: distIdentity}, StatusManual, []string{"no development code signing identity"}},
		{"unknown target checks like a physical one", IOSTargetUnknown, "", oneTeam, StatusManual, []string{"export ATOMYX_DEV_TEAM=TTTTT11111"}},
		{"unreadable certificate and a team set", IOSTargetPhysical, "TTTTT11111", unreadable, StatusOK, []string{"could not be read"}},
		{"unreadable certificate and no team set", IOSTargetPhysical, "", unreadable, StatusManual, []string{"could not be read", "export ATOMYX_DEV_TEAM=<id>"}},
		{"subject unit that is not a team id", IOSTargetPhysical, "", fakeIdentities{output: devIdentity, teams: map[string]string{devName: "not-a-team"}}, StatusManual, []string{"could not be read"}},
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
