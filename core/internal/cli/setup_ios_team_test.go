package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

// testCertificate returns a PEM certificate with the given subject
// common name and organizational units.
func testCertificate(t *testing.T, commonName string, units ...string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName, OrganizationalUnit: units, Organization: []string{"Test Org"}, Country: []string{"US"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestTeamFromCertificates(t *testing.T) {
	const name = "Apple Development: Test Person (MMMMM11111)"
	cases := []struct {
		name    string
		pem     string
		want    string
		wantErr error
	}{
		{"team is the unit and differs from the member id", testCertificate(t, name, "TTTTT22222"), "TTTTT22222", nil},
		{"unit that is not a team id is skipped", testCertificate(t, name, "lowercase", "TTTTT22222"), "TTTTT22222", nil},
		{"certificate of another identity is ignored", testCertificate(t, name+" other", "TTTTT22222") + testCertificate(t, name, "UUUUU33333"), "UUUUU33333", nil},
		{"no unit", testCertificate(t, name), "", errTeamMissing},
		{"no matching certificate", testCertificate(t, "Other (MMMMM11111)", "TTTTT22222"), "", errCertificateMissing},
		{"no certificate", "", "", errCertificateMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := teamFromCertificates(tc.pem, name)
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Errorf("teamFromCertificates = %q, %v, want %q, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestIdentityTeamIDAsksTheKeychainForTheNamedCertificate(t *testing.T) {
	const name = "Apple Development: Test Person (MMMMM11111)"
	var gotArgs []string
	h := hostToolOutput{run: func(_ context.Context, cmd string, args ...string) (string, error) {
		gotArgs = append([]string{cmd}, args...)
		return testCertificate(t, name, "TTTTT22222"), nil
	}}
	team, err := h.IdentityTeamID(context.Background(), name)
	if err != nil || team != "TTTTT22222" {
		t.Fatalf("IdentityTeamID = %q, %v", team, err)
	}
	if got, want := strings.Join(gotArgs, "|"), "security|find-certificate|-a|-c|"+name+"|-p"; got != want {
		t.Errorf("command = %s, want %s", got, want)
	}
	if _, err := (hostToolOutput{}).IdentityTeamID(context.Background(), name); !errors.Is(err, errHostCommandMissing) {
		t.Errorf("without a runner: %v", err)
	}
}
