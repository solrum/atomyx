package setup

import (
	"context"
	"regexp"
	"strings"
)

// SigningIdentityLister lists the code signing identities in the
// keychain and reads the team behind each of them.
type SigningIdentityLister interface {
	// CodeSigningIdentities returns the output of the system tool that
	// lists valid code signing identities, one numbered line per
	// identity.
	CodeSigningIdentities(ctx context.Context) (string, error)
	// IdentityTeamID returns the team id found in the organizational
	// unit of the subject of the certificate that belongs to the named
	// identity. It returns an error when the certificate cannot be
	// read or carries no organizational unit.
	IdentityTeamID(ctx context.Context, identity string) (string, error)
}

// IOSSigningTeamStepID is the stable identifier the step reports.
const IOSSigningTeamStepID = "ios-signing-team"

// teamEnvVar names the environment variable that selects the signing
// team for a physical device build.
const teamEnvVar = "ATOMYX_DEV_TEAM"

type iosSigningTeamStep struct {
	target      IOSTarget
	currentTeam string
	identities  SigningIdentityLister
}

// NewIOSSigningTeamStep returns the step that requires the signing
// team of a physical device build to be selected. currentTeam is the
// value of ATOMYX_DEV_TEAM, empty when unset. The step reads the team
// id from the certificate of every development identity in the
// keychain and passes when ATOMYX_DEV_TEAM names one of those teams.
// Otherwise it tells the operator which team to export. When no
// certificate can be read it trusts a set ATOMYX_DEV_TEAM and asks for
// one when it is unset. A simulator build needs no signing. The step
// never writes a file or an environment variable.
func NewIOSSigningTeamStep(target IOSTarget, currentTeam string, identities SigningIdentityLister) Step {
	return iosSigningTeamStep{target: target, currentTeam: strings.TrimSpace(currentTeam), identities: identities}
}

func (iosSigningTeamStep) ID() string { return IOSSigningTeamStepID }

const teamLookupHint = "The Team ID is also shown in Xcode, Settings, Accounts."

func (s iosSigningTeamStep) Check(ctx context.Context) Result {
	if s.target == IOSTargetSimulator {
		return Result{Status: StatusOK, Message: "a simulator build needs no code signing"}
	}
	output, err := s.identities.CodeSigningIdentities(ctx)
	if err != nil {
		return failedFrom("could not list the code signing identities", err,
			"Check that the macOS security tool is available, then run atomyx setup again.")
	}
	names := ParseDevelopmentIdentities(output)
	if len(names) == 0 {
		return Result{
			Status:  StatusManual,
			Message: "no development code signing identity was found in the keychain",
			Remedy:  "Open Xcode, then Settings, then Accounts, add your Apple ID, select the team, choose Manage Certificates and add an Apple Development certificate. Then run atomyx setup again.",
		}
	}
	teams := s.readTeams(ctx, names)
	if len(teams) == 0 {
		return s.unreadableTeams()
	}
	for _, team := range teams {
		if team == s.currentTeam {
			return Result{Status: StatusOK, Message: teamEnvVar + " is " + team + ", which has a signing identity"}
		}
	}
	if len(teams) == 1 {
		return Result{
			Status:  StatusManual,
			Message: "the signing team " + teams[0] + " was found, but " + teamEnvVar + " " + s.currentDescription(),
			Remedy:  "Run export " + teamEnvVar + "=" + teams[0] + " in your shell, and add the line to your shell profile to keep it. " + teamLookupHint,
		}
	}
	return Result{
		Status:  StatusManual,
		Message: "several signing teams were found: " + strings.Join(teams, ", ") + ". " + teamEnvVar + " " + s.currentDescription(),
		Remedy:  "Choose the team to build with and run export " + teamEnvVar + "=<id> using one of " + strings.Join(teams, ", ") + ". " + teamLookupHint,
	}
}

// readTeams returns the distinct team ids of the identities, in
// first-seen order. An identity whose certificate cannot be read, or
// whose organizational unit is not a team id, contributes nothing.
func (s iosSigningTeamStep) readTeams(ctx context.Context, names []string) []string {
	var teams []string
	seen := map[string]bool{}
	for _, name := range names {
		team, err := s.identities.IdentityTeamID(ctx, name)
		team = strings.TrimSpace(team)
		if err != nil || !teamID.MatchString(team) || seen[team] {
			continue
		}
		seen[team] = true
		teams = append(teams, team)
	}
	return teams
}

func (s iosSigningTeamStep) unreadableTeams() Result {
	if s.currentTeam != "" {
		return Result{
			Status:  StatusOK,
			Message: teamEnvVar + " is " + s.currentTeam + ". The team of the keychain identities could not be read, so it was not compared.",
		}
	}
	return Result{
		Status:  StatusManual,
		Message: "a development identity was found, but its team could not be read and " + teamEnvVar + " is not set",
		Remedy:  "Run export " + teamEnvVar + "=<id> with your Team ID in your shell, and add the line to your shell profile to keep it. " + teamLookupHint,
	}
}

func (s iosSigningTeamStep) currentDescription() string {
	if s.currentTeam == "" {
		return "is not set"
	}
	return "is " + s.currentTeam + ", which has no signing identity"
}

// identityLine matches one numbered line of the tool's output and
// captures the quoted identity name, for example
// Apple Development: Jane Doe (ABCDE12345). The parenthesised id in
// that name is the member id of the certificate holder, which is not
// the team id a build needs.
var identityLine = regexp.MustCompile(`^\s*\d+\)\s+[0-9A-Fa-f]{40}\s+"(.*)"\s*$`)

var teamID = regexp.MustCompile(`^[A-Z0-9]{10}$`)

var developmentPrefixes = []string{"Apple Development:", "iPhone Developer:"}

// ParseDevelopmentIdentities returns the distinct names, in first-seen
// order, of the development identities in the output of security
// find-identity -v -p codesigning. Distribution and other identity
// kinds are left out because a device build signs with a development
// identity.
func ParseDevelopmentIdentities(output string) []string {
	var names []string
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		match := identityLine.FindStringSubmatch(line)
		if match == nil || seen[match[1]] || !isDevelopmentIdentity(match[1]) {
			continue
		}
		seen[match[1]] = true
		names = append(names, match[1])
	}
	return names
}

func isDevelopmentIdentity(name string) bool {
	for _, prefix := range developmentPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
