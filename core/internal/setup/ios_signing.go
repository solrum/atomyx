package setup

import (
	"context"
	"regexp"
	"strings"
)

// SigningIdentityLister lists the code signing identities in the
// keychain.
type SigningIdentityLister interface {
	// CodeSigningIdentities returns the output of the system tool that
	// lists valid code signing identities, one numbered line per
	// identity.
	CodeSigningIdentities(ctx context.Context) (string, error)
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
// value of ATOMYX_DEV_TEAM, empty when unset. The step passes when it
// names one of the teams that have an identity in the keychain, and
// otherwise tells the operator which team to export. A simulator build
// needs no signing. The step never writes a file or an environment
// variable.
func NewIOSSigningTeamStep(target IOSTarget, currentTeam string, identities SigningIdentityLister) Step {
	return iosSigningTeamStep{target: target, currentTeam: strings.TrimSpace(currentTeam), identities: identities}
}

func (iosSigningTeamStep) ID() string { return IOSSigningTeamStepID }

const teamLookupHint = "If a build rejects the id, copy the Team ID from Xcode, Settings, Accounts."

func (s iosSigningTeamStep) Check(ctx context.Context) Result {
	if s.target == IOSTargetSimulator {
		return Result{Status: StatusOK, Message: "a simulator build needs no code signing"}
	}
	output, err := s.identities.CodeSigningIdentities(ctx)
	if err != nil {
		return failedFrom("could not list the code signing identities", err,
			"Check that the macOS security tool is available, then run atomyx setup again.")
	}
	teams := ParseSigningTeams(output)
	for _, team := range teams {
		if team == s.currentTeam {
			return Result{Status: StatusOK, Message: teamEnvVar + " is " + team + ", which has a signing identity"}
		}
	}
	switch len(teams) {
	case 0:
		return Result{
			Status:  StatusManual,
			Message: "no code signing identity was found in the keychain",
			Remedy:  "Open Xcode, then Settings, then Accounts, add your Apple ID, select the team, choose Manage Certificates and add an Apple Development certificate. Then run atomyx setup again.",
		}
	case 1:
		return Result{
			Status:  StatusManual,
			Message: "the signing team " + teams[0] + " was found, but " + teamEnvVar + " " + s.currentDescription(),
			Remedy:  "Run export " + teamEnvVar + "=" + teams[0] + " in your shell, and add the line to your shell profile to keep it. " + teamLookupHint,
		}
	default:
		return Result{
			Status:  StatusManual,
			Message: "several signing teams were found: " + strings.Join(teams, ", ") + ". " + teamEnvVar + " " + s.currentDescription(),
			Remedy:  "Choose the team to build with and run export " + teamEnvVar + "=<id> using one of " + strings.Join(teams, ", ") + ". " + teamLookupHint,
		}
	}
}

func (s iosSigningTeamStep) currentDescription() string {
	if s.currentTeam == "" {
		return "is not set"
	}
	return "is " + s.currentTeam + ", which has no signing identity"
}

// identityLine matches one numbered line of the tool's output and
// captures the quoted identity name. The name ends with the team id in
// parentheses, for example Apple Development: Jane Doe (ABCDE12345).
var identityLine = regexp.MustCompile(`^\s*\d+\)\s+[0-9A-Fa-f]{40}\s+"(.*)"\s*$`)

var teamSuffix = regexp.MustCompile(`\(([A-Z0-9]{10})\)$`)

// ParseSigningTeams returns the distinct team ids, in first-seen order,
// found in the output of security find-identity -v -p codesigning. A
// line whose identity name does not end in a parenthesised ten
// character id contributes nothing.
func ParseSigningTeams(output string) []string {
	var teams []string
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		match := identityLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		suffix := teamSuffix.FindStringSubmatch(match[1])
		if suffix == nil || seen[suffix[1]] {
			continue
		}
		seen[suffix[1]] = true
		teams = append(teams, suffix[1])
	}
	return teams
}
