package inspector

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
)

const (
	// tokenHeader carries the session token on every /api/ request.
	tokenHeader = "X-Atomyx-Token"
	// secretBytes is the entropy of a bootstrap code and of a token.
	secretBytes = 32
)

type digest [sha256.Size]byte

// authority issues the single-use bootstrap code and the session
// tokens it is exchanged for. It stores only SHA-256 digests, and
// compares a presented secret against them in constant time.
type authority struct {
	entropy io.Reader

	mu sync.Mutex
	// code is the digest of the one code not yet exchanged. Minting a
	// new code replaces it, so a URL that was never opened cannot be
	// used after a fresh one is printed.
	code *digest
	// tokens holds the digest of every token issued. A token stays
	// valid until the server stops, because a reload that mints a new
	// code must not cut off a tab that is still open.
	tokens []digest
}

func newAuthority(entropy io.Reader) *authority {
	return &authority{entropy: entropy}
}

func (a *authority) newSecret() (string, digest, error) {
	buf := make([]byte, secretBytes)
	if _, err := io.ReadFull(a.entropy, buf); err != nil {
		return "", digest{}, fmt.Errorf("read entropy: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	return secret, sha256.Sum256([]byte(secret)), nil
}

// mintCode returns a fresh bootstrap code and invalidates any earlier
// code that was not exchanged. Tokens already issued are untouched.
func (a *authority) mintCode() (string, error) {
	secret, sum, err := a.newSecret()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.code = &sum
	return secret, nil
}

// exchange spends code for a token. It reports false for a wrong code
// and for a code already spent, and a wrong code leaves the pending
// code usable.
func (a *authority) exchange(code string) (token string, ok bool, err error) {
	presented := sha256.Sum256([]byte(code))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.code == nil || subtle.ConstantTimeCompare(presented[:], a.code[:]) != 1 {
		return "", false, nil
	}
	secret, sum, err := a.newSecret()
	if err != nil {
		return "", false, err
	}
	a.code = nil
	a.tokens = append(a.tokens, sum)
	return secret, true, nil
}

// valid reports whether token was issued by exchange. It visits every
// issued token so the time taken does not reveal which one matched.
func (a *authority) valid(token string) bool {
	presented := sha256.Sum256([]byte(token))
	a.mu.Lock()
	defer a.mu.Unlock()
	match := 0
	for i := range a.tokens {
		match |= subtle.ConstantTimeCompare(presented[:], a.tokens[i][:])
	}
	return match == 1
}
