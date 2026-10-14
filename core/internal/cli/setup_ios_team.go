package cli

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"regexp"
)

var (
	errCertificateMissing = errors.New("no certificate matched the identity")
	errTeamMissing        = errors.New("the certificate subject carries no team id")
)

var teamIDPattern = regexp.MustCompile(`^[A-Z0-9]{10}$`)

// IdentityTeamID reads the team id from the organizational unit of the
// subject of the keychain certificate whose common name is the given
// identity name.
func (h hostToolOutput) IdentityTeamID(ctx context.Context, identity string) (string, error) {
	if h.run == nil {
		return "", errHostCommandMissing
	}
	out, err := h.run(ctx, "security", "find-certificate", "-a", "-c", identity, "-p")
	if err != nil {
		return "", err
	}
	return teamFromCertificates(out, identity)
}

// teamFromCertificates returns the first team id found in the subject
// of a PEM encoded certificate whose common name equals identity.
func teamFromCertificates(pemData, identity string) (string, error) {
	rest := []byte(pemData)
	matched := false
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || cert.Subject.CommonName != identity {
			continue
		}
		matched = true
		for _, unit := range cert.Subject.OrganizationalUnit {
			if teamIDPattern.MatchString(unit) {
				return unit, nil
			}
		}
	}
	if !matched {
		return "", errCertificateMissing
	}
	return "", errTeamMissing
}
